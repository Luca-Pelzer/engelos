package youtube

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

// capturedRequest records the salient parts of an outgoing request so tests
// can assert method, URL and body without a real network round-trip.
type capturedRequest struct {
	method string
	url    string
	body   string
	auth   string
}

// fakeTransport is an injected httpDoer that serves canned responses keyed by
// a matcher over the request, recording every call. A handler returning a nil
// response with a nil error means "no match"; the first matching handler wins.
type fakeTransport struct {
	mu             sync.Mutex
	calls          []capturedRequest
	handler        func(req *http.Request, body string) (*http.Response, error)
	fallbackStatus int
	fallbackBody   string
}

func (f *fakeTransport) Do(req *http.Request) (*http.Response, error) {
	var body string
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
	}
	f.mu.Lock()
	f.calls = append(f.calls, capturedRequest{
		method: req.Method,
		url:    req.URL.String(),
		body:   body,
		auth:   req.Header.Get("Authorization"),
	})
	handler := f.handler
	status := f.fallbackStatus
	fbody := f.fallbackBody
	f.mu.Unlock()

	if handler != nil {
		resp, err := handler(req, body)
		if err != nil || resp != nil {
			return resp, err
		}
	}
	if status != 0 {
		return jsonResponse(status, fbody), nil
	}
	return jsonResponse(http.StatusOK, "{}"), nil
}

func (f *fakeTransport) snapshot() []capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]capturedRequest, len(f.calls))
	copy(out, f.calls)
	return out
}

func newFallbackTransport(status int, body string) *fakeTransport {
	return &fakeTransport{fallbackStatus: status, fallbackBody: body}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func baseConfig(t *testing.T, transport httpDoer) Config {
	t.Helper()
	return Config{
		AccessToken: "test-token",
		LiveChatID:  "lc-123",
		Channel:     "engelchannel",
		HTTPClient:  transport,
		BaseURL:     "https://yt.test/v3",
	}
}

func TestConnectRequiresToken(t *testing.T) {
	a := New(Config{LiveChatID: "lc-1", HTTPClient: &fakeTransport{}})
	err := a.Connect(context.Background())
	assert.ErrorIs(t, err, ErrTokenRequired)
}

func TestConnectResolvesLiveChatIDFromVideoID(t *testing.T) {
	const videoResp = `{"items":[{"liveStreamingDetails":{"activeLiveChatId":"resolved-lc"}}]}`
	tr := &fakeTransport{
		handler: func(req *http.Request, _ string) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/videos") {
				assert.Equal(t, http.MethodGet, req.Method)
				assert.Equal(t, "liveStreamingDetails", req.URL.Query().Get("part"))
				assert.Equal(t, "vid-9", req.URL.Query().Get("id"))
				return jsonResponse(http.StatusOK, videoResp), nil
			}
			return jsonResponse(http.StatusOK, `{"pollingIntervalMillis":50,"items":[]}`), nil
		},
	}
	cfg := Config{
		AccessToken: "tok",
		VideoID:     "vid-9",
		Channel:     "ch",
		HTTPClient:  tr,
		BaseURL:     "https://yt.test/v3",
	}
	a := New(cfg)
	require.NoError(t, a.Connect(context.Background()))
	defer func() { _ = a.Disconnect(context.Background()) }()

	a.mu.Lock()
	got := a.liveChatID
	a.mu.Unlock()
	assert.Equal(t, "resolved-lc", got)
	assert.GreaterOrEqual(t, a.QuotaUsed(), costVideosList)
}

func TestConnectNoLiveChat(t *testing.T) {
	tr := &fakeTransport{
		handler: func(req *http.Request, _ string) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/videos") {
				return jsonResponse(http.StatusOK, `{"items":[]}`), nil
			}
			return nil, nil
		},
	}
	a := New(Config{AccessToken: "tok", VideoID: "missing", HTTPClient: tr, BaseURL: "https://yt.test/v3"})
	err := a.Connect(context.Background())
	assert.ErrorIs(t, err, ErrNoLiveChat)
}

