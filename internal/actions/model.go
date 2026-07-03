package actions

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// SchemaVersion is the version stamped onto every persisted [Rule] so a future
// loader can migrate config JSON whose meaning changed under a plugin type id.
const SchemaVersion = 1

var (
	// ErrNotFound is returned when a rule lookup matches no row.
	ErrNotFound = errors.New("actions: rule not found")

	// ErrAlreadyExists is returned when creating a rule whose (tenant,
	// channel, name) already exists.
	ErrAlreadyExists = errors.New("actions: rule already exists")

	// ErrInvalid is returned when a rule fails validation. The wrapped
	// detail says why.
	ErrInvalid = errors.New("actions: invalid rule")
)

// TriggerKind is the source category that can fire a rule. The string values
// are stable wire identifiers persisted in the database and sent to the future
// visual editor, so they must never change once shipped.
type TriggerKind string

const (
	TriggerEvent   TriggerKind = "event"
	TriggerCommand TriggerKind = "command"
	TriggerTimer   TriggerKind = "timer"
	TriggerManual  TriggerKind = "manual"
	TriggerWebhook TriggerKind = "webhook"
)

func (k TriggerKind) valid() bool {
	switch k {
	case TriggerEvent, TriggerCommand, TriggerTimer, TriggerManual, TriggerWebhook:
		return true
	default:
		return false
	}
}

// ConditionMode is how a [ConditionList] combines its members.
type ConditionMode string

const (
	ConditionModeAll  ConditionMode = "all"
	ConditionModeAny  ConditionMode = "any"
	ConditionModeNone ConditionMode = "none"
)

func (m ConditionMode) valid() bool {
	switch m {
	case ConditionModeAll, ConditionModeAny, ConditionModeNone:
		return true
	default:
		return false
	}
}

// ConditionInstance is one saved condition in a rule: a registry type id plus
// the opaque JSON config that the matching [ConditionType] decodes itself.
type ConditionInstance struct {
	TypeID string          `json:"type_id"`
	Config json.RawMessage `json:"config,omitempty"`
}

// ConditionList gates a rule's actions. An empty Conditions slice always passes
// regardless of Mode.
type ConditionList struct {
	Mode       ConditionMode       `json:"mode"`
	Conditions []ConditionInstance `json:"conditions,omitempty"`
}

// ActionInstance is one saved action in a rule: a registry type id, the opaque
// JSON config the matching [ActionType] decodes, and per-instance execution
// flags. Enabled lets an action be switched off without deleting it.
type ActionInstance struct {
	TypeID  string          `json:"type_id"`
	Config  json.RawMessage `json:"config,omitempty"`
	Enabled bool            `json:"enabled"`
}

// ActionList is the ordered list of actions a rule runs when its conditions
// pass. QueueID, when non-empty, routes the whole list through a named
// serialised queue so two rules sharing a queue never interleave.
type ActionList struct {
	Actions []ActionInstance `json:"actions"`
	QueueID string           `json:"queue_id,omitempty"`
}

// Rule is the persisted unit of the engine: a trigger bound to conditions and
// actions, scoped to a (tenant, channel) and uniquely named within that scope.
//
// TriggerFilter is a trigger-kind-specific opaque JSON predicate the engine
// uses to decide whether a fired trigger matches this rule (for example an
// event rule filters on event type, a command rule on the command word).
type Rule struct {
	ID            string          `json:"id"`
	TenantID      string          `json:"tenant_id"`
	Channel       string          `json:"channel"`
	Name          string          `json:"name"`
	Enabled       bool            `json:"enabled"`
	TriggerKind   TriggerKind     `json:"trigger_kind"`
	TriggerFilter json.RawMessage `json:"trigger_filter,omitempty"`
	Conditions    ConditionList   `json:"conditions"`
	Actions       ActionList      `json:"actions"`
	SchemaVersion int             `json:"schema_version"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// maxRuleNameLen caps a rule name so it fits comfortably in UI lists and never
// becomes an unbounded storage vector.
const maxRuleNameLen = 64

// newID returns a fresh lower-cased ULID, mirroring counters.newID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// normalizeName lower-cases and trims a rule name so lookups are
// case-insensitive and whitespace-insensitive.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// validate normalises the rule in place and enforces the structural invariants
// the store and engine both rely on, returning an error wrapping [ErrInvalid].
func (r *Rule) validate() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if strings.TrimSpace(r.Channel) == "" {
		return fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	r.Name = normalizeName(r.Name)
	if r.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(r.Name) > maxRuleNameLen {
		return fmt.Errorf("%w: name length %d exceeds %d", ErrInvalid, len(r.Name), maxRuleNameLen)
	}
	if !r.TriggerKind.valid() {
		return fmt.Errorf("%w: unknown trigger_kind %q", ErrInvalid, r.TriggerKind)
	}
	if r.Conditions.Mode == "" {
		r.Conditions.Mode = ConditionModeAll
	}
	if !r.Conditions.Mode.valid() {
		return fmt.Errorf("%w: unknown condition mode %q", ErrInvalid, r.Conditions.Mode)
	}
	for i, c := range r.Conditions.Conditions {
		if strings.TrimSpace(c.TypeID) == "" {
			return fmt.Errorf("%w: condition %d has empty type_id", ErrInvalid, i)
		}
	}
	if len(r.Actions.Actions) == 0 {
		return fmt.Errorf("%w: at least one action is required", ErrInvalid)
	}
	for i, a := range r.Actions.Actions {
		if strings.TrimSpace(a.TypeID) == "" {
			return fmt.Errorf("%w: action %d has empty type_id", ErrInvalid, i)
		}
	}
	return nil
}
