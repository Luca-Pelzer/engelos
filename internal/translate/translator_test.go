package translate

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBackend records calls and returns a canned translation or error.
type fakeBackend struct {
	mu    sync.Mutex
	calls int32
	out   string
	err   error
	last  string
}

func (f *fakeBackend) Translate(_ context.Context, text, _ string) (string, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	f.last = text
	f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	return f.out, nil
}

func (f *fakeBackend) count() int { return int(atomic.LoadInt32(&f.calls)) }

func newTr(b Backend, o Options) *Translator {
	tr := New(b, o)
	return tr
}

func TestTranslate_HappyPath(t *testing.T) {
	b := &fakeBackend{out: "Hello, how are you?"}
	tr := newTr(b, Options{MinWords: 1, CacheSize: 8})
	res, err := tr.Translate(context.Background(), "u1", "Hola, como estas amigo?", "en")
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.Translated != "Hello, how are you?" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if b.count() != 1 {
		t.Fatalf("want 1 backend call, got %d", b.count())
	}
}

func TestTranslate_SkipsCommandAndShortAndTokens(t *testing.T) {
	b := &fakeBackend{out: "x"}
	tr := newTr(b, Options{MinWords: 2})
	cases := []struct {
		text, reason string
	}{
		{"!help me please", "command"},
		{"123 456 !!!", "no-words"},
		{"hola", "too-short"},
		{"   ", "empty"},
	}
	for _, c := range cases {
		res, err := tr.Translate(context.Background(), "u1", c.text, "en")
		if err != nil {
			t.Fatal(err)
		}
		if !res.Skipped || res.Reason != c.reason {
			t.Fatalf("text %q: want skip reason %q, got %+v", c.text, c.reason, res)
		}
	}
	if b.count() != 0 {
		t.Fatalf("backend must not be called for skips, got %d", b.count())
	}
}

func TestTranslate_SkipsAlreadyTarget(t *testing.T) {
	b := &fakeBackend{out: "should not be used"}
	tr := newTr(b, Options{MinWords: 1})
	// Reliable Russian, target Russian -> skip without backend call.
	res, err := tr.Translate(context.Background(), "u1", "Привет как твои дела сегодня друг", "ru")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || res.Reason != "already-target" {
		t.Fatalf("want already-target skip, got %+v", res)
	}
	if b.count() != 0 {
		t.Fatalf("backend must not be called, got %d", b.count())
	}
}

func TestTranslate_CacheHit(t *testing.T) {
	b := &fakeBackend{out: "Hello friend"}
	tr := newTr(b, Options{MinWords: 1, CacheSize: 16})
	const msg = "Hola amigo querido"
	r1, _ := tr.Translate(context.Background(), "u1", msg, "en")
	r2, _ := tr.Translate(context.Background(), "u2", msg, "en")
	if r1.Cached {
		t.Fatalf("first call should not be cached")
	}
	if !r2.Cached || r2.Translated != "Hello friend" {
		t.Fatalf("second call should be a cache hit: %+v", r2)
	}
	if b.count() != 1 {
		t.Fatalf("want exactly 1 backend call with cache, got %d", b.count())
	}
}

func TestTranslate_NoChangeWhenEchoed(t *testing.T) {
	b := &fakeBackend{out: "Hola amigo querido"} // echoes input
	tr := newTr(b, Options{MinWords: 1, CacheSize: 0})
	res, err := tr.Translate(context.Background(), "u1", "Hola amigo querido", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || res.Reason != "no-change" {
		t.Fatalf("want no-change skip, got %+v", res)
	}
}

func TestTranslate_BackendErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	b := &fakeBackend{err: sentinel}
	tr := newTr(b, Options{MinWords: 1, CacheSize: 0})
	_, err := tr.Translate(context.Background(), "u1", "Hola amigo querido", "en")
	if !errors.Is(err, sentinel) {
		t.Fatalf("want backend error propagated, got %v", err)
	}
}

func TestTranslate_PerUserRateLimit(t *testing.T) {
	b := &fakeBackend{out: "Hi"}
	tr := newTr(b, Options{MinWords: 1, CacheSize: 0, PerUserEvery: time.Hour, PerUserBurst: 1})
	// Distinct messages avoid cache; same user. Burst=1 => second is limited.
	r1, _ := tr.Translate(context.Background(), "u1", "Hola amigo uno", "en")
	r2, _ := tr.Translate(context.Background(), "u1", "Hola amigo dos", "en")
	if r1.Skipped {
		t.Fatalf("first should pass: %+v", r1)
	}
	if !r2.Skipped || r2.Reason != "rate-limited" {
		t.Fatalf("second should be rate-limited: %+v", r2)
	}
	// A different user is independent.
	r3, _ := tr.Translate(context.Background(), "u2", "Hola amigo tres", "en")
	if r3.Skipped {
		t.Fatalf("other user should pass: %+v", r3)
	}
}

func TestTranslate_GlobalRateLimit(t *testing.T) {
	b := &fakeBackend{out: "Hi"}
	tr := newTr(b, Options{MinWords: 1, CacheSize: 0, GlobalPerSecond: 1})
	// Pin time so the bucket does not refill between calls.
	fixed := time.Now()
	tr.nowFunc = func() time.Time { return fixed }
	r1, _ := tr.Translate(context.Background(), "u1", "Hola amigo uno", "en")
	r2, _ := tr.Translate(context.Background(), "u2", "Hola amigo dos", "en")
	if r1.Skipped {
		t.Fatalf("first should pass: %+v", r1)
	}
	if !r2.Skipped || r2.Reason != "rate-limited" {
		t.Fatalf("second should hit global limit: %+v", r2)
	}
}

func TestNew_NilBackendPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on nil backend")
		}
	}()
	_ = New(nil, DefaultOptions())
}

func TestTokenBucket_Refill(t *testing.T) {
	start := time.Now()
	b := newTokenBucket(1, 1) // 1 token/sec, burst 1
	if !b.allow(start) {
		t.Fatal("first token should be available")
	}
	if b.allow(start) {
		t.Fatal("second immediate token should be denied")
	}
	if !b.allow(start.Add(time.Second)) {
		t.Fatal("token should refill after 1s")
	}
}
