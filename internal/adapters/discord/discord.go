package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

const (
	eventBuffer = 256
	botPrefix   = "Bot "
)

// Errors returned by the Discord adapter.
var (
	ErrNotConnected     = errors.New("discord: not connected")
	ErrAlreadyConnected = errors.New("discord: already connected")
	ErrTokenRequired    = errors.New("discord: token required")
	ErrUnknownAction    = errors.New("discord: unknown action type")
	ErrMissingPayload   = errors.New("discord: action payload missing")
	ErrUnknownGuild     = errors.New("discord: guild for channel is unknown; have not yet observed a message there")
)

// Config controls construction of a Discord [Adapter].
type Config struct {
	// Token is the Discord bot token. The conventional "Bot " prefix is
	// optional; the adapter adds it if missing. Empty Token causes
	// [Adapter.Connect] to fail with [ErrTokenRequired].
	Token string

	// Channels optionally restricts which Discord channel ids the adapter
	// will surface events for. An empty slice means "every channel the bot
	// can see".
	Channels []string

	// Logger receives structured log output. If nil, [slog.Default] is
	// used.
	Logger *slog.Logger

	// NewSession is a test hook. Production callers leave it nil and the
	// adapter constructs the session via discordgo.New("Bot " + Token).
	NewSession func(token string) (*discordgo.Session, error)
}

// Adapter is the Discord implementation of [adapters.Platform].
type Adapter struct {
	cfg            Config
	logger         *slog.Logger
	channelAllowed map[string]struct{}

	mu               sync.Mutex
	session          *discordgo.Session
	events           chan adapters.Event
	connected        bool
	healthErr        error
	channelToGuild   map[string]string
	handlerRemovers  []func()
	cancelCtxWatcher context.CancelFunc
}

// New constructs a Discord adapter from cfg. The returned adapter is
// disconnected; call [Adapter.Connect] to establish the gateway session.
func New(cfg Config) *Adapter {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "adapters.discord")

	allowed := make(map[string]struct{}, len(cfg.Channels))
	for _, c := range cfg.Channels {
		if c != "" {
			allowed[c] = struct{}{}
		}
	}
	return &Adapter{
		cfg:            cfg,
		logger:         logger,
		channelAllowed: allowed,
		healthErr:      ErrNotConnected,
		channelToGuild: make(map[string]string),
	}
}

// Name returns the platform identifier "discord".
func (a *Adapter) Name() string { return platformName }

// Connect builds a discordgo session (via [Config.NewSession] when set, or
// the default factory), registers event handlers, opens the gateway, and
// starts a goroutine that closes the adapter when ctx is cancelled.
func (a *Adapter) Connect(ctx context.Context) error {
	if strings.TrimSpace(a.cfg.Token) == "" {
		return ErrTokenRequired
	}

	a.mu.Lock()
	if a.connected {
		a.mu.Unlock()
		return ErrAlreadyConnected
	}
	a.mu.Unlock()

	token := a.cfg.Token
	if !strings.HasPrefix(token, botPrefix) {
		token = botPrefix + token
	}

	factory := a.cfg.NewSession
	if factory == nil {
		factory = discordgo.New
	}
	session, err := factory(token)
	if err != nil {
		return fmt.Errorf("discord: create session: %w", err)
	}

	a.mu.Lock()
	a.session = session
	a.events = make(chan adapters.Event, eventBuffer)
	a.connected = true
	a.healthErr = nil
	a.registerHandlersLocked()
	watcherCtx, cancel := context.WithCancel(context.Background())
	a.cancelCtxWatcher = cancel
	a.mu.Unlock()

	if err := session.Open(); err != nil {
		a.cleanup()
		return fmt.Errorf("discord: open session: %w", err)
	}

	go a.watchContext(ctx, watcherCtx)
	a.logger.Info("discord adapter connected")
	return nil
}

func (a *Adapter) watchContext(userCtx, watcherCtx context.Context) {
	select {
	case <-userCtx.Done():
		_ = a.Disconnect(context.Background())
	case <-watcherCtx.Done():
	}
}

