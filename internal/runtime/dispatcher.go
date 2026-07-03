package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

// PityGranter is the narrow contract the dispatcher needs from the pity
// subsystem. internal/features/pity.System satisfies it.
type PityGranter interface {
	GrantPoints(ctx context.Context, tenantID, channel, viewerID, username, reason string, amount int) (int, error)
}

// StreakTicker is the narrow contract the dispatcher needs from the streak
// subsystem. Implementations wrap internal/features/streak.System and return
// a decoupled [StreakOutcome] (mirroring the fields of streak.Result that the
// dispatcher needs to broadcast) so the runtime stays free of any streak
// import. The returned outcome drives the "feature.streak.milestone" and
// "feature.streak.broken" feature-event broadcasts emitted from onMessage.
type StreakTicker interface {
	TickStreak(ctx context.Context, tenantID, channel, viewerID, username string) (StreakOutcome, error)
}

// StreakOutcome is the decoupled result of a streak tick. It mirrors the
// fields of internal/features/streak.Result that the dispatcher needs to
// broadcast, without importing the streak package (which would create a
// dependency cycle and couple the runtime to a concrete feature type).
type StreakOutcome struct {
	// DaysCurrent is the viewer's streak length after this tick.
	DaysCurrent int
	// DaysLongest is the viewer's all-time longest streak after this tick.
	DaysLongest int
	// Milestone is >0 when this tick crossed a milestone (e.g. 7/30/100/365 days).
	Milestone int
	// BrokenFromDays is >0 when this tick broke a prior streak of that length.
	BrokenFromDays int
	// SameDayReTick is true when the viewer already ticked today (no-op day).
	SameDayReTick bool
}

// streakMilestonePayload is the JSON shape broadcast as "feature.streak.milestone".
type streakMilestonePayload struct {
	Platform    string `json:"platform"`
	Channel     string `json:"channel"`
	ViewerID    string `json:"viewer_id"`
	Username    string `json:"username"`
	DaysCurrent int    `json:"days_current"`
	DaysLongest int    `json:"days_longest"`
	Milestone   int    `json:"milestone"`
}

// streakBrokenPayload is the JSON shape broadcast as "feature.streak.broken".
type streakBrokenPayload struct {
	Platform       string `json:"platform"`
	Channel        string `json:"channel"`
	ViewerID       string `json:"viewer_id"`
	Username       string `json:"username"`
	BrokenFromDays int    `json:"broken_from_days"`
	DaysCurrent    int    `json:"days_current"`
}

// Broadcaster is the narrow contract for fanning out normalized events to
// real-time consumers (WebSocket clients, SSE clients, ...).
type Broadcaster interface {
	Broadcast(eventType string, payload any)
}

// WrappedRecorder is the narrow contract the dispatcher uses to accumulate
// per-viewer "Stream Wrapped" statistics (message/sub/raid counters). A thin
// adapter over internal/wrapped satisfies it (wired in main), so the runtime
// stays free of any wrapped import. The adapter owns the period bucketing
// (all-time plus current month). Every method must be safe for concurrent use
// and is best-effort: a recording error must never block message processing.
type WrappedRecorder interface {
	RecordMessage(ctx context.Context, channel, viewerID, username string)
	RecordSub(ctx context.Context, channel, viewerID, username string, giftCount int)
	RecordRaid(ctx context.Context, channel, fromUsername string)
}

// MessageTranslator is the narrow contract the dispatcher uses to optionally
// translate incoming chat messages for a channel. A thin adapter over the
// internal/translate orchestrator satisfies it (wired in main), so the runtime
// stays free of any translate import.
//
// Maybe reports whether the channel has translation enabled and, if so, returns
// the translated text to post back. It is best-effort: an empty reply (already
// in target language, skipped, rate-limited, or an internal error) means "post
// nothing", and it must never block message processing. Implementations run
// their own skip heuristics, language detection, caching and rate limiting.
type MessageTranslator interface {
	Maybe(ctx context.Context, channel, userID, text string) (reply string, ok bool)
}

// CoHost is the narrow contract the dispatcher uses to optionally answer chat
// messages that address the bot. A thin adapter over the internal/cohost
// responder satisfies it (wired in main), so the runtime stays free of any
// cohost import.
//
// Maybe reports whether the channel has the co-host enabled and the message
// addressed it, returning the reply to post back and whether the channel opted
// to also speak it aloud. It is best-effort: an empty reply means "post
// nothing", and it must never block message processing.
type CoHost interface {
	Maybe(ctx context.Context, channel, userID, username, text string) (reply string, speak bool, ok bool)
}

// CoHostSpeaker speaks a co-host reply aloud on a channel through the AI-voice
// overlay. A thin adapter over the internal/tts service satisfies it (wired in
// main). It must be safe for concurrent use and never block.
type CoHostSpeaker interface {
	Speak(channel, text string)
}

