package twitch

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	irc "github.com/gempir/go-twitch-irc/v4"
	"github.com/nicklaw5/helix/v2"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

const eventBuffer = 256

// Errors returned by the Twitch adapter.
var (
	ErrNotConnected     = errors.New("twitch: not connected")
	ErrAlreadyConnected = errors.New("twitch: already connected")
	ErrAnonymous        = errors.New("twitch: anonymous mode does not permit send")
	ErrAnonymousAction  = errors.New("twitch: anonymous mode does not permit moderation actions")
	ErrUnknownAction    = errors.New("twitch: unknown action type")
	ErrMissingPayload   = errors.New("twitch: action payload missing")
	ErrUnknownChannel   = errors.New("twitch: broadcaster id for channel is unknown; have not yet observed a message there")
	ErrHelixUnavailable = errors.New("twitch: helix client not configured")

	// ErrEmptyToken is returned by [Adapter.SetToken] when called with an
	// empty access token. No state is mutated in that case.
	ErrEmptyToken = errors.New("twitch: empty access token")

	// ErrAnonymousRotation is returned by [Adapter.SetToken] when invoked
	// on an adapter running in anonymous mode — there is no user token to
	// rotate.
	ErrAnonymousRotation = errors.New("twitch: cannot rotate token on anonymous adapter")
)

// Config controls construction of a Twitch [Adapter]. The zero value is a
// valid (anonymous) configuration that joins no channels — set at least
// Channels before calling [New].
type Config struct {
	// Channels lists the Twitch channel logins to JOIN once connected.
	Channels []string

	// Username is the IRC login used for authenticated mode. If empty, the
	// adapter runs anonymously as "justinfan" + 6 random digits.
	Username string

	// OAuthToken is the IRC password / Helix bearer token. The "oauth:"
	// prefix is optional for IRC. If empty, the adapter runs anonymously.
	OAuthToken string

	// ClientID is the Twitch developer application client id used for
	// Helix REST calls. Required whenever OAuthToken is set.
	ClientID string

	// Logger receives structured log output. nil falls back to slog.Default.
	Logger *slog.Logger

	// HelixBaseURL overrides the Helix API base URL — used by tests to
	// point the helix client at an httptest server. Empty means the real
	// Twitch endpoint.
	HelixBaseURL string

	// newIRCClient is an unexported test hook. Production callers leave it
	// nil and the adapter calls [irc.NewClient] / [irc.NewAnonymousClient].
	newIRCClient func(cfg Config) ircClient

	// newHelixClient is an unexported test hook for the same reason.
	newHelixClient func(cfg Config) (helixClient, error)
}

// ircClient is the minimal surface of [irc.Client] the adapter relies on.
// Production code wraps the real client; tests provide a fake.
type ircClient interface {
	OnConnect(func())
	OnPrivateMessage(func(irc.PrivateMessage))
	OnClearMessage(func(irc.ClearMessage))
	OnClearChatMessage(func(irc.ClearChatMessage))
	OnUserNoticeMessage(func(irc.UserNoticeMessage))
	OnReconnectMessage(func(irc.ReconnectMessage))
	Join(channels ...string)
	Say(channel, text string)
	Connect() error
	Disconnect() error
	SetIRCToken(token string)
}

// helixClient is the minimal surface of [helix.Client] the adapter relies
// on. Production code wraps the real client; tests provide a fake.
type helixClient interface {
	BanUser(*helix.BanUserParams) (*helix.BanUserResponse, error)
	UnbanUser(*helix.UnbanUserParams) (*helix.UnbanUserResponse, error)
	DeleteChatMessage(*helix.DeleteChatMessageParams) (*helix.DeleteChatMessageResponse, error)
	GetUsers(*helix.UsersParams) (*helix.UsersResponse, error)
	GetStreams(*helix.StreamsParams) (*helix.StreamsResponse, error)
	GetChannelFollows(*helix.GetChannelFollowsParams) (*helix.GetChannelFollowersResponse, error)
	CreateCustomReward(*helix.ChannelCustomRewardsParams) (*helix.ChannelCustomRewardResponse, error)
	GetCustomRewards(*helix.GetCustomRewardsParams) (*helix.ChannelCustomRewardResponse, error)
	DeleteCustomReward(*helix.DeleteCustomRewardsParams) (*helix.DeleteCustomRewardsResponse, error)
	UpdateChannelCustomRewardsRedemptionStatus(*helix.UpdateChannelCustomRewardsRedemptionStatusParams) (*helix.ChannelCustomRewardsRedemptionResponse, error)
	CreateEventSubSubscription(*helix.EventSubSubscription) (*helix.EventSubSubscriptionsResponse, error)
	SetUserAccessToken(token string)
}

