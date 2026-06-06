package actions

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureAction is a test action that records executions to a channel.
type captureAction struct {
	id string
	ch chan<- capturedExec
}

type capturedExec struct {
	RuleName  string
	ActionID  string
	Config    json.RawMessage
	StartTime time.Time
	EndTime   time.Time
}

func (a captureAction) Definition() PluginDefinition {
	return PluginDefinition{ID: a.id, Name: "capture", Description: "test action"}
}

func (a captureAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	start := time.Now()
	exec := capturedExec{
		RuleName:  ec.RuleName,
		ActionID:  a.id,
		Config:    config,
		StartTime: start,
		EndTime:   time.Now(),
	}
	select {
	case a.ch <- exec:
	case <-time.After(2 * time.Second):
	}
	return nil, nil
}

// slowCaptureAction records start/end times with a configurable delay.
type slowCaptureAction struct {
	id    string
	delay time.Duration
	ch    chan<- capturedExec
}

func (a slowCaptureAction) Definition() PluginDefinition {
	return PluginDefinition{ID: a.id, Name: "slow-capture", Description: "test action with delay"}
}

func (a slowCaptureAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	start := time.Now()
	time.Sleep(a.delay)
	exec := capturedExec{
		RuleName:  ec.RuleName,
		ActionID:  a.id,
		Config:    config,
		StartTime: start,
		EndTime:   time.Now(),
	}
	select {
	case a.ch <- exec:
	case <-time.After(2 * time.Second):
	}
	return nil, nil
}

// outputAction sets an output and optionally stops execution.
type outputAction struct {
	id string
}

type outputActionConfig struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Stop  bool   `json:"stop"`
}

func (a outputAction) Definition() PluginDefinition {
	return PluginDefinition{ID: a.id, Name: "output", Description: "sets output"}
}

func (a outputAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c outputActionConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, err
	}
	return &ActionResult{
		Outputs: map[string]any{c.Key: c.Value},
		Stop:    c.Stop,
	}, nil
}

// readOutputAction reads an output set by a previous action and records it.
type readOutputAction struct {
	id string
	ch chan<- string
}

func (a readOutputAction) Definition() PluginDefinition {
	return PluginDefinition{ID: a.id, Name: "read-output", Description: "reads output"}
}

func (a readOutputAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, err
	}
	val, ok := ec.Output(c.Key)
	result := ""
	if ok {
		if s, isStr := val.(string); isStr {
			result = s
		}
	}
	select {
	case a.ch <- result:
	case <-time.After(2 * time.Second):
	}
	return nil, nil
}

// failingAction always returns an error.
type failingAction struct {
	id string
	ch chan<- string
}

func (a failingAction) Definition() PluginDefinition {
	return PluginDefinition{ID: a.id, Name: "failing", Description: "always fails"}
}

func (a failingAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	select {
	case a.ch <- ec.RuleName:
	case <-time.After(2 * time.Second):
	}
	return nil, errors.New("intentional failure")
}

// boolCondition is a test condition that returns a configurable boolean.
type boolCondition struct {
	id string
}

type boolConditionConfig struct {
	Pass bool `json:"pass"`
}

func (c boolCondition) Definition() PluginDefinition {
	return PluginDefinition{ID: c.id, Name: "bool", Description: "test condition"}
}

func (c boolCondition) Evaluate(ec *ExecutionContext, config json.RawMessage) (bool, error) {
	var cfg boolConditionConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return false, err
	}
	return cfg.Pass, nil
}

func waitForExec(t *testing.T, ch <-chan capturedExec, timeout time.Duration) (capturedExec, bool) {
	t.Helper()
	select {
	case exec := <-ch:
		return exec, true
	case <-time.After(timeout):
		return capturedExec{}, false
	}
}

func waitForString(t *testing.T, ch <-chan string, timeout time.Duration) (string, bool) {
	t.Helper()
	select {
	case s := <-ch:
		return s, true
	case <-time.After(timeout):
		return "", false
	}
}

