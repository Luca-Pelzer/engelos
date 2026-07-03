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

func cleanEngine(t *testing.T, mode automod.FilterMode) *automod.Engine {
	t.Helper()
	cfg := automod.DefaultConfig()
	cfg.Mode = mode
	e, err := automod.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

func TestEscalateExternalActiveModeEscalatesAndAudits(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	audit, err := automodstate.OpenSQLiteStore(ctx, "file:escext-active?mode=memory&cache=shared", logger)
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })

	escal := automodstate.NewEscalator(automodstate.DefaultTiers(), 24*time.Hour)
	svc := New(Config{
		Engine:   cleanEngine(t, automod.ModeActive),
		Escal:    escal,
		Audit:    audit,
		TenantID: "local",
		Logger:   logger,
	})

	const ch, user = "streamer", "viewer"
	msg := Message{Channel: ch, Username: user, Text: "severe stuff"}

	// DefaultTiers: 1st warn (delete), 2nd 60s timeout, then longer timeouts.
	kind1, dur1, dry1 := svc.EscalateExternal(ctx, msg, nil)
	if dry1 {
		t.Fatal("active mode must not report dry-run")
	}
	if kind1 != ActionDelete || dur1 != 0 {
		t.Fatalf("first offense = (%v,%v), want (ActionDelete,0)", kind1, dur1)
	}

	kind2, dur2, _ := svc.EscalateExternal(ctx, msg, nil)
	if kind2 != ActionTimeout || dur2 != 60*time.Second {
		t.Fatalf("second offense = (%v,%v), want (ActionTimeout,60s)", kind2, dur2)
	}

	if got := svc.Offenses(ch, user, "contextmod"); got != 2 {
		t.Fatalf("Offenses = %d after two active escalations, want 2", got)
	}

	rows, err := svc.AuditList(ctx, ch, 10)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 audit rows, got %d", len(rows))
	}
	if rows[0].FilterName != "contextmod" {
		t.Fatalf("audit FilterName = %q, want contextmod", rows[0].FilterName)
	}
}

func TestEscalateExternalDryRunPeekDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	escal := automodstate.NewEscalator(automodstate.DefaultTiers(), 24*time.Hour)
	svc := New(Config{
		Engine:   cleanEngine(t, automod.ModeDryRun),
		Escal:    escal,
		TenantID: "local",
		Logger:   logger,
	})

	const ch, user = "streamer", "viewer"
	msg := Message{Channel: ch, Username: user, Text: "severe stuff"}

	for i := 0; i < 5; i++ {
		kind, _, dry := svc.EscalateExternal(ctx, msg, nil)
		if !dry {
			t.Fatalf("iteration %d: dry-run must report DryRun=true", i)
		}
		// Peek always previews the SAME first rung because state never mutates.
		if kind != ActionDelete {
			t.Fatalf("iteration %d: dry-run preview = %v, want stable ActionDelete", i, kind)
		}
		if got := escal.Offenses(ch, user, "contextmod"); got != 0 {
			t.Fatalf("iteration %d: dry-run mutated Offenses to %d, want 0", i, got)
		}
	}
}

func TestEscalateExternalNilServiceSafe(t *testing.T) {
	var svc *Service
	kind, dur, dry := svc.EscalateExternal(context.Background(), Message{}, nil)
	if kind != ActionNone || dur != 0 || dry {
		t.Fatalf("nil service = (%v,%v,%v), want (ActionNone,0,false)", kind, dur, dry)
	}
}