func TestConnectMissingBothChatAndVideo(t *testing.T) {
	a := New(Config{AccessToken: "tok", HTTPClient: &fakeTransport{}, BaseURL: "https://yt.test/v3"})
	err := a.Connect(context.Background())
	assert.ErrorIs(t, err, ErrNoLiveChat)
}

func TestPollEmitsAndSkips(t *testing.T) {
	const page = `{
		"pollingIntervalMillis": 25,
		"nextPageToken": "next-1",
		"items": [
			{"id":"m1","snippet":{"type":"textMessageEvent","displayMessage":"hi there","publishedAt":"2026-01-02T03:04:05Z"},
			 "authorDetails":{"channelId":"u1","displayName":"Alice","isChatModerator":true}},
			{"id":"m2","snippet":{"type":"newSponsorEvent","publishedAt":"2026-01-02T03:04:06Z"},
			 "authorDetails":{"channelId":"u2","displayName":"Bob"}},
			{"id":"m3","snippet":{"type":"superChatEvent","publishedAt":"2026-01-02T03:04:07Z"},
			 "authorDetails":{"channelId":"u3","displayName":"Cara"}}
		]
	}`
	var pollSeen int
	tr := &fakeTransport{
		handler: func(req *http.Request, _ string) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/liveChat/messages") && req.Method == http.MethodGet {
				assert.Equal(t, "lc-123", req.URL.Query().Get("liveChatId"))
				assert.Equal(t, "id,snippet,authorDetails", req.URL.Query().Get("part"))
				pollSeen++
				return jsonResponse(http.StatusOK, page), nil
			}
			return nil, nil
		},
	}
	a := New(baseConfig(t, tr))
	require.NoError(t, a.Connect(context.Background()))
	defer func() { _ = a.Disconnect(context.Background()) }()

	events := a.Events()

	first := requireEvent(t, events)
	assert.Equal(t, adapters.EventConnected, first.Type)

	msg := requireEvent(t, events)
	require.Equal(t, adapters.EventMessageCreated, msg.Type)
	require.NotNil(t, msg.Message)
	assert.Equal(t, "m1", msg.Message.ID)
	assert.Equal(t, "u1", msg.Message.UserID)
	assert.Equal(t, "Alice", msg.Message.Username)
	assert.Equal(t, "hi there", msg.Message.Content)
	assert.True(t, msg.Message.IsModerator)
	assert.Equal(t, "youtube", msg.Platform)
	assert.Equal(t, "engelchannel", msg.Channel)
	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, 2026, msg.OccurredAt.Year())

	sub := requireEvent(t, events)
	require.Equal(t, adapters.EventUserSubscribed, sub.Type)
	require.NotNil(t, sub.Subscription)
	assert.Equal(t, "u2", sub.Subscription.UserID)
	assert.Equal(t, "member", sub.Subscription.Tier)

	next := requireEvent(t, events)
	assert.Equal(t, adapters.EventMessageCreated, next.Type)

	require.NoError(t, a.Disconnect(context.Background()))
	assert.GreaterOrEqual(t, pollSeen, 1, "poll endpoint should have been called")

	calls := tr.snapshot()
	var sawPageToken bool
	for _, c := range calls {
		if strings.Contains(c.url, "pageToken=next-1") {
			sawPageToken = true
		}
	}
	assert.True(t, sawPageToken, "nextPageToken should be carried forward as pageToken")
}

func TestPollRespectsPollingInterval(t *testing.T) {
	const page = `{"pollingIntervalMillis":40,"nextPageToken":"n","items":[
		{"id":"m1","snippet":{"type":"textMessageEvent","displayMessage":"x","publishedAt":"2026-01-02T03:04:05Z"},
		 "authorDetails":{"channelId":"u1","displayName":"A"}}]}`
	tr := &fakeTransport{
		handler: func(req *http.Request, _ string) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/liveChat/messages") && req.Method == http.MethodGet {
				return jsonResponse(http.StatusOK, page), nil
			}
			return nil, nil
		},
	}
	cfg := baseConfig(t, tr)
	cfg.minPollInterval = time.Millisecond
	a := New(cfg)
	require.NoError(t, a.Connect(context.Background()))
	defer func() { _ = a.Disconnect(context.Background()) }()

	events := a.Events()
	_ = requireEvent(t, events)
	_ = requireEvent(t, events)

	var parsed liveChatListResponse
	require.NoError(t, json.Unmarshal([]byte(page), &parsed))
	assert.Equal(t, int64(40), parsed.PollingIntervalMillis)
}