// ClipDetector is the narrow contract the dispatcher feeds chat/sub/raid
// activity so an auto-clipper can decide when a clip-worthy moment happens. A
// thin adapter over the internal/clipper engine satisfies it (wired in main),
// so the runtime stays free of any clipper import.
//
// Each method reports whether THIS event triggered a clip moment; the adapter
// owns what to do on a trigger (create the clip, announce it). Every method
// must be safe for concurrent use and best-effort, never blocking the
// dispatcher.
type ClipDetector interface {
	Message(ctx context.Context, channel, userID, username, text string)
	Sub(ctx context.Context, channel string)
	Raid(ctx context.Context, channel string, viewers int)
}

// TTSNotifier is the narrow contract the dispatcher feeds celebration events
// (subs, resubs, gift subs, raids) so a text-to-speech feature can speak an
// alert. A thin adapter over the internal/tts service satisfies it (wired in
// main), keeping the runtime free of any tts import. Every method must be safe
// for concurrent use and never block.
type TTSNotifier interface {
	OnSubscribe(channel, username string)
	OnResubscribe(channel, username string, months int)
	OnGiftSubscribe(channel, gifter string)
	OnRaid(channel, fromUsername string, viewers int)
}

// Economy is the narrow contract the dispatcher uses to award loyalty points
// for chat activity. The adapter wired in main applies a per-viewer earn
// cooldown (anti-farming) so idle or bot accounts cannot accumulate points by
// flooding; the dispatcher simply reports every message and lets the adapter
// decide whether it earns. Award must be safe for concurrent use.
type Economy interface {
	Award(ctx context.Context, tenantID, channel, viewerID, username string)
}

// CommandInvocation is the platform-neutral context a [CommandRouter] needs
// to route a chat command. The dispatcher fills it from an incoming
// message event, including the author's privilege flags so the router can
// enforce per-command permissions.
type CommandInvocation struct {
	Platform string
	Channel  string
	UserID   string
	Username string
	Text     string

	IsBroadcaster bool
	IsModerator   bool
	IsVIP         bool
	IsSubscriber  bool
}

// CommandReply is the result of routing a [CommandInvocation]. Text is the
// chat message to send back; an empty Text means "send nothing".
type CommandReply struct {
	Text string
}

// CommandRouter is the narrow contract the dispatcher needs to turn chat
// messages into command replies. internal/commands.Engine satisfies a thin
// adapter of this (wired in main), keeping the runtime free of any commands
// import. Route returns handled=false when the message is not a command, in
// which case the dispatcher sends nothing.
type CommandRouter interface {
	Route(ctx context.Context, inv CommandInvocation) (reply CommandReply, handled bool)
}

// ActivityRecorder is the narrow contract the dispatcher uses to report
// chat activity per channel. The timers scheduler implements it so
// auto-announcements can be gated behind "chat has been active", keeping
// the runtime free of any timers import.
type ActivityRecorder interface {
	RecordChatActivity(channel string)
}

// ActionEngine is the narrow contract the dispatcher feeds so the user-defined
// Action-Engine can fire automation rules. A thin adapter over internal/actions
// satisfies it (wired in main), keeping the runtime free of any actions import.
// Both methods are best-effort and must never block the dispatcher: the engine
// hands work to its own worker pool. OnMessage carries the privilege flags a
// rule's role conditions read; OnEvent carries non-message events (subs, raids)
// with a small data map of their salient fields.
type ActionEngine interface {
	OnMessage(platform, channel, messageID, userID, username, text string,
		isBroadcaster, isModerator, isVIP, isSubscriber bool)
	OnEvent(platform, channel, eventType string, data map[string]any)
}

// ModAction enumerates the enforcement the dispatcher may carry out on a
// message, in increasing severity. ModActionNone leaves the message untouched.
type ModAction int

const (
	// ModActionNone means the message is clean; process it normally.
	ModActionNone ModAction = iota
	// ModActionDelete removes the message without timing the author out.
	ModActionDelete
	// ModActionTimeout removes the message and times the author out for Duration.
	ModActionTimeout
	// ModActionBan permanently bans the author.
	ModActionBan
)

// ModDecision is the decoupled moderation verdict the dispatcher acts on. It
// mirrors internal/moderation.Decision without importing it, keeping the
// runtime free of any moderation import (the same pattern as StreakOutcome).
type ModDecision struct {
	// Action is the enforcement to apply.
	Action ModAction
	// Duration is the timeout length, only meaningful for ModActionTimeout.
	Duration time.Duration
	// Reason is a short human-readable explanation (e.g. "82% caps").
	Reason string
	// DryRun is true when the engine is in shadow mode: the dispatcher logs
	// and records the decision but does NOT enforce it.
	DryRun bool
}

