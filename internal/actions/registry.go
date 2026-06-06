package actions

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// PluginDefinition is the static metadata a condition or action plugin exposes:
// a stable string id used as its registry key and persisted in rule configs,
// plus human-facing fields the dashboard and a future node-editor render.
type PluginDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ConditionType is a plugin that decides whether a rule's actions may run.
// Implementations decode their own config from the opaque blob and must treat
// it as untrusted input. Evaluate returns (false, nil) for a clean "does not
// match" and (false, err) only for an unexpected internal failure.
type ConditionType interface {
	Definition() PluginDefinition
	Evaluate(ec *ExecutionContext, config json.RawMessage) (bool, error)
}

// ActionResult is what an action returns. Stop halts the remaining actions in
// the rule's list (a clean early exit, not an error).
type ActionResult struct {
	Outputs map[string]any
	Stop    bool
}

// ActionType is a plugin that performs a side effect when a rule fires.
// Implementations decode their own config from the opaque blob and must treat
// it as untrusted input. A nil result means "success, continue".
type ActionType interface {
	Definition() PluginDefinition
	Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error)
}

// Registry holds the registered condition and action plugins keyed by their
// definition id. It is safe for concurrent use; registration happens at startup
// and lookups happen on the hot path, so reads take a shared lock.
type Registry struct {
	mu         sync.RWMutex
	conditions map[string]ConditionType
	actions    map[string]ActionType
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		conditions: make(map[string]ConditionType),
		actions:    make(map[string]ActionType),
	}
}

// RegisterCondition adds a condition plugin. It returns an error if the id is
// empty or already registered, so a typo never silently shadows a built-in.
func (r *Registry) RegisterCondition(c ConditionType) error {
	id := c.Definition().ID
	if id == "" {
		return fmt.Errorf("actions: condition has empty id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.conditions[id]; dup {
		return fmt.Errorf("actions: condition %q already registered", id)
	}
	r.conditions[id] = c
	return nil
}

// RegisterAction adds an action plugin. It returns an error if the id is empty
// or already registered.
func (r *Registry) RegisterAction(a ActionType) error {
	id := a.Definition().ID
	if id == "" {
		return fmt.Errorf("actions: action has empty id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.actions[id]; dup {
		return fmt.Errorf("actions: action %q already registered", id)
	}
	r.actions[id] = a
	return nil
}

// Condition returns the registered condition for id and whether it was found.
func (r *Registry) Condition(id string) (ConditionType, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.conditions[id]
	return c, ok
}

// Action returns the registered action for id and whether it was found.
func (r *Registry) Action(id string) (ActionType, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.actions[id]
	return a, ok
}

// Conditions returns every registered condition definition, sorted by id, so
// the dashboard can render the palette deterministically.
func (r *Registry) Conditions() []PluginDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PluginDefinition, 0, len(r.conditions))
	for _, c := range r.conditions {
		out = append(out, c.Definition())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Triggers returns the four trigger kinds as plugin definitions so the rule
// builder can enumerate them alongside conditions and actions. The IDs are the
// stable TriggerKind wire values and must not change.
func (r *Registry) Triggers() []PluginDefinition {
	return []PluginDefinition{
		{ID: "event", Name: "Event", Description: "Fires when a platform event of the configured type occurs."},
		{ID: "command", Name: "Command", Description: "Fires when a chat message begins with the configured command word."},
		{ID: "timer", Name: "Timer", Description: "Fires on a fixed interval set in seconds."},
		{ID: "manual", Name: "Manual", Description: "Fires only when run by hand from the dashboard."},
	}
}

// Actions returns every registered action definition, sorted by id.
func (r *Registry) Actions() []PluginDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PluginDefinition, 0, len(r.actions))
	for _, a := range r.actions {
		out = append(out, a.Definition())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
