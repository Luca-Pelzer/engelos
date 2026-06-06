package actions

import (
	"context"
	"sync"
)

// Trigger is the runtime payload that fired a rule. It is the platform-neutral,
// engine-internal view the dispatcher adapter fills from a normalized platform
// event (or a command / timer firing), carrying the well-known fields plugins
// read plus an open Data map for kind-specific extras.
type Trigger struct {
	Kind     TriggerKind
	Platform string
	Channel  string
	UserID   string
	Username string
	Text     string

	IsBroadcaster bool
	IsModerator   bool
	IsVIP         bool
	IsSubscriber  bool

	EventType string
	Data      map[string]any
}

// ExecutionContext is handed to every [ConditionType] and [ActionType] for a
// single rule firing. It is NOT safe for concurrent use by multiple goroutines:
// the executor runs one action at a time per rule firing and owns the lifecycle.
//
// Outputs accumulates named values produced by earlier actions so later actions
// and variable substitution can read them. Use [ExecutionContext.SetOutput] and
// [ExecutionContext.Output] rather than touching the map directly so access
// stays guarded for the rare plugin that fans out goroutines internally.
type ExecutionContext struct {
	Ctx      context.Context
	Trigger  Trigger
	RuleID   string
	RuleName string
	TenantID string
	Channel  string

	mu      sync.Mutex
	outputs map[string]any
}

// newExecutionContext builds a context for one rule firing.
func newExecutionContext(ctx context.Context, r Rule, t Trigger) *ExecutionContext {
	return &ExecutionContext{
		Ctx:      ctx,
		Trigger:  t,
		RuleID:   r.ID,
		RuleName: r.Name,
		TenantID: r.TenantID,
		Channel:  r.Channel,
		outputs:  make(map[string]any),
	}
}

// withCtx returns a shallow copy of ec whose Ctx is ctx. The outputs map is
// shared by reference so an action's SetOutput stays visible to later actions,
// while the per-action timeout context never leaks into the next iteration's
// parent context. The mutex is not copied (it must not be by value); the
// executor runs actions one at a time, so the copy and the parent are never
// touched concurrently.
func (e *ExecutionContext) withCtx(ctx context.Context) *ExecutionContext {
	return &ExecutionContext{
		Ctx:      ctx,
		Trigger:  e.Trigger,
		RuleID:   e.RuleID,
		RuleName: e.RuleName,
		TenantID: e.TenantID,
		Channel:  e.Channel,
		outputs:  e.outputs,
	}
}

// SetOutput records a named output for downstream actions and variables.
func (e *ExecutionContext) SetOutput(name string, value any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.outputs == nil {
		e.outputs = make(map[string]any)
	}
	e.outputs[name] = value
}

// Output returns a previously recorded output and whether it was present.
func (e *ExecutionContext) Output(name string) (any, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.outputs[name]
	return v, ok
}