// Adapter is the Twitch implementation of [adapters.Platform].
type Adapter struct {
	cfg    Config
	logger *slog.Logger
	anon   bool

	mu             sync.Mutex
	irc            ircClient
	helix          helixClient
	events         chan adapters.Event
	connected      bool
	healthErr      error
	channelToID    map[string]string
	moderatorID    string
	disconnectOnce sync.Once
	ircDone        chan struct{}
	cancelWatcher  context.CancelFunc

	// streamMu guards streamCache. It is dedicated (rather than reusing
	// a.mu) so a slow Helix GetStreams call never blocks the IRC / SetToken
	// paths that take a.mu.
	streamMu    sync.Mutex
	streamCache map[string]streamCacheEntry

	// rewardMu guards idCache, the login→broadcaster-id map used by the
	// Channel-Points reward methods. It is dedicated (not a.mu / streamMu)
	// so a slow Helix GetUsers lookup never blocks the IRC / SetToken /
	// StreamInfo paths. A login's broadcaster id is effectively immutable,
	// so entries are cached forever (never expired).
	rewardMu sync.Mutex
	idCache  map[string]string

	// nowFn is the clock seam for cache-expiry checks; defaults to
	// time.Now and is overridden by tests to drive TTL expiry.
	nowFn func() time.Time
}

type streamCacheEntry struct {
	info      StreamInfo
	fetchedAt time.Time
}

// streamCacheTTL bounds how long a [StreamInfo] result is reused so
// repeated !uptime calls cannot exhaust the Helix rate limit.
const streamCacheTTL = 60 * time.Second

// StreamInfo is a point-in-time view of a channel's live status, returned
// by [Adapter.StreamInfo].
type StreamInfo struct {
	Live        bool
	StartedAt   time.Time // zero when offline
	GameName    string    // current category; empty when offline/unknown
	Title       string    // stream title; empty when offline/unknown
	ViewerCount int       // current viewers; 0 when offline
}

// New constructs a Twitch adapter from cfg. The returned adapter is
// disconnected; call [Adapter.Connect] to establish the IRC connection.
func New(cfg Config) *Adapter {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "adapters.twitch")

	a := &Adapter{
		cfg:         cfg,
		logger:      logger,
		anon:        strings.TrimSpace(cfg.OAuthToken) == "",
		healthErr:   ErrNotConnected,
		channelToID: make(map[string]string),
		streamCache: make(map[string]streamCacheEntry),
		idCache:     make(map[string]string),
		nowFn:       time.Now,
	}
	return a
}

// Name returns the platform identifier "twitch".
func (a *Adapter) Name() string { return platformName }

