package actions

import "encoding/json"

// Template is a pre-built rule a user can instantiate into a channel. Rule holds
// the trigger, conditions and actions; TenantID, Channel and Name are filled in
// when the template is applied.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Rule        Rule   `json:"rule"`
}

// Templates returns the built-in workflow templates in stable id order.
func Templates() []Template {
	return []Template{donationTTSThanks(), kbAnswer()}
}

// TemplateByID returns the built-in template with the given id, if present.
func TemplateByID(id string) (Template, bool) {
	for _, t := range Templates() {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// donationTTSThanks is the flagship template: a Ko-fi donation fires an
// event-kind rule that renders a thank-you naming the donor and amount, then
// speaks it via TTS.
func donationTTSThanks() Template {
	return Template{
		ID:          "donation-tts-thanks",
		Name:        "Donation TTS thank-you",
		Description: "When a Ko-fi donation arrives, speak a thank-you naming the supporter and amount.",
		Rule: Rule{
			Name:          "donation-tts-thanks",
			Enabled:       true,
			TriggerKind:   TriggerEvent,
			TriggerFilter: json.RawMessage(`{"event_type":"donation"}`),
			Conditions:    ConditionList{Mode: ConditionModeAll},
			Actions: ActionList{Actions: []ActionInstance{
				{
					TypeID:  "transform:template",
					Enabled: true,
					Config:  json.RawMessage(`{"template":"Thank you $(donation.from) for $(donation.amount) $(donation.currency)! $(donation.message)","output_key":"thanks"}`),
				},
				{
					TypeID:  "tts:speak",
					Enabled: true,
					Config:  json.RawMessage(`{"text":"$(thanks)"}`),
				},
			}},
		},
	}
}

// kbAnswer wires the retrieval-augmented answer pattern into one rule: the
// "!ask" command searches the channel knowledge base with the viewer's question
// ($(args) is the message minus the command word), feeds the top entries to
// ai:generate as prompt context, and posts the answer. ai:generate mirrors its
// output under "answer" so the send-chat reads $(answer), not the $(text)
// trigger-var shadow.
func kbAnswer() Template {
	return Template{
		ID:          "kb-answer",
		Name:        "AI answer from knowledge base",
		Description: "When a viewer types !ask <question>, search the channel knowledge base and let the AI answer using the matching entries.",
		Rule: Rule{
			Name:          "kb-answer",
			Enabled:       true,
			TriggerKind:   TriggerCommand,
			TriggerFilter: json.RawMessage(`{"command":"ask"}`),
			Conditions:    ConditionList{Mode: ConditionModeAll},
			Actions: ActionList{Actions: []ActionInstance{
				{
					TypeID:  "kb:lookup",
					Enabled: true,
					Config:  json.RawMessage(`{"query":"$(args)","limit":3}`),
				},
				{
					TypeID:  "ai:generate",
					Enabled: true,
					Config:  json.RawMessage(`{"system":"You are the streamer's helpful chat assistant. Answer the viewer's question using ONLY the knowledge base entries provided. If they do not contain the answer, say you are not sure. Keep it to one or two sentences.","prompt":"Knowledge base entries:\n$(kb.context)\n\nViewer question: $(args)\n\nAnswer:","output_key":"answer"}`),
				},
				{
					TypeID:  "builtin:send-chat",
					Enabled: true,
					Config:  json.RawMessage(`{"text":"$(answer)"}`),
				},
			}},
		},
	}
}
