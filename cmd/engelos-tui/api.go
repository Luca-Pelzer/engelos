// Package main hosts the engelos-tui binary — a terminal dashboard that
// connects to a running engelOS daemon over HTTP/WebSocket and renders
// live stats, leaderboards, and chat events.
//
// This file holds the HTTP client used by the Bubble Tea model. It is
// deliberately kept dependency-light (stdlib + coder/websocket) so the
// TUI binary stays small.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// ErrInvalidCredentials is returned by Client.Login when the daemon
// responds with HTTP 401 (wrong email/password or disabled account).
// Callers MUST use errors.Is to detect it; the wire-level error message
// is intentionally not part of the public API.
var ErrInvalidCredentials = errors.New("invalid credentials")

// User mirrors the sanitized user JSON returned by /api/v1/auth/login
// and /api/v1/users/me. Field tags match the daemon's JSON output.
type User struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Email       string    `json:"email"`
	Username    string    `json:"username"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastLoginAt time.Time `json:"last_login_at,omitempty"`
	Disabled    bool      `json:"disabled"`
}

// DispatcherStats mirrors the dispatcher block of /api/v1/stats.
// All integer counters default to zero when the daemon omits them.
type DispatcherStats struct {
	Messages         int       `json:"messages"`
	Subscriptions    int       `json:"subscriptions"`
	Raids            int       `json:"raids"`
	PityGrantErrors  int       `json:"pity_grant_errors"`
	StreakTickErrors int       `json:"streak_tick_errors"`
	LastEventAt      time.Time `json:"last_event_at"`
}

// Stats is the full shape of GET /api/v1/stats.
type Stats struct {
	Version    string          `json:"version"`
	Phase      string          `json:"phase"`
	Dispatcher DispatcherStats `json:"dispatcher"`
}

// LeaderboardEntry is one row of /api/v1/streak/leaderboard (and the
// future pity equivalent). Fields not present in a given response remain
// zero — callers should treat them as best-effort.
type LeaderboardEntry struct {
	Channel     string `json:"channel"`
	ViewerID    string `json:"viewer_id"`
	Username    string `json:"username"`
	DaysCurrent int    `json:"days_current,omitempty"`
	DaysLongest int    `json:"days_longest,omitempty"`
	Points      int    `json:"points,omitempty"`
}

// PityStatus mirrors GET /api/v1/pity/status.
type PityStatus struct {
	Channel           string  `json:"channel"`
	ViewerID          string  `json:"viewer_id"`
	Points            int     `json:"points"`
	SoftPityHit       bool    `json:"soft_pity_hit"`
	NearGuaranteed    bool    `json:"near_guaranteed"`
	EffectiveChance   float64 `json:"effective_chance"`
	HardPityThreshold int     `json:"hard_pity_threshold"`
	SoftPityFraction  float64 `json:"soft_pity_fraction"`
}

// StreakStatus mirrors GET /api/v1/streak/status.
type StreakStatus struct {
	Channel          string    `json:"channel"`
	ViewerID         string    `json:"viewer_id"`
	DaysCurrent      int       `json:"days_current"`
	DaysLongest      int       `json:"days_longest"`
	FreezesAvailable int       `json:"freezes_available"`
	LastTickAt       time.Time `json:"last_tick_at"`
	NextMilestone    int       `json:"next_milestone"`
}

// WSEvent is the parsed envelope emitted by the WebSocket goroutine for
// every server-pushed message. Raw is the original payload — callers can
// re-parse it for nested fields if needed.
type WSEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
	Raw  []byte          `json:"-"`
}

// Client is a thin HTTP wrapper around the engelOS daemon. The embedded
// cookie jar means a successful Login transparently authenticates every
// subsequent call. Client is safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a Client pointed at baseURL (e.g. http://127.0.0.1:8080).
// When insecure is true, TLS certificate verification is skipped — useful
// for self-signed tailnet certs. The returned client always carries a
// cookie jar; the caller does not need to manage cookies manually.
func NewClient(baseURL string, insecure bool) (*Client, error) {
	bu := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if bu == "" {
		return nil, errors.New("api: baseURL is required")
	}
	if _, err := url.Parse(bu); err != nil {
		return nil, fmt.Errorf("api: parse baseURL: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("api: cookie jar: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // explicit opt-in via -insecure flag
		}
	}
	return &Client{
		baseURL: bu,
		http: &http.Client{
			Jar:       jar,
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}, nil
}

// BaseURL returns the daemon URL the client was built with.
func (c *Client) BaseURL() string { return c.baseURL }

// HTTPClient exposes the underlying *http.Client so the WebSocket dial
// can reuse the same cookie jar.
func (c *Client) HTTPClient() *http.Client { return c.http }

// Login posts the credentials to /api/v1/auth/login. On HTTP 200 the
// session cookie is stored in the jar and the user is returned. On HTTP
// 401 it returns ErrInvalidCredentials. Any other status surfaces as a
// generic error including the status code.
func (c *Client) Login(ctx context.Context, email, password string) (User, error) {
	body, err := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	if err != nil {
		return User{}, fmt.Errorf("api: marshal login: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/auth/login", strings.NewReader(string(body)))
	if err != nil {
		return User{}, fmt.Errorf("api: build login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return User{}, fmt.Errorf("api: login: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var payload struct {
			User User `json:"user"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return User{}, fmt.Errorf("api: decode login: %w", err)
		}
		return payload.User, nil
	case http.StatusUnauthorized:
		return User{}, ErrInvalidCredentials
	default:
		return User{}, fmt.Errorf("api: login: unexpected status %d", resp.StatusCode)
	}
}

