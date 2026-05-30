package eventsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// DefaultEventSubURL is Twitch's production EventSub WebSocket endpoint.
const DefaultEventSubURL = "wss://eventsub.wss.twitch.tv/ws"

const (
	defaultReconnectMinBackoff = 1 * time.Second
	defaultReconnectMaxBackoff = 30 * time.Second
	defaultKeepaliveTimeout    = 10 * time.Second
	keepaliveReadGrace         = 5 * time.Second
	maxMessageBytes            = 1 << 20

	redemptionSubscriptionPrefix = "channel.channel_points_custom_reward_redemption"
)

// RedemptionEvent is the neutral, helix-free view of a
// channel.channel_points_custom_reward_redemption.add notification.
type RedemptionEvent struct {
	ID                string
	BroadcasterUserID string
	UserID            string
	UserLogin         string
	UserName          string
	UserInput         string
	Status            string
	RewardID          string
	RewardTitle       string
	RewardCost        int
	RedeemedAt        time.Time
}

// Config configures a [Client]. The zero Config is usable but useless without
// a Handler; construct via [New].
type Config struct {
	// URL overrides the EventSub WS endpoint (default DefaultEventSubURL).
	// Tests point this at an httptest server.
	URL string
	// Dialer dials the WS endpoint. Default uses websocket.Dial. Injected
	// in tests. Must return a *websocket.Conn.
	Dialer func(ctx context.Context, url string) (*websocket.Conn, error)
	// OnSession is called once per successful connection AFTER the
	// session_welcome arrives, with the session id. The caller registers
	// EventSub subscriptions here (via Helix, outside this package). If it
	// returns an error the client treats the connection as failed and
	// reconnects. May be nil.
	OnSession func(ctx context.Context, sessionID string) error
	// Handler is called for every decoded redemption notification. Must be
	// non-nil to receive events. Called synchronously from the read loop;
	// keep it fast or hand off to a goroutine.
	Handler func(ctx context.Context, evt RedemptionEvent)
	// Logger; nil -> slog.Default().
	Logger *slog.Logger
	// ReconnectMinBackoff/ReconnectMaxBackoff bound the exponential backoff
	// used when a connection drops unexpectedly (defaults 1s / 30s).
	ReconnectMinBackoff time.Duration
	ReconnectMaxBackoff time.Duration
}

// Client is an EventSub WebSocket transport client. Construct via [New] and
// drive with [Client.Run].
type Client struct {
	url        string
	dialer     func(ctx context.Context, url string) (*websocket.Conn, error)
	onSession  func(ctx context.Context, sessionID string) error
	handler    func(ctx context.Context, evt RedemptionEvent)
	logger     *slog.Logger
	minBackoff time.Duration
	maxBackoff time.Duration
}

type metadata struct {
	MessageID        string `json:"message_id"`
	MessageType      string `json:"message_type"`
	MessageTimestamp string `json:"message_timestamp"`
	SubscriptionType string `json:"subscription_type"`
}

type sessionInfo struct {
	ID                     string `json:"id"`
	Status                 string `json:"status"`
	ReconnectURL           string `json:"reconnect_url"`
	KeepaliveTimeoutSecond int    `json:"keepalive_timeout_seconds"`
}

type envelope struct {
	Metadata metadata `json:"metadata"`
	Payload  struct {
		Session *sessionInfo    `json:"session"`
		Event   json.RawMessage `json:"event"`
	} `json:"payload"`
}