func TestEngineFireMatchingEventRule(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))

	rule := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "match-rule",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{
		Kind:    TriggerEvent,
		Channel: "testchan",
	})

	exec, ok := waitForExec(t, ch, 2*time.Second)
	require.True(t, ok, "expected action to execute")
	assert.Equal(t, "match-rule", exec.RuleName)
}

func TestEngineTriggerKindMismatchDoesNotFire(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))

	rule := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "event-rule",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{
		Kind:    TriggerCommand,
		Channel: "testchan",
		Text:    "!foo",
	})

	_, ok := waitForExec(t, ch, 500*time.Millisecond)
	assert.False(t, ok, "action should not execute for mismatched trigger kind")
}

func TestEngineEventTriggerFilterMatch(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))

	rule := Rule{
		TenantID:      "local",
		Channel:       "testchan",
		Name:          "sub-event-rule",
		Enabled:       true,
		TriggerKind:   TriggerEvent,
		TriggerFilter: json.RawMessage(`{"event_type":"subscription"}`),
		Conditions:    ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{
		Kind:      TriggerEvent,
		Channel:   "testchan",
		EventType: "subscription",
	})

	exec, ok := waitForExec(t, ch, 2*time.Second)
	require.True(t, ok, "expected action to execute for matching event_type")
	assert.Equal(t, "sub-event-rule", exec.RuleName)
}

func TestEngineEventTriggerFilterNonMatch(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))

	rule := Rule{
		TenantID:      "local",
		Channel:       "testchan",
		Name:          "sub-event-rule",
		Enabled:       true,
		TriggerKind:   TriggerEvent,
		TriggerFilter: json.RawMessage(`{"event_type":"subscription"}`),
		Conditions:    ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{
		Kind:      TriggerEvent,
		Channel:   "testchan",
		EventType: "follow",
	})

	_, ok := waitForExec(t, ch, 500*time.Millisecond)
	assert.False(t, ok, "action should not execute for non-matching event_type")
}

func TestEngineCommandTriggerFilterMatch(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))

	rule := Rule{
		TenantID:      "local",
		Channel:       "testchan",
		Name:          "foo-cmd-rule",
		Enabled:       true,
		TriggerKind:   TriggerCommand,
		TriggerFilter: json.RawMessage(`{"command":"foo"}`),
		Conditions:    ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{
		Kind:    TriggerCommand,
		Channel: "testchan",
		Text:    "!foo arg1 arg2",
	})

	exec, ok := waitForExec(t, ch, 2*time.Second)
	require.True(t, ok, "expected action to execute for matching command")
	assert.Equal(t, "foo-cmd-rule", exec.RuleName)
}

func TestEngineCommandTriggerFilterNonMatch(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))

	rule := Rule{
		TenantID:      "local",
		Channel:       "testchan",
		Name:          "foo-cmd-rule",
		Enabled:       true,
		TriggerKind:   TriggerCommand,
		TriggerFilter: json.RawMessage(`{"command":"foo"}`),
		Conditions:    ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{
		Kind:    TriggerCommand,
		Channel: "testchan",
		Text:    "!bar arg1",
	})

	_, ok := waitForExec(t, ch, 500*time.Millisecond)
	assert.False(t, ok, "action should not execute for non-matching command")
}

