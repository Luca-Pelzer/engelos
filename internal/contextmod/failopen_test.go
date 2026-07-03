package contextmod

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestFailOpenInvariant is the structural regression guard for the AI-Mod
// fail-open contract (target-arch §3, carried from review t_7b16fe6e):
// a backend/provider outage MUST surface as VerdictUnknown and must NEVER
// fail-closed into auto-actioning. ApplyPolicy on a VerdictUnknown decision
// must produce a clean allow with no timeout, no ladder, no audit gate.
//
// This test exists so the SC5 AI-Mod pillar consolidation (which unifies the
// /automod and /contextmod surfaces into one area) cannot accidentally couple
// the two pages in a way that breaks the fail-open guarantee.
func TestFailOpenInvariant(t *testing.T) {
	cases := []struct {
		name string
		// escalator inputs
		opts    Options
		backend *fakeBackend
	}{
		{
			name:    "backend returns error",
			opts:    Options{},
			backend: &fakeBackend{err: errors.New("provider 503")},
		},
		{
			name:    "backend returns empty output",
			opts:    Options{},
			backend: &fakeBackend{out: ""},
		},
		{
			name:    "backend returns unparseable garbage",
			opts:    Options{},
			backend: &fakeBackend{out: "I am not sure about this one"},
		},
		{
			name:    "rate-limited (no token available)",
			opts:    Options{GlobalPerSecond: 1},
			backend: &fakeBackend{out: "DELETE"},
		},
	}

	cfg := DefaultPolicy()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := NewEscalator(c.backend, c.opts)
			if c.name == "rate-limited (no token available)" {
				// Pin the clock so the bucket starts full, consume the only
				// token on the first call, then the second must be denied.
				fixed := time.Now()
				e.nowFunc = func() time.Time { return fixed }
				first := e.Classify(context.Background(), "rules", "u", "first judgeable message")
				if !first.Consulted {
					t.Fatalf("setup: first call should have consumed the token: %+v", first)
				}
			}

			d := e.Classify(context.Background(), "no slurs", "u1", "a borderline message worth judging")

			// 1. The decision must be VerdictUnknown — never a punish verdict.
			if d.Verdict != VerdictUnknown {
				t.Fatalf("fail-open violated: %s yielded verdict %q, want %q (must not fail-closed into actioning)",
					c.name, d.Verdict, VerdictUnknown)
			}

			// 2. The policy layer must turn VerdictUnknown into a clean allow
			//    with no enforcement of any kind.
			o := ApplyPolicy(d, cfg)
			if o.Action != VerdictAllow {
				t.Fatalf("fail-open violated: %s ApplyPolicy action %q, want allow", c.name, o.Action)
			}
			if o.Timeout != 0 {
				t.Fatalf("fail-open violated: %s produced timeout %v, want 0", c.name, o.Timeout)
			}
			if o.FeedLadder {
				t.Fatalf("fail-open violated: %s fed the escalation ladder", c.name)
			}
			if o.AuditOnly {
				t.Fatalf("fail-open violated: %s gated as audit-only (Unknown must be a clean allow, not audit-gated)", c.name)
			}
		})
	}
}

// TestClassifyToPolicy_LowConfidenceIsAuditOnly closes the end-to-end gap left
// by TestFailOpenInvariant (which covers outage/garbage at the verdict layer):
// a *parseable* punish verdict that lands below the confidence floor must,
// after the full Classify -> ApplyPolicy path, be recorded audit-only and never
// enforced. This is the shadow-pass guarantee that an unsure AI judgement
// cannot auto-timeout or auto-ban a viewer before trustworthy data exists.
func TestClassifyToPolicy_LowConfidenceIsAuditOnly(t *testing.T) {
	out := `{"action":"timeout","category":"harassment","severity":2,"confidence":0.30,"reason":"maybe rude"}`
	e := NewEscalator(&fakeBackend{out: out}, Options{})

	d := e.Classify(context.Background(), "be respectful", "viewer", "a borderline message worth judging")
	if !d.Consulted {
		t.Fatalf("a parseable verdict should be marked consulted: %+v", d)
	}

	o := ApplyPolicy(d, DefaultPolicy())
	if o.Action != VerdictAllow {
		t.Fatalf("low-confidence punish must not enforce, got action %q", o.Action)
	}
	if !o.AuditOnly {
		t.Fatalf("low-confidence punish must be recorded audit-only, got %+v", o)
	}
	if o.Timeout != 0 || o.FeedLadder {
		t.Fatalf("low-confidence punish must not time out or feed the ladder: %+v", o)
	}
}