type wireRedemption struct {
	ID                string `json:"id"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
	UserID            string `json:"user_id"`
	UserLogin         string `json:"user_login"`
	UserName          string `json:"user_name"`
	UserInput         string `json:"user_input"`
	Status            string `json:"status"`
	Reward            struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Cost  int    `json:"cost"`
	} `json:"reward"`
	RedeemedAt time.Time `json:"redeemed_at"`
}

// New constructs a Client from cfg, applying defaults for any zero fields.
func New(cfg Config) *Client {
	c := &Client{
		url:        cfg.URL,
		dialer:     cfg.Dialer,
		onSession:  cfg.OnSession,
		handler:    cfg.Handler,
		logger:     cfg.Logger,
		minBackoff: cfg.ReconnectMinBackoff,
		maxBackoff: cfg.ReconnectMaxBackoff,
	}
	if c.url == "" {
		c.url = DefaultEventSubURL
	}
	if c.dialer == nil {
		c.dialer = defaultDialer
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}
	if c.minBackoff <= 0 {
		c.minBackoff = defaultReconnectMinBackoff
	}
	if c.maxBackoff <= 0 {
		c.maxBackoff = defaultReconnectMaxBackoff
	}
	if c.maxBackoff < c.minBackoff {
		c.maxBackoff = c.minBackoff
	}
	return c
}

func defaultDialer(ctx context.Context, url string) (*websocket.Conn, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	return conn, err
}

// errReconnect flows from the read loop to [Client.serve] to request a graceful
// session_reconnect handoff. It never escapes to the caller.
var errReconnect = errors.New("eventsub: session reconnect requested")

// Run connects and processes messages until ctx is cancelled, reconnecting on
// transient failures. Returns ctx.Err() on cancellation. It NEVER returns on a
// normal reconnect — only ctx cancellation stops it.
func (c *Client) Run(ctx context.Context) error {
	backoff := c.minBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		conn, sessionID, keepalive, err := c.dialAndWelcome(ctx, c.url)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			c.logger.Warn("eventsub connect failed", "url", c.url, "err", err)
			if err := sleep(ctx, backoff); err != nil {
				return err
			}
			backoff = nextBackoff(backoff, c.maxBackoff)
			continue
		}

		// A dropped connection loses all subscriptions, so OnSession runs on
		// every fresh dial to (re)register them.
		if c.onSession != nil {
			if err := c.onSession(ctx, sessionID); err != nil {
				_ = conn.Close(websocket.StatusNormalClosure, "onsession failed")
				if ctx.Err() != nil {
					return ctx.Err()
				}
				c.logger.Warn("eventsub onsession failed, will redial", "err", err)
				if err := sleep(ctx, backoff); err != nil {
					return err
				}
				backoff = nextBackoff(backoff, c.maxBackoff)
				continue
			}
		}

		backoff = c.minBackoff

		err = c.serve(ctx, conn, keepalive)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.logger.Warn("eventsub connection dropped, will redial", "err", err)
		if err := sleep(ctx, backoff); err != nil {
			return err
		}
		backoff = nextBackoff(backoff, c.maxBackoff)
	}
}

// serve runs the read loop on conn and handles every session_reconnect inline:
// it dials the new URL and waits for its welcome BEFORE closing the old
// connection, so no notification is lost across the handoff. OnSession is not
// re-invoked because Twitch carries existing subscriptions to the new session.
// serve returns only when the connection drops or ctx is cancelled.
func (c *Client) serve(ctx context.Context, conn *websocket.Conn, keepalive time.Duration) error {
	for {
		reconnectURL, err := c.readLoop(ctx, conn, keepalive)
		if !errors.Is(err, errReconnect) {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return err
		}

		newConn, _, newKeepalive, derr := c.dialAndWelcome(ctx, reconnectURL)
		if derr != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return fmt.Errorf("reconnect handshake: %w", derr)
		}
		_ = conn.Close(websocket.StatusNormalClosure, "reconnecting")
		conn, keepalive = newConn, newKeepalive
		c.logger.Info("eventsub reconnected to new session", "url", reconnectURL)
	}
}

// dialAndWelcome dials url and blocks until the session_welcome envelope is
// read, returning the live connection, session id, and the keepalive timeout
// the server advertised.
func (c *Client) dialAndWelcome(ctx context.Context, url string) (*websocket.Conn, string, time.Duration, error) {
	conn, err := c.dialer(ctx, url)
	if err != nil {
		return nil, "", 0, fmt.Errorf("dial: %w", err)
	}
	conn.SetReadLimit(maxMessageBytes)

	keepalive := defaultKeepaliveTimeout
	for {
		env, err := readEnvelope(ctx, conn, keepalive)
		if err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return nil, "", 0, fmt.Errorf("await welcome: %w", err)
		}
		switch env.Metadata.MessageType {
		case "session_welcome":
			if env.Payload.Session == nil || env.Payload.Session.ID == "" {
				_ = conn.Close(websocket.StatusProtocolError, "missing session id")
				return nil, "", 0, errors.New("session_welcome missing session id")
			}
			if env.Payload.Session.KeepaliveTimeoutSecond > 0 {
				keepalive = time.Duration(env.Payload.Session.KeepaliveTimeoutSecond) * time.Second
			}
			return conn, env.Payload.Session.ID, keepalive, nil
		case "session_keepalive":
			continue
		default:
			c.logger.Debug("eventsub pre-welcome message ignored", "type", env.Metadata.MessageType)
		}
	}
}

// readLoop processes messages on conn until ctx is cancelled or the connection
// drops, returning the corresponding error. On a session_reconnect it returns
// the reconnect_url and errReconnect so [Client.serve] performs the handoff.
func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, keepalive time.Duration) (string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		env, err := readEnvelope(ctx, conn, keepalive)
		if err != nil {
			return "", err
		}

		switch env.Metadata.MessageType {
		case "session_keepalive":
			c.logger.Debug("eventsub keepalive")
		case "notification":
			c.dispatchNotification(ctx, env)
		case "session_reconnect":
			if env.Payload.Session == nil || env.Payload.Session.ReconnectURL == "" {
				return "", errors.New("session_reconnect missing reconnect_url")
			}
			return env.Payload.Session.ReconnectURL, errReconnect
		case "revocation":
			c.logger.Warn("eventsub subscription revoked", "subscription_type", env.Metadata.SubscriptionType)
		case "session_welcome":
			c.logger.Debug("eventsub duplicate welcome ignored")
		default:
			c.logger.Debug("eventsub unknown message type", "type", env.Metadata.MessageType)
		}
	}
}

// dispatchNotification decodes a redemption notification and forwards it to the
// handler. Non-redemption subscription types are ignored.
func (c *Client) dispatchNotification(ctx context.Context, env envelope) {
	if !strings.HasPrefix(env.Metadata.SubscriptionType, redemptionSubscriptionPrefix) {
		c.logger.Debug("eventsub notification ignored", "subscription_type", env.Metadata.SubscriptionType)
		return
	}
	if c.handler == nil {
		return
	}
	var wire wireRedemption
	if err := json.Unmarshal(env.Payload.Event, &wire); err != nil {
		c.logger.Warn("eventsub redemption decode failed", "err", err)
		return
	}
	c.handler(ctx, RedemptionEvent{
		ID:                wire.ID,
		BroadcasterUserID: wire.BroadcasterUserID,
		UserID:            wire.UserID,
		UserLogin:         wire.UserLogin,
		UserName:          wire.UserName,
		UserInput:         wire.UserInput,
		Status:            wire.Status,
		RewardID:          wire.Reward.ID,
		RewardTitle:       wire.Reward.Title,
		RewardCost:        wire.Reward.Cost,
		RedeemedAt:        wire.RedeemedAt,
	})
}

// readEnvelope reads one text frame and decodes it into an envelope. It derives
// a per-read deadline from keepalive (plus a grace margin) so a silent dead
// connection surfaces as an error instead of blocking forever.
func readEnvelope(ctx context.Context, conn *websocket.Conn, keepalive time.Duration) (envelope, error) {
	readCtx := ctx
	if keepalive > 0 {
		var cancel context.CancelFunc
		readCtx, cancel = context.WithTimeout(ctx, keepalive+keepaliveReadGrace)
		defer cancel()
	}
	mt, data, err := conn.Read(readCtx)
	if err != nil {
		if ctx.Err() != nil {
			return envelope{}, ctx.Err()
		}
		return envelope{}, err
	}
	if mt != websocket.MessageText {
		return envelope{}, fmt.Errorf("unexpected message type %v", mt)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	return env, nil
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
