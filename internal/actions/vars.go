package actions

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// varPattern matches a single substitution token of the form $(name). The name
// is restricted to a tight identifier charset so a token can never smuggle
// regex, JSON or shell metacharacters into a substituted value; anything that
// does not match is left untouched as a literal.
var varPattern = regexp.MustCompile(`\$\(([a-zA-Z][a-zA-Z0-9_.]*)\)`)

// substituteString replaces every $(name) token in s with its resolved value.
// Resolution reads, in order: the trigger's well-known fields, then accumulated
// action outputs, then the trigger Data map. An unknown token resolves to the
// empty string so a typo degrades gracefully instead of leaking the raw token.
func substituteString(ec *ExecutionContext, s string) string {
	if !strings.Contains(s, "$(") {
		return s
	}
	return varPattern.ReplaceAllStringFunc(s, func(token string) string {
		m := varPattern.FindStringSubmatch(token)
		if len(m) != 2 {
			return token
		}
		if v, ok := resolveVar(ec, m[1]); ok {
			return v
		}
		return ""
	})
}

// resolveVar looks up a single variable name against the execution context.
func resolveVar(ec *ExecutionContext, name string) (string, bool) {
	switch strings.ToLower(name) {
	case "user", "username":
		return ec.Trigger.Username, true
	case "userid", "user.id":
		return ec.Trigger.UserID, true
	case "channel":
		return ec.Trigger.Channel, true
	case "platform":
		return ec.Trigger.Platform, true
	case "message", "text":
		return ec.Trigger.Text, true
	case "event":
		return ec.Trigger.EventType, true
	}
	if v, ok := ec.Output(name); ok {
		return stringifyValue(v), true
	}
	if v, ok := ec.Trigger.Data[name]; ok {
		return stringifyValue(v), true
	}
	return "", false
}

// substituteRaw walks a JSON config and substitutes $(name) tokens inside every
// string leaf, returning a new blob. Object keys are never substituted; only
// string values are. A blob that fails to parse is returned unchanged so a
// plugin with a non-standard config shape still receives its original bytes.
func substituteRaw(ec *ExecutionContext, raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !strings.Contains(string(raw), "$(") {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	walked := substituteValue(ec, v)
	out, err := json.Marshal(walked)
	if err != nil {
		return raw
	}
	return out
}

// stringifyValue renders a resolved output or data value as a plain string for
// inlining into a substituted config, covering the JSON-decoded scalar types.
func stringifyValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// substituteValue recurses through a decoded JSON value, substituting tokens in
// string leaves while leaving numbers, bools and null untouched.
func substituteValue(ec *ExecutionContext, v any) any {
	switch t := v.(type) {
	case string:
		return substituteString(ec, t)
	case []any:
		for i := range t {
			t[i] = substituteValue(ec, t[i])
		}
		return t
	case map[string]any:
		for k, val := range t {
			t[k] = substituteValue(ec, val)
		}
		return t
	default:
		return v
	}
}
