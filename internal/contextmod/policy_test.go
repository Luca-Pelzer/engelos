package contextmod

import (
	"context"
	"testing"
	"time"
)

func TestParseAIVerdict_ValidJSON(t *testing.T) {
	d := parseAIVerdict(`{"action":"timeout","category":"threat","severity":3,"confidence":0.95,"reason":"threat of violence"}`)
	if d.Verdict != VerdictTimeout || d.Action != VerdictTimeout {
		t.Fatalf("verdict/action: %+v", d)
	}
	if d.Category != CategoryThreat {
		t.Fatalf("category: %v", d.Category)
	}
	if d.Severity != 3 {
		t.Fatalf("severity: %d", d.Severity)
	}
	if d.Confidence != 0.95 {
		t.Fatalf("confidence: %v", d.Confidence)
	}
	if d.Reason != "threat of violence" {
		t.Fatalf("reason: %q", d.Reason)
	}
}

func TestParseAIVerdict_JSONWithProseAndFences(t *testing.T) {
	out := "Sure, here is my verdict:\n```json\n" +
		`{"action":"delete","category":"spam","severity":1,"confidence":0.8,"reason":"promo link"}` +
		"\n```\nHope that helps."
	d := parseAIVerdict(out)
	if d.Verdict != VerdictDelete || d.Category != CategorySpam || d.Severity != 1 {
		t.Fatalf("prose/fence JSON not parsed: %+v", d)
	}
}

func TestParseAIVerdict_BadJSONFallsBackToLegacy(t *testing.T) {
	d := parseAIVerdict(`DELETE mild rule break {invalid: json}`)
	if d.Verdict != VerdictDelete {
		t.Fatalf("bad JSON should fall back to legacy DELETE: %+v", d)
	}
	if d.Category != CategoryOther {
		t.Fatalf("legacy punish should map to CategoryOther: %v", d.Category)
	}
}

func TestParseAIVerdict_PlaintextLegacyPath(t *testing.T) {
	d := parseAIVerdict("DELETE x")
	if d.Verdict != VerdictDelete || d.Action != VerdictDelete {
		t.Fatalf("plaintext DELETE: %+v", d)
	}
	if d.Reason != "x" {
		t.Fatalf("reason: %q", d.Reason)
	}
	if d.Category != CategoryOther {
		t.Fatalf("category: %v", d.Category)
	}

	a := parseAIVerdict("ALLOW it is fine")
	if a.Verdict != VerdictAllow || a.Category != CategoryNone {
		t.Fatalf("plaintext ALLOW: %+v", a)
	}
}

func TestParseAIVerdict_GarbageToUnknown(t *testing.T) {
	d := parseAIVerdict("MAYBE not sure about this one")
	if d.Verdict != VerdictUnknown {
		t.Fatalf("garbage should be unknown: %+v", d)
	}
}

func TestParseAIVerdict_EmptyToUnknown(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t "} {
		d := parseAIVerdict(in)
		if d.Verdict != VerdictUnknown || d.Action != VerdictUnknown {
			t.Fatalf("empty %q should be unknown: %+v", in, d)
		}
	}
}

func TestParseAIVerdict_Clamping(t *testing.T) {
	hi := parseAIVerdict(`{"action":"timeout","category":"hate","severity":9,"confidence":2.5,"reason":"x"}`)
	if hi.Severity != 3 {
		t.Fatalf("severity should clamp to 3: %d", hi.Severity)
	}
	if hi.Confidence != 1 {
		t.Fatalf("confidence should clamp to 1: %v", hi.Confidence)
	}
	lo := parseAIVerdict(`{"action":"delete","category":"hate","severity":-4,"confidence":-0.7,"reason":"x"}`)
	if lo.Severity != 0 {
		t.Fatalf("severity should clamp to 0: %d", lo.Severity)
	}
	if lo.Confidence != 0 {
		t.Fatalf("confidence should clamp to 0: %v", lo.Confidence)
	}
}

func TestParseAIVerdict_UnknownCategoryToOther(t *testing.T) {
	d := parseAIVerdict(`{"action":"delete","category":"weirdcat","severity":1,"confidence":0.7,"reason":"x"}`)
	if d.Category != CategoryOther {
		t.Fatalf("unknown category should map to other: %v", d.Category)
	}
}

func TestParseAIVerdict_MissingActionFallsBack(t *testing.T) {
	d := parseAIVerdict(`{"category":"spam","severity":1,"confidence":0.7}`)
	if d.Verdict != VerdictUnknown {
		t.Fatalf("missing action with no legacy token should be unknown: %+v", d)
	}
}

func TestApplyPolicy_AllowAndUnknown(t *testing.T) {
	cfg := DefaultPolicy()
	for _, v := range []Verdict{VerdictAllow, VerdictUnknown} {
		o := ApplyPolicy(Decision{Verdict: v}, cfg)
		if o.Action != VerdictAllow || o.FeedLadder || o.AuditOnly || o.Timeout != 0 {
			t.Fatalf("%v should yield clean allow: %+v", v, o)
		}
	}
}

func TestApplyPolicy_SpamDeleteNoLadder(t *testing.T) {
	cfg := DefaultPolicy()
	d := Decision{Verdict: VerdictDelete, Action: VerdictDelete, Category: CategorySpam, Severity: 1, Confidence: 0.9, Consulted: true}
	o := ApplyPolicy(d, cfg)
	if o.Action != VerdictDelete || o.FeedLadder || o.Timeout != 0 || o.AuditOnly {
		t.Fatalf("spam should delete without ladder: %+v", o)
	}
}

