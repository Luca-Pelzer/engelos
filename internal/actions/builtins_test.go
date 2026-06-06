package actions

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinsRegistration(t *testing.T) {
	reg := NewRegistry()
	chat := &recordingChat{}
	svc := Services{Chat: chat}

	err := RegisterBuiltins(reg, svc)
	require.NoError(t, err)

	cond1, ok := reg.Condition("builtin:message-contains")
	require.True(t, ok, "message-contains condition not registered")
	assert.Equal(t, "builtin:message-contains", cond1.Definition().ID)

	cond2, ok := reg.Condition("builtin:user-role")
	require.True(t, ok, "user-role condition not registered")
	assert.Equal(t, "builtin:user-role", cond2.Definition().ID)

	act1, ok := reg.Action("builtin:send-chat")
	require.True(t, ok, "send-chat action not registered")
	assert.Equal(t, "builtin:send-chat", act1.Definition().ID)

	act2, ok := reg.Action("builtin:delay")
	require.True(t, ok, "delay action not registered")
	assert.Equal(t, "builtin:delay", act2.Definition().ID)

	act3, ok := reg.Action("builtin:log")
	require.True(t, ok, "log action not registered")
	assert.Equal(t, "builtin:log", act3.Definition().ID)
}

func TestBuiltinsRegistrationDuplicate(t *testing.T) {
	reg := NewRegistry()
	svc := Services{Chat: &recordingChat{}}

	err := RegisterBuiltins(reg, svc)
	require.NoError(t, err)

	err = RegisterBuiltins(reg, svc)
	require.Error(t, err, "second RegisterBuiltins should fail with duplicate id")
}

func TestBuiltinsMessageContains(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	cond, ok := reg.Condition("builtin:message-contains")
	require.True(t, ok)

	rule := sampleRule("ch", "test")
	trigger := Trigger{Text: "Hello World"}
	ec := newExecutionContext(context.Background(), rule, trigger)

	t.Run("match case-insensitive", func(t *testing.T) {
		cfg := mustJSON(t, map[string]any{"substring": "hello"})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.True(t, pass)
	})

	t.Run("match case-insensitive uppercase substring", func(t *testing.T) {
		cfg := mustJSON(t, map[string]any{"substring": "WORLD"})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.True(t, pass)
	})

	t.Run("no match", func(t *testing.T) {
		cfg := mustJSON(t, map[string]any{"substring": "foo"})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.False(t, pass)
	})

	t.Run("case_sensitive true - match", func(t *testing.T) {
		cfg := mustJSON(t, map[string]any{"substring": "Hello", "case_sensitive": true})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.True(t, pass)
	})

	t.Run("case_sensitive true - no match", func(t *testing.T) {
		cfg := mustJSON(t, map[string]any{"substring": "hello", "case_sensitive": true})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.False(t, pass)
	})

	t.Run("empty substring returns false", func(t *testing.T) {
		cfg := mustJSON(t, map[string]any{"substring": ""})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.False(t, pass)
	})
}