// Connect builds the IRC (and, in authenticated mode, Helix) clients,
// registers handlers, opens the IRC connection in a background goroutine,
// joins the configured channels, and arranges for [Adapter.Disconnect] to
// be called when ctx is cancelled.
func (a *Adapter) Connect(ctx context.Context) error {
	a.mu.Lock()
	if a.connected {
		a.mu.Unlock()
		return ErrAlreadyConnected
	}
	a.mu.Unlock()

	ircFactory := a.cfg.newIRCClient
	if ircFactory == nil {
		ircFactory = defaultIRCFactory
	}
	client := ircFactory(a.cfg)

	var hx helixClient
	if !a.anon {
		hxFactory := a.cfg.newHelixClient
		if hxFactory == nil {
			hxFactory = defaultHelixFactory
		}
		built, err := hxFactory(a.cfg)
		if err != nil {
			return fmt.Errorf("twitch: build helix client: %w", err)
		}
		hx = built
	}

	a.mu.Lock()
	a.irc = client
	a.helix = hx
	a.events = make(chan adapters.Event, eventBuffer)
	a.connected = true
	a.healthErr = nil
	a.disconnectOnce = sync.Once{}
	a.ircDone = make(chan struct{})
	watcherCtx, cancel := context.WithCancel(context.Background())
	a.cancelWatcher = cancel
	a.registerHandlersLocked()
	a.mu.Unlock()

	if !a.anon {
		go a.resolveModeratorID()
	}

	go a.runIRC()
	go a.watchContext(ctx, watcherCtx)

	client.Join(a.cfg.Channels...)
	a.logger.Info("twitch adapter connected", "anonymous", a.anon, "channels", len(a.cfg.Channels))
	return nil
}

func (a *Adapter) registerHandlersLocked() {
	a.irc.OnConnect(a.onConnect)
	a.irc.OnPrivateMessage(a.onPrivateMessage)
	a.irc.OnClearMessage(a.onClearMessage)
	a.irc.OnClearChatMessage(a.onClearChatMessage)
	a.irc.OnUserNoticeMessage(a.onUserNotice)
	a.irc.OnReconnectMessage(a.onReconnect)
}

func (a *Adapter) runIRC() {
	a.mu.Lock()
	client := a.irc
	done := a.ircDone
	a.mu.Unlock()
	if client == nil || done == nil {
		return
	}
	err := client.Connect()
	if err != nil && !errors.Is(err, irc.ErrClientDisconnected) {
		a.mu.Lock()
		a.healthErr = err
		a.mu.Unlock()
		a.logger.Warn("twitch irc connect returned error", "err", err)
		a.emit(connectionEvent(adapters.EventDisconnected, "", "irc connect failed", err.Error()))
	}
	close(done)
}

func (a *Adapter) watchContext(userCtx, watcherCtx context.Context) {
	select {
	case <-userCtx.Done():
		_ = a.Disconnect(context.Background())
	case <-watcherCtx.Done():
	}
}

// Disconnect tears down the IRC connection and closes the events channel.
// It is idempotent.
func (a *Adapter) Disconnect(_ context.Context) error {
	var disconnectErr error
	a.disconnectOnce.Do(func() {
		a.mu.Lock()
		if !a.connected {
			a.mu.Unlock()
			return
		}
		client := a.irc
		events := a.events
		done := a.ircDone
		cancel := a.cancelWatcher

		a.connected = false
		a.irc = nil
		a.helix = nil
		a.events = nil
		a.ircDone = nil
		a.cancelWatcher = nil
		a.healthErr = ErrNotConnected
		a.mu.Unlock()

		if cancel != nil {
			cancel()
		}
		if client != nil {
			if err := client.Disconnect(); err != nil && !errors.Is(err, irc.ErrConnectionIsNotOpen) {
				disconnectErr = err
			}
		}
		if done != nil {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
		}
		if events != nil {
			close(events)
		}
		a.logger.Info("twitch adapter disconnected")
	})
	return disconnectErr
}

// Events returns the channel that delivers normalized events until
// disconnect.
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

