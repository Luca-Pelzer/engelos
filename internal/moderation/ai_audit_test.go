package moderation

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/automod"
	"github.com/Luca-Pelzer/engelos/internal/automodstate"
)

func aiAuditSvc(t *testing.T) *Service {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	audit, err := automodstate.OpenSQLiteStore(context.Background(),
		"file:modaiaudit-"+t.Name()+"?mode=memory&cache=shared", logger)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })
	return New(Config{
		Engine:   cleanEngine(t, automod.ModeActive),
		Escal:    automodstate.NewEscalator(automodstate.DefaultTiers(), 24*time.Hour),
		Audit:    audit,
		TenantID: "local",
		Logger:   logger,
	})
}

// TestLogExternal_AuditOnlyCarriesAIFields is the crux of Phase 1.5: a
// low-confidence, audit-only outcome (Kind None, never enforced) must still
// record the AI verdict so the operator can review what the model would have
// done.
func TestLogExternal_AuditOnlyCarriesAIFields(t *testing.T) {
	svc := aiAuditSvc(t)
	ctx := context.Background()
	svc.LogExternal(ctx, Message{Channel: "chan", Username: "v"}, Decision{
		Kind:   ActionNone,
		Reason: "AI[spam]: promo",
		Filter: "contextmod",
		AI:     &AIVerdict{Category: "spam", Severity: 1, Confidence: 0.42, Consulted: true},
	})

	rows, err := svc.AuditList(ctx, "chan", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Action != "none" {
		t.Fatalf("audit-only action should be none, got %q", r.Action)
	}
	if r.AICategory == nil || *r.AICategory != "spam" ||
		r.AISeverity == nil || *r.AISeverity != 1 ||
		r.AIConfidence == nil || *r.AIConfidence != 0.42 ||
		r.AIConsulted == nil || !*r.AIConsulted {
		t.Fatalf("audit-only row must carry all four AI fields: %+v", r)
	}
}

func TestEscalateExternal_CarriesAIFields(t *testing.T) {
	svc := aiAuditSvc(t)
	ctx := context.Background()
	svc.EscalateExternal(ctx, Message{Channel: "chan", Username: "v"},
		&AIVerdict{Category: "threat", Severity: 3, Confidence: 0.97, Consulted: true})

	rows, err := svc.AuditList(ctx, "chan", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.AICategory == nil || *r.AICategory != "threat" || r.AIConsulted == nil || !*r.AIConsulted {
		t.Fatalf("EscalateExternal must carry AI fields: %+v", r)
	}
}

func TestAuditListBySource_ServiceFilters(t *testing.T) {
	svc := aiAuditSvc(t)
	ctx := context.Background()
	// One AI row via LogExternal (forces filter "contextmod").
	svc.LogExternal(ctx, Message{Channel: "chan", Username: "v"}, Decision{
		Kind: ActionDelete, Filter: "contextmod",
		AI: &AIVerdict{Category: "spam", Severity: 1, Confidence: 0.9, Consulted: true},
	})
	// One fast-path row straight into the audit store (no AI verdict).
	if _, err := svc.audit.Log(ctx, automodstate.ModAction{
		TenantID: "local", Channel: "chan", Username: "v", FilterName: "caps", Action: "delete",
	}); err != nil {
		t.Fatalf("seed fast row: %v", err)
	}

	ai, _ := svc.AuditListBySource(ctx, "chan", automodstate.SourceAI, 10)
	if len(ai) != 1 || ai[0].FilterName != "contextmod" {
		t.Fatalf("SourceAI: %+v", ai)
	}
	fast, _ := svc.AuditListBySource(ctx, "chan", automodstate.SourceFast, 10)
	if len(fast) != 1 || fast[0].FilterName != "caps" {
		t.Fatalf("SourceFast: %+v", fast)
	}
}