func TestApplyPolicy_SpamHighSeverityStillNoLadder(t *testing.T) {
	cfg := DefaultPolicy()
	d := Decision{Verdict: VerdictTimeout, Action: VerdictTimeout, Category: CategorySpam, Severity: 3, Confidence: 0.9, Consulted: true}
	o := ApplyPolicy(d, cfg)
	if o.Action != VerdictDelete || o.FeedLadder || o.Timeout != 0 {
		t.Fatalf("spam at any severity should delete without ladder: %+v", o)
	}
}

func TestApplyPolicy_Severity1JokeDeleteNoTimeout(t *testing.T) {
	cfg := DefaultPolicy()
	d := Decision{Verdict: VerdictDelete, Action: VerdictDelete, Category: CategoryOther, Severity: 1, Confidence: 0.9, Consulted: true}
	o := ApplyPolicy(d, cfg)
	if o.Action != VerdictDelete || o.Timeout != 0 || o.FeedLadder || o.AuditOnly {
		t.Fatalf("severity1 joke should delete with no timeout/ladder: %+v", o)
	}
}

func TestApplyPolicy_Severity2TimeoutLadder(t *testing.T) {
	cfg := DefaultPolicy()
	d := Decision{Verdict: VerdictTimeout, Action: VerdictTimeout, Category: CategoryHarassment, Severity: 2, Confidence: 0.9, Consulted: true}
	o := ApplyPolicy(d, cfg)
	if o.Action != VerdictTimeout || o.Timeout != cfg.LowTimeout || !o.FeedLadder {
		t.Fatalf("severity2 should timeout(low)+ladder: %+v", o)
	}
}

func TestApplyPolicy_Severity3TimeoutLadder(t *testing.T) {
	cfg := DefaultPolicy()
	d := Decision{Verdict: VerdictTimeout, Action: VerdictTimeout, Category: CategoryThreat, Severity: 3, Confidence: 0.99, Consulted: true}
	o := ApplyPolicy(d, cfg)
	if o.Action != VerdictTimeout || o.Timeout != cfg.HighTimeout || !o.FeedLadder {
		t.Fatalf("severity3 should timeout(high)+ladder: %+v", o)
	}
}

func TestApplyPolicy_LowConfidencePunishIsAuditOnly(t *testing.T) {
	cfg := DefaultPolicy()
	d := Decision{Verdict: VerdictTimeout, Action: VerdictTimeout, Category: CategoryHate, Severity: 3, Confidence: 0.4, Consulted: true}
	o := ApplyPolicy(d, cfg)
	if !o.AuditOnly || o.Action != VerdictAllow || o.FeedLadder || o.Timeout != 0 {
		t.Fatalf("low-confidence punish should be audit-only allow: %+v", o)
	}
}

func TestApplyPolicy_ReconcileDeleteUpgradedToTimeout(t *testing.T) {
	cfg := DefaultPolicy()
	d := Decision{Verdict: VerdictDelete, Action: VerdictDelete, Category: CategoryHate, Severity: 2, Confidence: 0.9, Consulted: true}
	o := ApplyPolicy(d, cfg)
	if o.Action != VerdictTimeout || o.Timeout != cfg.LowTimeout || !o.FeedLadder {
		t.Fatalf("delete at severity2 should reconcile to timeout: %+v", o)
	}
}

func TestApplyPolicy_ReconcileTimeoutDowngradedToDelete(t *testing.T) {
	cfg := DefaultPolicy()
	d := Decision{Verdict: VerdictTimeout, Action: VerdictTimeout, Category: CategoryOther, Severity: 1, Confidence: 0.9, Consulted: true}
	o := ApplyPolicy(d, cfg)
	if o.Action != VerdictDelete || o.Timeout != 0 || o.FeedLadder {
		t.Fatalf("timeout at severity1 non-severe should reconcile to delete-no-timeout: %+v", o)
	}
}

func TestApplyPolicy_NotConsultedSkipsAuditGate(t *testing.T) {
	cfg := DefaultPolicy()
	d := Decision{Verdict: VerdictTimeout, Action: VerdictTimeout, Category: CategoryThreat, Severity: 3, Confidence: 0.0, Consulted: false}
	o := ApplyPolicy(d, cfg)
	if o.AuditOnly {
		t.Fatalf("not-consulted decision should not be gated as audit-only: %+v", o)
	}
	if o.Action != VerdictTimeout || o.Timeout != cfg.HighTimeout || !o.FeedLadder {
		t.Fatalf("severity3 not-consulted should still timeout(high)+ladder: %+v", o)
	}
}

func TestDefaultPolicy_Values(t *testing.T) {
	cfg := DefaultPolicy()
	if cfg.MinConfidence != 0.6 || cfg.LowTimeout != 60*time.Second || cfg.HighTimeout != 600*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestClassify_JSONPopulatesFields(t *testing.T) {
	out := `{"action":"timeout","category":"hate","severity":3,"confidence":0.92,"reason":"slur"}`
	e := NewEscalator(&fakeBackend{out: out}, Options{})
	d := e.Classify(context.Background(), "no hate", "u1", "borderline")
	if !d.Consulted {
		t.Fatalf("should be consulted")
	}
	if d.Verdict != VerdictTimeout || d.Action != VerdictTimeout || d.Category != CategoryHate || d.Severity != 3 || d.Confidence != 0.92 {
		t.Fatalf("fields not populated: %+v", d)
	}
}

func TestClassify_JSONFeedsPolicy(t *testing.T) {
	out := `{"action":"delete","category":"spam","severity":1,"confidence":0.9,"reason":"promo"}`
	e := NewEscalator(&fakeBackend{out: out}, Options{})
	d := e.Classify(context.Background(), "no spam", "u1", "buy followers at example.com")
	o := ApplyPolicy(d, DefaultPolicy())
	if o.Action != VerdictDelete || o.FeedLadder {
		t.Fatalf("spam end-to-end should delete without ladder: %+v", o)
	}
}
