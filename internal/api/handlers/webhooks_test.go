package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

type fakeWebhookStore struct {
	rule    actions.Rule
	hasRule bool
}

func (f *fakeWebhookStore) Get(_ context.Context, _, channel, name string) (actions.Rule, error) {
	if f.hasRule && channel == f.rule.Channel && name == f.rule.Name {
		return f.rule, nil
	}
	return actions.Rule{}, actions.ErrNotFound
}
func (f *fakeWebhookStore) Create(_ context.Context, r actions.Rule) (actions.Rule, error) {
	return r, nil
}
func (f *fakeWebhookStore) Update(_ context.Context, r actions.Rule) (actions.Rule, error) {
	return r, nil
}
func (f *fakeWebhookStore) Delete(context.Context, string, string, string) error { return nil }
func (f *fakeWebhookStore) List(context.Context, string, string) ([]actions.Rule, error) {
	return nil, nil
}
func (f *fakeWebhookStore) ListEnabled(context.Context, string, string) ([]actions.Rule, error) {
	return nil, nil
}
func (f *fakeWebhookStore) ListTimerRules(context.Context, string) ([]actions.Rule, error) {
	return nil, nil
}
func (f *fakeWebhookStore) ListEventChannels(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (f *fakeWebhookStore) SetEnabled(context.Context, string, string, string, bool) error {
	return nil
}
func (f *fakeWebhookStore) Close() error { return nil }

type recordingRunner struct {
	mu    sync.Mutex
	fired []actions.Trigger
}

func (r *recordingRunner) RunRule(_ actions.Rule, t actions.Trigger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fired = append(r.fired, t)
}

func (r *recordingRunner) calls() []actions.Trigger {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]actions.Trigger(nil), r.fired...)
}

func webhookRule(channel, name, secret string, enabled bool) actions.Rule {
	filter, _ := json.Marshal(map[string]string{"secret": secret})
	return actions.Rule{
		TenantID:      "local",
		Channel:       channel,
		Name:          name,
		Enabled:       enabled,
		TriggerKind:   actions.TriggerWebhook,
		TriggerFilter: filter,
	}
}

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func mountWebhooks(h *Webhooks) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/v1/channels/{channelSlug}/webhooks/{ruleName}", h.Handle)
	return r
}

func postWebhook(srv http.Handler, channel, name string, body []byte, sig string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/"+channel+"/webhooks/"+name, bytes.NewReader(body))
	if sig != "" {
		req.Header.Set(webhookSignatureHead, sig)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

const testSecret = "topsecret-0123456789abcdef0123456789abcdef"

func TestWebhook_ValidSignatureFires(t *testing.T) {
	store := &fakeWebhookStore{rule: webhookRule("chan", "deploy", testSecret, true), hasRule: true}
	runner := &recordingRunner{}
	srv := mountWebhooks(NewWebhooks(store, runner, "local", nil))

	body := []byte(`{"message":"hello world","count":3,"nested":{"a":1}}`)
	rec := postWebhook(srv, "chan", "deploy", body, signBody(testSecret, body))

	require.Equal(t, http.StatusAccepted, rec.Code)
	calls := runner.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, actions.TriggerWebhook, calls[0].Kind)
	assert.Equal(t, "chan", calls[0].Channel)
	assert.Equal(t, "hello world", calls[0].Data["message"])
	assert.Equal(t, "3", calls[0].Data["count"])
	assert.Equal(t, `{"a":1}`, calls[0].Data["nested"])
}

func TestWebhook_BadSignatureRejectedNoFire(t *testing.T) {
	store := &fakeWebhookStore{rule: webhookRule("chan", "deploy", testSecret, true), hasRule: true}
	runner := &recordingRunner{}
	srv := mountWebhooks(NewWebhooks(store, runner, "local", nil))

	body := []byte(`{"x":1}`)
	rec := postWebhook(srv, "chan", "deploy", body, signBody("wrong-secret", body))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, runner.calls())
}