// Moderator is the narrow contract the dispatcher consults for every chat
// message before command routing. A thin adapter over internal/moderation
// satisfies it (wired in main), so the runtime stays decoupled from the
// automod packages. Evaluate must be safe for concurrent use and return a
// ModActionNone decision for clean messages.
type Moderator interface {
	Evaluate(ctx context.Context, channel, messageID, userID, username, text string,
		emoteCount int, firstMsg, isMod, isVIP, isSub, isBroadcaster bool) ModDecision
}

// Config configures the Dispatcher.
type Config struct {
	// TenantID is the single-tenant identifier this runtime serves.
	TenantID string

	// Platforms are the connected adapters to consume events from. Their
	// Connect method is the caller's responsibility; the dispatcher only
	// reads from each platform's Events channel until it closes.
	Platforms []adapters.Platform

	// Pity, when non-nil, receives a GrantPoints call for every chat
	// message that crosses the dispatcher. PointsPerMessage controls the
	// amount.
	Pity PityGranter

	// PointsPerMessage is the credit granted per chat message. Zero or
	// negative disables pity grants even when Pity is non-nil.
	PointsPerMessage int

	// Streak, when non-nil, receives a TickStreak call for every chat
	// message that crosses the dispatcher.
	Streak StreakTicker

	// Broadcaster, when non-nil, receives every event the dispatcher sees
	// so WebSocket / SSE consumers can render live activity.
	Broadcaster Broadcaster

	// Activity, when non-nil, is notified of every chat message so the
	// timers scheduler can gate auto-announcements behind chat activity.
	Activity ActivityRecorder

	// TTS, when non-nil, is notified of subscription events so the
	// text-to-speech feature can speak an alert. Best-effort and
	// non-blocking.
	TTS TTSNotifier

	// Actions, when non-nil, is fed every chat message, sub and raid so the
	// user-defined Action-Engine can fire automation rules. Best-effort and
	// non-blocking: it never stalls event consumption.
	Actions ActionEngine

	// Commands, when non-nil, routes incoming chat messages to bot
	// commands (e.g. "!pity"). A non-empty reply is sent back to chat via
	// the originating platform's Do(ActionSendMessage).
	Commands CommandRouter

	// Moderator, when non-nil, screens every chat message for AutoMod
	// violations before command routing. A non-pass decision is enforced via
	// the platform (delete/timeout/ban) and the message is dropped from
	// further processing (no command, points or streak).
	Moderator Moderator

	// Economy, when non-nil, is told about every chat message so it can award
	// loyalty points (the adapter rate-limits earning per viewer).
	Economy Economy

	// Wrapped, when non-nil, accumulates per-viewer Stream-Wrapped statistics
	// for every message/sub/raid the dispatcher sees. Best-effort: recording
	// is fire-and-forget and never blocks message processing.
	Wrapped WrappedRecorder

	// Translator, when non-nil, optionally translates each chat message and
	// posts the translation back to chat. Best-effort: it runs after command
	// routing and a failure never blocks message processing.
	Translator MessageTranslator

	// CoHost, when non-nil, optionally answers chat messages that address the
	// bot and posts the reply to chat. Best-effort: it runs after command
	// routing and a failure never blocks message processing.
	CoHost CoHost

	// CoHostSpeaker, when non-nil, speaks a co-host reply aloud when the
	// channel opted in (CoHost.Maybe reports speak=true). Best-effort and
	// non-blocking.
	CoHostSpeaker CoHostSpeaker

	// ClipDetector, when non-nil, is fed every chat message, sub and raid so
	// an auto-clipper can capture clip-worthy moments. Best-effort and
	// fire-and-forget: it never blocks message processing.
	ClipDetector ClipDetector

	// DiscordChannels, when non-nil, resolves the workspace channels a Discord
	// gateway message fans out to: those with an enabled discord.message rule.
	// A Discord message has no native workspace channel, so this maps it onto
	// the rules that opted in (like the Ko-fi donation fan-out). Nil disables
	// Discord message routing to the Action-Engine.
	DiscordChannels func(ctx context.Context) []string

	// Logger receives lifecycle and per-event debug logs. Defaults to
	// slog.Default().
	Logger *slog.Logger
}

// routedEvent carries an event together with the platform it arrived on so a
// per-channel worker goroutine can call handle with the correct platform.
type routedEvent struct {
	p  adapters.Platform
	ev adapters.Event
}

