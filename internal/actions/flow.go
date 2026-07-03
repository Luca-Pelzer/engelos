package actions

import (
	"encoding/json"
	"fmt"
	"strings"
)

type stopIfConfig struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type stopIfAction struct{}

func (stopIfAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "flow:stop-if",
		Name:        "Stop if",
		Description: "Halts the remaining actions in the rule when a comparison matches: reads key (e.g. http.status or json.foo) from the accumulated outputs and compares it to value with op eq|ne|gt|lt|contains (numeric when both sides parse). A clean early exit, not an error.",
	}
}

func (stopIfAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c stopIfConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: flow:stop-if config: %w", err)
	}
	if strings.TrimSpace(c.Key) == "" {
		return nil, fmt.Errorf("actions: flow:stop-if: key is required")
	}
	left, _ := resolveVar(ec, strings.TrimSpace(c.Key))
	match, err := compareOp(c.Op, left, c.Value)
	if err != nil {
		return nil, fmt.Errorf("actions: flow:stop-if: %w", err)
	}
	return &ActionResult{Stop: match}, nil
}
