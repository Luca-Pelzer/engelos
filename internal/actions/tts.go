package actions

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TTSSpeaker enqueues spoken text for a channel's TTS overlay. The tts.Service
// satisfies it (fire-and-forget, best-effort). A nil value means TTS is not
// configured and the tts:speak action fails with a clear error.
type TTSSpeaker interface {
	Speak(channel, text string)
}

type ttsSpeakConfig struct {
	Text    string `json:"text"`
	Channel string `json:"channel"`
}

type ttsSpeakAction struct {
	tts TTSSpeaker
}

func (ttsSpeakAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "tts:speak",
		Name:        "Speak (TTS)",
		Description: "Enqueues text to be spoken aloud on the channel's TTS overlay. Text supports $(...) variables; an optional channel overrides the rule's channel.",
	}
}

func (a ttsSpeakAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c ttsSpeakConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: tts:speak config: %w", err)
	}
	text := strings.TrimSpace(c.Text)
	if text == "" {
		return nil, nil
	}
	if a.tts == nil {
		return nil, fmt.Errorf("actions: tts:speak: text-to-speech is not configured")
	}
	channel := strings.TrimSpace(c.Channel)
	if channel == "" {
		channel = ec.Channel
	}
	if channel == "" {
		channel = ec.Trigger.Channel
	}
	a.tts.Speak(channel, text)
	return nil, nil
}
