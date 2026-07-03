package contextmod

import (
	"context"
	"testing"
	"time"
)

type countingBackend struct {
	calls int
	out   string
}

func (c *countingBackend) Complete(_ context.Context, _, _ string) (string, error) {
	c.calls++
	return c.out, nil
}

func TestDedup_IdenticalMessageHitsBackendOnce(t *testing.T) {
	b := &countingBackend{out: `{"action":"delete","category":"spam","severity":1,"confidence":0.9,"reason":"ad"}`}
	e := NewEscalator(b, Options{DedupTTLSeconds: 45})

	msg := "check out my channel twitch.tv/spam now"
	d1 := e.Classify(context.Background(), "rules", "u1", msg)
	d2 := e.Classify(context.Background(), "rules", "u2", msg)

	if b.calls != 1 {
		t.Fatalf("expected 1 backend call for identical messages, got %d", b.calls)
	}
	if d1.Verdict != VerdictDelete || d2.Verdict != VerdictDelete {
		t.Fatalf("both should be delete: %v %v", d1.Verdict, d2.Verdict)
	}
	if d1.Consulted != true || d2.Consulted != false {
		t.Fatalf("first should be consulted, second served from cache: %v %v", d1.Consulted, d2.Consulted)
	}
}

func TestDedup_ExpiryReclassifies(t *testing.T) {
	b := &countingBackend{out: `{"action":"delete","category":"spam","severity":1,"confidence":0.9,"reason":"ad"}`}
	e := NewEscalator(b, Options{DedupTTLSeconds: 45})
	base := time.Now()
	e.dedup.nowFunc = func() time.Time { return base }

	msg := "check out my channel twitch.tv/spam now"
	e.Classify(context.Background(), "rules", "u1", msg)
	e.dedup.nowFunc = func() time.Time { return base.Add(60 * time.Second) }
	e.Classify(context.Background(), "rules", "u2", msg)

	if b.calls != 2 {
		t.Fatalf("expected reclassification after TTL expiry, got %d calls", b.calls)
	}
}

func TestDedup_DisabledWhenZero(t *testing.T) {
	b := &countingBackend{out: `{"action":"delete","category":"spam","severity":1,"confidence":0.9,"reason":"ad"}`}
	e := NewEscalator(b, Options{})
	if e.dedup != nil {
		t.Fatal("dedup cache should be nil when DedupTTLSeconds is 0")
	}
	msg := "check out my channel twitch.tv/spam now"
	e.Classify(context.Background(), "rules", "u1", msg)
	e.Classify(context.Background(), "rules", "u2", msg)
	if b.calls != 2 {
		t.Fatalf("no cache: expected 2 calls, got %d", b.calls)
	}
}
