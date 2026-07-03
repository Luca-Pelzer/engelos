package actions

import (
	"encoding/json"
	"fmt"
	"strings"
)

type outputConditionConfig struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type outputCondition struct{}

func (outputCondition) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "cond:output",
		Name:        "Compare a prior output",
		Description: "Compares a prior step's output (by key, e.g. http.status or json.foo) to a value with op eq|ne|gt|lt|contains; numeric when both sides parse as numbers. NOTE: the engine evaluates all conditions before any action runs, so no action outputs exist yet at condition time - this is for future branch support. To gate mid-chain today, use the flow:stop-if action instead.",
	}
}

func (outputCondition) Evaluate(ec *ExecutionContext, config json.RawMessage) (bool, error) {
	var c outputConditionConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return false, fmt.Errorf("actions: cond:output config: %w", err)
	}
	if strings.TrimSpace(c.Key) == "" {
		return false, fmt.Errorf("actions: cond:output: key is required")
	}
	left, _ := resolveVar(ec, strings.TrimSpace(c.Key))
	ok, err := compareOp(c.Op, left, c.Value)
	if err != nil {
		return false, fmt.Errorf("actions: cond:output: %w", err)
	}
	return ok, nil
}
