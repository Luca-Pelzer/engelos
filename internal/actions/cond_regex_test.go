package actions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegexCondition_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	_, ok := reg.Condition("cond:regex")
	assert.True(t, ok)
}

func TestRegexCondition_MatchesTriggerText(t *testing.T) {
	c := regexCondition{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{Text: "hello world"})

	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"pattern": "^hello"}))
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = c.Evaluate(ec, mustJSON(t, map[string]any{"pattern": "^world"}))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRegexCondition_TargetSubstituted(t *testing.T) {
	c := regexCondition{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{Username: "AdaLovelace"})

	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"pattern": "^Ada", "target": "$(user)"}))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestRegexCondition_CaseInsensitive(t *testing.T) {
	c := regexCondition{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{Text: "HELLO"})

	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"pattern": "hello", "case_insensitive": true}))
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = c.Evaluate(ec, mustJSON(t, map[string]any{"pattern": "hello"}))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRegexCondition_InvalidPatternFailsClosed(t *testing.T) {
	c := regexCondition{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{Text: "x"})

	// An unclosed group is invalid; it must fail closed WITHOUT returning an
	// error (the engine would otherwise log per message).
	for i := 0; i < 3; i++ {
		ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"pattern": "a(b"}))
		require.NoError(t, err)
		assert.False(t, ok)
	}
}

func TestRegexCondition_EmptyPattern(t *testing.T) {
	c := regexCondition{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{Text: "x"})

	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"pattern": "  "}))
	require.NoError(t, err)
	assert.False(t, ok)
}