func TestQuotaGuardTripsAndDisconnects(t *testing.T) {
	const page = `{"pollingIntervalMillis":10,"nextPageToken":"n","items":[]}`
	tr := &fakeTransport{
		handler: func(req *http.Request, _ string) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/liveChat/messages") {
				return jsonResponse(http.StatusOK, page), nil
			}
			return nil, nil
		},
	}
	cfg := baseConfig(t, tr)
	cfg.DailyQuotaUnits = costList
	a := New(cfg)
	require.NoError(t, a.Connect(context.Background()))
	defer func() { _ = a.Disconnect(context.Background()) }()

	events := a.Events()
	connected := requireEvent(t, events)
	require.Equal(t, adapters.EventConnected, connected.Type)

	var disconnect adapters.Event
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e, ok := <-events:
			require.True(t, ok, "events channel closed before disconnect event")
			if e.Type == adapters.EventDisconnected {
				disconnect = e
				goto done
			}
		case <-deadline:
			t.Fatal("timed out waiting for quota-exhausted disconnect")
		}
	}
done:
	require.NotNil(t, disconnect.Connection)
	assert.Contains(t, disconnect.Connection.Reason, "quota")
	assert.ErrorIs(t, a.Health(), ErrQuotaExhausted)
}

func TestQuotaCounterResetsOnNewPacificDay(t *testing.T) {
	loc := pacificLocation()
	day1 := time.Date(2026, 1, 1, 12, 0, 0, 0, loc)
	clock := &fakeClock{now: day1}

	tr := &fakeTransport{
		handler: func(req *http.Request, _ string) (*http.Response, error) {
			return jsonResponse(http.StatusOK, "{}"), nil
		},
	}
	cfg := baseConfig(t, tr)
	cfg.nowFn = clock.Now
	a := New(cfg)

	a.chargeQuota(100)
	assert.Equal(t, 100, a.QuotaUsed())

	clock.set(time.Date(2026, 1, 1, 23, 0, 0, 0, loc))
	a.chargeQuota(10)
	assert.Equal(t, 110, a.QuotaUsed(), "same day must accumulate")

	clock.set(time.Date(2026, 1, 2, 0, 30, 0, 0, loc))
	a.chargeQuota(5)
	assert.Equal(t, 5, a.QuotaUsed(), "new Pacific day must reset the counter")
}

func TestDoSendMessage(t *testing.T) {
	tr := newFallbackTransport(http.StatusOK, "{}")
	a := connectIdle(t, tr)
	defer func() { _ = a.Disconnect(context.Background()) }()

	err := a.Do(context.Background(), adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     "engelchannel",
		SendMessage: &adapters.SendMessageAction{Text: "hello world"},
	})
	require.NoError(t, err)

	call := lastCall(t, tr, "/liveChat/messages", http.MethodPost)
	assert.Contains(t, call.url, "part=snippet")
	var body insertMessageRequest
	require.NoError(t, json.Unmarshal([]byte(call.body), &body))
	assert.Equal(t, "lc-123", body.Snippet.LiveChatID)
	assert.Equal(t, "textMessageEvent", body.Snippet.Type)
	assert.Equal(t, "hello world", body.Snippet.TextMessageDetails.MessageText)
}

func TestDoDeleteMessage(t *testing.T) {
	tr := newFallbackTransport(http.StatusNoContent, "")
	a := connectIdle(t, tr)
	defer func() { _ = a.Disconnect(context.Background()) }()

	err := a.Do(context.Background(), adapters.Action{
		Type:          adapters.ActionDeleteMessage,
		Channel:       "engelchannel",
		DeleteMessage: &adapters.DeleteMessageAction{MessageID: "msg-77"},
	})
	require.NoError(t, err)

	call := lastCall(t, tr, "/liveChat/messages", http.MethodDelete)
	assert.Contains(t, call.url, "id=msg-77")
	assert.Empty(t, call.body)
}