// SetToken applies a freshly refreshed OAuth user-access token to the
// live adapter WITHOUT reconnecting. The IRC token is staged for the next
// (re)connect via SetIRCToken — the running IRC session is unaffected
// because Twitch only validates the token at the PASS/NICK handshake,
// never mid-session — and the Helix client's bearer is updated in place
// via SetUserAccessToken so the next REST call uses the new token
// immediately. cfg.OAuthToken is also updated so a future Connect uses
// the new token. This method is adapter-specific and intentionally not
// part of [adapters.Platform], because rotation semantics differ per
// platform.
//
// Returns [ErrEmptyToken] for empty input (no state mutated),
// [ErrNotConnected] if Connect has not run, and [ErrAnonymousRotation] on
// an anonymous adapter.
//
// Concurrency: a.mu is held only across field reads/writes, NOT across
// the library setter calls. This is safe because the helix client's
// SetUserAccessToken is itself mutex-protected and SetIRCToken merely
// stores a field consulted on (re)connect; holding a.mu across them
// would risk deadlock with the IRC reader / Do() paths.
func (a *Adapter) SetToken(accessToken string) error {
	if strings.TrimSpace(accessToken) == "" {
		return ErrEmptyToken
	}

	a.mu.Lock()
	if !a.connected {
		a.mu.Unlock()
		return ErrNotConnected
	}
	if a.anon {
		a.mu.Unlock()
		return ErrAnonymousRotation
	}
	ircc := a.irc
	hx := a.helix
	a.cfg.OAuthToken = accessToken
	a.mu.Unlock()

	raw := strings.TrimPrefix(accessToken, "oauth:")
	if ircc != nil {
		ircc.SetIRCToken("oauth:" + raw)
	}
	if hx != nil {
		hx.SetUserAccessToken(raw)
	}
	a.logger.Info("twitch token rotated", "anonymous", false)
	return nil
}

// Do dispatches a platform action via IRC (send) or the Helix REST API
// (moderation). It returns an error in anonymous mode for every action
// type.
func (a *Adapter) Do(ctx context.Context, action adapters.Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	if !a.connected {
		a.mu.Unlock()
		return ErrNotConnected
	}
	client := a.irc
	hx := a.helix
	a.mu.Unlock()

	switch action.Type {
	case adapters.ActionSendMessage:
		return a.doSendMessage(client, action)
	case adapters.ActionDeleteMessage:
		return a.doDeleteMessage(hx, action)
	case adapters.ActionBan:
		return a.doBan(hx, action)
	case adapters.ActionTimeout:
		return a.doTimeout(hx, action)
	case adapters.ActionUntimeout:
		return a.doUntimeout(hx, action)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAction, action.Type)
	}
}

func (a *Adapter) doSendMessage(client ircClient, act adapters.Action) error {
	if act.SendMessage == nil {
		return ErrMissingPayload
	}
	if a.anon {
		return ErrAnonymous
	}
	if client == nil {
		return ErrNotConnected
	}
	client.Say(act.Channel, act.SendMessage.Text)
	return nil
}

func (a *Adapter) doDeleteMessage(hx helixClient, act adapters.Action) error {
	if act.DeleteMessage == nil {
		return ErrMissingPayload
	}
	if a.anon {
		return ErrAnonymousAction
	}
	if hx == nil {
		return ErrHelixUnavailable
	}
	bid, err := a.broadcasterID(act.Channel)
	if err != nil {
		return err
	}
	modID := a.moderator()
	if modID == "" {
		modID = bid
	}
	resp, err := hx.DeleteChatMessage(&helix.DeleteChatMessageParams{
		BroadcasterID: bid,
		ModeratorID:   modID,
		MessageID:     act.DeleteMessage.MessageID,
	})
	if err != nil {
		return fmt.Errorf("twitch: delete chat message: %w", err)
	}
	return helixStatusError("delete chat message", resp.StatusCode, resp.ErrorMessage)
}

func (a *Adapter) doBan(hx helixClient, act adapters.Action) error {
	if act.Ban == nil {
		return ErrMissingPayload
	}
	if a.anon {
		return ErrAnonymousAction
	}
	if hx == nil {
		return ErrHelixUnavailable
	}
	bid, err := a.broadcasterID(act.Channel)
	if err != nil {
		return err
	}
	modID := a.moderator()
	if modID == "" {
		modID = bid
	}
	resp, err := hx.BanUser(&helix.BanUserParams{
		BroadcasterID: bid,
		ModeratorId:   modID,
		Body: helix.BanUserRequestBody{
			UserId: act.Ban.UserID,
			Reason: act.Ban.Reason,
		},
	})
	if err != nil {
		return fmt.Errorf("twitch: ban user: %w", err)
	}
	return helixStatusError("ban user", resp.StatusCode, resp.ErrorMessage)
}

