package actions

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// captureTTSAction stands in for the integration-provided tts:speak node so the
// donation template can run end-to-end inside the actions package. It records
// the text config it receives, which the engine has already substituted, so the
// test observes exactly what a real TTS node would speak.
type captureTTSAction struct {
	mu   sync.Mutex
	text string
}

func (a *captureTTSAction) Definition() PluginDefinition {
	return PluginDefinition{ID: "tts:speak", Name: "TTS speak (test)", Description: "captures resolved text"}
}

func (a *captureTTSAction) Execute(_ *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(config, &c)
	a.mu.Lock()
	a.text = c.Text
	a.mu.Unlock()
	return &ActionResult{}, nil
}

func (a *captureTTSAction) spoken() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.text
}

// TestE2E_DonationTemplateSpeaksThanks fires the flagship donation-tts-thanks
// template through a real engine: the transform:template node renders the
// thank-you from the donation.* trigger vars into the "thanks" output, then the
// tts:speak node receives that rendered text via $(thanks). It proves the whole
// Ko-fi donation path from event vars to spoken text.
func TestE2E_DonationTemplateSpeaksThanks(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterBuiltins(reg, Services{}); err != nil {
		t.Fatalf("register builtins: %v", err)
	}
	tts := &captureTTSAction{}
	if err := reg.RegisterAction(tts); err != nil {
		t.Fatalf("register tts: %v", err)
	}

	eng, err := New(Config{TenantID: "local", Source: emptySource{}, Registry: reg})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	eng.Start()
	defer eng.Stop()

	tmpl, ok := TemplateByID("donation-tts-thanks")
	if !ok {
		t.Fatal("donation-tts-thanks template not found")
	}
	rule := tmpl.Rule
	rule.TenantID = "local"
	rule.Channel = "streamer1"

	eng.RunRule(rule, Trigger{
		Kind:      TriggerEvent,
		Channel:   "streamer1",
		EventType: "donation",
		Data: map[string]any{
			"donation.from":     "Jo Example",
			"donation.amount":   "5.00",
			"donation.currency": "USD",
			"donation.message":  "gg",
			"donation.kind":     "Donation",
		},
	})

	want := "Thank you Jo Example for 5.00 USD! gg"
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tts.spoken() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("tts did not speak expected text; got %q want %q", tts.spoken(), want)
}
