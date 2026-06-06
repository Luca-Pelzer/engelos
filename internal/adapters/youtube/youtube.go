package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

const (
	eventBuffer = 256

	apiBaseURL = "https://www.googleapis.com/youtube/v3"

	defaultDailyQuota = 10000
	maxResults        = 2000

	// defaultMinPollInterval floors the server-suggested polling interval so
	// a zero or tiny pollingIntervalMillis cannot turn the read loop into a
	// hot spin.
	defaultMinPollInterval = 1000 * time.Millisecond

	costList       = 5
	costInsert     = 50
	costDelete     = 50
	costBan        = 50
	costVideosList = 1
)

// Errors returned by the YouTube adapter.
var (
	ErrNotConnected         = errors.New("youtube: not connected")
	ErrAlreadyConnected     = errors.New("youtube: already connected")
	ErrTokenRequired        = errors.New("youtube: access token or token source required")
	ErrUnknownAction        = errors.New("youtube: unknown action type")
	ErrMissingPayload       = errors.New("youtube: action payload missing")
	ErrNoLiveChat           = errors.New("youtube: no live chat id (set LiveChatID or a resolvable VideoID)")
	ErrQuotaExhausted       = errors.New("youtube: daily quota budget exhausted")
	ErrUntimeoutUnsupported = errors.New("youtube: untimeout requires the ban id, which this adapter does not retain")
)

// httpDoer is the minimal HTTP surface the adapter relies on. Production code
// uses an *http.Client; tests inject a fake that returns canned responses.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config controls construction of a YouTube [Adapter].
type Config struct {
	// AccessToken is a static OAuth2 access token used as the Bearer
	// credential. It is ignored when TokenSource is set. If both are empty
	// [Adapter.Connect] fails with [ErrTokenRequired].
	AccessToken string

	// TokenSource, when set, supplies (and refreshes) the OAuth2 access
	// token before each API call, taking precedence over AccessToken.
	TokenSource oauth2.TokenSource

	// LiveChatID is the live chat to poll. If empty the adapter resolves it
	// from VideoID at Connect.
	LiveChatID string

	// VideoID is resolved to a live chat id at Connect when LiveChatID is
	// empty. If both are empty Connect fails with [ErrNoLiveChat].
	VideoID string

	// Channel is the engelOS channel label copied into Event.Channel and
	// used as Action.Channel by the rest of the system.
	Channel string

	// DailyQuotaUnits caps how many quota units the adapter spends per
	// Pacific-time day before it stops polling. Zero means
	// defaultDailyQuota (10000).
	DailyQuotaUnits int

	// Logger receives structured log output. nil falls back to slog.Default.
	Logger *slog.Logger

	// HTTPClient overrides the HTTP transport. nil uses an *http.Client with
	// a sane timeout. Tests inject a fake httpDoer here.
	HTTPClient httpDoer

	// BaseURL overrides the REST base URL. Empty uses the real endpoint.
	BaseURL string

	// nowFn is the clock seam for the Pacific-day quota reset; defaults to
	// time.Now and is overridden by tests to drive the reset.
	nowFn func() time.Time

	// minPollInterval is an unexported test hook that lowers the polling
	// floor so the loop can be driven without real one-second waits. Zero
	// uses defaultMinPollInterval.
	minPollInterval time.Duration
}

// Adapter is the YouTube implementation of [adapters.Platform].
type Adapter struct {
	cfg         Config
	logger      *slog.Logger
	client      httpDoer
	baseURL     string
	nowFn       func() time.Time
	minInterval time.Duration

	mu            sync.Mutex
	events        chan adapters.Event
	connected     bool
	healthErr     error
	liveChatID    string
	cancelPoll    context.CancelFunc
	pollDone      chan struct{}
	quotaUsed     int
	quotaDayStart time.Time
}

// New constructs a YouTube adapter from cfg. The returned adapter is
// disconnected; call [Adapter.Connect] to resolve the live chat and start
// polling.
func New(cfg Config) *Adapter {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "adapters.youtube")

	nowFn := cfg.nowFn
	if nowFn == nil {
		nowFn = time.Now
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = apiBaseURL
	}

	minInterval := cfg.minPollInterval
	if minInterval <= 0 {
		minInterval = defaultMinPollInterval
	}

	return &Adapter{
		cfg:         cfg,
		logger:      logger,
		client:      client,
		baseURL:     strings.TrimRight(baseURL, "/"),
		nowFn:       nowFn,
		minInterval: minInterval,
		healthErr:   ErrNotConnected,
	}
}