func (a *Adapter) doTimeout(hx helixClient, act adapters.Action) error {
	if act.Timeout == nil {
		return ErrMissingPayload
	}
	if a.anon {
		return ErrAnonymousAction
	}
	if hx == nil {
		return ErrHelixUnavailable
	}
	bid, err := a.broadcasterID(act.Channel)
	if err != nil {
		return err
	}
	modID := a.moderator()
	if modID == "" {
		modID = bid
	}
	resp, err := hx.BanUser(&helix.BanUserParams{
		BroadcasterID: bid,
		ModeratorId:   modID,
		Body: helix.BanUserRequestBody{
			UserId:   act.Timeout.UserID,
			Reason:   act.Timeout.Reason,
			Duration: int(act.Timeout.Duration / time.Second),
		},
	})
	if err != nil {
		return fmt.Errorf("twitch: timeout user: %w", err)
	}
	return helixStatusError("timeout user", resp.StatusCode, resp.ErrorMessage)
}

func (a *Adapter) doUntimeout(hx helixClient, act adapters.Action) error {
	if act.Untimeout == nil {
		return ErrMissingPayload
	}
	if a.anon {
		return ErrAnonymousAction
	}
	if hx == nil {
		return ErrHelixUnavailable
	}
	bid, err := a.broadcasterID(act.Channel)
	if err != nil {
		return err
	}
	modID := a.moderator()
	if modID == "" {
		modID = bid
	}
	resp, err := hx.UnbanUser(&helix.UnbanUserParams{
		BroadcasterID: bid,
		ModeratorID:   modID,
		UserID:        act.Untimeout.UserID,
	})
	if err != nil {
		return fmt.Errorf("twitch: untimeout user: %w", err)
	}
	return helixStatusError("untimeout user", resp.StatusCode, resp.ErrorMessage)
}

// helixStatusError converts a non-2xx Helix response into a Go error. A 2xx
// status returns nil. status==0 is treated as success because some test
// fakes don't fill the field.
func helixStatusError(op string, status int, message string) error {
	if status == 0 || (status >= 200 && status < 300) {
		return nil
	}
	if message == "" {
		return fmt.Errorf("twitch: %s: helix returned status %d", op, status)
	}
	return fmt.Errorf("twitch: %s: helix status %d: %s", op, status, message)
}

func (a *Adapter) broadcasterID(channel string) (string, error) {
	channel = strings.ToLower(channel)
	a.mu.Lock()
	id, ok := a.channelToID[channel]
	hx := a.helix
	a.mu.Unlock()
	if ok && id != "" {
		return id, nil
	}
	if hx == nil {
		return "", fmt.Errorf("%w: channel=%s", ErrUnknownChannel, channel)
	}
	resp, err := hx.GetUsers(&helix.UsersParams{Logins: []string{channel}})
	if err != nil {
		return "", fmt.Errorf("twitch: lookup broadcaster id for %q: %w", channel, err)
	}
	if resp == nil || len(resp.Data.Users) == 0 || resp.Data.Users[0].ID == "" {
		return "", fmt.Errorf("%w: channel=%s", ErrUnknownChannel, channel)
	}
	id = resp.Data.Users[0].ID
	a.mu.Lock()
	a.channelToID[channel] = id
	a.mu.Unlock()
	return id, nil
}

func (a *Adapter) moderator() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.moderatorID
}

