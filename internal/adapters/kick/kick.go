package kick

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

const (
	platformName = "kick"

	eventBuffer = 256

	apiBaseURL = "https://api.kick.com"

	maxContentRunes = 500
	maxReasonChars  = 100

	minTimeoutMinutes = 1
	maxTimeoutMinutes = 10080

	replayWindow   = 5 * time.Minute
	dedupCacheSize = 4096

	defaultReadyDelay = 5 * time.Second

	headerEventType = "Kick-Event-Type"
	headerMessageID = "Kick-Event-Message-Id"
	headerSignature = "Kick-Event-Signature"
	headerTimestamp = "Kick-Event-Message-Timestamp"
)

const defaultPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAq/+l1WnlRrGSolDMA+A8
6rAhMbQGmQ2SapVcGM3zq8ANXjnhDWocMqfWcTd95btDydITa10kDvHzw9WQOqp2
MZI7ZyrfzJuz5nhTPCiJwTwnEtWft7nV14BYRDHvlfqPUaZ+1KR4OCaO/wWIk/rQ
L/TjY0M70gse8rlBkbo2a8rKhu69RQTRsoaf4DVhDPEeSeI5jVrRDGAMGL3cGuyY
6CLKGdjVEM78g3JfYOvDU/RvfqD7L89TZ3iN94jrmWdGz34JNlEI5hqK8dd7C5EF
BEbZ5jgB8s8ReQV8H+MkuffjdAj3ajDDX3DOJMIut1lBrUVD1AaSrGCKHooWoL2e
twIDAQAB
-----END PUBLIC KEY-----`

var subscribedEvents = []subscriptionEventRef{
	{Name: "chat.message.sent", Version: 1},
	{Name: "channel.subscription.new", Version: 1},
	{Name: "channel.subscription.renewal", Version: 1},
	{Name: "channel.subscription.gifts", Version: 1},
	{Name: "moderation.banned", Version: 1},
}

var (
	ErrNotConnected     = errors.New("kick: not connected")
	ErrAlreadyConnected = errors.New("kick: already connected")
	ErrTokenRequired    = errors.New("kick: access token or token source required")
	ErrUnknownAction    = errors.New("kick: unknown action type")
	ErrMissingPayload   = errors.New("kick: action payload missing")
	ErrRateLimited      = errors.New("kick: rate limited")
	ErrSignatureInvalid = errors.New("kick: webhook signature invalid")
	ErrReplayRejected   = errors.New("kick: webhook replay rejected")
	ErrContentTooLong   = errors.New("kick: message exceeds 500 code points")
	ErrPublicKeyInvalid = errors.New("kick: public key is not a valid RSA PEM")
)

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config controls construction of a Kick [Adapter].
type Config struct {
	ClientID     string
	ClientSecret string

	AppAccessToken string
	AppTokenSource oauth2.TokenSource

	UserAccessToken string
	UserTokenSource oauth2.TokenSource

	BroadcasterUserID int

	Channel string

	WebhookURL string

	PublicKeyPEM string

	ReadyCh <-chan struct{}

	Logger     *slog.Logger
	HTTPClient httpDoer
	BaseURL    string

	nowFn func() time.Time

	readyDelay time.Duration
}

// Adapter is the Kick implementation of [adapters.Platform].
type Adapter struct {
	cfg     Config
	logger  *slog.Logger
	client  httpDoer
	baseURL string
	nowFn   func() time.Time
	pubKey  *rsa.PublicKey
	seen    *lru.Cache[string, struct{}]

	mu         sync.Mutex
	events     chan adapters.Event
	connected  bool
	healthErr  error
	cancelSubs context.CancelFunc
	subsDone   chan struct{}
	readyDelay time.Duration
}

// New constructs a Kick adapter from cfg. The returned adapter is
// disconnected; call [Adapter.Connect] to register subscriptions and prepare
// the events channel, and mount [Adapter.WebhookHandler] on the API router.
func New(cfg Config) (*Adapter, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "adapters.kick")

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

	keyPEM := cfg.PublicKeyPEM
	if strings.TrimSpace(keyPEM) == "" {
		keyPEM = defaultPublicKeyPEM
	}
	pubKey, err := parseRSAPublicKey(keyPEM)
	if err != nil {
		return nil, err
	}

	cache, err := lru.New[string, struct{}](dedupCacheSize)
	if err != nil {
		return nil, fmt.Errorf("kick: build dedup cache: %w", err)
	}

	readyDelay := cfg.readyDelay
	if readyDelay <= 0 {
		readyDelay = defaultReadyDelay
	}

	return &Adapter{
		cfg:        cfg,
		logger:     logger,
		client:     client,
		baseURL:    strings.TrimRight(baseURL, "/"),
		nowFn:      nowFn,
		pubKey:     pubKey,
		seen:       cache,
		healthErr:  ErrNotConnected,
		readyDelay: readyDelay,
	}, nil
}

// Name returns the platform identifier "kick".
func (a *Adapter) Name() string { return platformName }

// Connect prepares the events channel and launches the subscription goroutine.
// It does not dial: Kick delivers events to [Adapter.WebhookHandler]. The
// goroutine waits for the HTTP server to be ready (Config.ReadyCh, or a short
// fallback delay) before registering webhook subscriptions, so Kick never
// POSTs to an endpoint that is not yet listening.
func (a *Adapter) Connect(_ context.Context) error {
	if _, err := a.userToken(); err != nil {
		return err
	}

	a.mu.Lock()
	if a.connected {
		a.mu.Unlock()
		return ErrAlreadyConnected
	}
	subCtx, cancel := context.WithCancel(context.Background())
	a.events = make(chan adapters.Event, eventBuffer)
	a.connected = true
	a.healthErr = nil
	a.cancelSubs = cancel
	a.subsDone = make(chan struct{})
	done := a.subsDone
	ready := a.cfg.ReadyCh
	delay := a.readyDelay
	a.mu.Unlock()

	go a.registerWhenReady(subCtx, ready, delay, done)

	a.emit(connectionEvent(adapters.EventConnected, a.cfg.Channel, "kick webhook adapter ready", ""))
	a.logger.Info("kick adapter connected", "channel", a.cfg.Channel)
	return nil
}

// Disconnect stops the subscription goroutine and closes the events channel.
// It is idempotent and safe to call even if Connect never ran.
func (a *Adapter) Disconnect(_ context.Context) error {
	a.mu.Lock()
	if !a.connected {
		a.mu.Unlock()
		return nil
	}
	events := a.events
	cancel := a.cancelSubs
	done := a.subsDone
	a.connected = false
	a.events = nil
	a.cancelSubs = nil
	a.subsDone = nil
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
	a.logger.Info("kick adapter disconnected")
	return nil
}

// Events returns the channel that delivers normalized events until disconnect.
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

// Do dispatches a platform action against the Kick REST API.
func (a *Adapter) Do(ctx context.Context, action adapters.Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	connected := a.connected
	a.mu.Unlock()
	if !connected {
		return ErrNotConnected
	}

	switch action.Type {
	case adapters.ActionSendMessage:
		return a.doSendMessage(ctx, action)
	case adapters.ActionDeleteMessage:
		return a.doDeleteMessage(ctx, action)
	case adapters.ActionBan:
		return a.doBan(ctx, action)
	case adapters.ActionTimeout:
		return a.doTimeout(ctx, action)
	case adapters.ActionUntimeout:
		return a.doUntimeout(ctx, action)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAction, action.Type)
	}
}

func (a *Adapter) doSendMessage(ctx context.Context, act adapters.Action) error {
	if act.SendMessage == nil {
		return ErrMissingPayload
	}
	if utf8.RuneCountInString(act.SendMessage.Text) > maxContentRunes {
		return ErrContentTooLong
	}
	body := sendMessageRequest{
		BroadcasterUserID: a.cfg.BroadcasterUserID,
		Content:           act.SendMessage.Text,
		Type:              "bot",
		ReplyToMessageID:  act.SendMessage.ReplyTo,
	}
	return a.doJSON(ctx, http.MethodPost, "/public/v1/chat", body, nil)
}

func (a *Adapter) doDeleteMessage(ctx context.Context, act adapters.Action) error {
	if act.DeleteMessage == nil {
		return ErrMissingPayload
	}
	path := "/public/v1/chat/" + act.DeleteMessage.MessageID
	return a.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

func (a *Adapter) doBan(ctx context.Context, act adapters.Action) error {
	if act.Ban == nil {
		return ErrMissingPayload
	}
	userID, err := strconv.Atoi(strings.TrimSpace(act.Ban.UserID))
	if err != nil {
		return fmt.Errorf("kick: ban: invalid user id %q: %w", act.Ban.UserID, err)
	}
	body := banRequest{
		BroadcasterUserID: a.cfg.BroadcasterUserID,
		UserID:            userID,
		Reason:            clampReason(act.Ban.Reason),
	}
	return a.doJSON(ctx, http.MethodPost, "/public/v1/moderation/bans", body, nil)
}

func (a *Adapter) doTimeout(ctx context.Context, act adapters.Action) error {
	if act.Timeout == nil {
		return ErrMissingPayload
	}
	userID, err := strconv.Atoi(strings.TrimSpace(act.Timeout.UserID))
	if err != nil {
		return fmt.Errorf("kick: timeout: invalid user id %q: %w", act.Timeout.UserID, err)
	}
	minutes := clampTimeoutMinutes(act.Timeout.Duration)
	body := banRequest{
		BroadcasterUserID: a.cfg.BroadcasterUserID,
		UserID:            userID,
		Duration:          &minutes,
		Reason:            clampReason(act.Timeout.Reason),
	}
	return a.doJSON(ctx, http.MethodPost, "/public/v1/moderation/bans", body, nil)
}

func (a *Adapter) doUntimeout(ctx context.Context, act adapters.Action) error {
	if act.Untimeout == nil {
		return ErrMissingPayload
	}
	userID, err := strconv.Atoi(strings.TrimSpace(act.Untimeout.UserID))
	if err != nil {
		return fmt.Errorf("kick: untimeout: invalid user id %q: %w", act.Untimeout.UserID, err)
	}
	body := unbanRequest{
		BroadcasterUserID: a.cfg.BroadcasterUserID,
		UserID:            userID,
	}
	return a.doJSON(ctx, http.MethodDelete, "/public/v1/moderation/bans", body, nil)
}

func clampReason(reason string) string {
	r := strings.TrimSpace(reason)
	if utf8.RuneCountInString(r) <= maxReasonChars {
		return r
	}
	runes := []rune(r)
	return string(runes[:maxReasonChars])
}

func clampTimeoutMinutes(d time.Duration) int {
	minutes := int(d / time.Minute)
	if minutes < minTimeoutMinutes {
		return minTimeoutMinutes
	}
	if minutes > maxTimeoutMinutes {
		return maxTimeoutMinutes
	}
	return minutes
}

// registerWhenReady waits for the server-ready signal, then reconciles the
// webhook subscriptions: stale subscriptions for our event names are removed
// before the current webhook URL is registered. A subscription failure is
// recorded as a health error but never crashes the adapter.
func (a *Adapter) registerWhenReady(ctx context.Context, ready <-chan struct{}, delay time.Duration, done chan struct{}) {
	defer close(done)

	if ready != nil {
		select {
		case <-ready:
		case <-ctx.Done():
			return
		}
	} else {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return
		}
	}

	if err := a.reconcileSubscriptions(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		a.mu.Lock()
		a.healthErr = err
		a.mu.Unlock()
		a.logger.Warn("kick subscription registration failed", "err", err)
		return
	}
	a.logger.Info("kick webhook subscriptions registered", "broadcaster", a.cfg.BroadcasterUserID)
}

func (a *Adapter) reconcileSubscriptions(ctx context.Context) error {
	var list subscriptionListResponse
	if err := a.appJSON(ctx, http.MethodGet, "/public/v1/events/subscriptions", nil, &list); err != nil {
		return fmt.Errorf("kick: list subscriptions: %w", err)
	}

	wanted := make(map[string]struct{}, len(subscribedEvents))
	for _, e := range subscribedEvents {
		wanted[e.Name] = struct{}{}
	}
	var staleIDs []string
	for _, sub := range list.Data {
		if _, ok := wanted[sub.Name]; ok && sub.ID != "" {
			staleIDs = append(staleIDs, sub.ID)
		}
	}
	if len(staleIDs) > 0 {
		del := subscriptionDeleteRequest{ID: staleIDs}
		if err := a.appJSON(ctx, http.MethodDelete, "/public/v1/events/subscriptions", del, nil); err != nil {
			a.logger.Warn("kick stale subscription cleanup failed", "err", err)
		}
	}

	create := subscriptionCreateRequest{
		Method:            "webhook",
		BroadcasterUserID: a.cfg.BroadcasterUserID,
		Events:            subscribedEvents,
	}
	if err := a.appJSON(ctx, http.MethodPost, "/public/v1/events/subscriptions", create, nil); err != nil {
		return fmt.Errorf("kick: create subscription: %w", err)
	}
	return nil
}

func (a *Adapter) doJSON(ctx context.Context, method, path string, body, out any) error {
	token, err := a.userToken()
	if err != nil {
		return err
	}
	return a.request(ctx, method, path, token, body, out)
}

func (a *Adapter) appJSON(ctx context.Context, method, path string, body, out any) error {
	token, err := a.appToken()
	if err != nil {
		return err
	}
	return a.request(ctx, method, path, token, body, out)
}

func (a *Adapter) request(ctx context.Context, method, path, token string, body, out any) error {
	endpoint := a.baseURL + path

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("kick: encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("kick: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("kick: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("kick: read response: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("kick: %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("kick: decode response: %w", err)
		}
	}
	return nil
}

func (a *Adapter) userToken() (string, error) {
	return resolveToken(a.cfg.UserTokenSource, a.cfg.UserAccessToken)
}

func (a *Adapter) appToken() (string, error) {
	return resolveToken(a.cfg.AppTokenSource, a.cfg.AppAccessToken)
}

func resolveToken(src oauth2.TokenSource, static string) (string, error) {
	if src != nil {
		tok, err := src.Token()
		if err != nil {
			return "", fmt.Errorf("kick: fetch token: %w", err)
		}
		if tok == nil || tok.AccessToken == "" {
			return "", ErrTokenRequired
		}
		return tok.AccessToken, nil
	}
	if strings.TrimSpace(static) == "" {
		return "", ErrTokenRequired
	}
	return static, nil
}

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
		a.logger.Warn("kick events channel full; dropping event", "type", e.Type)
	}
}

func connectionEvent(t adapters.EventType, channel, reason, errMsg string) adapters.Event {
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       t,
		Platform:   platformName,
		Channel:    channel,
		OccurredAt: time.Now().UTC(),
		Connection: &adapters.ConnectionEvent{Reason: reason, Error: errMsg},
	}
}

func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, ErrPublicKeyInvalid
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPublicKeyInvalid, err)
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, ErrPublicKeyInvalid
	}
	return key, nil
}

var _ adapters.Platform = (*Adapter)(nil)
