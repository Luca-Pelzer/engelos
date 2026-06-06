package actions

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ChatSender is the single side-effect dependency the built-in chat action
// needs. main wires a thin adapter over the platform layer, keeping this
// package free of any engelos/internal import. Send posts text to channel on
// behalf of the bot.
type ChatSender interface {
	Send(channel, text string) error
}

// Services bundles the host-provided side-effect dependencies the first-party
// action plugins call out to. A nil field disables the actions that need it
// (they no-op with a registration that still exists), so the engine runs even
// in a headless test with no platform attached.
type Services struct {
	Chat ChatSender
	OBS  OBSController
}

// RegisterBuiltins installs the first-party condition and action plugins into
// reg. It is called once at startup after the registry is created. Returning an
// error on a duplicate id surfaces a double-registration bug immediately rather
// than at first fire.
func RegisterBuiltins(reg *Registry, svc Services) error {
	conds := []ConditionType{
		messageContainsCondition{},
		userRoleCondition{},
	}
	for _, c := range conds {
		if err := reg.RegisterCondition(c); err != nil {
			return err
		}
	}
	acts := []ActionType{
		sendChatAction{chat: svc.Chat},
		delayAction{},
		logAction{},
		switchSceneAction{obs: svc.OBS},
		toggleSourceAction{obs: svc.OBS},
	}
	for _, a := range acts {
		if err := reg.RegisterAction(a); err != nil {
			return err
		}
	}
	return nil
}

// eventTriggerFilter is the persisted predicate for an event-kind rule: it fires
// only for the named event type (empty means any event).
type eventTriggerFilter struct {
	EventType string `json:"event_type"`
}

// commandTriggerFilter is the persisted predicate for a command-kind rule: the
// first whitespace token of the message must equal Command (with a leading "!"
// tolerated on either side).
type commandTriggerFilter struct {
	Command string `json:"command"`
}

// timerTriggerFilter is the persisted predicate for a timer-kind rule. The
// scheduler reads IntervalSeconds to set the ticker cadence; matchTriggerFilter
// itself never gates on it because the scheduler already targets one rule per
// tick via Engine.RunRule.
type timerTriggerFilter struct {
	IntervalSeconds int `json:"interval_seconds"`
}

// matchTriggerFilter decodes a rule's kind-specific TriggerFilter and reports
// whether trigger t satisfies it. An absent or unparseable filter matches
// everything of the right kind so a rule never silently stops firing because of
// a malformed blob; the stricter validation lives at write time.
func matchTriggerFilter(r Rule, t Trigger) bool {
	switch r.TriggerKind {
	case TriggerEvent:
		if len(r.TriggerFilter) == 0 {
			return true
		}
		var f eventTriggerFilter
		if err := json.Unmarshal(r.TriggerFilter, &f); err != nil {
			return true
		}
		return f.EventType == "" || strings.EqualFold(f.EventType, t.EventType)
	case TriggerCommand:
		var f commandTriggerFilter
		if err := json.Unmarshal(r.TriggerFilter, &f); err != nil {
			return false
		}
		want := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(f.Command)), "!")
		if want == "" {
			return false
		}
		got := firstToken(t.Text)
		return got == want
	case TriggerTimer, TriggerManual:
		// The scheduler tick and the manual fire endpoint both target one
		// specific rule via Engine.RunRule, so a kind match is sufficient and
		// the interval/payload need not be re-checked here.
		return true
	default:
		return true
	}
}

// firstToken returns the lower-cased first whitespace-delimited word of s with a
// leading "!" stripped, the canonical form a command rule matches against.
func firstToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(fields[0]), "!")
}

// ─── Conditions ───────────────────────────────────────────────────────────────

type messageContainsCondition struct{}

type messageContainsConfig struct {
	Substring     string `json:"substring"`
	CaseSensitive bool   `json:"case_sensitive"`
}

func (messageContainsCondition) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "builtin:message-contains",
		Name:        "Message contains text",
		Description: "Passes when the triggering message contains a substring.",
	}
}

func (messageContainsCondition) Evaluate(ec *ExecutionContext, config json.RawMessage) (bool, error) {
	var c messageContainsConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return false, fmt.Errorf("actions: message-contains config: %w", err)
	}
	if c.Substring == "" {
		return false, nil
	}
	if c.CaseSensitive {
		return strings.Contains(ec.Trigger.Text, c.Substring), nil
	}
	return strings.Contains(strings.ToLower(ec.Trigger.Text),
		strings.ToLower(c.Substring)), nil
}

type userRoleCondition struct{}

type userRoleConfig struct {
	Role string `json:"role"`
}

func (userRoleCondition) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "builtin:user-role",
		Name:        "User has role",
		Description: "Passes when the triggering user holds the required role.",
	}
}

func (userRoleCondition) Evaluate(ec *ExecutionContext, config json.RawMessage) (bool, error) {
	var c userRoleConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return false, fmt.Errorf("actions: user-role config: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(c.Role)) {
	case "broadcaster":
		return ec.Trigger.IsBroadcaster, nil
	case "moderator", "mod":
		return ec.Trigger.IsModerator || ec.Trigger.IsBroadcaster, nil
	case "vip":
		return ec.Trigger.IsVIP, nil
	case "subscriber", "sub":
		return ec.Trigger.IsSubscriber, nil
	case "", "everyone", "any":
		return true, nil
	default:
		return false, fmt.Errorf("actions: user-role: unknown role %q", c.Role)
	}
}

// ─── Actions ──────────────────────────────────────────────────────────────────

type sendChatAction struct {
	chat ChatSender
}

type sendChatConfig struct {
	Text string `json:"text"`
}

func (sendChatAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "builtin:send-chat",
		Name:        "Send chat message",
		Description: "Posts a message to the channel chat. Supports $(user), $(message) and other variables.",
	}
}

func (a sendChatAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c sendChatConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: send-chat config: %w", err)
	}
	if strings.TrimSpace(c.Text) == "" {
		return nil, nil
	}
	if a.chat == nil {
		return nil, nil
	}
	channel := ec.Channel
	if channel == "" {
		channel = ec.Trigger.Channel
	}
	if err := a.chat.Send(channel, c.Text); err != nil {
		return nil, fmt.Errorf("actions: send-chat: %w", err)
	}
	return nil, nil
}

type delayAction struct{}

type delayConfig struct {
	Seconds float64 `json:"seconds"`
}

func (delayAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "builtin:delay",
		Name:        "Delay",
		Description: "Pauses the action list for a number of seconds before continuing.",
	}
}

func (delayAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c delayConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: delay config: %w", err)
	}
	if c.Seconds <= 0 {
		return nil, nil
	}
	d := time.Duration(c.Seconds * float64(time.Second))
	select {
	case <-time.After(d):
		return nil, nil
	case <-ec.Ctx.Done():
		return nil, ec.Ctx.Err()
	}
}

type logAction struct{}

type logConfig struct {
	Message string `json:"message"`
}

func (logAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "builtin:log",
		Name:        "Log message",
		Description: "Records a message to the engine log. Useful for testing a rule.",
	}
}

func (logAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c logConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: log config: %w", err)
	}
	return &ActionResult{Outputs: map[string]any{"logged": c.Message}}, nil
}
