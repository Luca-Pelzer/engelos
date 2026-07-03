package avatar

import "encoding/json"

// Message type discriminators carried in the "type" field of every frame.
const (
	// TypeSpeak carries synthesized speech for the overlay to play. Audio is
	// delivered inline as base64 MP3 (AudioB64) or, in a future transport, by
	// reference (AudioURL).
	TypeSpeak = "speak"
	// TypeExpression selects an avatar pose/expression.
	TypeExpression = "expression"
	// TypeEnd marks the end of a speak directive with the matching ID. The
	// overlay may also derive "end" locally when inline audio finishes; this
	// frame exists so the backend can signal an explicit stop/interrupt.
	TypeEnd = "end"
)

// Message is the JSON envelope broadcast to overlay subscribers. Fields are
// omitted when empty so each directive carries only what it needs; the "type"
// discriminator tells the overlay which fields to read.
//
// This is the stable wire contract the OBS overlay page is built against:
//
//	speak:      {"type":"speak","id":"..","text":"..","voice":"..?","audio_b64":"..?","audio_url":"..?","alignment":..?}
//	expression: {"type":"expression","id":"..","expression":"talking","hold_seconds":0}
//	end:        {"type":"end","id":".."}
type Message struct {
	Type string `json:"type"`
	// ID correlates a speak/end pair (and lets the overlay dedupe replays).
	ID string `json:"id"`

	// Text is the spoken line (speak). Informational; the overlay renders
	// captions from it and never needs it to play audio.
	Text string `json:"text,omitempty"`
	// Voice is the voice id used to synthesize (speak). Informational.
	Voice string `json:"voice,omitempty"`
	// AudioB64 is the base64-encoded MP3 payload (speak).
	AudioB64 string `json:"audio_b64,omitempty"`
	// AudioURL is an out-of-band audio location (speak); unused by the v1
	// inline transport but reserved so the overlay can support both.
	AudioURL string `json:"audio_url,omitempty"`
	// Alignment is an opaque character-timestamp passthrough (speak). The v1
	// synthesis path does not produce it, so it is normally absent; the field
	// exists so the overlay can consume timestamps once the TTS client emits
	// them, with no wire change.
	Alignment json.RawMessage `json:"alignment,omitempty"`

	// Expression is the pose id (expression), one of the values enumerated by
	// the avatar:expression node.
	Expression string `json:"expression,omitempty"`
	// HoldSeconds tells the overlay how long to hold the expression before
	// reverting to idle; 0 means hold until changed (expression).
	HoldSeconds int `json:"hold_seconds,omitempty"`
}
