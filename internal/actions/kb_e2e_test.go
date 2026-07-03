package actions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signalChat records the last Send and signals a channel so an engine test can
// wait for the async action list to finish before asserting.
type signalChat struct {
	done chan sentMessage
}

func (c *signalChat) Send(channel, text string) error {
	c.done <- sentMessage{Channel: channel, Text: text}
	return nil
}

// promptCapturingAI records the prompt it was asked to complete and returns a
// fixed answer, so the e2e test can prove the KB context reached the AI prompt.
type promptCapturingAI struct {
	gotPrompt chan string
	reply     string
}

func (a *promptCapturingAI) Complete(_ context.Context, _, user string) (string, error) {
	a.gotPrompt <- user
	return a.reply, nil
}

// TestEngineE2E_KBAnswerPath drives the full retrieval-augmented answer flow
// through the engine: a `!ask` command with a viewer question runs kb:lookup
// (fed by a fake KB), whose kb.context substitutes into ai:generate's prompt,
// whose answer (mirrored under $(answer) to dodge the $(text) shadow) is posted
// by send-chat. It proves the KB content reaches the AI prompt AND the AI answer
// reaches chat.
func TestEngineE2E_KBAnswerPath(t *testing.T) {
	kb := &fakeKB{hits: []KBHit{
		{Category: "schedule", Title: "Stream days", Content: "Tuesdays and Thursdays at 8pm CET"},
	}}
	ai := &promptCapturingAI{gotPrompt: make(chan string, 1), reply: "I stream Tue/Thu 8pm CET."}
	chat := &signalChat{done: make(chan sentMessage, 1)}

	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{Chat: chat, KB: kb}))
	require.NoError(t, RegisterAINodes(reg, ai))

	tmpl, ok := TemplateByID("kb-answer")
	require.True(t, ok)
	rule := tmpl.Rule
	rule.TenantID = "local"
	rule.Channel = "mychan"

	eng, err := New(Config{
		TenantID: "local",
		Source:   &staticSource{rules: []Rule{rule}},
		Registry: reg,
		Logger:   discardLogger(),
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{
		Kind:     TriggerCommand,
		Platform: "twitch",
		Channel:  "mychan",
		Text:     "!ask when do you stream",
	})

	select {
	case prompt := <-ai.gotPrompt:
		assert.Contains(t, prompt, "Tuesdays and Thursdays at 8pm CET",
			"the KB entry content must be embedded in the AI prompt")
		assert.Contains(t, prompt, "when do you stream",
			"the viewer question ($(args)) must be in the AI prompt")
	case <-time.After(3 * time.Second):
		t.Fatal("ai:generate was not reached within timeout")
	}

	select {
	case msg := <-chat.done:
		assert.Equal(t, "mychan", msg.Channel)
		assert.Equal(t, "I stream Tue/Thu 8pm CET.", msg.Text,
			"send-chat must post the AI answer via $(answer), not the shadowed $(text)")
	case <-time.After(3 * time.Second):
		t.Fatal("send-chat was not reached within timeout")
	}

	// The lookup received the command arguments, not the raw message.
	assert.Equal(t, "when do you stream", kb.lastQuery)
}

// TestEngineE2E_KBAnswerEmptyResult proves the flow degrades gracefully: with no
// KB hits, ai:generate still runs (its prompt just has an empty context block)
// and chat still receives an answer, so a miss never aborts the rule.
func TestEngineE2E_KBAnswerEmptyResult(t *testing.T) {
	ai := &promptCapturingAI{gotPrompt: make(chan string, 1), reply: "Not sure, sorry!"}
	chat := &signalChat{done: make(chan sentMessage, 1)}

	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{Chat: chat, KB: &fakeKB{hits: nil}}))
	require.NoError(t, RegisterAINodes(reg, ai))

	rule := Rule{
		TenantID: "local", Channel: "mychan", Name: "kb-answer", Enabled: true,
		TriggerKind:   TriggerCommand,
		TriggerFilter: json.RawMessage(`{"command":"ask"}`),
		Conditions:    ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{Actions: []ActionInstance{
			{TypeID: "kb:lookup", Enabled: true, Config: json.RawMessage(`{"query":"$(args)"}`)},
			{TypeID: "ai:generate", Enabled: true, Config: json.RawMessage(`{"prompt":"ctx=[$(kb.context)] q=$(args)","output_key":"answer"}`)},
			{TypeID: "builtin:send-chat", Enabled: true, Config: json.RawMessage(`{"text":"$(answer)"}`)},
		}},
	}

	eng, err := New(Config{TenantID: "local", Source: &staticSource{rules: []Rule{rule}}, Registry: reg, Logger: discardLogger()})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerCommand, Channel: "mychan", Text: "!ask anything"})

	select {
	case prompt := <-ai.gotPrompt:
		assert.True(t, strings.Contains(prompt, "ctx=[]"), "empty KB context renders as an empty block")
	case <-time.After(3 * time.Second):
		t.Fatal("ai:generate not reached")
	}
	select {
	case msg := <-chat.done:
		assert.Equal(t, "Not sure, sorry!", msg.Text)
	case <-time.After(3 * time.Second):
		t.Fatal("send-chat not reached")
	}
}
