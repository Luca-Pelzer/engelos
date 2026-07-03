package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/automod"
	"github.com/Luca-Pelzer/engelos/internal/automodstate"
	"github.com/Luca-Pelzer/engelos/internal/moderation"
)

func newAutoModHandler(t *testing.T) (*AutoMod, *automodstate.AuditStore) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	audit, err := automodstate.OpenSQLiteStore(context.Background(),
		"file:automodhandler-"+t.Name()+"?mode=memory&cache=shared", logger)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })
	engine, err := automod.NewEngine(automod.DefaultConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	svc := moderation.New(moderation.Config{Engine: engine, Audit: audit, TenantID: "local", Logger: logger})
	return NewAutoMod(svc, logger), audit
}

func automodAIRow() automodstate.ModAction {
	cat, sev, conf, cons := "harassment", 2, 0.81, true
	return automodstate.ModAction{
		TenantID: "local", Channel: "chan", Username: "dave", MessageText: "back off",
		FilterName: "contextmod", Reason: "AI[harassment]: targeted insult", Action: "timeout", DurationSec: 60,
		AICategory: &cat, AISeverity: &sev, AIConfidence: &conf, AIConsulted: &cons,
	}
}

func automodFastRow() automodstate.ModAction {
	return automodstate.ModAction{
		TenantID: "local", Channel: "chan", Username: "bob", MessageText: "AAAAA",
		FilterName: "caps", Reason: "excessive caps", Action: "delete",
	}
}

func fetchAuditActions(t *testing.T, h *AutoMod, query string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/automod/audit"+query, nil)
	rec := httptest.NewRecorder()
	h.Audit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Actions []map[string]any `json:"actions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body.Actions
}

func TestAutoMod_AuditIncludesAIFieldsAdditively(t *testing.T) {
	h, audit := newAutoModHandler(t)
	ctx := context.Background()
	if _, err := audit.Log(ctx, automodFastRow()); err != nil {
		t.Fatalf("seed fast: %v", err)
	}
	if _, err := audit.Log(ctx, automodAIRow()); err != nil {
		t.Fatalf("seed ai: %v", err)
	}

	actions := fetchAuditActions(t, h, "?channel=chan&limit=100")
	if len(actions) != 2 {
		t.Fatalf("want 2 actions, got %d", len(actions))
	}

	var ai, fast map[string]any
	for _, a := range actions {
		if a["filter_name"] == "contextmod" {
			ai = a
		} else {
			fast = a
		}
	}
	if ai == nil || fast == nil {
		t.Fatalf("missing ai or fast action: %+v", actions)
	}

	if ai["ai_category"] != "harassment" {
		t.Errorf("ai_category = %v", ai["ai_category"])
	}
	if ai["ai_severity"].(float64) != 2 {
		t.Errorf("ai_severity = %v", ai["ai_severity"])
	}
	if ai["ai_confidence"].(float64) != 0.81 {
		t.Errorf("ai_confidence = %v", ai["ai_confidence"])
	}
	if ai["ai_consulted"] != true {
		t.Errorf("ai_consulted = %v", ai["ai_consulted"])
	}

	// Fast-path row omits the AI fields entirely (additive: existing consumers
	// see the unchanged shape).
	for _, k := range []string{"ai_category", "ai_severity", "ai_confidence", "ai_consulted"} {
		if _, ok := fast[k]; ok {
			t.Errorf("fast-path row must omit %q", k)
		}
	}
	if fast["action"] != "delete" || ai["action"] != "timeout" {
		t.Errorf("existing action field must be unchanged: fast=%v ai=%v", fast["action"], ai["action"])
	}
}

func TestAutoMod_AuditSourceFilter(t *testing.T) {
	h, audit := newAutoModHandler(t)
	ctx := context.Background()
	_, _ = audit.Log(ctx, automodFastRow())
	_, _ = audit.Log(ctx, automodAIRow())

	if aiOnly := fetchAuditActions(t, h, "?channel=chan&source=ai"); len(aiOnly) != 1 || aiOnly[0]["filter_name"] != "contextmod" {
		t.Fatalf("source=ai: %+v", aiOnly)
	}
	if fastOnly := fetchAuditActions(t, h, "?channel=chan&source=fast"); len(fastOnly) != 1 || fastOnly[0]["filter_name"] != "caps" {
		t.Fatalf("source=fast: %+v", fastOnly)
	}
	if all := fetchAuditActions(t, h, "?channel=chan&source=all"); len(all) != 2 {
		t.Fatalf("source=all: want 2, got %d", len(all))
	}
}