// Dispatcher is the runtime fan-in router.
type Dispatcher struct {
	cfg       Config
	logger    *slog.Logger
	stats     stats
	started   atomic.Bool
	stopped   atomic.Bool
	doneCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once

	// workers maps a channel name to its dedicated work queue. Each channel
	// is drained by exactly one channelWorker goroutine, which preserves
	// per-channel event ordering while isolating a slow channel (one stuck
	// on a blocking platform call) from every other channel and from the
	// platform reader loop in consume.
	workers   map[string]chan routedEvent
	workersMu sync.RWMutex
	workerWg  sync.WaitGroup
}

type stats struct {
	mu               sync.Mutex
	messages         int64
	subs             int64
	raids            int64
	pityGrantErrors  int64
	streakTickErrors int64
	droppedEvents    uint64
	lastEventAt      time.Time
}

// New constructs a Dispatcher.
func New(cfg Config) *Dispatcher {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.TenantID == "" {
		cfg.TenantID = "default"
	}
	return &Dispatcher{
		cfg:     cfg,
		logger:  cfg.Logger.With("component", "runtime.dispatcher"),
		doneCh:  make(chan struct{}),
		workers: make(map[string]chan routedEvent),
	}
}

// Run consumes events from every configured platform until ctx is cancelled
// or all platform Events channels close. Safe to call exactly once.
func (d *Dispatcher) Run(ctx context.Context) error {
	if !d.started.CompareAndSwap(false, true) {
		return ErrAlreadyStarted
	}
	defer d.stopOnce.Do(func() {
		d.stopped.Store(true)
		close(d.doneCh)
	})

	if len(d.cfg.Platforms) == 0 {
		d.logger.Info("dispatcher idle (no platforms configured)")
		<-ctx.Done()
		return nil
	}

	d.logger.Info("dispatcher starting", "platforms", platformNames(d.cfg.Platforms))

	var wg sync.WaitGroup
	for _, p := range d.cfg.Platforms {
		wg.Add(1)
		go func(p adapters.Platform) {
			defer wg.Done()
			d.consume(ctx, p)
		}(p)
	}

	wg.Wait()

	// Every consume loop has returned, so no producer can send to a worker
	// queue any more. Closing the queues here therefore cannot race a send.
	// Workers drain remaining buffered events, then exit on the closed
	// channel; workerWg.Wait blocks until they all return.
	d.workersMu.Lock()
	for _, ch := range d.workers {
		close(ch)
	}
	d.workersMu.Unlock()
	d.workerWg.Wait()

	d.logger.Info("dispatcher stopped",
		"messages", d.stats.snapshot().messages,
		"subs", d.stats.snapshot().subs,
		"raids", d.stats.snapshot().raids,
	)
	return nil
}

// Done returns a channel closed when Run returns.
func (d *Dispatcher) Done() <-chan struct{} { return d.doneCh }

// Stats returns a snapshot of internal counters. Safe for concurrent use.
func (d *Dispatcher) Stats() Stats { return d.stats.snapshot().public() }

// Stats is the public counter view returned by [Dispatcher.Stats].
type Stats struct {
	Messages         int64     `json:"messages"`
	Subscriptions    int64     `json:"subscriptions"`
	Raids            int64     `json:"raids"`
	PityGrantErrors  int64     `json:"pity_grant_errors"`
	StreakTickErrors int64     `json:"streak_tick_errors"`
	DroppedEvents    uint64    `json:"dropped_events"`
	LastEventAt      time.Time `json:"last_event_at"`
}

func (s *stats) snapshot() statsSnap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return statsSnap{
		messages:         s.messages,
		subs:             s.subs,
		raids:            s.raids,
		pityGrantErrors:  s.pityGrantErrors,
		streakTickErrors: s.streakTickErrors,
		droppedEvents:    s.droppedEvents,
		lastEventAt:      s.lastEventAt,
	}
}

type statsSnap struct {
	messages         int64
	subs             int64
	raids            int64
	pityGrantErrors  int64
	streakTickErrors int64
	droppedEvents    uint64
	lastEventAt      time.Time
}

func (s statsSnap) public() Stats {
	return Stats{
		Messages:         s.messages,
		Subscriptions:    s.subs,
		Raids:            s.raids,
		PityGrantErrors:  s.pityGrantErrors,
		StreakTickErrors: s.streakTickErrors,
		DroppedEvents:    s.droppedEvents,
		LastEventAt:      s.lastEventAt,
	}
}

func (d *Dispatcher) consume(ctx context.Context, p adapters.Platform) {
	log := d.logger.With("platform", p.Name())
	events := p.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				log.Info("platform events channel closed")
				return
			}
			d.route(ctx, p, ev)
		}
	}
}

