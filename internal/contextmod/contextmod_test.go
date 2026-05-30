package contextmod

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeBackend struct {
	out   string
	err   error
	calls int
}

func (f *fakeBackend) Complete(_ context.Context, _, _ string) (string, error) {
	f.calls++
	return f.out, f.err
}

func TestClassify_Verdicts(t *testing.T) {
	cases := map[string]Verdict{
		"ALLOW it is fine":            VerdictAllow,
		"DELETE mild rule break":      VerdictDelete,
		"TIMEOUT slur detected":       VerdictTimeout,
		"allow":                       VerdictAllow,
		"timeout.":                    VerdictTimeout,
		"MAYBE not sure":              VerdictUnknown,
		"DELETE\nsecond line ignored": VerdictDelete,
	}
	for out, want := range cases {
		e := NewEscalator(&fakeBackend{out: out}, Options{})
		d := e.Classify(context.Background(), "no spam", "u1", "borderline text")
		if d.Verdict != want {
			t.Fatalf("out %q: want %v got %v", out, want, d.Verdict)
		}
		if !d.Consulted {
			t.Fatalf("out %q: expected Consulted", out)
		}
	}
}

func TestClassify_EmptyTextOrRulesSkips(t *testing.T) {
	b := &fakeBackend{out: "DELETE"}
	e := NewEscalator(b, Options{})
	if d := e.Classify(context.Background(), "rules", "u", "   "); d.Verdict != VerdictUnknown || d.Consulted {
		t.Fatalf("empty text should skip: %+v", d)
	}
	if d := e.Classify(context.Background(), "  ", "u", "text"); d.Verdict != VerdictUnknown || d.Consulted {
		t.Fatalf("empty rules should skip: %+v", d)
	}
	if b.calls != 0 {
		t.Fatalf("backend must not be called, got %d", b.calls)
	}
}

func TestClassify_BackendErrorFailsOpen(t *testing.T) {
	e := NewEscalator(&fakeBackend{err: errors.New("boom")}, Options{})
	d := e.Classify(context.Background(), "rules", "u", "text")
	if d.Verdict != VerdictUnknown {
		t.Fatalf("backend error should fail open to unknown, got %v", d.Verdict)
	}
}

func TestClassify_GlobalRateLimit(t *testing.T) {
	b := &fakeBackend{out: "DELETE"}
	e := NewEscalator(b, Options{GlobalPerSecond: 1})
	fixed := time.Now()
	e.nowFunc = func() time.Time { return fixed }
	d1 := e.Classify(context.Background(), "rules", "u", "text one")
	d2 := e.Classify(context.Background(), "rules", "u", "text two")
	if !d1.Consulted {
		t.Fatalf("first call should consult backend")
	}
	if d2.Consulted || d2.Verdict != VerdictUnknown {
		t.Fatalf("second call should be rate-limited to unknown: %+v", d2)
	}
}

func TestTimeoutDuration(t *testing.T) {
	e := NewEscalator(&fakeBackend{}, Options{TimeoutSeconds: 120})
	if e.TimeoutDuration() != 120*time.Second {
		t.Fatalf("want 120s, got %v", e.TimeoutDuration())
	}
	def := NewEscalator(&fakeBackend{}, Options{})
	if def.TimeoutDuration() != 600*time.Second {
		t.Fatalf("want default 600s, got %v", def.TimeoutDuration())
	}
}

func TestNewEscalator_NilBackendPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on nil backend")
		}
	}()
	_ = NewEscalator(nil, DefaultOptions())
}
