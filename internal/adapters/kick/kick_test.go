package kick

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

type fakeDoer struct {
	calls    []recordedCall
	response func(req *http.Request) (*http.Response, error)
}

type recordedCall struct {
	method string
	path   string
	body   string
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		body = string(raw)
	}
	f.calls = append(f.calls, recordedCall{method: req.Method, path: req.URL.Path, body: body})
	if f.response != nil {
		return f.response(req)
	}
	return jsonResponse(http.StatusOK, "{}"), nil
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func testKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return key, string(pemBytes)
}

func signPayload(t *testing.T, key *rsa.PrivateKey, messageID, timestamp string, body []byte) string {
	t.Helper()
	signed := messageID + "." + timestamp + "." + string(body)
	hashed := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func newTestAdapter(t *testing.T, pubKeyPEM string, doer httpDoer) *Adapter {
	t.Helper()
	a, err := New(Config{
		UserAccessToken:   "user-token",
		AppAccessToken:    "app-token",
		BroadcasterUserID: 12345,
		Channel:           "engelswtf",
		PublicKeyPEM:      pubKeyPEM,
		HTTPClient:        doer,
		BaseURL:           "https://api.test",
		nowFn:             func() time.Time { return time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC) },
		readyDelay:        time.Hour,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func connectAdapter(t *testing.T, a *Adapter) {
	t.Helper()
	if err := a.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	drainConnected(a)
}

func drainConnected(a *Adapter) {
	select {
	case <-a.Events():
	case <-time.After(time.Second):
	}
}

func postWebhook(a *Adapter, key *rsa.PrivateKey, t *testing.T, eventType, messageID, timestamp string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/kick", strings.NewReader(string(body)))
	req.Header.Set(headerEventType, eventType)
	req.Header.Set(headerMessageID, messageID)
	req.Header.Set(headerTimestamp, timestamp)
	req.Header.Set(headerSignature, signPayload(t, key, messageID, timestamp, body))
	rec := httptest.NewRecorder()
	a.WebhookHandler()(rec, req)
	return rec
}

func TestWebhookSignatureValidAndTampered(t *testing.T) {
	key, pubPEM := testKeyPair(t)
	a := newTestAdapter(t, pubPEM, &fakeDoer{})
	connectAdapter(t, a)

	ts := "2026-06-06T11:59:30Z"
	body := []byte(`{"message_id":"m1","content":"hi","sender":{"user_id":7,"username":"bob"}}`)
	rec := postWebhook(a, key, t, "chat.message.sent", "msg-1", ts, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid signature: got %d want 202", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhooks/kick", strings.NewReader(string(body)))
	req.Header.Set(headerEventType, "chat.message.sent")
	req.Header.Set(headerMessageID, "msg-2")
	req.Header.Set(headerTimestamp, ts)
	req.Header.Set(headerSignature, base64.StdEncoding.EncodeToString([]byte("not-a-valid-signature")))
	rec2 := httptest.NewRecorder()
	a.WebhookHandler()(rec2, req)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("tampered signature: got %d want 403", rec2.Code)
	}
}

func TestWebhookReplayRejection(t *testing.T) {
	key, pubPEM := testKeyPair(t)
	a := newTestAdapter(t, pubPEM, &fakeDoer{})
	connectAdapter(t, a)

	body := []byte(`{"message_id":"m1","content":"hi","sender":{"user_id":7,"username":"bob"}}`)

	staleTS := "2026-06-06T11:50:00Z"
	rec := postWebhook(a, key, t, "chat.message.sent", "stale-1", staleTS, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stale timestamp: got %d want 403", rec.Code)
	}

	freshTS := "2026-06-06T11:59:30Z"
	rec1 := postWebhook(a, key, t, "chat.message.sent", "dup-1", freshTS, body)
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("first delivery: got %d want 202", rec1.Code)
	}
	if got := receiveEvent(t, a); got.Type != adapters.EventMessageCreated {
		t.Fatalf("expected message event, got %q", got.Type)
	}
	rec2 := postWebhook(a, key, t, "chat.message.sent", "dup-1", freshTS, body)
	if rec2.Code != http.StatusOK {
		t.Fatalf("duplicate delivery: got %d want 200", rec2.Code)
	}
	if ev, ok := tryReceive(a); ok {
		t.Fatalf("duplicate should not re-emit, got %q", ev.Type)
	}
}

func TestWebhookChatNormalization(t *testing.T) {
	key, pubPEM := testKeyPair(t)
	a := newTestAdapter(t, pubPEM, &fakeDoer{})
	connectAdapter(t, a)

	body := []byte(`{
		"message_id":"abc","content":"hello chat","created_at":"2026-06-06T11:59:00Z",
		"replies_to":{"message_id":"parent-1"},
		"sender":{"user_id":99,"username":"mod_user","identity":{"badges":[{"type":"moderator"},{"type":"subscriber"}]}}
	}`)
	rec := postWebhook(a, key, t, "chat.message.sent", "n-1", "2026-06-06T11:59:30Z", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d want 202", rec.Code)
	}
	ev := receiveEvent(t, a)
	if ev.Message == nil {
		t.Fatal("expected message payload")
	}
	if ev.Message.UserID != "99" || ev.Message.Username != "mod_user" || ev.Message.Content != "hello chat" {
		t.Fatalf("bad mapping: %+v", ev.Message)
	}
	if !ev.Message.IsModerator || !ev.Message.IsSubscriber {
		t.Fatalf("badge detection failed: %+v", ev.Message)
	}
	if ev.Message.ReplyTo != "parent-1" {
		t.Fatalf("reply mapping failed: %q", ev.Message.ReplyTo)
	}
	if ev.Message.ID != "abc" {
		t.Fatalf("message id mapping failed: %q", ev.Message.ID)
	}
}

func TestWebhookBanNormalization(t *testing.T) {
	key, pubPEM := testKeyPair(t)
	a := newTestAdapter(t, pubPEM, &fakeDoer{})
	connectAdapter(t, a)

	body := []byte(`{
		"moderator":{"username":"themod"},
		"banned_user":{"username":"baddie"},
		"metadata":{"reason":"spam","created_at":"2026-06-06T11:59:00Z","expires_at":null}
	}`)
	rec := postWebhook(a, key, t, "moderation.banned", "b-1", "2026-06-06T11:59:30Z", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d want 202", rec.Code)
	}
	ev := receiveEvent(t, a)
	if ev.Type != adapters.EventUserBanned || ev.UserAction == nil {
		t.Fatalf("expected ban event, got %+v", ev)
	}
	if ev.UserAction.TargetUser != "baddie" || ev.UserAction.Moderator != "themod" || ev.UserAction.Reason != "spam" {
		t.Fatalf("bad ban mapping: %+v", ev.UserAction)
	}
}

func TestWebhookUnknownEventIgnored(t *testing.T) {
	key, pubPEM := testKeyPair(t)
	a := newTestAdapter(t, pubPEM, &fakeDoer{})
	connectAdapter(t, a)

	rec := postWebhook(a, key, t, "livestream.status.updated", "u-1", "2026-06-06T11:59:30Z", []byte(`{}`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("unknown event: got %d want 202", rec.Code)
	}
	if ev, ok := tryReceive(a); ok {
		t.Fatalf("unknown event should not emit, got %q", ev.Type)
	}
}

func TestWebhookNonBlockingWhenChannelFull(t *testing.T) {
	key, pubPEM := testKeyPair(t)
	a := newTestAdapter(t, pubPEM, &fakeDoer{})
	connectAdapter(t, a)

	a.mu.Lock()
	ch := make(chan adapters.Event, 1)
	ch <- adapters.Event{Type: adapters.EventConnected}
	a.events = ch
	a.mu.Unlock()

	body := []byte(`{"message_id":"x","content":"hi","sender":{"user_id":1,"username":"a"}}`)
	done := make(chan int, 1)
	go func() {
		rec := postWebhook(a, key, t, "chat.message.sent", "full-1", "2026-06-06T11:59:30Z", body)
		done <- rec.Code
	}()
	select {
	case code := <-done:
		if code != http.StatusAccepted {
			t.Fatalf("got %d want 202", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler blocked on full channel")
	}
}

func TestDoSendMessage(t *testing.T) {
	doer := &fakeDoer{}
	a := newTestAdapter(t, "", doer)
	connectAdapter(t, a)

	err := a.Do(context.Background(), adapters.Action{
		Type:        adapters.ActionSendMessage,
		SendMessage: &adapters.SendMessageAction{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	last := doer.calls[len(doer.calls)-1]
	if last.method != http.MethodPost || last.path != "/public/v1/chat" {
		t.Fatalf("bad call: %+v", last)
	}
	if !strings.Contains(last.body, `"type":"bot"`) || !strings.Contains(last.body, `"content":"hello"`) {
		t.Fatalf("bad body: %s", last.body)
	}
}

func TestDoSendMessageTooLong(t *testing.T) {
	a := newTestAdapter(t, "", &fakeDoer{})
	connectAdapter(t, a)
	long := strings.Repeat("x", maxContentRunes+1)
	err := a.Do(context.Background(), adapters.Action{
		Type:        adapters.ActionSendMessage,
		SendMessage: &adapters.SendMessageAction{Text: long},
	})
	if !errors.Is(err, ErrContentTooLong) {
		t.Fatalf("got %v want ErrContentTooLong", err)
	}
}

func TestDoSendMessageRateLimited(t *testing.T) {
	doer := &fakeDoer{response: func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusTooManyRequests, "{}"), nil
	}}
	a := newTestAdapter(t, "", doer)
	connectAdapter(t, a)
	err := a.Do(context.Background(), adapters.Action{
		Type:        adapters.ActionSendMessage,
		SendMessage: &adapters.SendMessageAction{Text: "hi"},
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("got %v want ErrRateLimited", err)
	}
}

func TestDoDeleteMessage(t *testing.T) {
	doer := &fakeDoer{response: func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNoContent, ""), nil
	}}
	a := newTestAdapter(t, "", doer)
	connectAdapter(t, a)
	err := a.Do(context.Background(), adapters.Action{
		Type:          adapters.ActionDeleteMessage,
		DeleteMessage: &adapters.DeleteMessageAction{MessageID: "msg-9"},
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	last := doer.calls[len(doer.calls)-1]
	if last.method != http.MethodDelete || last.path != "/public/v1/chat/msg-9" {
		t.Fatalf("bad call: %+v", last)
	}
}

func TestDoBanPermanent(t *testing.T) {
	doer := &fakeDoer{}
	a := newTestAdapter(t, "", doer)
	connectAdapter(t, a)
	err := a.Do(context.Background(), adapters.Action{
		Type: adapters.ActionBan,
		Ban:  &adapters.BanAction{UserID: "555", Reason: "spam"},
	})
	if err != nil {
		t.Fatalf("ban: %v", err)
	}
	last := doer.calls[len(doer.calls)-1]
	if last.method != http.MethodPost || last.path != "/public/v1/moderation/bans" {
		t.Fatalf("bad call: %+v", last)
	}
	if strings.Contains(last.body, "duration") {
		t.Fatalf("permanent ban must omit duration: %s", last.body)
	}
	if !strings.Contains(last.body, `"user_id":555`) {
		t.Fatalf("bad body: %s", last.body)
	}
}

func TestDoTimeoutClamp(t *testing.T) {
	doer := &fakeDoer{}
	a := newTestAdapter(t, "", doer)
	connectAdapter(t, a)
	err := a.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionTimeout,
		Timeout: &adapters.TimeoutAction{UserID: "555", Duration: 100000 * time.Minute},
	})
	if err != nil {
		t.Fatalf("timeout: %v", err)
	}
	last := doer.calls[len(doer.calls)-1]
	if !strings.Contains(last.body, `"duration":10080`) {
		t.Fatalf("duration not clamped to max: %s", last.body)
	}
}

func TestDoTimeoutMinClamp(t *testing.T) {
	doer := &fakeDoer{}
	a := newTestAdapter(t, "", doer)
	connectAdapter(t, a)
	err := a.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionTimeout,
		Timeout: &adapters.TimeoutAction{UserID: "1", Duration: 10 * time.Second},
	})
	if err != nil {
		t.Fatalf("timeout: %v", err)
	}
	last := doer.calls[len(doer.calls)-1]
	if !strings.Contains(last.body, `"duration":1`) {
		t.Fatalf("duration not clamped to min: %s", last.body)
	}
}

func TestDoUntimeout(t *testing.T) {
	doer := &fakeDoer{}
	a := newTestAdapter(t, "", doer)
	connectAdapter(t, a)
	err := a.Do(context.Background(), adapters.Action{
		Type:      adapters.ActionUntimeout,
		Untimeout: &adapters.UntimeoutAction{UserID: "555"},
	})
	if err != nil {
		t.Fatalf("untimeout: %v", err)
	}
	last := doer.calls[len(doer.calls)-1]
	if last.method != http.MethodDelete || last.path != "/public/v1/moderation/bans" {
		t.Fatalf("bad call: %+v", last)
	}
}

func TestDoUnknownAndMissingPayload(t *testing.T) {
	a := newTestAdapter(t, "", &fakeDoer{})
	connectAdapter(t, a)

	if err := a.Do(context.Background(), adapters.Action{Type: "nope"}); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("got %v want ErrUnknownAction", err)
	}
	if err := a.Do(context.Background(), adapters.Action{Type: adapters.ActionSendMessage}); !errors.Is(err, ErrMissingPayload) {
		t.Fatalf("got %v want ErrMissingPayload", err)
	}
}

func TestConnectTwiceAndDisconnectIdempotent(t *testing.T) {
	a := newTestAdapter(t, "", &fakeDoer{})
	if err := a.Connect(context.Background()); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	drainConnected(a)
	if err := a.Connect(context.Background()); !errors.Is(err, ErrAlreadyConnected) {
		t.Fatalf("got %v want ErrAlreadyConnected", err)
	}
	if err := a.Disconnect(context.Background()); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if err := a.Disconnect(context.Background()); err != nil {
		t.Fatalf("second disconnect should be nil, got %v", err)
	}
}

func TestConnectRequiresToken(t *testing.T) {
	a, err := New(Config{PublicKeyPEM: defaultPublicKeyPEM, HTTPClient: &fakeDoer{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Connect(context.Background()); !errors.Is(err, ErrTokenRequired) {
		t.Fatalf("got %v want ErrTokenRequired", err)
	}
}

func TestNewRejectsBadPublicKey(t *testing.T) {
	_, err := New(Config{UserAccessToken: "t", PublicKeyPEM: "not a pem"})
	if !errors.Is(err, ErrPublicKeyInvalid) {
		t.Fatalf("got %v want ErrPublicKeyInvalid", err)
	}
}

func TestSubscriptionReconcile(t *testing.T) {
	listBody, _ := json.Marshal(subscriptionListResponse{
		Data: []struct {
			ID   string `json:"subscription_id"`
			Name string `json:"name"`
		}{
			{ID: "old-1", Name: "chat.message.sent"},
		},
	})
	var posted, deleted bool
	doer := &fakeDoer{response: func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodGet:
			return jsonResponse(http.StatusOK, string(listBody)), nil
		case http.MethodDelete:
			deleted = true
			return jsonResponse(http.StatusOK, "{}"), nil
		case http.MethodPost:
			posted = true
			return jsonResponse(http.StatusOK, "{}"), nil
		}
		return jsonResponse(http.StatusOK, "{}"), nil
	}}
	a := newTestAdapter(t, "", doer)
	if err := a.reconcileSubscriptions(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !deleted || !posted {
		t.Fatalf("expected stale delete and create: deleted=%v posted=%v", deleted, posted)
	}
}

func receiveEvent(t *testing.T, a *Adapter) adapters.Event {
	t.Helper()
	select {
	case ev := <-a.Events():
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return adapters.Event{}
	}
}

func tryReceive(a *Adapter) (adapters.Event, bool) {
	select {
	case ev := <-a.Events():
		return ev, true
	case <-time.After(150 * time.Millisecond):
		return adapters.Event{}, false
	}
}