// route dispatches an event to its per-channel worker queue, preserving
// per-channel ordering. Channel-less events keep today's synchronous behavior
// so the platform reader stays responsible for them directly.
func (d *Dispatcher) route(ctx context.Context, p adapters.Platform, ev adapters.Event) {
	if ev.Channel == "" {
		d.handle(ctx, p, ev)
		return
	}
	ch := d.workerQueue(ctx, ev.Channel)
	rev := routedEvent{p: p, ev: ev}
	select {
	case ch <- rev:
	default:
		d.stats.mu.Lock()
		d.stats.droppedEvents++
		d.stats.mu.Unlock()
	}
}

// workerQueue returns the work queue for channel, lazily creating it and
// starting its single draining goroutine on first use. The read lock covers
// the common already-exists path; creation upgrades to the write lock and
// re-checks so two producers cannot start two workers for one channel.
func (d *Dispatcher) workerQueue(ctx context.Context, channel string) chan routedEvent {
	d.workersMu.RLock()
	ch, ok := d.workers[channel]
	d.workersMu.RUnlock()
	if ok {
		return ch
	}

	d.workersMu.Lock()
	defer d.workersMu.Unlock()
	if ch, ok = d.workers[channel]; ok {
		return ch
	}
	ch = make(chan routedEvent, 256)
	d.workers[channel] = ch
	d.workerWg.Add(1)
	go d.channelWorker(ctx, ch)
	return ch
}

// channelWorker drains a single channel's queue, calling handle one event at a
// time so each channel keeps strict input order. It exits on ctx cancellation
// or when its queue is closed during shutdown.
func (d *Dispatcher) channelWorker(ctx context.Context, ch <-chan routedEvent) {
	defer d.workerWg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case rev, ok := <-ch:
			if !ok {
				return
			}
			d.handle(ctx, rev.p, rev.ev)
		}
	}
}

func (d *Dispatcher) handle(ctx context.Context, p adapters.Platform, ev adapters.Event) {
	d.stats.mu.Lock()
	d.stats.lastEventAt = ev.OccurredAt
	d.stats.mu.Unlock()

	if d.cfg.Broadcaster != nil {
		d.cfg.Broadcaster.Broadcast(string(ev.Type), ev)
	}

	switch ev.Type {
	case adapters.EventMessageCreated:
		d.onMessage(ctx, p, ev)
	case adapters.EventUserSubscribed, adapters.EventUserResubscribed:
		d.stats.mu.Lock()
		d.stats.subs++
		d.stats.mu.Unlock()
		if d.cfg.Wrapped != nil && ev.Subscription != nil && ev.Subscription.UserID != "" {
			d.cfg.Wrapped.RecordSub(ctx, ev.Channel,
				ev.Subscription.UserID, ev.Subscription.Username, 0)
		}
		if d.cfg.ClipDetector != nil && ev.Channel != "" {
			d.cfg.ClipDetector.Sub(ctx, ev.Channel)
		}
		if d.cfg.TTS != nil && ev.Channel != "" && ev.Subscription != nil {
			switch {
			case ev.Subscription.IsGift:
				d.cfg.TTS.OnGiftSubscribe(ev.Channel, ev.Subscription.GiftedBy)
			case ev.Type == adapters.EventUserResubscribed:
				d.cfg.TTS.OnResubscribe(ev.Channel, ev.Subscription.Username, ev.Subscription.MonthsTotal)
			default:
				d.cfg.TTS.OnSubscribe(ev.Channel, ev.Subscription.Username)
			}
		}
		if d.cfg.Actions != nil && ev.Channel != "" {
			d.cfg.Actions.OnEvent(ev.Platform, ev.Channel, string(ev.Type), subEventData(ev))
		}
	case adapters.EventChannelRaided:
		d.stats.mu.Lock()
		d.stats.raids++
		d.stats.mu.Unlock()
		if d.cfg.Wrapped != nil && ev.Raid != nil && ev.Raid.FromUsername != "" {
			d.cfg.Wrapped.RecordRaid(ctx, ev.Channel, ev.Raid.FromUsername)
		}
		if d.cfg.ClipDetector != nil && ev.Raid != nil && ev.Channel != "" {
			d.cfg.ClipDetector.Raid(ctx, ev.Channel, ev.Raid.ViewerCount)
		}
		if d.cfg.TTS != nil && ev.Raid != nil && ev.Channel != "" {
			d.cfg.TTS.OnRaid(ev.Channel, ev.Raid.FromUsername, ev.Raid.ViewerCount)
		}
		if d.cfg.Actions != nil && ev.Channel != "" {
			d.cfg.Actions.OnEvent(ev.Platform, ev.Channel, string(ev.Type), raidEventData(ev))
		}
	case adapters.EventStreamOnline, adapters.EventStreamOffline:
		if d.cfg.Actions != nil && ev.Channel != "" {
			d.cfg.Actions.OnEvent(ev.Platform, ev.Channel, string(ev.Type), streamEventData(ev))
		}
	case adapters.EventDonation:
		if d.cfg.Actions != nil && ev.Channel != "" {
			d.cfg.Actions.OnEvent(ev.Platform, ev.Channel, string(ev.Type), donationEventData(ev))
		}
	case adapters.EventDiscordMessage:
		d.onDiscordMessage(ctx, ev)
	}
}

