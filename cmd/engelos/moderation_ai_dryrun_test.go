package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/automod"
	"github.com/Luca-Pelzer/engelos/internal/automodstate"
	"github.com/Luca-Pelzer/engelos/internal/contextmod"
	"github.com/Luca-Pelzer/engelos/internal/moderation"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
)

type fakeContextBackend struct{ reply string }

func (f fakeContextBackend) Complete(ctx context.Context, systemPrompt, userText string) (string, error) {
	return f.reply, nil
}

func TestModerationAdapter_AIVerdictDryRunIsGatedAndAudited(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	audit, err := automodstate.OpenSQLiteStore(ctx, "file:ai-dryrun-test?mode=memory&cache=shared", logger)
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })

	cfg := automod.DefaultConfig()
	cfg.Mode = automod.ModeDryRun
	engine, err := automod.NewEngine(cfg)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	svc := moderation.New(moderation.Config{
		Engine:   engine,
		Audit:    audit,
		TenantID: "local",
		Logger:   logger,
	})

	escal := contextmod.NewEscalator(fakeContextBackend{reply: `{"action":"delete","category":"spam","severity":1,"confidence":0.9,"reason":"off-topic spam"}`}, contextmod.DefaultOptions())
	rules := &contextRulesProvider{envRules: "no spam", logger: logger}

	adapter := moderationAdapter{svc: svc, escalator: escal, rules: rules}

	const channel = "streamer"
	dec := adapter.Evaluate(ctx, channel, "msg-1", "u-1", "viewer", "buy followers cheap",
		0, false, false, false, false, false)

	if dec.Action != runtime.ModActionDelete {
		t.Fatalf("expected AI delete verdict, got action=%v", dec.Action)
	}
	if !dec.DryRun {
		t.Fatal("AI verdict in dry-run mode must carry DryRun=true so the dispatcher gate suppresses enforcement")
	}

	rows, err := svc.AuditList(ctx, channel, 10)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 audit row for the AI verdict, got %d", len(rows))
	}
	row := rows[0]
	if row.FilterName != "contextmod" {
		t.Fatalf("audit row Filter = %q, want %q", row.FilterName, "contextmod")
	}
	if !row.DryRun {
		t.Fatal("audit row should record DryRun=true")
	}
	if row.Action != "delete" {
		t.Fatalf("audit row Action = %q, want %q", row.Action, "delete")
	}
}
