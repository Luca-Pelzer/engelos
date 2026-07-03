package actions

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type transformTemplateConfig struct {
	Template   string `json:"template"`
	OutputKey  string `json:"output_key"`
	JSONSource string `json:"json_source"`
	JSONPath   string `json:"json_path"`
}

type transformTemplateAction struct{}

func (transformTemplateAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "transform:template",
		Name:        "Transform / template",
		Description: "Renders a template of $(...) variables (including prior outputs) into output_key (default text). Alternatively, with json_source (an output key holding a JSON string) and json_path (a dot path like user.name or items.0.id), extracts a value from that JSON into output_key.",
	}
}

func (transformTemplateAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c transformTemplateConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: transform:template config: %w", err)
	}
	outKey := strings.TrimSpace(c.OutputKey)
	if outKey == "" {
		outKey = "text"
	}
	if strings.TrimSpace(c.JSONSource) != "" {
		raw, _ := resolveVar(ec, strings.TrimSpace(c.JSONSource))
		val, err := extractJSONPath(raw, c.JSONPath)
		if err != nil {
			return nil, fmt.Errorf("actions: transform:template: %w", err)
		}
		return &ActionResult{Outputs: map[string]any{outKey: val}}, nil
	}
	// Template mode: the engine already substituted $(...) in the config before
	// Execute, so c.Template is the rendered text. Storing it (rather than
	// re-substituting) keeps rendering to exactly one pass, so payload-derived
	// values can never be re-expanded as a second round of variables.
	return &ActionResult{Outputs: map[string]any{outKey: c.Template}}, nil
}

// extractJSONPath parses raw as JSON and walks a dot path of object keys and
// numeric array indices (e.g. user.name, items.0.id), returning the value at
// the path stringified. An empty path returns the whole document; a path that
// does not resolve is an error so a rule can branch on the failure.
func extractJSONPath(raw, path string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("json_source is empty")
	}
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return "", fmt.Errorf("json_source is not valid JSON")
	}
	cur := doc
	path = strings.TrimSpace(path)
	if path == "" {
		return stringifyValue(cur), nil
	}
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return "", fmt.Errorf("json_path: key %q not found", seg)
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return "", fmt.Errorf("json_path: bad array index %q", seg)
			}
			cur = node[idx]
		default:
			return "", fmt.Errorf("json_path: cannot descend into %q", seg)
		}
	}
	return stringifyValue(cur), nil
}
