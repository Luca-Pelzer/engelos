package actions

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplates_ListsDonationThanks(t *testing.T) {
	found := false
	for _, tmpl := range Templates() {
		if tmpl.ID == "donation-tts-thanks" {
			found = true
		}
	}
	assert.True(t, found, "donation-tts-thanks template must be listed")
}

func TestTemplateByID_DonationThanks(t *testing.T) {
	tmpl, ok := TemplateByID("donation-tts-thanks")
	require.True(t, ok)
	assert.Equal(t, "donation-tts-thanks", tmpl.ID)
	assert.Equal(t, TriggerEvent, tmpl.Rule.TriggerKind)

	var f struct {
		EventType string `json:"event_type"`
	}
	require.NoError(t, json.Unmarshal(tmpl.Rule.TriggerFilter, &f))
	assert.Equal(t, "donation", f.EventType)

	require.Len(t, tmpl.Rule.Actions.Actions, 2)
	assert.Equal(t, "transform:template", tmpl.Rule.Actions.Actions[0].TypeID)
	assert.Equal(t, "tts:speak", tmpl.Rule.Actions.Actions[1].TypeID)
	assert.Contains(t, string(tmpl.Rule.Actions.Actions[0].Config), "$(donation.from)")
	assert.Contains(t, string(tmpl.Rule.Actions.Actions[0].Config), "$(donation.amount)")

	_, ok = TemplateByID("nope")
	assert.False(t, ok)
}

func TestTemplate_RuleValidates(t *testing.T) {
	tmpl, _ := TemplateByID("donation-tts-thanks")
	r := tmpl.Rule
	r.TenantID = "local"
	r.Channel = "chan"
	require.NoError(t, r.validate(), "template rule must pass store validation")
}

func TestTemplateByID_KBAnswer(t *testing.T) {
	tmpl, ok := TemplateByID("kb-answer")
	require.True(t, ok)
	assert.Equal(t, TriggerCommand, tmpl.Rule.TriggerKind)

	var f struct {
		Command string `json:"command"`
	}
	require.NoError(t, json.Unmarshal(tmpl.Rule.TriggerFilter, &f))
	assert.Equal(t, "ask", f.Command)

	require.Len(t, tmpl.Rule.Actions.Actions, 3)
	assert.Equal(t, "kb:lookup", tmpl.Rule.Actions.Actions[0].TypeID)
	assert.Equal(t, "ai:generate", tmpl.Rule.Actions.Actions[1].TypeID)
	assert.Equal(t, "builtin:send-chat", tmpl.Rule.Actions.Actions[2].TypeID)

	// The lookup searches with the command arguments, the AI prompt embeds the
	// retrieved context, and the reply reads the un-shadowed $(answer) output.
	assert.Contains(t, string(tmpl.Rule.Actions.Actions[0].Config), "$(args)")
	assert.Contains(t, string(tmpl.Rule.Actions.Actions[1].Config), "$(kb.context)")
	assert.Contains(t, string(tmpl.Rule.Actions.Actions[1].Config), `"output_key":"answer"`)
	assert.Contains(t, string(tmpl.Rule.Actions.Actions[2].Config), "$(answer)")
}

func TestTemplate_KBAnswerRuleValidates(t *testing.T) {
	tmpl, _ := TemplateByID("kb-answer")
	r := tmpl.Rule
	r.TenantID = "local"
	r.Channel = "chan"
	require.NoError(t, r.validate(), "kb-answer template rule must pass store validation")
}