// StreamInfo reports whether the given channel (Twitch login, without '#')
// is live and, if so, when the stream started. Results are cached for a
// short TTL so repeated !uptime calls cannot exhaust the Helix rate limit.
// Returns [ErrHelixUnavailable] when the adapter has no Helix client
// (anonymous mode).
//
// Locking: streamMu is held only around cache reads/writes, never across
// the Helix network call (read under lock → unlock → call Helix → re-lock
// → store). A duplicate concurrent fetch is tolerated rather than
// serialised. Successful results are cached; on a Helix error nothing is
// cached so the next call retries.
func (a *Adapter) StreamInfo(_ context.Context, login string) (StreamInfo, error) {
	login = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(login)), "#")

	a.mu.Lock()
	hx := a.helix
	a.mu.Unlock()
	if hx == nil {
		return StreamInfo{}, ErrHelixUnavailable
	}

	a.streamMu.Lock()
	if e, ok := a.streamCache[login]; ok && a.nowFn().Sub(e.fetchedAt) < streamCacheTTL {
		a.streamMu.Unlock()
		return e.info, nil
	}
	a.streamMu.Unlock()

	resp, err := hx.GetStreams(&helix.StreamsParams{UserLogins: []string{login}})
	if err != nil {
		return StreamInfo{}, fmt.Errorf("twitch: get streams for %q: %w", login, err)
	}

	var info StreamInfo
	if resp != nil {
		for _, s := range resp.Data.Streams {
			if s.Type == "live" || !s.StartedAt.IsZero() {
				info = StreamInfo{
					Live:        true,
					StartedAt:   s.StartedAt,
					GameName:    s.GameName,
					Title:       s.Title,
					ViewerCount: s.ViewerCount,
				}
				break
			}
		}
	}

	a.streamMu.Lock()
	a.streamCache[login] = streamCacheEntry{info: info, fetchedAt: a.nowFn()}
	a.streamMu.Unlock()
	return info, nil
}

func (a *Adapter) resolveModeratorID() {
	a.mu.Lock()
	hx := a.helix
	a.mu.Unlock()
	if hx == nil {
		return
	}
	resp, err := hx.GetUsers(&helix.UsersParams{})
	if err != nil {
		a.logger.Warn("twitch: resolve moderator id", "err", err)
		return
	}
	if resp == nil || len(resp.Data.Users) == 0 {
		return
	}
	a.mu.Lock()
	a.moderatorID = resp.Data.Users[0].ID
	a.mu.Unlock()
}

func (a *Adapter) onConnect() {
	a.mu.Lock()
	a.healthErr = nil
	a.mu.Unlock()
	a.emit(connectionEvent(adapters.EventConnected, "", "twitch irc connected", ""))
}

func (a *Adapter) onPrivateMessage(m irc.PrivateMessage) {
	if m.Channel != "" {
		a.mu.Lock()
		if id, ok := a.channelToID[strings.ToLower(m.Channel)]; !ok || id == "" {
			if m.RoomID != "" {
				a.channelToID[strings.ToLower(m.Channel)] = m.RoomID
			}
		}
		a.mu.Unlock()
	}
	a.emit(translatePrivateMessage(m))
}

func (a *Adapter) onClearMessage(m irc.ClearMessage) {
	a.emit(translateClearMessage(m))
}

func (a *Adapter) onClearChatMessage(m irc.ClearChatMessage) {
	evt := translateClearChat(m)
	if evt.Type == "" {
		return
	}
	a.emit(evt)
}

func (a *Adapter) onUserNotice(m irc.UserNoticeMessage) {
	evt := translateUserNotice(m)
	if evt.Type == "" {
		return
	}
	a.emit(evt)
}

func (a *Adapter) onReconnect(_ irc.ReconnectMessage) {
	a.mu.Lock()
	a.healthErr = errors.New("twitch: server requested reconnect")
	a.mu.Unlock()
	a.emit(connectionEvent(adapters.EventReconnecting, "", "twitch server requested reconnect", ""))
}

// emit pushes an event onto the buffered channel. If the channel is full
// (consumer is too slow) the event is dropped and a warning is logged so
// the IRC reader goroutine never blocks.
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
		a.logger.Warn("twitch events channel full; dropping event", "type", e.Type)
	}
}