func TestWebhook_MissingSignatureRejected(t *testing.T) {
	store := &fakeWebhookStore{rule: webhookRule("chan", "deploy", testSecret, true), hasRule: true}
	runner := &recordingRunner{}
	srv := mountWebhooks(NewWebhooks(store, runner, "local", nil))

	rec := postWebhook(srv, "chan", "deploy", []byte(`{"x":1}`), "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, runner.calls())
}

func TestWebhook_OversizedBodyRejected(t *testing.T) {
	store := &fakeWebhookStore{rule: webhookRule("chan", "deploy", testSecret, true), hasRule: true}
	runner := &recordingRunner{}
	srv := mountWebhooks(NewWebhooks(store, runner, "local", nil))

	big := bytes.Repeat([]byte("a"), webhookMaxBodyBytes+1)
	rec := postWebhook(srv, "chan", "deploy", big, signBody(testSecret, big))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Empty(t, runner.calls())
}

func TestWebhook_UnknownRuleIs404(t *testing.T) {
	store := &fakeWebhookStore{hasRule: false}
	runner := &recordingRunner{}
	srv := mountWebhooks(NewWebhooks(store, runner, "local", nil))

	rec := postWebhook(srv, "chan", "nope", []byte(`{}`), "deadbeef")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, runner.calls())
}

func TestWebhook_DisabledRuleIs404(t *testing.T) {
	store := &fakeWebhookStore{rule: webhookRule("chan", "deploy", testSecret, false), hasRule: true}
	runner := &recordingRunner{}
	srv := mountWebhooks(NewWebhooks(store, runner, "local", nil))

	body := []byte(`{"x":1}`)
	rec := postWebhook(srv, "chan", "deploy", body, signBody(testSecret, body))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, runner.calls())
}

func TestWebhook_RateLimitedAfterBurst(t *testing.T) {
	store := &fakeWebhookStore{rule: webhookRule("chan", "deploy", testSecret, true), hasRule: true}
	runner := &recordingRunner{}
	h := NewWebhooks(store, runner, "local", nil)
	// Freeze the clock so the bucket never refills mid-test.
	frozen := time.Now()
	h.limiter = newRuleRateLimiter(webhookRatePerSecond, webhookBurst, func() time.Time { return frozen })
	srv := mountWebhooks(h)

	body := []byte(`{"n":1}`)
	sig := signBody(testSecret, body)
	accepted, lastCode := 0, 0
	for i := 0; i < 6; i++ {
		rec := postWebhook(srv, "chan", "deploy", body, sig)
		if rec.Code == http.StatusAccepted {
			accepted++
		}
		lastCode = rec.Code
	}
	assert.Equal(t, 5, accepted, "burst of 5 accepted")
	assert.Equal(t, http.StatusTooManyRequests, lastCode, "6th rapid request rate-limited")
}

func TestWebhook_SecretMaskedInActionsResponse(t *testing.T) {
	store := &fakeWebhookStore{}
	h := NewActions(store, actions.NewRegistry(), nil, nil, "local", nil)

	body := `{"channel":"chan","name":"deploy","enabled":true,"trigger_kind":"webhook","actions":{"actions":[{"type_id":"builtin:log","enabled":true,"config":{"message":"hi"}}]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/actions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	out := rec.Body.String()
	assert.Contains(t, out, "secret_set")
	assert.Contains(t, out, "secret_hint")
	assert.NotContains(t, out, `"secret":`, "raw secret must never appear in an API response")
}

func TestMaskTriggerFilter_HidesWebhookSecret(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"secret": "abcdef0123456789ghijklmnop"})
	masked := string(maskTriggerFilter(actions.TriggerWebhook, raw))
	assert.NotContains(t, masked, "abcdef0123456789ghijklmnop")
	assert.Contains(t, masked, "secret_set")
	assert.Contains(t, masked, "secret_hint")

	ev, _ := json.Marshal(map[string]string{"event_type": "message.created"})
	assert.JSONEq(t, string(ev), string(maskTriggerFilter(actions.TriggerEvent, ev)))
}

func TestEnsureWebhookSecret_GeneratesAndPreserves(t *testing.T) {
	r, err := ensureWebhookSecret(actions.Rule{TriggerKind: actions.TriggerWebhook}, "")
	require.NoError(t, err)
	first := webhookSecretFrom(r.TriggerFilter)
	assert.NotEmpty(t, first)

	r2, err := ensureWebhookSecret(actions.Rule{TriggerKind: actions.TriggerWebhook}, first)
	require.NoError(t, err)
	assert.Equal(t, first, webhookSecretFrom(r2.TriggerFilter), "empty incoming secret preserves the prior one")

	r3, err := ensureWebhookSecret(actions.Rule{TriggerKind: actions.TriggerCommand}, "")
	require.NoError(t, err)
	assert.Empty(t, r3.TriggerFilter, "non-webhook rules are untouched")
}