// Logout posts to /api/v1/auth/logout. The call is idempotent on the
// server side, so a missing cookie still returns nil.
func (c *Client) Logout(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/auth/logout", nil)
	if err != nil {
		return fmt.Errorf("api: build logout request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("api: logout: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("api: logout: server error %d", resp.StatusCode)
	}
	return nil
}

// Me fetches /api/v1/users/me.
func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	if err := c.getJSON(ctx, "/api/v1/users/me", &u); err != nil {
		return User{}, err
	}
	return u, nil
}

// Stats fetches /api/v1/stats. The dispatcher block is best-effort —
// older daemons may omit it; the returned struct will then have zero
// values in DispatcherStats.
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	if err := c.getJSON(ctx, "/api/v1/stats", &s); err != nil {
		return Stats{}, err
	}
	return s, nil
}

// PityLeaderboard returns the top-N pity holders for a channel.
//
// The /api/v1/pity/leaderboard endpoint does not exist yet, so this
// method currently returns (nil, nil) without contacting the daemon.
// TODO: wire when /api/v1/pity/leaderboard ships.
func (c *Client) PityLeaderboard(_ context.Context, _ string, _ int) ([]LeaderboardEntry, error) {
	return nil, nil
}

// StreakLeaderboard fetches /api/v1/streak/leaderboard?channel=&limit=.
// An empty channel asks the daemon for the cross-channel top-N.
func (c *Client) StreakLeaderboard(ctx context.Context, channel string, limit int) ([]LeaderboardEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	q := url.Values{}
	if channel != "" {
		q.Set("channel", channel)
	}
	q.Set("limit", strconv.Itoa(limit))
	var payload struct {
		Channel string             `json:"channel"`
		Limit   int                `json:"limit"`
		Entries []LeaderboardEntry `json:"entries"`
	}
	if err := c.getJSON(ctx, "/api/v1/streak/leaderboard?"+q.Encode(), &payload); err != nil {
		return nil, err
	}
	return payload.Entries, nil
}

// PityStatus fetches /api/v1/pity/status?channel=&viewer_id=.
func (c *Client) PityStatus(ctx context.Context, channel, viewerID string) (PityStatus, error) {
	q := url.Values{"channel": {channel}, "viewer_id": {viewerID}}
	var st PityStatus
	if err := c.getJSON(ctx, "/api/v1/pity/status?"+q.Encode(), &st); err != nil {
		return PityStatus{}, err
	}
	return st, nil
}

// StreakStatus fetches /api/v1/streak/status?channel=&viewer_id=.
func (c *Client) StreakStatus(ctx context.Context, channel, viewerID string) (StreakStatus, error) {
	q := url.Values{"channel": {channel}, "viewer_id": {viewerID}}
	var st StreakStatus
	if err := c.getJSON(ctx, "/api/v1/streak/status?"+q.Encode(), &st); err != nil {
		return StreakStatus{}, err
	}
	return st, nil
}

// StreamWebSocket dials /api/v1/ws and returns a channel that emits one
// WSEvent per received JSON envelope. The cookie jar is reused so the
// connection inherits the session.
//
// The channel is closed when ctx is cancelled, the server disconnects,
// or a fatal decode error occurs. The caller MUST cancel ctx to release
// the goroutine; the returned channel will then drain to zero.
func (c *Client) StreamWebSocket(ctx context.Context) (<-chan WSEvent, error) {
	wsURL, err := buildWSURL(c.baseURL, "/api/v1/ws")
	if err != nil {
		return nil, err
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: c.http,
	})
	if err != nil {
		return nil, fmt.Errorf("api: ws dial: %w", err)
	}
	// 1 MiB ceiling matches the server-side maxMessageBytes.
	conn.SetReadLimit(1 << 20)

	out := make(chan WSEvent, 64)
	go func() {
		defer close(out)
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "client closing") }()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			ev := WSEvent{Raw: append([]byte(nil), data...)}
			// Tolerate non-JSON frames silently — the server may send
			// plain text pings in some configurations.
			_ = json.Unmarshal(data, &ev)
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// getJSON performs an authenticated GET and decodes the response body
// into out. Non-2xx responses surface as errors; 401 is mapped to
// ErrInvalidCredentials so the caller can prompt for re-login.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("api: build GET %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("api: GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrInvalidCredentials
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read a bit of the body to surface server-side error messages.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("api: GET %s: status %d: %s",
			path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("api: decode %s: %w", path, err)
	}
	return nil
}

// buildWSURL converts an http(s) base URL plus a path into the
// equivalent ws(s) URL. Returns a clear error for non-http schemes.
func buildWSURL(base, path string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("api: parse ws base: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("api: unsupported scheme %q (need http/https)", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String(), nil
}
