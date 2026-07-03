package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAI struct {
	lastSystem string
	lastUser   string
	reply      string
	err        error
}

func (f *fakeAI) Complete(_ context.Context, system, user string) (string, error) {
	f.lastSystem = system
	f.lastUser = user
	if f.err != nil {
		return "", f.err
	}
	return f.reply, nil
}

func TestAIActions_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterAINodes(reg, &fakeAI{}))
	_, ok := reg.Action("ai:generate")
	assert.True(t, ok)
	_, ok = reg.Action("ai:classify")
	assert.True(t, ok)
}

func TestAIGenerate_ReturnsText(t *testing.T) {
	ai := &fakeAI{reply: "  a witty line  "}
	act := aiGenerateAction{ai: ai}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"system": "be funny", "prompt": "greet"}))
	require.NoError(t, err)
	assert.Equal(t, "a witty line", res.Outputs["text"])
	assert.Equal(t, "be funny", ai.lastSystem)
	assert.Equal(t, "greet", ai.lastUser)
}

func TestAIGenerate_OutputKeyMirrorsText(t *testing.T) {
	ai := &fakeAI{reply: "the answer"}
	act := aiGenerateAction{ai: ai}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{Text: "the question"})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"prompt": "p", "output_key": "answer"}))
	require.NoError(t, err)
	// "text" stays for backward compatibility and the custom key mirrors it, so
	// a send-chat can reference $(answer) instead of the shadowed $(text).
	assert.Equal(t, "the answer", res.Outputs["text"])
	assert.Equal(t, "the answer", res.Outputs["answer"])
}

func TestAIGenerate_RequiresPrompt(t *testing.T) {
	act := aiGenerateAction{ai: &fakeAI{reply: "x"}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"prompt": "   "}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

func TestAIGenerate_NilBackendFailsOpen(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterAINodes(reg, nil))
	act, _ := reg.Action("ai:generate")
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"prompt": "hi"}))
	require.NoError(t, err)
	assert.Equal(t, "", res.Outputs["text"])
}

func TestAIGenerate_BackendErrorFailsOpen(t *testing.T) {
	act := aiGenerateAction{ai: &fakeAI{err: errors.New("rate limited")}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"prompt": "hi"}))
	require.NoError(t, err)
	assert.Equal(t, "", res.Outputs["text"])
}

func TestAIClassify_MatchesLabel(t *testing.T) {
	ai := &fakeAI{reply: "Toxic"}
	act := aiClassifyAction{ai: ai}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{
		"input":  "you are the worst",
		"labels": []string{"toxic", "friendly"},
	}))
	require.NoError(t, err)
	assert.Equal(t, "toxic", res.Outputs["label"])
	assert.Equal(t, "Toxic", res.Outputs["raw"])
}

func TestAIClassify_DefaultsToTriggerText(t *testing.T) {
	ai := &fakeAI{reply: "friendly"}
	act := aiClassifyAction{ai: ai}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{Text: "have a nice day"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"labels": []string{"toxic", "friendly"}}))
	require.NoError(t, err)
	assert.Equal(t, "have a nice day", ai.lastUser)
}

func TestAIClassify_UnknownReplyEmptyLabel(t *testing.T) {
	ai := &fakeAI{reply: "no idea"}
	act := aiClassifyAction{ai: ai}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{
		"input":  "hmm",
		"labels": []string{"toxic", "friendly"},
	}))
	require.NoError(t, err)
	assert.Equal(t, "", res.Outputs["label"])
}

func TestAIClassify_BackendErrorFailsOpen(t *testing.T) {
	act := aiClassifyAction{ai: &fakeAI{err: errors.New("timeout")}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{
		"input":  "hi",
		"labels": []string{"toxic", "friendly"},
	}))
	require.NoError(t, err)
	assert.Equal(t, "", res.Outputs["label"])
}

func TestAIClassify_RejectsBadLabelCount(t *testing.T) {
	act := aiClassifyAction{ai: &fakeAI{reply: "x"}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{
		"input":  "hi",
		"labels": []string{"only-one"},
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "between")
}

func TestAIClassify_InstructionRefinesSystemPrompt(t *testing.T) {
	ai := &fakeAI{reply: "toxic"}
	act := aiClassifyAction{ai: ai}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{
		"input":       "hi",
		"labels":      []string{"toxic", "friendly"},
		"instruction": "Treat sarcasm as toxic.",
	}))
	require.NoError(t, err)
	assert.Contains(t, ai.lastSystem, "Treat sarcasm as toxic.")
}
