package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signalTTS records spoken text and signals a channel when Speak is called, so
// an engine test can wait for the async action list to finish before asserting.
type signalTTS struct {
	done chan sentMessage
}

func (s *signalTTS) Speak(channel, text string) {
	s.done <- sentMessage{Channel: channel, Text: text}
}

// TestEngineE2E_HTTPRequestFeedsTTS drives a real rule through the engine:
// `command !test` runs http:request against a local stub, whose status output
// substitutes into a downstream tts:speak. It proves outputs flow between nodes
// end to end - the stub is hit AND the speaker receives the substituted status.
func TestEngineE2E_HTTPRequestFeedsTTS(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	speaker := &signalTTS{done: make(chan sentMessage, 1)}
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	require.NoError(t, RegisterTTSNodes(reg, speaker))

	rule := Rule{
		TenantID:      "local",
		Channel:       "mychan",
		Name:          "test-chain",
		Enabled:       true,
		TriggerKind:   TriggerCommand,
		TriggerFilter: json.RawMessage(`{"command":"test"}`),
		Conditions:    ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "http:request", Enabled: true, Config: mustJSON(t, map[string]any{
					"url": srv.URL, "allow_private": true,
				})},
				{TypeID: "tts:speak", Enabled: true, Config: mustJSON(t, map[string]any{
					"text": "status $(status)",
				})},
			},
		},
	}

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
		Text:     "!test",
	})

	select {
	case msg := <-speaker.done:
		assert.Equal(t, "mychan", msg.Channel)
		assert.Equal(t, "status 200", msg.Text)
	case <-time.After(3 * time.Second):
		t.Fatal("tts:speak was not reached within timeout")
	}
	assert.Equal(t, int64(1), atomic.LoadInt64(&hits), "http stub should be hit exactly once")
}