// registerHandlersLocked attaches discordgo event callbacks; caller holds
// a.mu.
func (a *Adapter) registerHandlersLocked() {
	a.handlerRemovers = nil

	a.handlerRemovers = append(a.handlerRemovers,
		a.session.AddHandler(func(_ *discordgo.Session, r *discordgo.Ready) {
			a.onReady(r)
		}),
		a.session.AddHandler(func(_ *discordgo.Session, m *discordgo.MessageCreate) {
			a.onMessageCreate(m)
		}),
		a.session.AddHandler(func(_ *discordgo.Session, m *discordgo.MessageDelete) {
			a.onMessageDelete(m)
		}),
		a.session.AddHandler(func(_ *discordgo.Session, _ *discordgo.Disconnect) {
			a.onDisconnect()
		}),
		a.session.AddHandler(func(_ *discordgo.Session, _ *discordgo.Connect) {
			a.onConnect()
		}),
	)
}

// Disconnect tears down the discord gateway connection and closes the
// events channel. It is idempotent.
func (a *Adapter) Disconnect(_ context.Context) error {
	a.mu.Lock()
	if !a.connected {
		a.mu.Unlock()
		return nil
	}
	session := a.session
	events := a.events
	removers := a.handlerRemovers
	cancel := a.cancelCtxWatcher

	a.connected = false
	a.session = nil
	a.events = nil
	a.handlerRemovers = nil
	a.cancelCtxWatcher = nil
	a.healthErr = ErrNotConnected
	a.mu.Unlock()

	for _, rm := range removers {
		rm()
	}
	if cancel != nil {
		cancel()
	}
	var err error
	if session != nil {
		err = session.Close()
	}
	if events != nil {
		close(events)
	}
	a.logger.Info("discord adapter disconnected")
	return err
}

// cleanup is the failure-path counterpart to Disconnect used when Open
// returns an error and there is no live session to close.
func (a *Adapter) cleanup() {
	a.mu.Lock()
	events := a.events
	cancel := a.cancelCtxWatcher
	a.session = nil
	a.events = nil
	a.connected = false
	a.cancelCtxWatcher = nil
	a.healthErr = ErrNotConnected
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if events != nil {
		close(events)
	}
}

// Events returns a channel that delivers normalized events until the
// adapter is disconnected.
func (a *Adapter) Events() <-chan adapters.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.events
}

// Health reports the cached connection state without touching the network.
func (a *Adapter) Health() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.connected {
		return ErrNotConnected
	}
	return a.healthErr
}

// Do dispatches a platform action via the Discord REST API.
func (a *Adapter) Do(ctx context.Context, action adapters.Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	if !a.connected {
		a.mu.Unlock()
		return ErrNotConnected
	}
	session := a.session
	a.mu.Unlock()

	switch action.Type {
	case adapters.ActionSendMessage:
		return a.doSendMessage(session, action)
	case adapters.ActionDeleteMessage:
		return a.doDeleteMessage(session, action)
	case adapters.ActionBan:
		return a.doBan(session, action)
	case adapters.ActionTimeout:
		return a.doTimeout(session, action)
	case adapters.ActionUntimeout:
		return a.doUntimeout(session, action)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAction, action.Type)
	}
}

func (a *Adapter) doSendMessage(s *discordgo.Session, act adapters.Action) error {
	if act.SendMessage == nil {
		return ErrMissingPayload
	}
	if act.SendMessage.ReplyTo != "" {
		_, err := s.ChannelMessageSendReply(act.Channel, act.SendMessage.Text, &discordgo.MessageReference{
			MessageID: act.SendMessage.ReplyTo,
			ChannelID: act.Channel,
		})
		return err
	}
	_, err := s.ChannelMessageSend(act.Channel, act.SendMessage.Text)
	return err
}

func (a *Adapter) doDeleteMessage(s *discordgo.Session, act adapters.Action) error {
	if act.DeleteMessage == nil {
		return ErrMissingPayload
	}
	return s.ChannelMessageDelete(act.Channel, act.DeleteMessage.MessageID)
}

func (a *Adapter) doBan(s *discordgo.Session, act adapters.Action) error {
	if act.Ban == nil {
		return ErrMissingPayload
	}
	guildID, err := a.guildForChannel(act.Channel)
	if err != nil {
		return err
	}
	return s.GuildBanCreateWithReason(guildID, act.Ban.UserID, act.Ban.Reason, 0)
}

