// SPDX-License-Identifier: Apache-2.0

package sdk

import "encoding/json"

// Definition is the static metadata a workflow node exposes. ID is the stable
// string key used in rule configs and the node palette (e.g. "greeter:hello");
// Name and Description are human-facing.
type Definition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ExecContext is the narrow surface a node may use during a single rule firing.
// It is not safe for concurrent use; the engine runs one node at a time per
// firing. The engine already substitutes $(...) variables in a node's config
// before execution, so most nodes read their config and never touch Data.
type ExecContext interface {
	// Data returns a value from the trigger's open data map (e.g. the
	// "donation.from" a donation event carries), and whether it was present.
	Data(key string) (any, bool)
	// Output returns a value produced by an earlier node in the same rule, and
	// whether it was present.
	Output(key string) (any, bool)
	// SetOutput records a named output for later nodes and variable
	// substitution. Returning it in [Result.Outputs] does the same thing.
	SetOutput(key string, value any)
	// Channel is the channel login the rule is firing for.
	Channel() string
	// Username is the triggering user's display name (empty for non-user
	// triggers such as timers).
	Username() string
}

// Result is what an [Action] returns. Outputs are merged into the firing's
// output set (readable by later nodes and $(...) variables). Stop halts the
// remaining nodes in the rule as a clean early exit, not an error. A nil
// *Result means "success, continue".
type Result struct {
	Outputs map[string]any
	Stop    bool
}

// Action performs a side effect when a rule fires. It decodes its own config
// from the opaque JSON blob and must treat it as untrusted input.
type Action interface {
	Definition() Definition
	Execute(ec ExecContext, config json.RawMessage) (*Result, error)
}

// Condition decides whether a rule's actions may run. Evaluate returns
// (false, nil) for a clean "does not match" and (false, err) only for an
// unexpected internal failure.
type Condition interface {
	Definition() Definition
	Evaluate(ec ExecContext, config json.RawMessage) (bool, error)
}
