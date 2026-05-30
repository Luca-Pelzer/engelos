package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fixedClock returns a deterministic time for $(time) assertions: a UTC
// instant so the formatted zone is stable across machines.
func fixedClock() func() time.Time {
	t := time.Date(2026, 5, 30, 15, 4, 5, 0, time.UTC)
	return func() time.Time { return t }
}

// zeroRand is a deterministic randIntN stub that always selects the lowest
// index (0), i.e. the first option / the low end of a range.
func zeroRand(n int) int { return 0 }

// depsWith builds expandDeps with a fixed clock, the zero-rand stub, and the
// supplied counter (nil => $(count) is empty).
func depsWith(counter func() (int, bool)) expandDeps {
	return expandDeps{now: fixedClock(), randIntN: zeroRand, counter: counter}
}

func sampleMsg() Message {
	return Message{
		Platform: "twitch",
		Channel:  "#somechan",
		UserID:   "u1",
		Username: "alice",
		Text:     "!cmd hello world",
	}
}

func TestExpandWith_LegacyTokens(t *testing.T) {
	msg := sampleMsg()
	args := []string{"hello", "world"}
	got := expandWith("$user in $channel says $args", msg, args, depsWith(nil))
	// Legacy $channel is verbatim (keeps the leading '#').
	assert.Equal(t, "alice in #somechan says hello world", got)
}

func TestExpandWith_LegacyTokens_NoArgs(t *testing.T) {
	msg := sampleMsg()
	got := expandWith("[$args]", msg, nil, depsWith(nil))
	assert.Equal(t, "[]", got)
}

func TestExpandVariables_PublicSignature(t *testing.T) {
	// The public entry point keeps the legacy signature and uses real deps;
	// $(count) is empty (no counter), legacy + simple vars still resolve.
	msg := sampleMsg()
	got := ExpandVariables("$(user)/$(channel)/$(count)", msg, nil)
	assert.Equal(t, "alice/somechan/", got)
}

