package actions

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

type regexConditionConfig struct {
	Pattern         string `json:"pattern"`
	Target          string `json:"target"`
	CaseInsensitive bool   `json:"case_insensitive"`
}

type regexCondition struct{}

func (regexCondition) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "cond:regex",
		Name:        "Matches regular expression",
		Description: "Passes when the target matches a regular expression. Target defaults to the triggering message and supports $(...) variables (substituted before matching); case_insensitive folds case. An invalid pattern fails closed and is logged once, never per message.",
	}
}

func (regexCondition) Evaluate(ec *ExecutionContext, config json.RawMessage) (bool, error) {
	var c regexConditionConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return false, fmt.Errorf("actions: cond:regex config: %w", err)
	}
	if strings.TrimSpace(c.Pattern) == "" {
		return false, nil
	}
	re, ok := compileRegexCached(c.Pattern, c.CaseInsensitive)
	if !ok {
		return false, nil
	}
	target := c.Target
	if strings.TrimSpace(target) == "" {
		target = ec.Trigger.Text
	} else {
		target = substituteString(ec, target)
	}
	return re.MatchString(target), nil
}

var (
	regexCacheMu sync.Mutex
	regexCache   = map[string]*regexp.Regexp{}
	regexBadSeen = map[string]bool{}
)

// compileRegexCached compiles pattern once per (pattern, caseInsensitive) key
// and caches the result on the hot path. A pattern that fails to compile is
// remembered so the warning is logged exactly once - never per message - and ok
// is false thereafter, letting the caller fail closed.
func compileRegexCached(pattern string, caseInsensitive bool) (*regexp.Regexp, bool) {
	key := pattern
	if caseInsensitive {
		key = "i:" + pattern
	}
	regexCacheMu.Lock()
	defer regexCacheMu.Unlock()
	if re, ok := regexCache[key]; ok {
		return re, true
	}
	if regexBadSeen[key] {
		return nil, false
	}
	expr := pattern
	if caseInsensitive {
		expr = "(?i)" + pattern
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		regexBadSeen[key] = true
		slog.Default().Warn("actions: cond:regex invalid pattern, failing closed",
			"pattern", pattern, "err", err)
		return nil, false
	}
	regexCache[key] = re
	return re, true
}
