package actions

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	summaryValueMaxRunes = 200
	summaryTotalMaxRunes = 400
)

// sensitiveKeyRe matches output/data keys whose VALUES must never be persisted
// in a run summary. Applied case-insensitively as a substring match, so
// api_key, X-Auth-Token, session-cookie, ... are all caught.
var sensitiveKeyRe = regexp.MustCompile(`(?i)secret|token|key|password|authorization|cookie`)

// truncateRunes shortens s to at most n runes, appending an ellipsis when cut.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// summarizeTrigger builds a compact, non-sensitive trigger summary: the kind
// plus the minimum identifying detail (command word, event type, or the webhook
// payload's key NAMES only) - never a full payload dump.
func summarizeTrigger(t Trigger) string {
	var s string
	switch t.Kind {
	case TriggerCommand:
		s = "command:" + firstToken(t.Text)
	case TriggerEvent:
		if strings.TrimSpace(t.EventType) != "" {
			s = "event:" + t.EventType
		} else {
			s = "event"
		}
	case TriggerTimer:
		s = "timer"
	case TriggerManual:
		s = "manual"
	case TriggerWebhook:
		s = "webhook:{" + strings.Join(sortedKeys(t.Data), ",") + "}"
	default:
		s = string(t.Kind)
	}
	return truncateRunes(s, summaryTotalMaxRunes)
}

// summarizeOutputs builds a redacted one-line summary of an action's outputs:
// sensitive-named keys are masked, values are truncated, and http:request is
// special-cased to expose only status, body length and json field NAMES.
func summarizeOutputs(typeID string, outputs map[string]any) string {
	if len(outputs) == 0 {
		return ""
	}
	if typeID == "http:request" {
		return summarizeHTTPOutputs(outputs)
	}
	parts := make([]string, 0, len(outputs))
	for _, k := range sortedKeys(outputs) {
		if sensitiveKeyRe.MatchString(k) {
			parts = append(parts, k+"=<redacted>")
			continue
		}
		parts = append(parts, k+"="+truncateRunes(stringifyValue(outputs[k]), summaryValueMaxRunes))
	}
	return truncateRunes(strings.Join(parts, " "), summaryTotalMaxRunes)
}

// summarizeHTTPOutputs redacts an http:request result to status + body length +
// the NAMES of any extracted json.* fields, never their values, the body, or
// (which the outputs never carry anyway) request headers.
func summarizeHTTPOutputs(outputs map[string]any) string {
	status := stringifyValue(outputs["status"])
	bodyLen := len(stringifyValue(outputs["body"]))
	var jsonKeys []string
	for k := range outputs {
		if strings.HasPrefix(k, "json.") {
			jsonKeys = append(jsonKeys, k)
		}
	}
	sort.Strings(jsonKeys)
	return fmt.Sprintf("status=%s body_len=%d json_keys=[%s]", status, bodyLen, strings.Join(jsonKeys, ","))
}

// sortedKeys returns a map's keys in sorted order for a deterministic summary.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