func TestResolveVar_User(t *testing.T) {
	got := expandWith("$(user)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "alice", got)
}

func TestResolveVar_Channel_StripsHash(t *testing.T) {
	got := expandWith("$(channel)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "somechan", got)
}

func TestResolveVar_Channel_NoHash(t *testing.T) {
	msg := sampleMsg()
	msg.Channel = "plain"
	got := expandWith("$(channel)", msg, nil, depsWith(nil))
	assert.Equal(t, "plain", got)
}

func TestResolveVar_Args(t *testing.T) {
	got := expandWith("$(args)", sampleMsg(), []string{"a", "b", "c"}, depsWith(nil))
	assert.Equal(t, "a b c", got)
}

func TestResolveVar_ArgsEmpty(t *testing.T) {
	got := expandWith("$(args)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "", got)
}

func TestResolveVar_Touser_FromArg(t *testing.T) {
	got := expandWith("$(touser)", sampleMsg(), []string{"@bob", "rest"}, depsWith(nil))
	assert.Equal(t, "bob", got)
}

func TestResolveVar_Touser_NoAtPrefix(t *testing.T) {
	got := expandWith("$(touser)", sampleMsg(), []string{"bob"}, depsWith(nil))
	assert.Equal(t, "bob", got)
}

func TestResolveVar_Touser_FallbackToUsername(t *testing.T) {
	got := expandWith("$(touser)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "alice", got)
}

func TestResolveVar_NumberedArgs(t *testing.T) {
	args := []string{"first", "second"}
	assert.Equal(t, "first", expandWith("$(1)", sampleMsg(), args, depsWith(nil)))
	assert.Equal(t, "second", expandWith("$(2)", sampleMsg(), args, depsWith(nil)))
	// Out of bounds → empty.
	assert.Equal(t, "", expandWith("$(3)", sampleMsg(), args, depsWith(nil)))
	assert.Equal(t, "", expandWith("$(9)", sampleMsg(), args, depsWith(nil)))
}

func TestResolveVar_NumberedArgs_Combined(t *testing.T) {
	args := []string{"first", "second"}
	got := expandWith("$(1)-$(2)-$(3)", sampleMsg(), args, depsWith(nil))
	assert.Equal(t, "first-second-", got)
}

func TestResolveVar_Count_NoCounter(t *testing.T) {
	got := expandWith("count=$(count)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "count=", got)
}

func TestResolveVar_Count_WithCounter(t *testing.T) {
	deps := depsWith(func() (int, bool) { return 42, true })
	got := expandWith("count=$(count)", sampleMsg(), nil, deps)
	assert.Equal(t, "count=42", got)
}

func TestResolveVar_Count_CounterNotOK(t *testing.T) {
	deps := depsWith(func() (int, bool) { return 0, false })
	got := expandWith("count=$(count)", sampleMsg(), nil, deps)
	assert.Equal(t, "count=", got)
}

func TestResolveVar_Random_LowEnd(t *testing.T) {
	// zeroRand → a + 0 = a (the low end of [a,b]).
	got := expandWith("$(random 5 10)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "5", got)
}

func TestResolveVar_Random_NumberAlias(t *testing.T) {
	got := expandWith("$(random.number 3 9)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "3", got)
}

func TestResolveVar_Random_TopEndViaStub(t *testing.T) {
	// A stub returning n-1 selects the high end; for [5,10] => 5+5 = 10.
	deps := expandDeps{now: fixedClock(), randIntN: func(n int) int { return n - 1 }}
	got := expandWith("$(random 5 10)", sampleMsg(), nil, deps)
	assert.Equal(t, "10", got)
}

func TestResolveVar_Random_Invalid(t *testing.T) {
	assert.Equal(t, "", expandWith("$(random)", sampleMsg(), nil, depsWith(nil)))
	assert.Equal(t, "", expandWith("$(random 5)", sampleMsg(), nil, depsWith(nil)))
	assert.Equal(t, "", expandWith("$(random a b)", sampleMsg(), nil, depsWith(nil)))
	// a > b → empty.
	assert.Equal(t, "", expandWith("$(random 10 5)", sampleMsg(), nil, depsWith(nil)))
	// Equal bounds are valid: [7,7] → 7.
	assert.Equal(t, "7", expandWith("$(random 7 7)", sampleMsg(), nil, depsWith(nil)))
}

func TestResolveVar_RandomPick(t *testing.T) {
	// zeroRand → first option.
	got := expandWith("$(random.pick red green blue)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "red", got)
}

func TestResolveVar_RandomPick_StubSelectsSecond(t *testing.T) {
	deps := expandDeps{now: fixedClock(), randIntN: func(n int) int { return 1 % n }}
	got := expandWith("$(random.pick red green blue)", sampleMsg(), nil, deps)
	assert.Equal(t, "green", got)
}

func TestResolveVar_RandomPick_Empty(t *testing.T) {
	got := expandWith("$(random.pick)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "", got)
}

func TestResolveVar_Math(t *testing.T) {
	cases := map[string]string{
		"$(math 1 + 2 * 3)":  "7",   // precedence
		"$(math (4+5)/3)":    "3",   // parens + truncating division
		"$(math (1+2)*3)":    "9",   // parens override precedence
		"$(math 10 - 4 - 3)": "3",   // left-associative subtraction
		"$(math 7/2)":        "3",   // truncation
		"$(math -5 + 2)":     "-3",  // negative literal
		"$(math 2 * -3)":     "-6",  // unary minus in factor
		"$(math -(3+4))":     "-7",  // unary minus on a group
		"$(math 100)":        "100", // bare integer
		"$(math ((2)))":      "2",   // nested parens
		"$(math 6 / 0)":      "",    // divide by zero
		"$(math 1 +)":        "",    // dangling operator
		"$(math 1 + )":       "",    // dangling operator + space
		"$(math 2 ** 3)":     "",    // unsupported operator
		"$(math abc)":        "",    // non-numeric
		"$(math )":           "",    // empty expression
		"$(math 2 3)":        "",    // trailing garbage
	}
	for in, want := range cases {
		got := expandWith(in, sampleMsg(), nil, depsWith(nil))
		assert.Equalf(t, want, got, "math input %q", in)
	}
}

func TestResolveVar_Time(t *testing.T) {
	got := expandWith("$(time)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "15:04 UTC", got)
}

func TestResolveVar_UnknownAndOutOfScope(t *testing.T) {
	for _, in := range []string{
		"$(bogus)",
		"$(eval alert(1))",
		"$(urlfetch http://example.com)",
		"$(uptime)",
	} {
		got := expandWith("x"+in+"y", sampleMsg(), nil, depsWith(nil))
		assert.Equalf(t, "xy", got, "input %q", in)
	}
}

func TestResolveVar_EmptyInner(t *testing.T) {
	got := expandWith("a$()b", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "ab", got)
}

func TestExpand_Escaping(t *testing.T) {
	// "\$(user)" renders the literal "$(user)" and does NOT expand.
	got := expandWith(`\$(user)`, sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "$(user)", got)
}

func TestExpand_Escaping_MixedWithRealVar(t *testing.T) {
	got := expandWith(`\$(user) is $(user)`, sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "$(user) is alice", got)
}

func TestExpand_NonRecursive(t *testing.T) {
	// $(1) expands to a literal "$(user)" string; that output must NOT be
	// re-scanned/re-expanded (injection guard).
	args := []string{"$(user)"}
	got := expandWith("$(1)", sampleMsg(), args, depsWith(nil))
	assert.Equal(t, "$(user)", got)
}

func TestExpand_NonRecursive_Args(t *testing.T) {
	args := []string{"$(touser)"}
	got := expandWith("[$(args)]", sampleMsg(), args, depsWith(nil))
	assert.Equal(t, "[$(touser)]", got)
}

func TestExpand_EmptyTemplate(t *testing.T) {
	assert.Equal(t, "", expandWith("", sampleMsg(), nil, depsWith(nil)))
	assert.Equal(t, "", ExpandVariables("", sampleMsg(), nil))
}

func TestExpand_UnmatchedParenBestEffort(t *testing.T) {
	// An unmatched "$(" does not panic and is emitted best-effort.
	got := expandWith("a $(user b", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "a $(user b", got)
}

func TestExpand_MultipleVarsSinglePass(t *testing.T) {
	got := expandWith("$(user) -> $(touser): $(args)", sampleMsg(), []string{"@bob"}, depsWith(nil))
	assert.Equal(t, "alice -> bob: @bob", got)
}

func TestExpand_LegacyAndFuncCombined(t *testing.T) {
	// Legacy $user expands first, then $(channel) in the func pass.
	got := expandWith("$user@$(channel)", sampleMsg(), nil, depsWith(nil))
	assert.Equal(t, "alice@somechan", got)
}
