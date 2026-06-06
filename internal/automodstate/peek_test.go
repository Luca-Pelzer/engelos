package automodstate

import (
	"testing"
	"time"
)

func TestPeekMatchesRecordWithoutMutating(t *testing.T) {
	tiers := DefaultTiers()
	e := NewEscalator(tiers, 24*time.Hour)

	const ch, user, filter = "chan", "spammer", "links"

	first := tiers[0]
	for i := 0; i < 10; i++ {
		pa, pd := e.Peek(ch, user, filter)
		if e.Offenses(ch, user, filter) != 0 {
			t.Fatalf("Peek mutated offense count to %d on iteration %d", e.Offenses(ch, user, filter), i)
		}
		if pa != first.Action || pd != first.Timeout {
			t.Fatalf("Peek iteration %d = (%q,%v), want first-offense preview (%q,%v)", i, pa, pd, first.Action, first.Timeout)
		}
	}

	if got := e.Offenses(ch, user, filter); got != 0 {
		t.Fatalf("after 10 Peeks, Offenses = %d, want 0", got)
	}
}

func TestPeekTracksRecordProgression(t *testing.T) {
	tiers := DefaultTiers()
	e := NewEscalator(tiers, 24*time.Hour)

	const ch, user, filter = "chan", "user", "banned_words"

	for i := 0; i < len(tiers)+2; i++ {
		peekAction, peekDur := e.Peek(ch, user, filter)
		recAction, recDur := e.Record(ch, user, filter)
		if peekAction != recAction || peekDur != recDur {
			t.Fatalf("offense %d: Peek=(%q,%v) but Record=(%q,%v)", i+1, peekAction, peekDur, recAction, recDur)
		}
	}
}

func TestPermitConsumeOnce(t *testing.T) {
	p := NewPermitTracker(60 * time.Second)
	p.Grant("chan", "user")

	if !p.Consume("chan", "user") {
		t.Fatal("granted permit should be consumable once")
	}
	if p.Consume("chan", "user") {
		t.Fatal("permit should be gone after a single Consume")
	}
}
