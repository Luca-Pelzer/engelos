package main

import (
	"context"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/contextmod"
	"github.com/Luca-Pelzer/engelos/internal/moderation"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
)

// fakeModSvc records whether EscalateExternal was called and returns a
// configured ladder result, so tests can assert ladder participation.
type fakeModSvc struct {
	dryRun         bool
	ladderAction   moderation.ActionKind
	ladderDur      time.Duration
	escalateCalls  int
	logCalls       int
	lastLog        moderation.Decision
	lastEscalateAI *moderation.AIVerdict
}

func (f *fakeModSvc) Evaluate(ctx context.Context, msg moderation.Message) moderation.Decision {
	return moderation.Decision{Kind: moderation.ActionNone}
}
func (f *fakeModSvc) DryRun() bool { return f.dryRun }
func (f *fakeModSvc) LogExternal(ctx context.Context, msg moderation.Message, dec moderation.Decision) {
	f.logCalls++
	f.lastLog = dec
}
func (f *fakeModSvc) EscalateExternal(ctx context.Context, msg moderation.Message, ai *moderation.AIVerdict) (moderation.ActionKind, time.Duration, bool) {
	f.escalateCalls++
	f.lastEscalateAI = ai
	return f.ladderAction, f.ladderDur, f.dryRun
}

type fakeClassifier struct{ d contextmod.Decision }

func (f fakeClassifier) ClassifyInChannel(ctx context.Context, channel, rules, username, text string) contextmod.Decision {
	return f.d
}

func (f fakeClassifier) Observe(channel, username, text string) {}

type fakeRules struct{ rules string }

func (f fakeRules) rulesFor(ctx context.Context, channel string) string { return f.rules }

func newAdapter(svc *fakeModSvc, d contextmod.Decision) moderationAdapter {
	return moderationAdapter{
		svc:       svc,
		escalator: fakeClassifier{d: d},
		rules:     fakeRules{rules: "no spam"},
	}
}

func evalAI(a moderationAdapter) runtime.ModDecision {
	return a.Evaluate(context.Background(), "chan", "m1", "u1", "viewer", "text",
		0, false, false, false, false, false)
}

func TestAIPolicy_SpamDeleteNoLadder(t *testing.T) {
	svc := &fakeModSvc{}
	a := newAdapter(svc, contextmod.Decision{
		Verdict: contextmod.VerdictDelete, Action: contextmod.VerdictDelete,
		Category: contextmod.CategorySpam, Severity: 1, Confidence: 0.9, Consulted: true,
	})
	dec := evalAI(a)
	if dec.Action != runtime.ModActionDelete || dec.Duration != 0 {
		t.Fatalf("spam = (%v,%v), want (Delete,0)", dec.Action, dec.Duration)
	}
	if svc.escalateCalls != 0 {
		t.Fatalf("spam must NOT feed ladder, got %d EscalateExternal calls", svc.escalateCalls)
	}
	if svc.logCalls != 1 {
		t.Fatalf("expected exactly 1 audit log, got %d", svc.logCalls)
	}
}

func TestAIPolicy_Severity1JokeDeleteNoTimeout(t *testing.T) {
	svc := &fakeModSvc{}
	a := newAdapter(svc, contextmod.Decision{
		Verdict: contextmod.VerdictDelete, Action: contextmod.VerdictDelete,
		Category: contextmod.CategoryOther, Severity: 1, Confidence: 0.9, Consulted: true,
	})
	dec := evalAI(a)
	if dec.Action != runtime.ModActionDelete || dec.Duration != 0 {
		t.Fatalf("severity1 = (%v,%v), want (Delete,0) - jokes must not be timed out", dec.Action, dec.Duration)
	}
	if svc.escalateCalls != 0 {
		t.Fatalf("severity1 must NOT feed ladder, got %d calls", svc.escalateCalls)
	}
}

func TestAIPolicy_Severity2TimeoutWithLadder(t *testing.T) {
	svc := &fakeModSvc{ladderAction: moderation.ActionDelete} // first offence: warn rung -> delete
	a := newAdapter(svc, contextmod.Decision{
		Verdict: contextmod.VerdictTimeout, Action: contextmod.VerdictTimeout,
		Category: contextmod.CategoryHarassment, Severity: 2, Confidence: 0.9, Consulted: true,
	})
	dec := evalAI(a)
	if dec.Action != runtime.ModActionTimeout || dec.Duration != 60*time.Second {
		t.Fatalf("severity2 = (%v,%v), want (Timeout,60s)", dec.Action, dec.Duration)
	}
	if svc.escalateCalls != 1 {
		t.Fatalf("severity2 MUST feed ladder, got %d calls", svc.escalateCalls)
	}
}