func TestDoBan(t *testing.T) {
	tr := newFallbackTransport(http.StatusOK, "{}")
	a := connectIdle(t, tr)
	defer func() { _ = a.Disconnect(context.Background()) }()

	err := a.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionBan,
		Channel: "engelchannel",
		Ban:     &adapters.BanAction{UserID: "chan-evil"},
	})
	require.NoError(t, err)

	call := lastCall(t, tr, "/liveChat/bans", http.MethodPost)
	var body insertBanRequest
	require.NoError(t, json.Unmarshal([]byte(call.body), &body))
	assert.Equal(t, "lc-123", body.Snippet.LiveChatID)
	assert.Equal(t, "permanent", body.Snippet.Type)
	assert.Equal(t, "chan-evil", body.Snippet.BannedUserDetails.ChannelID)
	assert.Zero(t, body.Snippet.BanDurationSeconds)
}

func TestDoTimeout(t *testing.T) {
	tr := newFallbackTransport(http.StatusOK, "{}")
	a := connectIdle(t, tr)
	defer func() { _ = a.Disconnect(context.Background()) }()

	err := a.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionTimeout,
		Channel: "engelchannel",
		Timeout: &adapters.TimeoutAction{UserID: "chan-rude", Duration: 300 * time.Second},
	})
	require.NoError(t, err)

	call := lastCall(t, tr, "/liveChat/bans", http.MethodPost)
	var body insertBanRequest
	require.NoError(t, json.Unmarshal([]byte(call.body), &body))
	assert.Equal(t, "temporary", body.Snippet.Type)
	assert.Equal(t, uint64(300), body.Snippet.BanDurationSeconds)
	assert.Equal(t, "chan-rude", body.Snippet.BannedUserDetails.ChannelID)
}

func TestDoUntimeoutUnsupported(t *testing.T) {
	tr := newFallbackTransport(http.StatusOK, "{}")
	a := connectIdle(t, tr)
	defer func() { _ = a.Disconnect(context.Background()) }()

	err := a.Do(context.Background(), adapters.Action{
		Type:      adapters.ActionUntimeout,
		Channel:   "engelchannel",
		Untimeout: &adapters.UntimeoutAction{UserID: "chan-x"},
	})
	assert.ErrorIs(t, err, ErrUntimeoutUnsupported)
}

func TestDoUnknownAction(t *testing.T) {
	tr := newFallbackTransport(http.StatusOK, "{}")
	a := connectIdle(t, tr)
	defer func() { _ = a.Disconnect(context.Background()) }()

	err := a.Do(context.Background(), adapters.Action{Type: adapters.ActionType("nonsense")})
	assert.ErrorIs(t, err, ErrUnknownAction)
}

func TestDoNotConnected(t *testing.T) {
	a := New(baseConfig(t, &fakeTransport{}))
	err := a.Do(context.Background(), adapters.Action{
		Type:        adapters.ActionSendMessage,
		SendMessage: &adapters.SendMessageAction{Text: "x"},
	})
	assert.ErrorIs(t, err, ErrNotConnected)
}

func TestDisconnectIdempotentWithoutConnect(t *testing.T) {
	a := New(baseConfig(t, &fakeTransport{}))
	assert.NoError(t, a.Disconnect(context.Background()))
	assert.NoError(t, a.Disconnect(context.Background()))
	assert.ErrorIs(t, a.Health(), ErrNotConnected)
}

func TestConnectTwiceFails(t *testing.T) {
	tr := newFallbackTransport(http.StatusOK, `{"pollingIntervalMillis":10,"items":[]}`)
	a := connectIdle(t, tr)
	defer func() { _ = a.Disconnect(context.Background()) }()
	err := a.Connect(context.Background())
	assert.ErrorIs(t, err, ErrAlreadyConnected)
}