func TestEngineConditionModeAll(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))
	require.NoError(t, reg.RegisterCondition(boolCondition{id: "test:bool"}))

	t.Run("all pass", func(t *testing.T) {
		rule := Rule{
			TenantID:    "local",
			Channel:     "testchan",
			Name:        "all-pass",
			Enabled:     true,
			TriggerKind: TriggerEvent,
			Conditions: ConditionList{
				Mode: ConditionModeAll,
				Conditions: []ConditionInstance{
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":true}`)},
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":true}`)},
				},
			},
			Actions: ActionList{
				Actions: []ActionInstance{
					{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
				},
			},
		}

		src := &staticSource{rules: []Rule{rule}}
		eng, err := New(Config{
			Source:        src,
			Registry:      reg,
			Logger:        discardLogger(),
			Workers:       1,
			ActionTimeout: 5 * time.Second,
		})
		require.NoError(t, err)
		eng.Start()
		defer eng.Stop()

		eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

		_, ok := waitForExec(t, ch, 2*time.Second)
		assert.True(t, ok, "all conditions pass, action should execute")
	})

	t.Run("one false blocks", func(t *testing.T) {
		rule := Rule{
			TenantID:    "local",
			Channel:     "testchan2",
			Name:        "one-false",
			Enabled:     true,
			TriggerKind: TriggerEvent,
			Conditions: ConditionList{
				Mode: ConditionModeAll,
				Conditions: []ConditionInstance{
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":true}`)},
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":false}`)},
				},
			},
			Actions: ActionList{
				Actions: []ActionInstance{
					{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
				},
			},
		}

		src := &staticSource{rules: []Rule{rule}}
		eng, err := New(Config{
			Source:        src,
			Registry:      reg,
			Logger:        discardLogger(),
			Workers:       1,
			ActionTimeout: 5 * time.Second,
		})
		require.NoError(t, err)
		eng.Start()
		defer eng.Stop()

		eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan2"})

		_, ok := waitForExec(t, ch, 500*time.Millisecond)
		assert.False(t, ok, "one false condition should block action")
	})
}

func TestEngineConditionModeAny(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))
	require.NoError(t, reg.RegisterCondition(boolCondition{id: "test:bool"}))

	t.Run("one true passes", func(t *testing.T) {
		rule := Rule{
			TenantID:    "local",
			Channel:     "testchan",
			Name:        "any-one-true",
			Enabled:     true,
			TriggerKind: TriggerEvent,
			Conditions: ConditionList{
				Mode: ConditionModeAny,
				Conditions: []ConditionInstance{
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":false}`)},
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":true}`)},
				},
			},
			Actions: ActionList{
				Actions: []ActionInstance{
					{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
				},
			},
		}

		src := &staticSource{rules: []Rule{rule}}
		eng, err := New(Config{
			Source:        src,
			Registry:      reg,
			Logger:        discardLogger(),
			Workers:       1,
			ActionTimeout: 5 * time.Second,
		})
		require.NoError(t, err)
		eng.Start()
		defer eng.Stop()

		eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

		_, ok := waitForExec(t, ch, 2*time.Second)
		assert.True(t, ok, "one true condition should pass in ANY mode")
	})

	t.Run("all false blocks", func(t *testing.T) {
		rule := Rule{
			TenantID:    "local",
			Channel:     "testchan2",
			Name:        "any-all-false",
			Enabled:     true,
			TriggerKind: TriggerEvent,
			Conditions: ConditionList{
				Mode: ConditionModeAny,
				Conditions: []ConditionInstance{
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":false}`)},
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":false}`)},
				},
			},
			Actions: ActionList{
				Actions: []ActionInstance{
					{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
				},
			},
		}

		src := &staticSource{rules: []Rule{rule}}
		eng, err := New(Config{
			Source:        src,
			Registry:      reg,
			Logger:        discardLogger(),
			Workers:       1,
			ActionTimeout: 5 * time.Second,
		})
		require.NoError(t, err)
		eng.Start()
		defer eng.Stop()

		eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan2"})

		_, ok := waitForExec(t, ch, 500*time.Millisecond)
		assert.False(t, ok, "all false conditions should block in ANY mode")
	})
}

func TestEngineConditionModeNone(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))
	require.NoError(t, reg.RegisterCondition(boolCondition{id: "test:bool"}))

	t.Run("all false passes", func(t *testing.T) {
		rule := Rule{
			TenantID:    "local",
			Channel:     "testchan",
			Name:        "none-all-false",
			Enabled:     true,
			TriggerKind: TriggerEvent,
			Conditions: ConditionList{
				Mode: ConditionModeNone,
				Conditions: []ConditionInstance{
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":false}`)},
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":false}`)},
				},
			},
			Actions: ActionList{
				Actions: []ActionInstance{
					{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
				},
			},
		}

		src := &staticSource{rules: []Rule{rule}}
		eng, err := New(Config{
			Source:        src,
			Registry:      reg,
			Logger:        discardLogger(),
			Workers:       1,
			ActionTimeout: 5 * time.Second,
		})
		require.NoError(t, err)
		eng.Start()
		defer eng.Stop()

		eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

		_, ok := waitForExec(t, ch, 2*time.Second)
		assert.True(t, ok, "all false conditions should pass in NONE mode")
	})

	t.Run("any true blocks", func(t *testing.T) {
		rule := Rule{
			TenantID:    "local",
			Channel:     "testchan2",
			Name:        "none-one-true",
			Enabled:     true,
			TriggerKind: TriggerEvent,
			Conditions: ConditionList{
				Mode: ConditionModeNone,
				Conditions: []ConditionInstance{
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":false}`)},
					{TypeID: "test:bool", Config: json.RawMessage(`{"pass":true}`)},
				},
			},
			Actions: ActionList{
				Actions: []ActionInstance{
					{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
				},
			},
		}

		src := &staticSource{rules: []Rule{rule}}
		eng, err := New(Config{
			Source:        src,
			Registry:      reg,
			Logger:        discardLogger(),
			Workers:       1,
			ActionTimeout: 5 * time.Second,
		})
		require.NoError(t, err)
		eng.Start()
		defer eng.Stop()

		eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan2"})

		_, ok := waitForExec(t, ch, 500*time.Millisecond)
		assert.False(t, ok, "any true condition should block in NONE mode")
	})
}

func TestEngineUnknownConditionFailsClosed(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))

	rule := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "unknown-cond",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions: ConditionList{
			Mode: ConditionModeAll,
			Conditions: []ConditionInstance{
				{TypeID: "nonexistent:condition", Config: json.RawMessage(`{}`)},
			},
		},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

	_, ok := waitForExec(t, ch, 500*time.Millisecond)
	assert.False(t, ok, "unknown condition type should fail closed, action should not run")
}