// onDiscordMessage routes a Discord gateway message to the Action-Engine. A
// Discord message has no native workspace channel, so it fans out to every
// channel with an enabled discord.message rule (via DiscordChannels). For each
// such channel it fires the event trigger (discord.message, carrying the
// discord.* vars) and the message path (so command-kind rules and message rules
// fire too, with $(source)=discord). It deliberately does NOT run the
// points/streak/economy machinery of onMessage: Discord chat must not mint
// Twitch loyalty. Message content is never logged above debug.
func (d *Dispatcher) onDiscordMessage(ctx context.Context, ev adapters.Event) {
	if ev.Discord == nil || ev.Discord.UserID == "" || d.cfg.Actions == nil || d.cfg.DiscordChannels == nil {
		return
	}
	d.stats.mu.Lock()
	d.stats.messages++
	d.stats.mu.Unlock()

	channels := d.cfg.DiscordChannels(ctx)
	if len(channels) == 0 {
		return
	}
	data := discordEventData(ev)
	dm := ev.Discord
	d.logger.Debug("discord message routed",
		"channels", len(channels), "guild", dm.GuildID, "discord_channel", dm.ChannelID, "user", dm.Username)
	for _, ch := range channels {
		if ch == "" {
			continue
		}
		d.cfg.Actions.OnEvent(ev.Platform, ch, string(ev.Type), data)
		d.cfg.Actions.OnMessage(ev.Platform, ch, dm.MessageID, dm.UserID, dm.Username, dm.Text,
			false, dm.IsModerator, false, false)
	}
}

// Dispatch routes a synthesized event through the same handling path platform
// producers use, so out-of-band sources (the Ko-fi donation webhook) reach the
// action engine identically to native events. It runs synchronously on the
// caller's goroutine with a nil platform, which the donation branch never
// dereferences; do not use it for message events, whose handling needs a live
// platform to reply on.
func (d *Dispatcher) Dispatch(ctx context.Context, ev adapters.Event) {
	d.handle(ctx, nil, ev)
}

func (d *Dispatcher) onMessage(ctx context.Context, p adapters.Platform, ev adapters.Event) {
	if ev.Message == nil {
		return
	}
	d.stats.mu.Lock()
	d.stats.messages++
	d.stats.mu.Unlock()

	if ev.Message.UserID == "" || ev.Channel == "" {
		return
	}

	if d.cfg.Activity != nil {
		d.cfg.Activity.RecordChatActivity(ev.Channel)
	}

	if d.moderate(ctx, p, ev) {
		return
	}

	d.routeCommand(ctx, p, ev)

	d.translate(ctx, p, ev)

	d.cohost(ctx, p, ev)

	if d.cfg.ClipDetector != nil {
		d.cfg.ClipDetector.Message(ctx, ev.Channel, ev.Message.UserID, ev.Message.Username, ev.Message.Content)
	}

	if d.cfg.Actions != nil {
		isBroadcaster := ev.Message.Username != "" &&
			strings.EqualFold(ev.Message.Username, ev.Channel)
		d.cfg.Actions.OnMessage(ev.Platform, ev.Channel, ev.Message.ID,
			ev.Message.UserID, ev.Message.Username, ev.Message.Content,
			isBroadcaster, ev.Message.IsModerator, ev.Message.IsVIP, ev.Message.IsSubscriber)
	}

	if d.cfg.Pity != nil && d.cfg.PointsPerMessage > 0 {
		if _, err := d.cfg.Pity.GrantPoints(ctx, d.cfg.TenantID,
			ev.Channel, ev.Message.UserID, ev.Message.Username,
			"chat:"+ev.Platform, d.cfg.PointsPerMessage); err != nil {
			d.stats.mu.Lock()
			d.stats.pityGrantErrors++
			d.stats.mu.Unlock()
			d.logger.Warn("pity grant failed",
				"platform", ev.Platform,
				"channel", ev.Channel,
				"viewer", ev.Message.UserID,
				"err", err,
			)
		}
	}

	if d.cfg.Economy != nil {
		d.cfg.Economy.Award(ctx, d.cfg.TenantID,
			ev.Channel, ev.Message.UserID, ev.Message.Username)
	}

	if d.cfg.Wrapped != nil {
		d.cfg.Wrapped.RecordMessage(ctx, ev.Channel,
			ev.Message.UserID, ev.Message.Username)
	}

	if d.cfg.Streak != nil {
		outcome, err := d.cfg.Streak.TickStreak(ctx, d.cfg.TenantID,
			ev.Channel, ev.Message.UserID, ev.Message.Username)
		if err != nil {
			d.stats.mu.Lock()
			d.stats.streakTickErrors++
			d.stats.mu.Unlock()
			d.logger.Warn("streak tick failed",
				"platform", ev.Platform,
				"channel", ev.Channel,
				"viewer", ev.Message.UserID,
				"err", err,
			)
			return
		}
		if d.cfg.Broadcaster != nil {
			if outcome.Milestone > 0 {
				d.cfg.Broadcaster.Broadcast("feature.streak.milestone", streakMilestonePayload{
					Platform:    ev.Platform,
					Channel:     ev.Channel,
					ViewerID:    ev.Message.UserID,
					Username:    ev.Message.Username,
					DaysCurrent: outcome.DaysCurrent,
					DaysLongest: outcome.DaysLongest,
					Milestone:   outcome.Milestone,
				})
			}
			if outcome.BrokenFromDays > 0 {
				d.cfg.Broadcaster.Broadcast("feature.streak.broken", streakBrokenPayload{
					Platform:       ev.Platform,
					Channel:        ev.Channel,
					ViewerID:       ev.Message.UserID,
					Username:       ev.Message.Username,
					BrokenFromDays: outcome.BrokenFromDays,
					DaysCurrent:    outcome.DaysCurrent,
				})
			}
		}
	}
}

