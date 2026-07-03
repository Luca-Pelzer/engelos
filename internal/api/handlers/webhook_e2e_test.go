package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

type emptySource struct{}

func (emptySource) ListEnabled(context.Context, string, string) ([]actions.Rule, error) {
	return nil, nil
}

type signalChat struct{ done chan string }

func (c *signalChat) Send(_, text string) error {
	select {
	case c.done <- text:
	default:
	}
	return nil
}

func rawCfg(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func e2eWebhookRule(t *testing.T) actions.Rule {
	return actions.Rule{
		TenantID:      "local",
		Channel:       "chan",
		Name:          "deploy",
		Enabled:       true,
		TriggerKind:   actions.TriggerWebhook,
		TriggerFilter: rawCfg(t, map[string]string{"secret": testSecret}),
		Conditions: actions.ConditionList{
			Mode: actions.ConditionModeAll,
			Conditions: []actions.ConditionInstance{
				// Use a payload key that does not collide with a well-known
				// trigger variable (message/text/user/... resolve to trigger
				// fields, not webhook payload data).
				{TypeID: "cond:regex", Config: rawCfg(t, map[string]any{"pattern": "released$", "target": "$(release)"})},
			},
		},
		Actions: actions.ActionList{Actions: []actions.ActionInstance{
			{TypeID: "transform:template", Enabled: true, Config: rawCfg(t, map[string]any{"template": "deploy $(release)", "output_key": "rendered"})},
			{TypeID: "builtin:send-chat", Enabled: true, Config: rawCfg(t, map[string]any{"text": "$(rendered)"})},
		}},
	}
}

// TestWebhookE2E_RegexTransformToChat drives the whole trigger stack: a signed
// webhook POST fires a rule whose cond:regex gates on a payload field, whose
// transform:template renders the payload into an output, which the send-chat
// action then posts - proving the webhook, conditions, transform and outputs
// all flow end to end.
func TestWebhookE2E_RegexTransformToChat(t *testing.T) {
	chat := &signalChat{done: make(chan string, 1)}
	reg := actions.NewRegistry()
	require.NoError(t, actions.RegisterBuiltins(reg, actions.Services{Chat: chat}))
	engine, err := actions.New(actions.Config{
		TenantID: "local",
		Source:   emptySource{},
		Registry: reg,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	engine.Start()
	defer engine.Stop()

	store := &fakeWebhookStore{rule: e2eWebhookRule(t), hasRule: true}
	srv := mountWebhooks(NewWebhooks(store, engine, "local", nil))

	body := []byte(`{"release":"v1.2.3 released"}`)
	rec := postWebhook(srv, "chan", "deploy", body, signBody(testSecret, body))
	require.Equal(t, http.StatusAccepted, rec.Code)

	select {
	case got := <-chat.done:
		assert.Equal(t, "deploy v1.2.3 released", got)
	case <-time.After(3 * time.Second):
		t.Fatal("chat sender never received the rendered text")
	}
}

// TestWebhookE2E_RegexGatesNonMatching proves the cond:regex actually gates: a
// payload that fails the pattern accepts the webhook (202) but fires no chat.
func TestWebhookE2E_RegexGatesNonMatching(t *testing.T) {
	chat := &signalChat{done: make(chan string, 1)}
	reg := actions.NewRegistry()
	require.NoError(t, actions.RegisterBuiltins(reg, actions.Services{Chat: chat}))
	engine, err := actions.New(actions.Config{
		TenantID: "local",
		Source:   emptySource{},
		Registry: reg,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	engine.Start()
	defer engine.Stop()

	store := &fakeWebhookStore{rule: e2eWebhookRule(t), hasRule: true}
	srv := mountWebhooks(NewWebhooks(store, engine, "local", nil))

	body := []byte(`{"release":"just chatting"}`)
	rec := postWebhook(srv, "chan", "deploy", body, signBody(testSecret, body))
	require.Equal(t, http.StatusAccepted, rec.Code)

	select {
	case got := <-chat.done:
		t.Fatalf("chat must not fire when the regex condition fails, got %q", got)
	case <-time.After(300 * time.Millisecond):
	}
}