func TestEngineDisabledActionSkipped(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture1", ch: ch}))
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture2", ch: ch}))

	rule := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "disabled-action",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:capture1", Enabled: false, Config: json.RawMessage(`{}`)},
				{TypeID: "test:capture2", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

	exec, ok := waitForExec(t, ch, 2*time.Second)
	require.True(t, ok, "enabled action should execute")
	assert.Equal(t, "test:capture2", exec.ActionID, "only enabled action should run")

	_, ok = waitForExec(t, ch, 200*time.Millisecond)
	assert.False(t, ok, "disabled action should not have run")
}

func TestEngineActionStopHaltsSubsequent(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(outputAction{id: "test:output"}))
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))

	rule := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "stop-action",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:output", Enabled: true, Config: json.RawMessage(`{"key":"k","value":"v","stop":true}`)},
				{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

	_, ok := waitForExec(t, ch, 500*time.Millisecond)
	assert.False(t, ok, "action after Stop should not execute")
}

func TestEngineFailingActionDoesNotAbortRemaining(t *testing.T) {
	failCh := make(chan string, 10)
	captureCh := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(failingAction{id: "test:fail", ch: failCh}))
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: captureCh}))

	rule := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "fail-continue",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:fail", Enabled: true, Config: json.RawMessage(`{}`)},
				{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

	_, ok := waitForString(t, failCh, 2*time.Second)
	require.True(t, ok, "failing action should have executed")

	exec, ok := waitForExec(t, captureCh, 2*time.Second)
	require.True(t, ok, "action after failure should still execute")
	assert.Equal(t, "fail-continue", exec.RuleName)
}