// moderate screens a message through the configured Moderator and, on a
// non-pass decision, enforces it on the platform (delete/timeout/ban) unless
// the decision is a dry-run. It returns true when the message was handled by
// moderation and the dispatcher should stop processing it (no command, points
// or streak for a rule-breaking message). A nil Moderator always returns false.
func (d *Dispatcher) moderate(ctx context.Context, p adapters.Platform, ev adapters.Event) bool {
	if d.cfg.Moderator == nil || p == nil {
		return false
	}
	m := ev.Message
	isBroadcaster := m.Username != "" && strings.EqualFold(m.Username, ev.Channel)
	dec := d.cfg.Moderator.Evaluate(ctx, ev.Channel, m.ID, m.UserID, m.Username,
		m.Content, len(m.EmotesUsed), false,
		m.IsModerator, m.IsVIP, m.IsSubscriber, isBroadcaster)
	if dec.Action == ModActionNone {
		return false
	}

	d.logger.Info("automod",
		"channel", ev.Channel, "user", m.Username,
		"action", dec.Action, "reason", dec.Reason, "dry_run", dec.DryRun)

	if dec.DryRun {
		return true
	}

	for _, act := range moderationActions(ev.Channel, m, dec) {
		if err := p.Do(ctx, act); err != nil {
			d.logger.Warn("automod action failed",
				"channel", ev.Channel, "type", act.Type, "err", err)
		}
	}
	return true
}

// moderationActions translates a moderation decision into the concrete platform
// actions to perform: every punishment first deletes the offending message,
// then a timeout or ban additionally restricts the user.
func moderationActions(channel string, m *adapters.MessageEvent, dec ModDecision) []adapters.Action {
	acts := make([]adapters.Action, 0, 2)
	if m.ID != "" {
		acts = append(acts, adapters.Action{
			Type:          adapters.ActionDeleteMessage,
			Channel:       channel,
			DeleteMessage: &adapters.DeleteMessageAction{MessageID: m.ID},
		})
	}
	switch dec.Action {
	case ModActionTimeout:
		acts = append(acts, adapters.Action{
			Type:    adapters.ActionTimeout,
			Channel: channel,
			Timeout: &adapters.TimeoutAction{UserID: m.UserID, Reason: dec.Reason, Duration: dec.Duration},
		})
	case ModActionBan:
		acts = append(acts, adapters.Action{
			Type:    adapters.ActionBan,
			Channel: channel,
			Ban:     &adapters.BanAction{UserID: m.UserID, Reason: dec.Reason},
		})
	}
	return acts
}

// routeCommand offers the message to the command router (when configured)
// and sends any non-empty reply back to chat on the originating platform.
// Send failures are logged but never propagated - a failed reply must not
// disrupt event consumption.
func (d *Dispatcher) routeCommand(ctx context.Context, p adapters.Platform, ev adapters.Event) {
	if d.cfg.Commands == nil || p == nil {
		return
	}
	reply, handled := d.cfg.Commands.Route(ctx, CommandInvocation{
		Platform: ev.Platform,
		Channel:  ev.Channel,
		UserID:   ev.Message.UserID,
		Username: ev.Message.Username,
		Text:     ev.Message.Content,
		// The broadcaster's chat login equals the channel name on Twitch;
		// no badge flag is emitted for it, so derive it here.
		IsBroadcaster: ev.Message.Username != "" &&
			strings.EqualFold(ev.Message.Username, ev.Channel),
		IsModerator:  ev.Message.IsModerator,
		IsVIP:        ev.Message.IsVIP,
		IsSubscriber: ev.Message.IsSubscriber,
	})
	if !handled || reply.Text == "" {
		return
	}
	if err := p.Do(ctx, adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     ev.Channel,
		SendMessage: &adapters.SendMessageAction{Text: reply.Text},
	}); err != nil {
		d.logger.Warn("command reply send failed",
			"platform", ev.Platform, "channel", ev.Channel, "err", err)
	}
}