// defaultIRCFactory builds the production [irc.Client] wrapper from cfg.
// Anonymous mode uses a fresh "justinfanNNNNNN" login per call so multiple
// adapters in the same process don't collide; the password is the
// well-known throwaway value Twitch documents for guest logins.
func defaultIRCFactory(cfg Config) ircClient {
	if strings.TrimSpace(cfg.OAuthToken) == "" {
		username := cfg.Username
		if username == "" {
			username = anonymousUsername()
		}
		return irc.NewClient(username, "oauth:anonymous")
	}
	token := cfg.OAuthToken
	if !strings.HasPrefix(token, "oauth:") {
		token = "oauth:" + token
	}
	return irc.NewClient(cfg.Username, token)
}

// defaultHelixFactory builds the production [helix.Client] from cfg.
func defaultHelixFactory(cfg Config) (helixClient, error) {
	opts := &helix.Options{
		ClientID:        cfg.ClientID,
		UserAccessToken: strings.TrimPrefix(cfg.OAuthToken, "oauth:"),
		APIBaseURL:      cfg.HelixBaseURL,
	}
	c, err := helix.NewClient(opts)
	if err != nil {
		return nil, err
	}
	return &helixWrapper{c: c}, nil
}

// helixWrapper adapts *helix.Client to our local [helixClient] interface.
type helixWrapper struct{ c *helix.Client }

func (w *helixWrapper) BanUser(p *helix.BanUserParams) (*helix.BanUserResponse, error) {
	return w.c.BanUser(p)
}
func (w *helixWrapper) UnbanUser(p *helix.UnbanUserParams) (*helix.UnbanUserResponse, error) {
	return w.c.UnbanUser(p)
}
func (w *helixWrapper) DeleteChatMessage(p *helix.DeleteChatMessageParams) (*helix.DeleteChatMessageResponse, error) {
	return w.c.DeleteChatMessage(p)
}
func (w *helixWrapper) GetUsers(p *helix.UsersParams) (*helix.UsersResponse, error) {
	return w.c.GetUsers(p)
}
func (w *helixWrapper) GetStreams(p *helix.StreamsParams) (*helix.StreamsResponse, error) {
	return w.c.GetStreams(p)
}
func (w *helixWrapper) GetChannelFollows(p *helix.GetChannelFollowsParams) (*helix.GetChannelFollowersResponse, error) {
	return w.c.GetChannelFollows(p)
}
func (w *helixWrapper) CreateCustomReward(p *helix.ChannelCustomRewardsParams) (*helix.ChannelCustomRewardResponse, error) {
	return w.c.CreateCustomReward(p)
}
func (w *helixWrapper) GetCustomRewards(p *helix.GetCustomRewardsParams) (*helix.ChannelCustomRewardResponse, error) {
	return w.c.GetCustomRewards(p)
}
func (w *helixWrapper) DeleteCustomReward(p *helix.DeleteCustomRewardsParams) (*helix.DeleteCustomRewardsResponse, error) {
	return w.c.DeleteCustomRewards(p)
}
func (w *helixWrapper) UpdateChannelCustomRewardsRedemptionStatus(p *helix.UpdateChannelCustomRewardsRedemptionStatusParams) (*helix.ChannelCustomRewardsRedemptionResponse, error) {
	return w.c.UpdateChannelCustomRewardsRedemptionStatus(p)
}
func (w *helixWrapper) CreateEventSubSubscription(p *helix.EventSubSubscription) (*helix.EventSubSubscriptionsResponse, error) {
	return w.c.CreateEventSubSubscription(p)
}
func (w *helixWrapper) SetUserAccessToken(token string) { w.c.SetUserAccessToken(token) }

// anonymousUsername returns a fresh Twitch anonymous login of the form
// justinfanNNNNNN where N is a random digit. crypto/rand is used so two
// adapters started in the same process don't collide.
func anonymousUsername() string {
	var sb strings.Builder
	sb.WriteString("justinfan")
	for range 6 {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			sb.WriteString("0")
			continue
		}
		sb.WriteString(n.String())
	}
	return sb.String()
}

var _ adapters.Platform = (*Adapter)(nil)