func TestBuiltinsUserRole(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	cond, ok := reg.Condition("builtin:user-role")
	require.True(t, ok)

	rule := sampleRule("ch", "test")

	t.Run("broadcaster role", func(t *testing.T) {
		trigger := Trigger{IsBroadcaster: true}
		ec := newExecutionContext(context.Background(), rule, trigger)
		cfg := mustJSON(t, map[string]any{"role": "broadcaster"})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.True(t, pass)

		trigger2 := Trigger{IsBroadcaster: false}
		ec2 := newExecutionContext(context.Background(), rule, trigger2)
		pass2, err := cond.Evaluate(ec2, cfg)
		require.NoError(t, err)
		assert.False(t, pass2)
	})

	t.Run("moderator role", func(t *testing.T) {
		trigger := Trigger{IsModerator: true}
		ec := newExecutionContext(context.Background(), rule, trigger)
		cfg := mustJSON(t, map[string]any{"role": "moderator"})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.True(t, pass)

		cfgMod := mustJSON(t, map[string]any{"role": "mod"})
		pass2, err := cond.Evaluate(ec, cfgMod)
		require.NoError(t, err)
		assert.True(t, pass2)
	})

	t.Run("moderator true when only broadcaster set", func(t *testing.T) {
		trigger := Trigger{IsBroadcaster: true, IsModerator: false}
		ec := newExecutionContext(context.Background(), rule, trigger)
		cfg := mustJSON(t, map[string]any{"role": "moderator"})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.True(t, pass, "moderator should pass when IsBroadcaster is true")
	})

	t.Run("vip role", func(t *testing.T) {
		trigger := Trigger{IsVIP: true}
		ec := newExecutionContext(context.Background(), rule, trigger)
		cfg := mustJSON(t, map[string]any{"role": "vip"})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.True(t, pass)

		trigger2 := Trigger{IsVIP: false}
		ec2 := newExecutionContext(context.Background(), rule, trigger2)
		pass2, err := cond.Evaluate(ec2, cfg)
		require.NoError(t, err)
		assert.False(t, pass2)
	})

	t.Run("subscriber role", func(t *testing.T) {
		trigger := Trigger{IsSubscriber: true}
		ec := newExecutionContext(context.Background(), rule, trigger)
		cfg := mustJSON(t, map[string]any{"role": "subscriber"})
		pass, err := cond.Evaluate(ec, cfg)
		require.NoError(t, err)
		assert.True(t, pass)

		cfgSub := mustJSON(t, map[string]any{"role": "sub"})
		pass2, err := cond.Evaluate(ec, cfgSub)
		require.NoError(t, err)
		assert.True(t, pass2)
	})

	t.Run("everyone role", func(t *testing.T) {
		trigger := Trigger{}
		ec := newExecutionContext(context.Background(), rule, trigger)

		for _, role := range []string{"everyone", "any", ""} {
			cfg := mustJSON(t, map[string]any{"role": role})
			pass, err := cond.Evaluate(ec, cfg)
			require.NoError(t, err)
			assert.True(t, pass, "role %q should always pass", role)
		}
	})

	t.Run("unknown role returns error", func(t *testing.T) {
		trigger := Trigger{}
		ec := newExecutionContext(context.Background(), rule, trigger)
		cfg := mustJSON(t, map[string]any{"role": "unknown_role"})
		_, err := cond.Evaluate(ec, cfg)
		require.Error(t, err)
	})
}