// Name returns the platform identifier "youtube".
func (a *Adapter) Name() string { return platformName }

// QuotaUsed returns the number of quota units consumed in the current
// Pacific-time day budget window.
func (a *Adapter) QuotaUsed() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.quotaUsed
}

// Connect validates credentials, resolves the live chat id (from VideoID
// when LiveChatID is not given), and starts the poll loop in a background
// goroutine. The loop stops on Disconnect or ctx cancellation.
func (a *Adapter) Connect(ctx context.Context) error {
	if a.cfg.TokenSource == nil && strings.TrimSpace(a.cfg.AccessToken) == "" {
		return ErrTokenRequired
	}

	a.mu.Lock()
	if a.connected {
		a.mu.Unlock()
		return ErrAlreadyConnected
	}
	a.mu.Unlock()

	liveChatID := strings.TrimSpace(a.cfg.LiveChatID)
	if liveChatID == "" {
		resolved, err := a.resolveLiveChatID(ctx, strings.TrimSpace(a.cfg.VideoID))
		if err != nil {
			return err
		}
		liveChatID = resolved
	}
	if liveChatID == "" {
		return ErrNoLiveChat
	}

	pollCtx, cancel := context.WithCancel(context.Background())

	a.mu.Lock()
	a.events = make(chan adapters.Event, eventBuffer)
	a.connected = true
	a.healthErr = nil
	a.liveChatID = liveChatID
	a.cancelPoll = cancel
	a.pollDone = make(chan struct{})
	a.resetQuotaIfNewDayLocked()
	done := a.pollDone
	a.mu.Unlock()

	go a.runPoll(pollCtx, ctx, done)
	go a.watchContext(ctx, pollCtx)
	a.emit(connectionEvent(adapters.EventConnected, a.cfg.Channel, "youtube live chat connected", ""))
	a.logger.Info("youtube adapter connected", "channel", a.cfg.Channel)
	return nil
}

// watchContext closes the adapter when the caller's context is cancelled. It
// exits without side effects when the internal poll context is cancelled
// first (i.e. Disconnect ran), so it never leaks past disconnect.
func (a *Adapter) watchContext(userCtx, pollCtx context.Context) {
	select {
	case <-userCtx.Done():
		_ = a.Disconnect(context.Background())
	case <-pollCtx.Done():
	}
}

