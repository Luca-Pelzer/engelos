package actions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopIfAction_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	_, ok := reg.Action("flow:stop-if")
	assert.True(t, ok)
}

func TestStopIfAction_StopsOnMatch(t *testing.T) {
	act := stopIfAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})
	ec.SetOutput("http.status", "404")

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"key": "http.status", "op": "eq", "value": "404"}))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Stop)
}

func TestStopIfAction_ContinuesOnNoMatch(t *testing.T) {
	act := stopIfAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})
	ec.SetOutput("http.status", "200")

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"key": "http.status", "op": "eq", "value": "404"}))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.False(t, res.Stop)
}

func TestStopIfAction_NumericComparison(t *testing.T) {
	act := stopIfAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})
	ec.SetOutput("http.status", "503")

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"key": "http.status", "op": "gt", "value": "499"}))
	require.NoError(t, err)
	assert.True(t, res.Stop)
}

func TestStopIfAction_RequiresKey(t *testing.T) {
	act := stopIfAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"op": "eq", "value": "x"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key is required")
}