func TestContextCancelStopsPolling(t *testing.T) {
	tr := newFallbackTransport(http.StatusOK, `{"pollingIntervalMillis":5,"items":[]}`)
	ctx, cancel := context.WithCancel(context.Background())
	cfg := baseConfig(t, tr)
	cfg.minPollInterval = time.Millisecond
	a := New(cfg)
	require.NoError(t, a.Connect(ctx))

	events := a.Events()
	_ = requireEvent(t, events)

	cancel()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				assert.ErrorIs(t, a.Health(), ErrNotConnected)
				return
			}
		case <-deadline:
			t.Fatal("events channel not closed after context cancel")
		}
	}
}

func TestTokenSourcePreferred(t *testing.T) {
	tr := &fakeTransport{
		handler: func(req *http.Request, _ string) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/liveChat/messages") && req.Method == http.MethodDelete {
				return jsonResponse(http.StatusOK, "{}"), nil
			}
			return jsonResponse(http.StatusOK, `{"pollingIntervalMillis":60000,"items":[]}`), nil
		},
	}
	cfg := baseConfig(t, tr)
	cfg.AccessToken = ""
	cfg.TokenSource = staticTokenSource{token: "refreshed-xyz"}
	a := connectIdleCfg(t, cfg)
	defer func() { _ = a.Disconnect(context.Background()) }()

	err := a.Do(context.Background(), adapters.Action{
		Type:          adapters.ActionDeleteMessage,
		DeleteMessage: &adapters.DeleteMessageAction{MessageID: "m"},
	})
	require.NoError(t, err)
	call := lastCall(t, tr, "/liveChat/messages", http.MethodDelete)
	assert.Equal(t, "Bearer refreshed-xyz", call.auth)
}

func TestNonOKStatusReturnsError(t *testing.T) {
	tr := newFallbackTransport(http.StatusForbidden, `{"error":"quotaExceeded"}`)
	a := connectIdle(t, tr)
	defer func() { _ = a.Disconnect(context.Background()) }()

	err := a.Do(context.Background(), adapters.Action{
		Type:          adapters.ActionDeleteMessage,
		DeleteMessage: &adapters.DeleteMessageAction{MessageID: "m"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestTranslateMessageTable(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantAction translateAction
		wantType   adapters.EventType
		check      func(t *testing.T, e adapters.Event)
	}{
		{
			name:       "text message",
			raw:        `{"id":"m1","snippet":{"type":"textMessageEvent","displayMessage":"yo","publishedAt":"2026-03-04T05:06:07Z"},"authorDetails":{"channelId":"u1","displayName":"A","isChatOwner":true,"isChatSponsor":true}}`,
			wantAction: translateEmit,
			wantType:   adapters.EventMessageCreated,
			check: func(t *testing.T, e adapters.Event) {
				require.NotNil(t, e.Message)
				assert.Equal(t, "yo", e.Message.Content)
				assert.True(t, e.Message.IsModerator)
				assert.True(t, e.Message.IsSubscriber)
			},
		},
		{
			name:       "text message falls back to messageText",
			raw:        `{"id":"m1","snippet":{"type":"textMessageEvent","textMessageDetails":{"messageText":"fallback"},"publishedAt":"2026-03-04T05:06:07Z"},"authorDetails":{"channelId":"u1","displayName":"A"}}`,
			wantAction: translateEmit,
			wantType:   adapters.EventMessageCreated,
			check: func(t *testing.T, e adapters.Event) {
				assert.Equal(t, "fallback", e.Message.Content)
			},
		},
		{
			name:       "new sponsor",
			raw:        `{"id":"m2","snippet":{"type":"newSponsorEvent","publishedAt":"2026-03-04T05:06:07Z"},"authorDetails":{"channelId":"u2","displayName":"B"}}`,
			wantAction: translateEmit,
			wantType:   adapters.EventUserSubscribed,
			check: func(t *testing.T, e adapters.Event) {
				require.NotNil(t, e.Subscription)
				assert.Equal(t, "member", e.Subscription.Tier)
			},
		},
		{
			name:       "message deleted",
			raw:        `{"id":"m3","snippet":{"type":"messageDeletedEvent","messageDeletedDetails":{"deletedMessageId":"gone-1"},"publishedAt":"2026-03-04T05:06:07Z"},"authorDetails":{}}`,
			wantAction: translateEmit,
			wantType:   adapters.EventMessageDeleted,
			check: func(t *testing.T, e adapters.Event) {
				require.NotNil(t, e.Message)
				assert.Equal(t, "gone-1", e.Message.ID)
			},
		},
		{
			name:       "user banned",
			raw:        `{"id":"m4","snippet":{"type":"userBannedEvent","userBannedDetails":{"bannedUserDetails":{"channelId":"c9","displayName":"Victim"}},"publishedAt":"2026-03-04T05:06:07Z"},"authorDetails":{}}`,
			wantAction: translateEmit,
			wantType:   adapters.EventUserBanned,
			check: func(t *testing.T, e adapters.Event) {
				require.NotNil(t, e.UserAction)
				assert.Equal(t, "ban", e.UserAction.Action)
				assert.Equal(t, "Victim", e.UserAction.TargetUser)
			},
		},
		{
			name:       "chat ended",
			raw:        `{"id":"m5","snippet":{"type":"chatEndedEvent","publishedAt":"2026-03-04T05:06:07Z"},"authorDetails":{}}`,
			wantAction: translateChatEnded,
		},
		{
			name:       "super chat skipped",
			raw:        `{"id":"m6","snippet":{"type":"superChatEvent","publishedAt":"2026-03-04T05:06:07Z"},"authorDetails":{}}`,
			wantAction: translateSkip,
		},
		{
			name:       "tombstone skipped",
			raw:        `{"id":"m7","snippet":{"type":"tombstone"},"authorDetails":{}}`,
			wantAction: translateSkip,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var m liveChatMessage
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &m))
			evt, action := translateMessage(m, "ch")
			assert.Equal(t, tc.wantAction, action)
			if tc.wantAction == translateEmit {
				assert.Equal(t, tc.wantType, evt.Type)
				assert.Equal(t, "youtube", evt.Platform)
				assert.Equal(t, "ch", evt.Channel)
				assert.NotEmpty(t, evt.ID)
				if tc.check != nil {
					tc.check(t, evt)
				}
			}
		})
	}
}

