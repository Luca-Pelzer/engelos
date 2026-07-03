package actions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutputCondition_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	_, ok := reg.Condition("cond:output")
	assert.True(t, ok)
}

func TestOutputCondition_NumericCompare(t *testing.T) {
	c := outputCondition{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})
	ec.SetOutput("http.status", "200")

	cases := []struct {
		op, value string
		want      bool
	}{
		{"eq", "200", true},
		{"ne", "500", true},
		{"gt", "199", true},
		{"lt", "500", true},
		{"gt", "200", false},
		{"lt", "200", false},
	}
	for _, tc := range cases {
		ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"key": "http.status", "op": tc.op, "value": tc.value}))
		require.NoError(t, err)
		assert.Equal(t, tc.want, ok, "%s %s", tc.op, tc.value)
	}
}

func TestOutputCondition_StringCompare(t *testing.T) {
	c := outputCondition{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})
	ec.SetOutput("label", "toxic")

	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"key": "label", "op": "eq", "value": "toxic"}))
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = c.Evaluate(ec, mustJSON(t, map[string]any{"key": "label", "op": "contains", "value": "tox"}))
	require.NoError(t, err)
	assert.True(t, ok)

	// "toxic" > "friendly" lexicographically (both non-numeric).
	ok, err = c.Evaluate(ec, mustJSON(t, map[string]any{"key": "label", "op": "gt", "value": "friendly"}))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestOutputCondition_MissingKeyIsEmpty(t *testing.T) {
	c := outputCondition{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"key": "nope", "op": "eq", "value": ""}))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestOutputCondition_RequiresKeyAndKnownOp(t *testing.T) {
	c := outputCondition{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := c.Evaluate(ec, mustJSON(t, map[string]any{"op": "eq", "value": "x"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key is required")

	ec.SetOutput("k", "v")
	_, err = c.Evaluate(ec, mustJSON(t, map[string]any{"key": "k", "op": "matches", "value": "v"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown op")
}
