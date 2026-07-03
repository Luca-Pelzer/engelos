package automodstate

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func strp(s string) *string   { return &s }
func intp(i int) *int         { return &i }
func f64p(f float64) *float64 { return &f }
func boolp(b bool) *bool      { return &b }

// aiAction returns a sampleAction() tagged as an AI-escalation row.
func aiAction(cat string, sev int, conf float64, consulted bool) ModAction {
	a := sampleAction()
	a.FilterName = "contextmod"
	a.AICategory = strp(cat)
	a.AISeverity = intp(sev)
	a.AIConfidence = f64p(conf)
	a.AIConsulted = boolp(consulted)
	return a
}

func TestAudit_AIFieldsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.Log(ctx, aiAction("harassment", 3, 0.92, true)); err != nil {
		t.Fatalf("log ai: %v", err)
	}
	if _, err := s.Log(ctx, sampleAction()); err != nil { // fast-path, no AI fields
		t.Fatalf("log fast: %v", err)
	}

	rows, err := s.List(ctx, "local", "chan", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}

	var ai, fast *ModAction
	for i := range rows {
		if rows[i].FilterName == "contextmod" {
			ai = &rows[i]
		} else {
			fast = &rows[i]
		}
	}
	if ai == nil || fast == nil {
		t.Fatalf("missing ai or fast row: %+v", rows)
	}
	if ai.AICategory == nil || *ai.AICategory != "harassment" {
		t.Fatalf("ai_category: %v", ai.AICategory)
	}
	if ai.AISeverity == nil || *ai.AISeverity != 3 {
		t.Fatalf("ai_severity: %v", ai.AISeverity)
	}
	if ai.AIConfidence == nil || *ai.AIConfidence != 0.92 {
		t.Fatalf("ai_confidence: %v", ai.AIConfidence)
	}
	if ai.AIConsulted == nil || !*ai.AIConsulted {
		t.Fatalf("ai_consulted: %v", ai.AIConsulted)
	}
	if fast.AICategory != nil || fast.AISeverity != nil || fast.AIConfidence != nil || fast.AIConsulted != nil {
		t.Fatalf("fast-path row must leave AI fields nil: %+v", fast)
	}
}

func TestAudit_AIConsultedFalseStillMarksAIRow(t *testing.T) {
	// A cached/pre-filtered AI decision has Consulted=false but is still an AI
	// row (ai_consulted IS NOT NULL), so source=ai must include it.
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.Log(ctx, aiAction("spam", 1, 0.4, false)); err != nil {
		t.Fatalf("log: %v", err)
	}
	rows, err := s.ListBySource(ctx, "local", "chan", SourceAI, 10)
	if err != nil {
		t.Fatalf("list ai: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 AI row, got %d", len(rows))
	}
	if rows[0].AIConsulted == nil || *rows[0].AIConsulted {
		t.Fatalf("ai_consulted should be non-nil false: %v", rows[0].AIConsulted)
	}
}

func TestAudit_ListBySourceFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.Log(ctx, aiAction("hate", 3, 0.95, true)); err != nil {
		t.Fatalf("log ai: %v", err)
	}
	if _, err := s.Log(ctx, sampleAction()); err != nil {
		t.Fatalf("log fast: %v", err)
	}

	all, _ := s.ListBySource(ctx, "local", "chan", SourceAll, 10)
	if len(all) != 2 {
		t.Fatalf("SourceAll: want 2, got %d", len(all))
	}
	ai, _ := s.ListBySource(ctx, "local", "chan", SourceAI, 10)
	if len(ai) != 1 || ai[0].FilterName != "contextmod" {
		t.Fatalf("SourceAI wrong: %+v", ai)
	}
	fast, _ := s.ListBySource(ctx, "local", "chan", SourceFast, 10)
	if len(fast) != 1 || fast[0].FilterName == "contextmod" {
		t.Fatalf("SourceFast wrong: %+v", fast)
	}
}

func TestParseAuditSource(t *testing.T) {
	cases := map[string]AuditSource{"ai": SourceAI, "AI": SourceAI, " fast ": SourceFast, "": SourceAll, "bogus": SourceAll}
	for in, want := range cases {
		if got := ParseAuditSource(in); got != want {
			t.Errorf("ParseAuditSource(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestMigration_AppliesOnExistingPre15DB builds a pre-1.5 database (the 0001
// schema with a legacy row, no AI columns), then opens it through the store so
// the 0002 migration adds the AI columns, and confirms the legacy row and a new
// AI row both read back correctly. It also re-opens to prove the ADD COLUMN
// re-run is tolerated.
func TestMigration_AppliesOnExistingPre15DB(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "audit.db")

	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.ExecContext(ctx, pre15Schema); err != nil {
		t.Fatalf("create pre-1.5 schema: %v", err)
	}
	if _, err := raw.ExecContext(ctx,
		`INSERT INTO automod_audit (id, tenant_id, channel, filter_name, action, created_at)
		 VALUES ('legacy1','local','chan','caps','delete',?)`, time.Now().Unix()); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// First open runs 0001 (idempotent) + 0002 (adds AI columns).
	s0, err := OpenSQLiteStore(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("first migrate on existing db: %v", err)
	}
	_ = s0.Close()

	// Re-open: migrations re-run and the 0002 ADD COLUMN is tolerated.
	s, err := OpenSQLiteStore(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("re-run migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, err := s.Log(ctx, aiAction("threat", 3, 0.99, true)); err != nil {
		t.Fatalf("log ai row after migrate: %v", err)
	}

	rows, err := s.List(ctx, "local", "chan", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (legacy + ai), got %d", len(rows))
	}
	for _, r := range rows {
		if r.ID == "legacy1" {
			if r.AICategory != nil || r.AIConsulted != nil {
				t.Fatalf("legacy row must have nil AI fields: %+v", r)
			}
		} else if r.AICategory == nil || *r.AICategory != "threat" {
			t.Fatalf("migrated AI row missing fields: %+v", r)
		}
	}
}

const pre15Schema = `CREATE TABLE automod_audit (
    id            TEXT    PRIMARY KEY,
    tenant_id     TEXT    NOT NULL,
    channel       TEXT    NOT NULL,
    user_id       TEXT    NOT NULL DEFAULT '',
    username      TEXT    NOT NULL DEFAULT '',
    message_id    TEXT    NOT NULL DEFAULT '',
    message_text  TEXT    NOT NULL DEFAULT '',
    filter_name   TEXT    NOT NULL,
    reason        TEXT    NOT NULL DEFAULT '',
    matched_text  TEXT    NOT NULL DEFAULT '',
    action        TEXT    NOT NULL,
    duration_sec  INTEGER NOT NULL DEFAULT 0,
    dry_run       INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL
);`