// Disconnect stops the poll loop and closes the events channel. It is
// idempotent and safe to call even if Connect never ran.
func (a *Adapter) Disconnect(_ context.Context) error {
	a.mu.Lock()
	if !a.connected {
		a.mu.Unlock()
		return nil
	}
	events := a.events
	cancel := a.cancelPoll
	done := a.pollDone

	a.connected = false
	a.events = nil
	a.cancelPoll = nil
	a.pollDone = nil
	a.healthErr = ErrNotConnected
	a.mu.Unlock()

	if cancel != nil {
		cancel()
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
	a.logger.Info("youtube adapter disconnected")
	return nil
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

// Do dispatches a platform action against the YouTube REST API.
func (a *Adapter) Do(ctx context.Context, action adapters.Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	if !a.connected {
		a.mu.Unlock()
		return ErrNotConnected
	}
	liveChatID := a.liveChatID
	a.mu.Unlock()

	switch action.Type {
	case adapters.ActionSendMessage:
		return a.doSendMessage(ctx, liveChatID, action)
	case adapters.ActionDeleteMessage:
		return a.doDeleteMessage(ctx, action)
	case adapters.ActionBan:
		return a.doBan(ctx, liveChatID, action)
	case adapters.ActionTimeout:
		return a.doTimeout(ctx, liveChatID, action)
	case adapters.ActionUntimeout:
		return ErrUntimeoutUnsupported
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAction, action.Type)
	}
}

func (a *Adapter) doSendMessage(ctx context.Context, liveChatID string, act adapters.Action) error {
	if act.SendMessage == nil {
		return ErrMissingPayload
	}
	body := insertMessageRequest{}
	body.Snippet.LiveChatID = liveChatID
	body.Snippet.Type = "textMessageEvent"
	body.Snippet.TextMessageDetails.MessageText = act.SendMessage.Text

	a.chargeQuota(costInsert)
	return a.doJSON(ctx, http.MethodPost, "/liveChat/messages",
		url.Values{"part": {"snippet"}}, body, nil)
}

func (a *Adapter) doDeleteMessage(ctx context.Context, act adapters.Action) error {
	if act.DeleteMessage == nil {
		return ErrMissingPayload
	}
	a.chargeQuota(costDelete)
	return a.doJSON(ctx, http.MethodDelete, "/liveChat/messages",
		url.Values{"id": {act.DeleteMessage.MessageID}}, nil, nil)
}

func (a *Adapter) doBan(ctx context.Context, liveChatID string, act adapters.Action) error {
	if act.Ban == nil {
		return ErrMissingPayload
	}
	body := insertBanRequest{}
	body.Snippet.LiveChatID = liveChatID
	body.Snippet.Type = "permanent"
	body.Snippet.BannedUserDetails.ChannelID = act.Ban.UserID

	a.chargeQuota(costBan)
	return a.doJSON(ctx, http.MethodPost, "/liveChat/bans",
		url.Values{"part": {"snippet"}}, body, nil)
}

func (a *Adapter) doTimeout(ctx context.Context, liveChatID string, act adapters.Action) error {
	if act.Timeout == nil {
		return ErrMissingPayload
	}
	body := insertBanRequest{}
	body.Snippet.LiveChatID = liveChatID
	body.Snippet.Type = "temporary"
	body.Snippet.BanDurationSeconds = uint64(act.Timeout.Duration / time.Second)
	body.Snippet.BannedUserDetails.ChannelID = act.Timeout.UserID

	a.chargeQuota(costBan)
	return a.doJSON(ctx, http.MethodPost, "/liveChat/bans",
		url.Values{"part": {"snippet"}}, body, nil)
}

// resolveLiveChatID looks up the active live chat id for a video. It charges
// the videos.list cost (1 unit) against the quota budget.
func (a *Adapter) resolveLiveChatID(ctx context.Context, videoID string) (string, error) {
	if videoID == "" {
		return "", ErrNoLiveChat
	}
	a.chargeQuota(costVideosList)
	var resp videoListResponse
	q := url.Values{
		"part": {"liveStreamingDetails"},
		"id":   {videoID},
	}
	if err := a.doJSON(ctx, http.MethodGet, "/videos", q, nil, &resp); err != nil {
		return "", fmt.Errorf("youtube: resolve live chat id: %w", err)
	}
	if len(resp.Items) == 0 || resp.Items[0].LiveStreamingDetails.ActiveLiveChatID == "" {
		return "", ErrNoLiveChat
	}
	return resp.Items[0].LiveStreamingDetails.ActiveLiveChatID, nil
}

// runPoll is the read-path goroutine. It repeatedly fetches the live chat
// messages page, emitting normalized events, and sleeps for the
// server-suggested interval between polls. It stops on context cancellation,
// a chatEnded event, or when the quota budget would be exceeded.
func (a *Adapter) runPoll(pollCtx, userCtx context.Context, done chan struct{}) {
	defer close(done)

	var pageToken string
	for {
		if a.quotaWouldExceed(costList) {
			a.mu.Lock()
			a.healthErr = ErrQuotaExhausted
			a.mu.Unlock()
			a.emit(connectionEvent(adapters.EventDisconnected, a.cfg.Channel,
				"quota budget exhausted", ErrQuotaExhausted.Error()))
			a.logger.Warn("youtube poll stopped: quota budget exhausted", "used", a.QuotaUsed())
			return
		}

		a.chargeQuota(costList)
		page, err := a.fetchPage(pollCtx, pageToken)
		if err != nil {
			if pollCtx.Err() != nil || userCtx.Err() != nil {
				return
			}
			a.mu.Lock()
			a.healthErr = err
			a.mu.Unlock()
			a.logger.Warn("youtube poll fetch failed", "err", err)
			if !sleepCtx(pollCtx, userCtx, a.minInterval) {
				return
			}
			continue
		}

		a.mu.Lock()
		a.healthErr = nil
		a.mu.Unlock()

		stop := false
		for i := range page.Items {
			evt, action := translateMessage(page.Items[i], a.cfg.Channel)
			switch action {
			case translateEmit:
				a.emit(evt)
			case translateChatEnded:
				a.emit(connectionEvent(adapters.EventDisconnected, a.cfg.Channel,
					"live chat ended", ""))
				stop = true
			case translateSkip:
			}
			if stop {
				break
			}
		}
		if stop {
			return
		}

		pageToken = page.NextPageToken
		interval := max(time.Duration(page.PollingIntervalMillis)*time.Millisecond, a.minInterval)
		if !sleepCtx(pollCtx, userCtx, interval) {
			return
		}
	}
}

func (a *Adapter) fetchPage(ctx context.Context, pageToken string) (liveChatListResponse, error) {
	a.mu.Lock()
	liveChatID := a.liveChatID
	a.mu.Unlock()

	q := url.Values{
		"liveChatId": {liveChatID},
		"part":       {"id,snippet,authorDetails"},
		"maxResults": {strconv.Itoa(maxResults)},
	}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	var resp liveChatListResponse
	if err := a.doJSON(ctx, http.MethodGet, "/liveChat/messages", q, nil, &resp); err != nil {
		return liveChatListResponse{}, err
	}
	return resp, nil
}

// sleepCtx waits for d or until either context is cancelled. It returns false
// if a context fired (signalling the caller to stop), true on a clean wake.
func sleepCtx(pollCtx, userCtx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-pollCtx.Done():
		return false
	case <-userCtx.Done():
		return false
	}
}