func TestAIPolicy_Severity3RepeatEscalatesToBan(t *testing.T) {
	// A repeat offender's ladder rung returns Ban; the merge must escalate the
	// policy timeout up to a ban.
	svc := &fakeModSvc{ladderAction: moderation.ActionBan}
	a := newAdapter(svc, contextmod.Decision{
		Verdict: contextmod.VerdictTimeout, Action: contextmod.VerdictTimeout,
		Category: contextmod.CategoryThreat, Severity: 3, Confidence: 0.95, Consulted: true,
	})
	dec := evalAI(a)
	if dec.Action != runtime.ModActionBan {
		t.Fatalf("severity3 repeat = %v, want Ban via ladder merge", dec.Action)
	}
	if svc.escalateCalls != 1 {
		t.Fatalf("severity3 MUST feed ladder, got %d calls", svc.escalateCalls)
	}
}

func TestAIPolicy_LowConfidenceAuditOnlyPasses(t *testing.T) {
	svc := &fakeModSvc{}
	a := newAdapter(svc, contextmod.Decision{
		Verdict: contextmod.VerdictTimeout, Action: contextmod.VerdictTimeout,
		Category: contextmod.CategoryHarassment, Severity: 3, Confidence: 0.3, Consulted: true,
	})
	dec := evalAI(a)
	if dec.Action != runtime.ModActionNone {
		t.Fatalf("low confidence = %v, want None (pass, audit only)", dec.Action)
	}
	if svc.escalateCalls != 0 {
		t.Fatalf("low confidence must NOT punish/feed ladder, got %d calls", svc.escalateCalls)
	}
	if svc.logCalls != 1 {
		t.Fatalf("low confidence must audit exactly once, got %d", svc.logCalls)
	}
	if svc.lastLog.Kind != moderation.ActionNone {
		t.Fatalf("audit-only row Kind = %v, want None", svc.lastLog.Kind)
	}
}

func TestAIPolicyDecision_MergeRules(t *testing.T) {
	low := contextmod.DefaultPolicy().LowTimeout
	high := contextmod.DefaultPolicy().HighTimeout

	// Spam-style: no ladder, single-shot delete.
	got, fed := aiPolicyDecision(contextmod.PolicyOutcome{Action: contextmod.VerdictDelete}, moderation.ActionBan, time.Hour, false, "r")
	if fed || got.Action != runtime.ModActionDelete || got.Duration != 0 {
		t.Fatalf("no-ladder delete = (%v,%v,fed=%v), want (Delete,0,false)", got.Action, got.Duration, fed)
	}

	// Ladder warn rung must not downgrade the policy timeout.
	got, fed = aiPolicyDecision(contextmod.PolicyOutcome{Action: contextmod.VerdictTimeout, Timeout: low, FeedLadder: true}, moderation.ActionDelete, 0, false, "r")
	if !fed || got.Action != runtime.ModActionTimeout || got.Duration != low {
		t.Fatalf("warn-rung merge = (%v,%v), want (Timeout,%v)", got.Action, got.Duration, low)
	}

	// Longer ladder timeout wins.
	got, _ = aiPolicyDecision(contextmod.PolicyOutcome{Action: contextmod.VerdictTimeout, Timeout: low, FeedLadder: true}, moderation.ActionTimeout, high, false, "r")
	if got.Action != runtime.ModActionTimeout || got.Duration != high {
		t.Fatalf("longer-ladder merge = (%v,%v), want (Timeout,%v)", got.Action, got.Duration, high)
	}

	// Ladder ban escalates above the policy timeout.
	got, _ = aiPolicyDecision(contextmod.PolicyOutcome{Action: contextmod.VerdictTimeout, Timeout: high, FeedLadder: true}, moderation.ActionBan, 0, false, "r")
	if got.Action != runtime.ModActionBan || got.Duration != 0 {
		t.Fatalf("ban merge = (%v,%v), want (Ban,0)", got.Action, got.Duration)
	}
}