func TestParseTimestampFallback(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	got := parseTimestamp("not-a-timestamp")
	assert.False(t, got.Before(before))
}

func TestImplementsPlatform(t *testing.T) {
	var _ adapters.Platform = New(baseConfig(t, &fakeTransport{}))
}

// requireEvent reads one event with a generous timeout to keep tests
// non-flaky without sleeping on the wall clock.
func requireEvent(t *testing.T, ch <-chan adapters.Event) adapters.Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		require.True(t, ok, "events channel closed unexpectedly")
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return adapters.Event{}
	}
}

// connectIdle connects an adapter whose poll loop sees an empty page on a long
// interval so the Do-path tests are not racing against incoming chat.
func connectIdle(t *testing.T, tr *fakeTransport) *Adapter {
	t.Helper()
	return connectIdleCfg(t, baseConfig(t, tr))
}

func connectIdleCfg(t *testing.T, cfg Config) *Adapter {
	t.Helper()
	tr, ok := cfg.HTTPClient.(*fakeTransport)
	require.True(t, ok)
	tr.mu.Lock()
	if tr.handler == nil && tr.fallbackStatus == 0 {
		tr.fallbackStatus = http.StatusOK
		tr.fallbackBody = `{"pollingIntervalMillis":60000,"items":[]}`
	}
	tr.mu.Unlock()
	a := New(cfg)
	require.NoError(t, a.Connect(context.Background()))
	require.Equal(t, adapters.EventConnected, requireEvent(t, a.Events()).Type)
	return a
}

func lastCall(t *testing.T, tr *fakeTransport, pathFragment, method string) capturedRequest {
	t.Helper()
	calls := tr.snapshot()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].method == method && strings.Contains(calls[i].url, pathFragment) {
			return calls[i]
		}
	}
	t.Fatalf("no %s call matching %q found in %d calls", method, pathFragment, len(calls))
	return capturedRequest{}
}

// fakeClock is a controllable time source for the quota-reset tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

type staticTokenSource struct {
	token string
	err   error
}

func (s staticTokenSource) Token() (*oauth2.Token, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &oauth2.Token{AccessToken: s.token}, nil
}