func TestBuiltinsSendChat(t *testing.T) {
	t.Run("sends to ec.Channel", func(t *testing.T) {
		chat := &recordingChat{}
		reg := NewRegistry()
		require.NoError(t, RegisterBuiltins(reg, Services{Chat: chat}))
		act, _ := reg.Action("builtin:send-chat")

		rule := Rule{
			TenantID:    "local",
			Channel:     "rule-channel",
			Name:        "test",
			Enabled:     true,
			TriggerKind: TriggerEvent,
			Conditions:  ConditionList{Mode: ConditionModeAll},
			Actions:     ActionList{Actions: []ActionInstance{{TypeID: "builtin:log", Enabled: true}}},
		}
		trigger := Trigger{Channel: "trigger-channel"}
		ec := newExecutionContext(context.Background(), rule, trigger)

		cfg := mustJSON(t, map[string]any{"text": "hello"})
		_, err := act.Execute(ec, cfg)
		require.NoError(t, err)

		msgs := chat.messages()
		require.Len(t, msgs, 1)
		assert.Equal(t, "rule-channel", msgs[0].Channel)
		assert.Equal(t, "hello", msgs[0].Text)
	})

	t.Run("falls back to trigger.Channel when ec.Channel empty", func(t *testing.T) {
		chat := &recordingChat{}
		reg := NewRegistry()
		require.NoError(t, RegisterBuiltins(reg, Services{Chat: chat}))
		act, _ := reg.Action("builtin:send-chat")

		rule := Rule{
			TenantID:    "local",
			Channel:     "",
			Name:        "test",
			Enabled:     true,
			TriggerKind: TriggerEvent,
			Conditions:  ConditionList{Mode: ConditionModeAll},
			Actions:     ActionList{Actions: []ActionInstance{{TypeID: "builtin:log", Enabled: true}}},
		}
		trigger := Trigger{Channel: "trigger-channel"}
		ec := newExecutionContext(context.Background(), rule, trigger)

		cfg := mustJSON(t, map[string]any{"text": "world"})
		_, err := act.Execute(ec, cfg)
		require.NoError(t, err)

		msgs := chat.messages()
		require.Len(t, msgs, 1)
		assert.Equal(t, "trigger-channel", msgs[0].Channel)
		assert.Equal(t, "world", msgs[0].Text)
	})

	t.Run("empty text no-op", func(t *testing.T) {
		chat := &recordingChat{}
		reg := NewRegistry()
		require.NoError(t, RegisterBuiltins(reg, Services{Chat: chat}))
		act, _ := reg.Action("builtin:send-chat")

		rule := sampleRule("ch", "test")
		trigger := Trigger{}
		ec := newExecutionContext(context.Background(), rule, trigger)

		cfg := mustJSON(t, map[string]any{"text": ""})
		_, err := act.Execute(ec, cfg)
		require.NoError(t, err)
		assert.Empty(t, chat.messages())

		cfg2 := mustJSON(t, map[string]any{"text": "   "})
		_, err = act.Execute(ec, cfg2)
		require.NoError(t, err)
		assert.Empty(t, chat.messages())
	})

	t.Run("nil chat no-op no error", func(t *testing.T) {
		reg := NewRegistry()
		require.NoError(t, RegisterBuiltins(reg, Services{Chat: nil}))
		act, _ := reg.Action("builtin:send-chat")

		rule := sampleRule("ch", "test")
		trigger := Trigger{}
		ec := newExecutionContext(context.Background(), rule, trigger)

		cfg := mustJSON(t, map[string]any{"text": "hello"})
		_, err := act.Execute(ec, cfg)
		require.NoError(t, err)
	})
}

