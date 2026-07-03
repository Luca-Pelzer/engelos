package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// AICompleter is the single-turn completion surface the ai:* actions need. The
// aibackend manager satisfies it; a nil value disables the AI actions with a
// clear runtime error rather than a silent no-op.
type AICompleter interface {
	Complete(ctx context.Context, systemPrompt, userText string) (string, error)
}

// maxClassifyLabels bounds the label set an ai:classify action may carry so a
// rule cannot balloon the prompt.
const (
	minClassifyLabels = 2
	maxClassifyLabels = 10
)

type aiGenerateConfig struct {
	System string `json:"system"`
	Prompt string `json:"prompt"`

	// OutputKey, when set, mirrors the generated text under an extra output
	// name in addition to the always-present "text". The well-known $(text)
	// variable resolves to the triggering message, not to this output, so a
	// downstream node (e.g. send-chat) that must read the generated answer
	// references it via a distinct key like $(answer).
	OutputKey string `json:"output_key"`
}

type aiGenerateAction struct {
	ai AICompleter
}

func (aiGenerateAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "ai:generate",
		Name:        "AI generate text",
		Description: "Generates text from a prompt via the configured AI backend. The optional system prompt and the prompt support $(...) variables. Outputs text (and a copy under output_key when set, so a reply node can read the answer without the $(text) shadow).",
	}
}

func (a aiGenerateAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c aiGenerateConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: ai:generate config: %w", err)
	}
	prompt := strings.TrimSpace(c.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("actions: ai:generate: prompt is required")
	}
	// AI nodes fail open: an unconfigured or erroring backend yields an empty
	// text output and never aborts the rule's action list.
	if a.ai == nil {
		return &ActionResult{Outputs: aiGenerateOutputs("", c.OutputKey)}, nil
	}
	out, err := a.ai.Complete(ec.Ctx, c.System, prompt)
	if err != nil {
		return &ActionResult{Outputs: aiGenerateOutputs("", c.OutputKey)}, nil
	}
	return &ActionResult{Outputs: aiGenerateOutputs(strings.TrimSpace(out), c.OutputKey)}, nil
}

// aiGenerateOutputs builds the output map: "text" is always present so existing
// rules keep working, and a non-empty, non-"text" outputKey mirrors the same
// value under that name so a rule can dodge the $(text) trigger-var shadow.
func aiGenerateOutputs(text, outputKey string) map[string]any {
	out := map[string]any{"text": text}
	if k := strings.TrimSpace(outputKey); k != "" && k != "text" {
		out[k] = text
	}
	return out
}

type aiClassifyConfig struct {
	Input       string   `json:"input"`
	Labels      []string `json:"labels"`
	Instruction string   `json:"instruction"`
}

type aiClassifyAction struct {
	ai AICompleter
}

func (aiClassifyAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "ai:classify",
		Name:        "AI classify",
		Description: "Classifies the input into one of a fixed label set (2-10 labels) via the AI backend. Input supports $(...); defaults to the triggering message. An optional instruction refines the classifier. Outputs label (the matched label, or empty) and raw.",
	}
}

func (a aiClassifyAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c aiClassifyConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: ai:classify config: %w", err)
	}

	labels := cleanLabels(c.Labels)
	if len(labels) < minClassifyLabels || len(labels) > maxClassifyLabels {
		return nil, fmt.Errorf("actions: ai:classify: need between %d and %d labels, got %d", minClassifyLabels, maxClassifyLabels, len(labels))
	}

	input := strings.TrimSpace(c.Input)
	if input == "" {
		input = strings.TrimSpace(ec.Trigger.Text)
	}
	if input == "" {
		return nil, fmt.Errorf("actions: ai:classify: input is empty")
	}
	// Fail open: an unconfigured or erroring backend yields an empty label
	// (as if the reply matched nothing) and never aborts the action list.
	if a.ai == nil {
		return &ActionResult{Outputs: map[string]any{"label": "", "raw": ""}}, nil
	}

	system := "You are a strict text classifier. Read the user's message and reply with EXACTLY ONE of the following labels, verbatim, and nothing else: " + strings.Join(labels, ", ") + "."
	if instr := strings.TrimSpace(c.Instruction); instr != "" {
		system += " " + instr
	}
	out, err := a.ai.Complete(ec.Ctx, system, input)
	if err != nil {
		return &ActionResult{Outputs: map[string]any{"label": "", "raw": ""}}, nil
	}
	raw := strings.TrimSpace(out)
	return &ActionResult{Outputs: map[string]any{
		"label": matchLabel(raw, labels),
		"raw":   raw,
	}}, nil
}

// cleanLabels trims and de-duplicates the label set, dropping blanks while
// preserving order.
func cleanLabels(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, l := range in {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		key := strings.ToLower(l)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, l)
	}
	return out
}

// matchLabel maps a model reply back onto the canonical label set: an exact
// (case-insensitive) match wins, else the first label contained in the reply.
// It returns "" when nothing matches, so a rule can branch on an unknown answer.
func matchLabel(reply string, labels []string) string {
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), reply) {
			return l
		}
	}
	lowerReply := strings.ToLower(reply)
	for _, l := range labels {
		if ll := strings.ToLower(strings.TrimSpace(l)); ll != "" && strings.Contains(lowerReply, ll) {
			return l
		}
	}
	return ""
}
