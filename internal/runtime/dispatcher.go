package runtime

import (
	"context"
	"errors"
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

	// Commands, when non-nil, routes incoming chat messages to bot
	// commands (e.g. "!pity"). A non-empty reply is sent back to chat via
	// the originating platform's Do(ActionSendMessage).
	Commands CommandRouter

	// Logger receives lifecycle and per-event debug logs. Defaults to
	// slog.Default().
	Logger *slog.Logger
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
}

type stats struct {
	mu               sync.Mutex
	messages         int64
	subs             int64
	raids            int64
	pityGrantErrors  int64
	streakTickErrors int64
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
		cfg:    cfg,
		logger: cfg.Logger.With("component", "runtime.dispatcher"),
		doneCh: make(chan struct{}),
	}
}

// Run consumes events from every configured platform until ctx is cancelled
// or all platform Events channels close. Safe to call exactly once.
func (d *Dispatcher) Run(ctx context.Context) error {
	if !d.started.CompareAndSwap(false, true) {
		return errors.New("runtime: dispatcher already started")
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
		lastEventAt:      s.lastEventAt,
	}
}

type statsSnap struct {
	messages         int64
	subs             int64
	raids            int64
	pityGrantErrors  int64
	streakTickErrors int64
	lastEventAt      time.Time
}

func (s statsSnap) public() Stats {
	return Stats{
		Messages:         s.messages,
		Subscriptions:    s.subs,
		Raids:            s.raids,
		PityGrantErrors:  s.pityGrantErrors,
		StreakTickErrors: s.streakTickErrors,
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
			d.handle(ctx, p, ev)
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
	case adapters.EventChannelRaided:
		d.stats.mu.Lock()
		d.stats.raids++
		d.stats.mu.Unlock()
	}
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

	d.routeCommand(ctx, p, ev)

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

// routeCommand offers the message to the command router (when configured)
// and sends any non-empty reply back to chat on the originating platform.
// Send failures are logged but never propagated — a failed reply must not
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

func platformNames(ps []adapters.Platform) []string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name()
	}
	return names
}

// ErrAlreadyStarted is returned by Run on the second call.
var ErrAlreadyStarted = fmt.Errorf("runtime: dispatcher already started")
