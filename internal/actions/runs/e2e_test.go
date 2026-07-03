package runs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

type emptySrc struct{}

func (emptySrc) ListEnabled(context.Context, string, string) ([]actions.Rule, error) {
	return nil, nil
}

// failAction always fails with a fixed message, for exercising the fail path.
type failAction struct{ msg string }

func (failAction) Definition() actions.PluginDefinition {
	return actions.PluginDefinition{ID: "test:fail", Name: "Always fails", Description: "test-only failing action"}
}

func (a failAction) Execute(*actions.ExecutionContext, json.RawMessage) (*actions.ActionResult, error) {
	return nil, errors.New(a.msg)
}

// TestE2E_EngineRecordsPartialRun fires a 3-action rule (transform ok, a
// deliberately failing action, transform writing a secret-named output) through
// a real engine wired to the SQLite run store, then reads the run back: the run
// is partial with 3 action nodes, the failing node carries a truncated error,
// and the secret-named output is masked.
func TestE2E_EngineRecordsPartialRun(t *testing.T) {
	store := newTestStore(t, 500)

	reg := actions.NewRegistry()
	require.NoError(t, actions.RegisterBuiltins(reg, actions.Services{}))
	require.NoError(t, reg.RegisterAction(failAction{msg: strings.Repeat("x", 400)}))

	eng, err := actions.New(actions.Config{
		TenantID: "local",
		Source:   emptySrc{},
		Registry: reg,
		Recorder: store,
		Logger:   discardLogger(),
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	rule := actions.Rule{
		TenantID:    "local",
		Channel:     "chan",
		Name:        "deploy",
		Enabled:     true,
		TriggerKind: actions.TriggerManual,
		Actions: actions.ActionList{Actions: []actions.ActionInstance{
			{TypeID: "transform:template", Enabled: true, Config: raw(`{"template":"hi","output_key":"greeting"}`)},
			{TypeID: "test:fail", Enabled: true, Config: raw(`{}`)},
			{TypeID: "transform:template", Enabled: true, Config: raw(`{"template":"tok","output_key":"api_secret"}`)},
		}},
	}
	eng.RunRule(rule, actions.Trigger{Kind: actions.TriggerManual, Channel: "chan"})

	ctx := context.Background()
	require.Eventually(t, func() bool {
		list, _ := store.ListRuns(ctx, "local", "chan", "deploy", 10)
		return len(list) == 1
	}, 3*time.Second, 10*time.Millisecond)

	list, err := store.ListRuns(ctx, "local", "chan", "deploy", 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, actions.RunStatusPartial, list[0].Status)

	run, err := store.GetRun(ctx, "local", "chan", "deploy", list[0].ID)
	require.NoError(t, err)

	var actionNodes int
	var failNode, secretNode actions.RunNode
	for _, n := range run.Nodes {
		if n.NodeKind == "action" {
			actionNodes++
		}
		if n.Status == "fail" {
			failNode = n
		}
		if strings.Contains(n.OutputSummary, "api_secret") {
			secretNode = n
		}
	}
	assert.Equal(t, 3, actionNodes, "three action entries")
	assert.Equal(t, "test:fail", failNode.TypeID)
	assert.Equal(t, 301, len([]rune(failNode.Error)), "error truncated to 300 runes + ellipsis")
	assert.Contains(t, secretNode.OutputSummary, "api_secret=<redacted>")
	assert.NotContains(t, secretNode.OutputSummary, "tok")
}