// chargeQuota records consumed units, rolling the budget window first when the
// Pacific day has advanced.
func (a *Adapter) chargeQuota(units int) {
	a.mu.Lock()
	a.resetQuotaIfNewDayLocked()
	a.quotaUsed += units
	a.mu.Unlock()
}

// quotaWouldExceed reports whether spending units now would push past the
// configured daily budget. It rolls the window first so a fresh Pacific day
// re-enables polling.
func (a *Adapter) quotaWouldExceed(units int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resetQuotaIfNewDayLocked()
	return a.quotaUsed+units > a.dailyQuotaLocked()
}

func (a *Adapter) dailyQuotaLocked() int {
	if a.cfg.DailyQuotaUnits > 0 {
		return a.cfg.DailyQuotaUnits
	}
	return defaultDailyQuota
}

// resetQuotaIfNewDayLocked zeroes the consumed counter when the current
// America/Los_Angeles calendar day differs from the window start. Google's
// quota resets at Pacific midnight, so the window is tracked in that zone.
// Caller holds a.mu.
func (a *Adapter) resetQuotaIfNewDayLocked() {
	loc := pacificLocation()
	today := a.nowFn().In(loc)
	if a.quotaDayStart.IsZero() {
		a.quotaDayStart = today
		return
	}
	if !sameDay(a.quotaDayStart, today) {
		a.quotaUsed = 0
		a.quotaDayStart = today
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// pacificLocation returns America/Los_Angeles, falling back to a fixed -08:00
// zone if the tzdata is unavailable so the reset logic still works.
func pacificLocation() *time.Location {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return time.FixedZone("PST", -8*3600)
	}
	return loc
}

// doJSON performs a single REST call: it attaches the Bearer token, encodes
// body as JSON when non-nil, and decodes the response into out when non-nil. A
// non-2xx status is returned as an error carrying the response body.
func (a *Adapter) doJSON(ctx context.Context, method, path string, query url.Values, body, out any) error {
	token, err := a.token()
	if err != nil {
		return err
	}

	endpoint := a.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("youtube: encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("youtube: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("youtube: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("youtube: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("youtube: %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("youtube: decode response: %w", err)
		}
	}
	return nil
}

// token returns the Bearer credential, preferring TokenSource (which refreshes)
// over the static AccessToken.
func (a *Adapter) token() (string, error) {
	if a.cfg.TokenSource != nil {
		tok, err := a.cfg.TokenSource.Token()
		if err != nil {
			return "", fmt.Errorf("youtube: fetch token: %w", err)
		}
		if tok == nil || tok.AccessToken == "" {
			return "", ErrTokenRequired
		}
		return tok.AccessToken, nil
	}
	if strings.TrimSpace(a.cfg.AccessToken) == "" {
		return "", ErrTokenRequired
	}
	return a.cfg.AccessToken, nil
}

// emit pushes an event onto the buffered channel. If the channel is full
// (consumer is too slow) the event is dropped and a warning is logged so the
// poll goroutine never blocks.
func (a *Adapter) emit(e adapters.Event) {
	if e.Type == "" {
		return
	}
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
		a.logger.Warn("youtube events channel full; dropping event", "type", e.Type)
	}
}

var _ adapters.Platform = (*Adapter)(nil)
