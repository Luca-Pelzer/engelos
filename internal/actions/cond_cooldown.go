package actions

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// cooldownSweepEvery bounds the map: after this many arming writes the store
// opportunistically drops entries whose window has already elapsed (they would
// pass on the next check anyway, so eviction is behaviour-neutral).
const cooldownSweepEvery = 256

// cooldownEntry records when a cond:cooldown key last armed and for how long, so
// eviction can tell a still-active entry from an elapsed one without knowing the
// per-key window globally.
type cooldownEntry struct {
	at     time.Time
	window time.Duration
}

// cooldownStore is the in-memory, thread-safe record of the last passing
// cond:cooldown evaluation per key. It is NOT persisted: a restart resets every
// cooldown. Safe for concurrent use by the engine's worker pools.
type cooldownStore struct {
	mu   sync.Mutex
	last map[string]cooldownEntry
	now  func() time.Time
	ops  int
}

// newCooldownStore returns an empty store using the wall clock.
func newCooldownStore() *cooldownStore {
	return &cooldownStore{last: make(map[string]cooldownEntry), now: time.Now}
}

// allow reports whether window has elapsed since key last armed. On a pass it
// records the current time (arming the cooldown) and returns true; within the
// window it returns false without touching the timestamp.
func (s *cooldownStore) allow(key string, window time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if e, ok := s.last[key]; ok && now.Sub(e.at) < e.window {
		return false
	}
	s.last[key] = cooldownEntry{at: now, window: window}
	s.ops++
	if s.ops >= cooldownSweepEvery {
		s.ops = 0
		for k, e := range s.last {
			if now.Sub(e.at) >= e.window {
				delete(s.last, k)
			}
		}
	}
	return true
}

type cooldownConditionConfig struct {
	Seconds int    `json:"seconds"`
	Per     string `json:"per"`
}

// cooldownCondition rate-limits a rule via an in-memory window. It is
// constructed once at startup so its store persists across firings.
type cooldownCondition struct {
	store *cooldownStore
}

func (cooldownCondition) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "cond:cooldown",
		Name:        "Cooldown",
		Description: "Passes at most once per `seconds` window (min 1) and blocks within it. per=rule (default) shares one window per (channel, rule); per=user gives each user their own. The cooldown ARMS whenever this condition itself passes — conditions run before actions, so with additional conditions or mode:any the window advances even if the rule ultimately does not run its actions; put cond:cooldown last, or use it alone, if that matters. In-memory only: a restart resets all cooldowns.",
	}
}

func (a cooldownCondition) Evaluate(ec *ExecutionContext, config json.RawMessage) (bool, error) {
	var c cooldownConditionConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return false, fmt.Errorf("actions: cond:cooldown config: %w", err)
	}
	if c.Seconds < 1 {
		return false, fmt.Errorf("actions: cond:cooldown: seconds must be >= 1, got %d", c.Seconds)
	}
	per := strings.ToLower(strings.TrimSpace(c.Per))
	if per == "" {
		per = "rule"
	}
	if per != "rule" && per != "user" {
		return false, fmt.Errorf("actions: cond:cooldown: per must be rule or user, got %q", c.Per)
	}
	if a.store == nil {
		return true, nil
	}
	key := ec.RuleID + "\x00" + ec.Channel
	if per == "user" {
		key += "\x00" + ec.Trigger.Username
	}
	return a.store.allow(key, time.Duration(c.Seconds)*time.Second), nil
}