func (d *Dispatcher) translate(ctx context.Context, p adapters.Platform, ev adapters.Event) {
	if d.cfg.Translator == nil || p == nil || ev.Message == nil {
		return
	}
	reply, ok := d.cfg.Translator.Maybe(ctx, ev.Channel, ev.Message.UserID, ev.Message.Content)
	if !ok || reply == "" {
		return
	}
	if err := p.Do(ctx, adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     ev.Channel,
		SendMessage: &adapters.SendMessageAction{Text: reply},
	}); err != nil {
		d.logger.Warn("translation send failed",
			"platform", ev.Platform, "channel", ev.Channel, "err", err)
	}
}

func (d *Dispatcher) cohost(ctx context.Context, p adapters.Platform, ev adapters.Event) {
	if d.cfg.CoHost == nil || p == nil || ev.Message == nil {
		return
	}
	reply, speak, ok := d.cfg.CoHost.Maybe(ctx, ev.Channel, ev.Message.UserID, ev.Message.Username, ev.Message.Content)
	if !ok || reply == "" {
		return
	}
	if err := p.Do(ctx, adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     ev.Channel,
		SendMessage: &adapters.SendMessageAction{Text: reply},
	}); err != nil {
		d.logger.Warn("cohost send failed",
			"platform", ev.Platform, "channel", ev.Channel, "err", err)
	}
	if speak && d.cfg.CoHostSpeaker != nil {
		d.cfg.CoHostSpeaker.Speak(ev.Channel, reply)
	}
}

// subEventData flattens a subscription event into the salient fields a rule's
// conditions and variables read, with nil-safe access to the optional payload.
func subEventData(ev adapters.Event) map[string]any {
	d := map[string]any{}
	if ev.Subscription != nil {
		d["user_id"] = ev.Subscription.UserID
		d["username"] = ev.Subscription.Username
		d["tier"] = ev.Subscription.Tier
		d["months_total"] = ev.Subscription.MonthsTotal
		d["is_gift"] = ev.Subscription.IsGift
	}
	return d
}

// raidEventData flattens a raid event into the salient fields a rule reads.
func raidEventData(ev adapters.Event) map[string]any {
	d := map[string]any{}
	if ev.Raid != nil {
		d["from_username"] = ev.Raid.FromUsername
		d["viewer_count"] = ev.Raid.ViewerCount
	}
	return d
}

// streamEventData flattens a stream event into the salient fields a rule reads.
func streamEventData(ev adapters.Event) map[string]any {
	d := map[string]any{}
	if ev.Stream != nil {
		d["is_live"] = ev.Stream.IsLive
		if !ev.Stream.StartedAt.IsZero() {
			d["started_at"] = ev.Stream.StartedAt.Format(time.RFC3339)
		}
	}
	return d
}

func donationEventData(ev adapters.Event) map[string]any {
	d := map[string]any{}
	if ev.Donation != nil {
		d["donation.from"] = ev.Donation.From
		d["donation.amount"] = ev.Donation.Amount
		d["donation.currency"] = ev.Donation.Currency
		d["donation.message"] = ev.Donation.Message
		d["donation.kind"] = ev.Donation.Kind
	}
	return d
}

func discordEventData(ev adapters.Event) map[string]any {
	d := map[string]any{}
	if ev.Discord != nil {
		d["discord.username"] = ev.Discord.Username
		d["discord.user_id"] = ev.Discord.UserID
		d["discord.text"] = ev.Discord.Text
		d["discord.channel"] = ev.Discord.ChannelName
		d["discord.channel_id"] = ev.Discord.ChannelID
		d["discord.guild"] = ev.Discord.GuildID
		d["discord.message_id"] = ev.Discord.MessageID
		d["discord.is_dm"] = ev.Discord.IsDM
		d["source"] = ev.Platform
	}
	return d
}

func platformNames(ps []adapters.Platform) []string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name()
	}
	return names
}

// ErrAlreadyStarted is returned by Run on the second call.
var ErrAlreadyStarted = fmt.Errorf("runtime: dispatcher already started")
