package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

type fakeKofiCreds struct {
	token string
	err   error
}

func (f fakeKofiCreds) Get(context.Context, string, string, string) (string, error) {
	return f.token, f.err
}

type fakeKofiChannels struct {
	channels []string
	err      error
}

func (f fakeKofiChannels) ListEventChannels(context.Context, string, string) ([]string, error) {
	return f.channels, f.err
}

type fakeDispatcher struct {
	mu     sync.Mutex
	events []adapters.Event
}

func (f *fakeDispatcher) Dispatch(_ context.Context, ev adapters.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
}

// kofiForm builds the application/x-www-form-urlencoded body Ko-fi posts: a
// single "data" field holding the JSON payload.
func kofiForm(jsonPayload string) string {
	return url.Values{"data": {jsonPayload}}.Encode()
}

func postKofi(t *testing.T, h *Kofi, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/kofi/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Handle(rec, req)
	return rec
}

// realKofiPayload mirrors an actual Ko-fi "Donation" webhook body so the test
// exercises the exact field names Ko-fi sends, not an idealized subset.
const realKofiPayload = `{"verification_token":"tok-secret","message_id":"3a6d8f0e-1111-2222-3333-444455556666","timestamp":"2026-07-02T10:00:00Z","type":"Donation","is_public":true,"from_name":"Jo Example","message":"Great stream!","amount":"5.00","url":"https://ko-fi.com/Home/CoffeeShop?txid=abc","currency":"USD","kofi_transaction_id":"abc"}`

func newKofiHandler(token string, channels []string, disp *fakeDispatcher) *Kofi {
	return NewKofi(
		fakeKofiCreds{token: token},
		fakeKofiChannels{channels: channels},
		disp,
		"local",
		nil,
	)
}

func TestKofiWebhookValidTokenFansOut(t *testing.T) {
	disp := &fakeDispatcher{}
	h := newKofiHandler("tok-secret", []string{"streamer1", "streamer2"}, disp)

	rec := postKofi(t, h, kofiForm(realKofiPayload))

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Len(t, disp.events, 2)
	ev := disp.events[0]
	assert.Equal(t, adapters.EventDonation, ev.Type)
	assert.Equal(t, "kofi", ev.Platform)
	assert.Equal(t, "streamer1", ev.Channel)
	require.NotNil(t, ev.Donation)
	assert.Equal(t, "Jo Example", ev.Donation.From)
	assert.Equal(t, "5.00", ev.Donation.Amount)
	assert.Equal(t, "USD", ev.Donation.Currency)
	assert.Equal(t, "Great stream!", ev.Donation.Message)
	assert.Equal(t, "Donation", ev.Donation.Kind)
	assert.Equal(t, "streamer2", disp.events[1].Channel)
}

func TestKofiWebhookWrongTokenIsUnauthorized(t *testing.T) {
	disp := &fakeDispatcher{}
	h := newKofiHandler("tok-secret", []string{"streamer1"}, disp)

	rec := postKofi(t, h, kofiForm(`{"verification_token":"WRONG","type":"Donation","from_name":"x","amount":"1.00","currency":"USD"}`))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, disp.events)
}

// A token of a different length must also fail: subtle.ConstantTimeCompare
// returns 0 for unequal-length inputs, so this guards the token-verify path.
func TestKofiWebhookDifferentLengthTokenIsUnauthorized(t *testing.T) {
	disp := &fakeDispatcher{}
	h := newKofiHandler("tok-secret", []string{"streamer1"}, disp)

	rec := postKofi(t, h, kofiForm(`{"verification_token":"tok-secret-longer","type":"Donation","from_name":"x","amount":"1.00","currency":"USD"}`))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, disp.events)
}

func TestKofiWebhookMissingTokenIsUnauthorized(t *testing.T) {
	disp := &fakeDispatcher{}
	h := newKofiHandler("tok-secret", []string{"streamer1"}, disp)

	rec := postKofi(t, h, kofiForm(`{"type":"Donation","from_name":"x","amount":"1.00","currency":"USD"}`))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, disp.events)
}

func TestKofiWebhookNoStoredTokenIsUnauthorized(t *testing.T) {
	disp := &fakeDispatcher{}
	h := NewKofi(
		fakeKofiCreds{err: errors.New("not found")},
		fakeKofiChannels{channels: []string{"streamer1"}},
		disp,
		"local",
		nil,
	)

	rec := postKofi(t, h, kofiForm(realKofiPayload))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, disp.events)
}

func TestKofiWebhookEmptyStoredTokenIsUnauthorized(t *testing.T) {
	disp := &fakeDispatcher{}
	h := newKofiHandler("", []string{"streamer1"}, disp)

	rec := postKofi(t, h, kofiForm(realKofiPayload))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, disp.events)
}

func TestKofiWebhookMissingDataFieldIsBadRequest(t *testing.T) {
	disp := &fakeDispatcher{}
	h := newKofiHandler("tok-secret", []string{"streamer1"}, disp)

	rec := postKofi(t, h, "notdata=xyz")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, disp.events)
}

func TestKofiWebhookInvalidJSONIsBadRequest(t *testing.T) {
	disp := &fakeDispatcher{}
	h := newKofiHandler("tok-secret", []string{"streamer1"}, disp)

	rec := postKofi(t, h, kofiForm(`{not json`))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, disp.events)
}

func TestKofiWebhookOversizedBodyIsRejected(t *testing.T) {
	disp := &fakeDispatcher{}
	h := newKofiHandler("tok-secret", []string{"streamer1"}, disp)

	big := "data=" + strings.Repeat("a", webhookMaxBodyBytes+10)
	rec := postKofi(t, h, big)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Empty(t, disp.events)
}

func TestKofiWebhookRateLimited(t *testing.T) {
	disp := &fakeDispatcher{}
	h := newKofiHandler("tok-secret", []string{"streamer1"}, disp)

	var lastCode int
	for i := 0; i < kofiBurst+2; i++ {
		lastCode = postKofi(t, h, kofiForm(realKofiPayload)).Code
	}
	assert.Equal(t, http.StatusTooManyRequests, lastCode)
}

func TestKofiWebhookNilDepsNotImplemented(t *testing.T) {
	h := NewKofi(nil, nil, nil, "local", nil)

	rec := postKofi(t, h, kofiForm(realKofiPayload))

	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}