func TestEngineOutputsVisibleToLaterAction(t *testing.T) {
	readCh := make(chan string, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(outputAction{id: "test:output"}))
	require.NoError(t, reg.RegisterAction(readOutputAction{id: "test:read", ch: readCh}))

	rule := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "output-chain",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:output", Enabled: true, Config: json.RawMessage(`{"key":"mykey","value":"myvalue","stop":false}`)},
				{TypeID: "test:read", Enabled: true, Config: json.RawMessage(`{"key":"mykey"}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

	val, ok := waitForString(t, readCh, 2*time.Second)
	require.True(t, ok, "read action should have executed")
	assert.Equal(t, "myvalue", val, "output from first action should be visible to second")
}

func TestEngineSerialQueueExecutesSerially(t *testing.T) {
	ch := make(chan capturedExec, 20)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(slowCaptureAction{id: "test:slow", delay: 50 * time.Millisecond, ch: ch}))

	rule1 := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "serial-rule-1",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			QueueID: "serial-queue",
			Actions: []ActionInstance{
				{TypeID: "test:slow", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	rule2 := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "serial-rule-2",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			QueueID: "serial-queue",
			Actions: []ActionInstance{
				{TypeID: "test:slow", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule1, rule2}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       4,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

	var execs []capturedExec
	for i := 0; i < 2; i++ {
		exec, ok := waitForExec(t, ch, 2*time.Second)
		require.True(t, ok, "expected 2 executions")
		execs = append(execs, exec)
	}

	require.Len(t, execs, 2)
	first, second := execs[0], execs[1]
	if first.StartTime.After(second.StartTime) {
		first, second = second, first
	}

	assert.True(t, first.EndTime.Before(second.StartTime) || first.EndTime.Equal(second.StartTime),
		"serial queue actions should not overlap: first ended %v, second started %v",
		first.EndTime, second.StartTime)
}

func TestEngineDefaultQueueUsesPool(t *testing.T) {
	ch := make(chan capturedExec, 20)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(slowCaptureAction{id: "test:slow", delay: 100 * time.Millisecond, ch: ch}))

	rule1 := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "pool-rule-1",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:slow", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	rule2 := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "pool-rule-2",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:slow", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule1, rule2}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       4,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

	var execs []capturedExec
	for i := 0; i < 2; i++ {
		exec, ok := waitForExec(t, ch, 2*time.Second)
		require.True(t, ok, "expected 2 executions")
		execs = append(execs, exec)
	}

	require.Len(t, execs, 2)
	first, second := execs[0], execs[1]
	if first.StartTime.After(second.StartTime) {
		first, second = second, first
	}

	overlap := first.EndTime.After(second.StartTime)
	assert.True(t, overlap, "pool actions should overlap: first ended %v, second started %v",
		first.EndTime, second.StartTime)
}

func TestEngineStopDrainsCleanly(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(slowCaptureAction{id: "test:slow", delay: 50 * time.Millisecond, ch: ch}))

	rule := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "drain-rule",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:slow", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       2,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		eng.Stop()
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within timeout")
	}
}

func TestEngineActionsExecuteInOrder(t *testing.T) {
	ch := make(chan capturedExec, 10)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture1", ch: ch}))
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture2", ch: ch}))
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture3", ch: ch}))

	rule := Rule{
		TenantID:    "local",
		Channel:     "testchan",
		Name:        "ordered-actions",
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "test:capture1", Enabled: true, Config: json.RawMessage(`{}`)},
				{TypeID: "test:capture2", Enabled: true, Config: json.RawMessage(`{}`)},
				{TypeID: "test:capture3", Enabled: true, Config: json.RawMessage(`{}`)},
			},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "testchan"})

	var execs []capturedExec
	for i := 0; i < 3; i++ {
		exec, ok := waitForExec(t, ch, 2*time.Second)
		require.True(t, ok, "expected 3 executions")
		execs = append(execs, exec)
	}

	require.Len(t, execs, 3)
	assert.Equal(t, "test:capture1", execs[0].ActionID)
	assert.Equal(t, "test:capture2", execs[1].ActionID)
	assert.Equal(t, "test:capture3", execs[2].ActionID)
}
