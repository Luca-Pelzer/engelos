package actions

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeKB struct {
	lastTenant   string
	lastChannel  string
	lastQuery    string
	lastCategory string
	lastLimit    int
	hits         []KBHit
	err          error
}

func (f *fakeKB) Search(_ context.Context, tenant, channel, query, category string, limit int) ([]KBHit, error) {
	f.lastTenant = tenant
	f.lastChannel = channel
	f.lastQuery = query
	f.lastCategory = category
	f.lastLimit = limit
	return f.hits, f.err
}

func TestKBLookup_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	_, ok := reg.Action("kb:lookup")
	assert.True(t, ok)
}

func TestKBLookup_RendersContextAndOutputs(t *testing.T) {
	kb := &fakeKB{hits: []KBHit{
		{Category: "schedule", Title: "Stream days", Content: "Tue/Thu 8pm"},
		{Category: "games", Title: "Current game", Content: "ARC Raiders"},
	}}
	act := kbLookupAction{kb: kb}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "kb"), Trigger{Text: "when do you stream"})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"query": "schedule", "limit": 5, "category": "schedule"}))
	require.NoError(t, err)
	assert.Equal(t, 2, res.Outputs["kb.hits"])
	assert.Equal(t, "Stream days", res.Outputs["kb.top_title"])
	ctxStr := res.Outputs["kb.context"].(string)
	assert.Equal(t, "[schedule] Stream days: Tue/Thu 8pm\n[games] Current game: ARC Raiders", ctxStr)

	assert.Equal(t, "schedule", kb.lastQuery)
	assert.Equal(t, "schedule", kb.lastCategory)
	assert.Equal(t, 5, kb.lastLimit)
	assert.Equal(t, "chan", kb.lastChannel)
}

func TestKBLookup_DefaultsQueryToTriggerTextAndLimit(t *testing.T) {
	kb := &fakeKB{hits: []KBHit{{Category: "faq", Title: "t", Content: "c"}}}
	act := kbLookupAction{kb: kb}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "kb"), Trigger{Text: "how do i sub"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{}))
	require.NoError(t, err)
	assert.Equal(t, "how do i sub", kb.lastQuery)
	assert.Equal(t, kbDefaultLimit, kb.lastLimit)
}

func TestKBLookup_LimitClampedToMax(t *testing.T) {
	kb := &fakeKB{}
	act := kbLookupAction{kb: kb}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "kb"), Trigger{Text: "x"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"query": "x", "limit": 999}))
	require.NoError(t, err)
	assert.Equal(t, kbMaxLimit, kb.lastLimit)
}

func TestKBLookup_EmptyResultIsOK(t *testing.T) {
	kb := &fakeKB{hits: nil}
	act := kbLookupAction{kb: kb}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "kb"), Trigger{Text: "unknown"})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"query": "unknown"}))
	require.NoError(t, err)
	assert.Equal(t, 0, res.Outputs["kb.hits"])
	assert.Equal(t, "", res.Outputs["kb.context"])
	assert.Equal(t, "", res.Outputs["kb.top_title"])
}

func TestKBLookup_NilBackendFailsOpen(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	act, _ := reg.Action("kb:lookup")
	ec := newExecutionContext(context.Background(), sampleRule("chan", "kb"), Trigger{Text: "hi"})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"query": "hi"}))
	require.NoError(t, err)
	assert.Equal(t, 0, res.Outputs["kb.hits"])
	assert.Equal(t, "", res.Outputs["kb.context"])
}

func TestKBLookup_EmptyQueryShortCircuits(t *testing.T) {
	kb := &fakeKB{hits: []KBHit{{Title: "x"}}}
	act := kbLookupAction{kb: kb}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "kb"), Trigger{Text: "   "})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"query": "   "}))
	require.NoError(t, err)
	assert.Equal(t, 0, res.Outputs["kb.hits"])
	assert.Empty(t, kb.lastQuery, "search must not run for an empty query")
}

// TestKBLookup_ContextCapped feeds many oversized entries and asserts the
// rendered context stays under the byte cap, truncating on a line boundary so a
// downstream AI prompt cannot be blown up.
func TestKBLookup_ContextCapped(t *testing.T) {
	big := strings.Repeat("x", 2000)
	hits := make([]KBHit, 0, 10)
	for i := 0; i < 10; i++ {
		hits = append(hits, KBHit{Category: "lore", Title: "t", Content: big})
	}
	act := kbLookupAction{kb: &fakeKB{hits: hits}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "kb"), Trigger{Text: "q"})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"query": "q", "limit": 10}))
	require.NoError(t, err)
	ctxStr := res.Outputs["kb.context"].(string)
	assert.LessOrEqual(t, len(ctxStr), kbMaxContextBytes)
	assert.NotContains(t, ctxStr, "\n\n", "truncation happens on a line boundary")
}