func (a *Adapter) doTimeout(s *discordgo.Session, act adapters.Action) error {
	if act.Timeout == nil {
		return ErrMissingPayload
	}
	guildID, err := a.guildForChannel(act.Channel)
	if err != nil {
		return err
	}
	until := time.Now().Add(act.Timeout.Duration)
	return s.GuildMemberTimeout(guildID, act.Timeout.UserID, &until)
}

func (a *Adapter) doUntimeout(s *discordgo.Session, act adapters.Action) error {
	if act.Untimeout == nil {
		return ErrMissingPayload
	}
	guildID, err := a.guildForChannel(act.Channel)
	if err != nil {
		return err
	}
	return s.GuildMemberTimeout(guildID, act.Untimeout.UserID, nil)
}

func (a *Adapter) guildForChannel(channelID string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gid, ok := a.channelToGuild[channelID]; ok && gid != "" {
		return gid, nil
	}
	return "", fmt.Errorf("%w: channel=%s", ErrUnknownGuild, channelID)
}

func (a *Adapter) onReady(r *discordgo.Ready) {
	a.mu.Lock()
	if r != nil {
		for _, g := range r.Guilds {
			if g == nil {
				continue
			}
			for _, ch := range g.Channels {
				if ch != nil {
					a.channelToGuild[ch.ID] = g.ID
				}
			}
		}
	}
	a.healthErr = nil
	a.mu.Unlock()
	a.emit(connectionEvent(adapters.EventConnected, "discord ready", ""))
}

func (a *Adapter) onConnect() {
	a.mu.Lock()
	a.healthErr = nil
	a.mu.Unlock()
}

func (a *Adapter) onDisconnect() {
	a.mu.Lock()
	a.healthErr = errors.New("discord: gateway disconnected")
	a.mu.Unlock()
	a.emit(connectionEvent(adapters.EventDisconnected, "discord gateway disconnected", ""))
	a.emit(connectionEvent(adapters.EventReconnecting, "discord gateway reconnecting", ""))
}

func (a *Adapter) onMessageCreate(m *discordgo.MessageCreate) {
	if m == nil || m.Message == nil {
		return
	}
	if !a.channelAllowedID(m.ChannelID) {
		return
	}
	if m.GuildID != "" {
		a.mu.Lock()
		a.channelToGuild[m.ChannelID] = m.GuildID
		a.mu.Unlock()
	}
	a.emit(translateMessageCreate(m, a.roleLookup(m.GuildID)))
}

func (a *Adapter) onMessageDelete(m *discordgo.MessageDelete) {
	if m == nil || m.Message == nil {
		return
	}
	if !a.channelAllowedID(m.ChannelID) {
		return
	}
	a.emit(translateMessageDelete(m))
}

func (a *Adapter) channelAllowedID(id string) bool {
	if len(a.channelAllowed) == 0 {
		return true
	}
	_, ok := a.channelAllowed[id]
	return ok
}

// roleLookup returns a function that resolves a role id within the given
// guild against the discordgo state cache. If the session has no cached
// state, the returned function always yields nil.
func (a *Adapter) roleLookup(guildID string) func(string) *discordgo.Role {
	a.mu.Lock()
	session := a.session
	a.mu.Unlock()
	return func(roleID string) *discordgo.Role {
		if session == nil || session.State == nil || guildID == "" || roleID == "" {
			return nil
		}
		role, err := session.State.Role(guildID, roleID)
		if err != nil {
			return nil
		}
		return role
	}
}

// emit pushes an event onto the buffered channel. If the channel is full
// (consumer is too slow) the event is dropped and a warning logged so the
// discord gateway goroutine never blocks.
func (a *Adapter) emit(e adapters.Event) {
	a.mu.Lock()
	ch := a.events
	connected := a.connected
	a.mu.Unlock()
	if !connected || ch == nil {
		return
	}
	select {
	case ch <- e:
	default:
		a.logger.Warn("discord events channel full; dropping event", "type", e.Type)
	}
}

var _ adapters.Platform = (*Adapter)(nil)