func TestBuiltinsDelay(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	act, ok := reg.Action("builtin:delay")
	require.True(t, ok)

	rule := sampleRule("ch", "test")
	trigger := Trigger{}

	t.Run("zero seconds returns immediately", func(t *testing.T) {
		ec := newExecutionContext(context.Background(), rule, trigger)
		cfg := mustJSON(t, map[string]any{"seconds": 0})
		start := time.Now()
		_, err := act.Execute(ec, cfg)
		require.NoError(t, err)
		assert.Less(t, time.Since(start), 50*time.Millisecond)
	})

	t.Run("negative seconds returns immediately", func(t *testing.T) {
		ec := newExecutionContext(context.Background(), rule, trigger)
		cfg := mustJSON(t, map[string]any{"seconds": -1})
		start := time.Now()
		_, err := act.Execute(ec, cfg)
		require.NoError(t, err)
		assert.Less(t, time.Since(start), 50*time.Millisecond)
	})

	t.Run("cancelled context returns error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		ec := newExecutionContext(ctx, rule, trigger)
		cfg := mustJSON(t, map[string]any{"seconds": 0.05})
		_, err := act.Execute(ec, cfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestBuiltinsLog(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	act, ok := reg.Action("builtin:log")
	require.True(t, ok)

	rule := sampleRule("ch", "test")
	trigger := Trigger{}
	ec := newExecutionContext(context.Background(), rule, trigger)

	cfg := mustJSON(t, map[string]any{"message": "test-log-message"})
	result, err := act.Execute(ec, cfg)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Outputs)
	assert.Equal(t, "test-log-message", result.Outputs["logged"])
}

func TestBuiltinsMatchTriggerFilter(t *testing.T) {
	t.Run("event rule with event_type matches", func(t *testing.T) {
		rule := Rule{
			TriggerKind:   TriggerEvent,
			TriggerFilter: json.RawMessage(`{"event_type":"user.subscribed"}`),
		}
		trigger := Trigger{Kind: TriggerEvent, EventType: "user.subscribed"}
		assert.True(t, matchTriggerFilter(rule, trigger))
	})

	t.Run("event rule with event_type does not match different type", func(t *testing.T) {
		rule := Rule{
			TriggerKind:   TriggerEvent,
			TriggerFilter: json.RawMessage(`{"event_type":"user.subscribed"}`),
		}
		trigger := Trigger{Kind: TriggerEvent, EventType: "message.created"}
		assert.False(t, matchTriggerFilter(rule, trigger))
	})

	t.Run("event rule with empty event_type matches any", func(t *testing.T) {
		rule := Rule{
			TriggerKind:   TriggerEvent,
			TriggerFilter: json.RawMessage(`{"event_type":""}`),
		}
		trigger := Trigger{Kind: TriggerEvent, EventType: "anything"}
		assert.True(t, matchTriggerFilter(rule, trigger))
	})

	t.Run("event rule with absent filter matches any", func(t *testing.T) {
		rule := Rule{
			TriggerKind:   TriggerEvent,
			TriggerFilter: nil,
		}
		trigger := Trigger{Kind: TriggerEvent, EventType: "anything"}
		assert.True(t, matchTriggerFilter(rule, trigger))
	})

	t.Run("event rule with unparseable filter matches any", func(t *testing.T) {
		rule := Rule{
			TriggerKind:   TriggerEvent,
			TriggerFilter: json.RawMessage(`{invalid json`),
		}
		trigger := Trigger{Kind: TriggerEvent, EventType: "anything"}
		assert.True(t, matchTriggerFilter(rule, trigger))
	})

	t.Run("command rule matches with leading !", func(t *testing.T) {
		rule := Rule{
			TriggerKind:   TriggerCommand,
			TriggerFilter: json.RawMessage(`{"command":"!foo"}`),
		}
		trigger := Trigger{Kind: TriggerCommand, Text: "!foo bar"}
		assert.True(t, matchTriggerFilter(rule, trigger))
	})

	t.Run("command rule matches without leading !", func(t *testing.T) {
		rule := Rule{
			TriggerKind:   TriggerCommand,
			TriggerFilter: json.RawMessage(`{"command":"!foo"}`),
		}
		trigger := Trigger{Kind: TriggerCommand, Text: "foo bar"}
		assert.True(t, matchTriggerFilter(rule, trigger))
	})

	t.Run("command rule does not match different command", func(t *testing.T) {
		rule := Rule{
			TriggerKind:   TriggerCommand,
			TriggerFilter: json.RawMessage(`{"command":"!foo"}`),
		}
		trigger := Trigger{Kind: TriggerCommand, Text: "bar"}
		assert.False(t, matchTriggerFilter(rule, trigger))
	})

	t.Run("command rule unparseable filter returns false", func(t *testing.T) {
		rule := Rule{
			TriggerKind:   TriggerCommand,
			TriggerFilter: json.RawMessage(`{invalid json`),
		}
		trigger := Trigger{Kind: TriggerCommand, Text: "!foo"}
		assert.False(t, matchTriggerFilter(rule, trigger))
	})

	t.Run("command rule empty command returns false", func(t *testing.T) {
		rule := Rule{
			TriggerKind:   TriggerCommand,
			TriggerFilter: json.RawMessage(`{"command":""}`),
		}
		trigger := Trigger{Kind: TriggerCommand, Text: "!foo"}
		assert.False(t, matchTriggerFilter(rule, trigger))
	})
}

func TestBuiltinsFirstToken(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"!foo bar", "foo"},
		{"foo bar", "foo"},
		{"FOO", "foo"},
		{"!FOO", "foo"},
		{"  !foo  bar  ", "foo"},
		{"", ""},
		{"   ", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, firstToken(tc.input))
		})
	}
}
