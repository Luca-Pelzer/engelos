package actions

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// StreamStateProvider reports whether a channel is currently live. The host
// wires a concrete provider fed by the platform's stream.online/stream.offline
// events; a nil provider disables cond:stream-state, which then evaluates false
// rather than panicking.
type StreamStateProvider interface {
	IsLive(channel string) bool
}

type streamStateConfig struct {
	State string `json:"state"`
}

type streamStateCondition struct {
	provider StreamStateProvider
}

func (streamStateCondition) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "cond:stream-state",
		Name:        "Stream is live/offline",
		Description: "Passes when the channel's stream matches the configured state (live|offline), tracked from stream.online/stream.offline events.",
	}
}

var streamStateNilOnce sync.Once

func (a streamStateCondition) Evaluate(ec *ExecutionContext, config json.RawMessage) (bool, error) {
	var c streamStateConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return false, fmt.Errorf("actions: cond:stream-state config: %w", err)
	}
	want := strings.ToLower(strings.TrimSpace(c.State))
	if want != "live" && want != "offline" {
		return false, fmt.Errorf("actions: cond:stream-state: state must be live or offline, got %q", c.State)
	}
	if a.provider == nil {
		streamStateNilOnce.Do(func() {
			slog.Default().Warn("actions: cond:stream-state has no live-state provider; evaluating false")
		})
		return false, nil
	}
	channel := ec.Channel
	if channel == "" {
		channel = ec.Trigger.Channel
	}
	live := a.provider.IsLive(channel)
	if want == "live" {
		return live, nil
	}
	return !live, nil
}
