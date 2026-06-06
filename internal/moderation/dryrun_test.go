package moderation

import (
	"context"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/automod"
	"github.com/Luca-Pelzer/engelos/internal/automodstate"
)

func bannedEngine(t *testing.T, mode automod.FilterMode) *automod.Engine {
	t.Helper()
	cfg := automod.DefaultConfig()
	cfg.Mode = mode
	cfg.BannedWords.Enabled = true
	cfg.BannedWords.Entries = []automod.BannedEntry{
		{Phrase: "nuke", MatchMode: automod.MatchAnywhere, Verdict: automod.VerdictDelete},
	}
	e, err := automod.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

func TestDryRunDoesNotMutateEscalationOrPermit(t *testing.T) {
	escal := automodstate.NewEscalator(automodstate.DefaultTiers(), 24*time.Hour)
	permits := automodstate.NewPermitTracker(60 * time.Second)
	svc := New(Config{
		Engine:  bannedEngine(t, automod.ModeDryRun),
		Escal:   escal,
		Permits: permits,
	})

	const ch, user, filter = "chan", "spammer", "banned_words"
	msg := Message{Channel: ch, Username: user, Text: "nuke them all"}

	for i := 0; i < 5; i++ {
		dec := svc.Evaluate(context.Background(), msg)
		if !dec.DryRun {
			t.Fatalf("iteration %d: expected DryRun=true", i)
		}
		if dec.Kind == ActionNone {
			t.Fatalf("iteration %d: violating message should still produce a non-none preview", i)
		}
		if got := escal.Offenses(ch, user, filter); got != 0 {
			t.Fatalf("iteration %d: dry-run mutated Offenses to %d, want 0", i, got)
		}
	}

	permits.Grant(ch, "linker")
	linkMsg := Message{Channel: ch, Username: "linker", Text: "visit https://evil.example.com now"}
	cfg := automod.DefaultConfig()
	cfg.Mode = automod.ModeDryRun
	cfg.Links.Enabled = true
	linkEngine, err := automod.NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine links: %v", err)
	}
	linkSvc := New(Config{Engine: linkEngine, Escal: escal, Permits: permits})
	linkSvc.Evaluate(context.Background(), linkMsg)
	if !permits.Consume(ch, "linker") {
		t.Fatal("dry-run evaluation should NOT have burned the link permit")
	}
}

func TestActiveModeRecordsAfterDryRunPreviews(t *testing.T) {
	escal := automodstate.NewEscalator(automodstate.DefaultTiers(), 24*time.Hour)
	permits := automodstate.NewPermitTracker(60 * time.Second)
	dry := New(Config{Engine: bannedEngine(t, automod.ModeDryRun), Escal: escal, Permits: permits})
	active := New(Config{Engine: bannedEngine(t, automod.ModeActive), Escal: escal, Permits: permits})

	const ch, user, filter = "chan", "user", "banned_words"
	msg := Message{Channel: ch, Username: user, Text: "nuke"}

	for i := 0; i < 3; i++ {
		dry.Evaluate(context.Background(), msg)
	}
	if got := escal.Offenses(ch, user, filter); got != 0 {
		t.Fatalf("after dry-run previews Offenses = %d, want 0", got)
	}

	dec := active.Evaluate(context.Background(), msg)
	if dec.DryRun {
		t.Fatal("active mode decision should not be DryRun")
	}
	if got := escal.Offenses(ch, user, filter); got != 1 {
		t.Fatalf("first active offense should yield Offenses = 1, got %d", got)
	}
}
