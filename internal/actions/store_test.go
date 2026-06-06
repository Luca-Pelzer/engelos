package actions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_CreateGetRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := sampleRule("chan-A", "my-rule")
	created, err := s.Create(ctx, r)
	require.NoError(t, err)

	assert.NotEmpty(t, created.ID)
	assert.Equal(t, SchemaVersion, created.SchemaVersion)
	assert.False(t, created.CreatedAt.IsZero())
	assert.False(t, created.UpdatedAt.IsZero())
	assert.Equal(t, created.CreatedAt, created.UpdatedAt)
	assert.Equal(t, "my-rule", created.Name)
	assert.Equal(t, "local", created.TenantID)
	assert.Equal(t, "chan-A", created.Channel)
	assert.True(t, created.Enabled)

	got, err := s.Get(ctx, "local", "chan-A", "my-rule")
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, created.Name, got.Name)
	assert.Equal(t, created.SchemaVersion, got.SchemaVersion)
	assert.WithinDuration(t, created.CreatedAt, got.CreatedAt, time.Second)
	assert.WithinDuration(t, created.UpdatedAt, got.UpdatedAt, time.Second)
}

func TestStore_CreateDuplicateReturnsErrAlreadyExists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := sampleRule("chan-A", "dup-rule")
	_, err := s.Create(ctx, r)
	require.NoError(t, err)

	_, err = s.Create(ctx, r)
	assert.ErrorIs(t, err, ErrAlreadyExists)
}

func TestStore_GetMissingReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Get(ctx, "local", "chan-A", "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStore_UpdateMissingReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := sampleRule("chan-A", "missing-rule")
	_, err := s.Update(ctx, r)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStore_UpdateChangesFieldsAndBumpsUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := sampleRule("chan-A", "upd-rule")
	created, err := s.Create(ctx, r)
	require.NoError(t, err)
	originalUpdatedAt := created.UpdatedAt

	time.Sleep(10 * time.Millisecond)

	created.Enabled = false
	created.TriggerKind = TriggerCommand
	created.Actions = ActionList{
		Actions: []ActionInstance{
			{TypeID: "builtin:chat", Enabled: true, Config: json.RawMessage(`{"msg":"updated"}`)},
		},
	}
	updated, err := s.Update(ctx, created)
	require.NoError(t, err)

	assert.False(t, updated.Enabled)
	assert.Equal(t, TriggerCommand, updated.TriggerKind)
	assert.True(t, updated.UpdatedAt.After(originalUpdatedAt))

	got, err := s.Get(ctx, "local", "chan-A", "upd-rule")
	require.NoError(t, err)
	assert.False(t, got.Enabled)
	assert.Equal(t, TriggerCommand, got.TriggerKind)
	require.Len(t, got.Actions.Actions, 1)
	assert.Equal(t, "builtin:chat", got.Actions.Actions[0].TypeID)
}

func TestStore_DeleteRemovesAndSecondDeleteReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := sampleRule("chan-A", "del-rule")
	_, err := s.Create(ctx, r)
	require.NoError(t, err)

	err = s.Delete(ctx, "local", "chan-A", "del-rule")
	require.NoError(t, err)

	_, err = s.Get(ctx, "local", "chan-A", "del-rule")
	assert.ErrorIs(t, err, ErrNotFound)

	err = s.Delete(ctx, "local", "chan-A", "del-rule")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStore_ListReturnsRulesOrderedByNameASC(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleRule("chan-A", "zebra"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleRule("chan-A", "alpha"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleRule("chan-A", "middle"))
	require.NoError(t, err)

	rules, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, rules, 3)
	assert.Equal(t, "alpha", rules[0].Name)
	assert.Equal(t, "middle", rules[1].Name)
	assert.Equal(t, "zebra", rules[2].Name)
}

func TestStore_ListScopedToTenantChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleRule("chan-A", "rule-a"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleRule("chan-B", "rule-b"))
	require.NoError(t, err)

	ruleOther := sampleRule("chan-A", "rule-other-tenant")
	ruleOther.TenantID = "other-tenant"
	_, err = s.Create(ctx, ruleOther)
	require.NoError(t, err)

	rulesA, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, rulesA, 1)
	assert.Equal(t, "rule-a", rulesA[0].Name)

	rulesB, err := s.List(ctx, "local", "chan-B")
	require.NoError(t, err)
	require.Len(t, rulesB, 1)
	assert.Equal(t, "rule-b", rulesB[0].Name)

	rulesOther, err := s.List(ctx, "other-tenant", "chan-A")
	require.NoError(t, err)
	require.Len(t, rulesOther, 1)
	assert.Equal(t, "rule-other-tenant", rulesOther[0].Name)
}

func TestStore_ListEnabledExcludesDisabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	enabled := sampleRule("chan-A", "enabled-rule")
	enabled.Enabled = true
	_, err := s.Create(ctx, enabled)
	require.NoError(t, err)

	disabled := sampleRule("chan-A", "disabled-rule")
	disabled.Enabled = false
	_, err = s.Create(ctx, disabled)
	require.NoError(t, err)

	allRules, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Len(t, allRules, 2)

	enabledRules, err := s.ListEnabled(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, enabledRules, 1)
	assert.Equal(t, "enabled-rule", enabledRules[0].Name)
}

func TestStore_SetEnabledTogglesFlagAndReflectedByListEnabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := sampleRule("chan-A", "toggle-rule")
	r.Enabled = true
	_, err := s.Create(ctx, r)
	require.NoError(t, err)

	enabled, err := s.ListEnabled(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, enabled, 1)

	err = s.SetEnabled(ctx, "local", "chan-A", "toggle-rule", false)
	require.NoError(t, err)

	enabled, err = s.ListEnabled(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Len(t, enabled, 0)

	got, err := s.Get(ctx, "local", "chan-A", "toggle-rule")
	require.NoError(t, err)
	assert.False(t, got.Enabled)

	err = s.SetEnabled(ctx, "local", "chan-A", "toggle-rule", true)
	require.NoError(t, err)

	enabled, err = s.ListEnabled(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, enabled, 1)
	assert.Equal(t, "toggle-rule", enabled[0].Name)
}

func TestStore_SetEnabledMissingReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.SetEnabled(ctx, "local", "chan-A", "nonexistent", true)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStore_ChannelScoping(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleRule("channelA", "scoped-rule"))
	require.NoError(t, err)

	_, err = s.Get(ctx, "local", "channelB", "scoped-rule")
	assert.ErrorIs(t, err, ErrNotFound)

	rulesB, err := s.List(ctx, "local", "channelB")
	require.NoError(t, err)
	assert.Empty(t, rulesB)
}

func TestStore_JSONRoundTripConditionsAndActions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := Rule{
		TenantID:      "local",
		Channel:       "chan-A",
		Name:          "json-test",
		Enabled:       true,
		TriggerKind:   TriggerEvent,
		TriggerFilter: json.RawMessage(`{"event_type":"follow"}`),
		Conditions: ConditionList{
			Mode: ConditionModeAll,
			Conditions: []ConditionInstance{
				{TypeID: "builtin:cooldown", Config: json.RawMessage(`{"seconds":30}`)},
			},
		},
		Actions: ActionList{
			QueueID: "my-queue",
			Actions: []ActionInstance{
				{TypeID: "builtin:log", Config: json.RawMessage(`{"level":"info"}`), Enabled: true},
				{TypeID: "builtin:chat", Config: json.RawMessage(`{"message":"hello"}`), Enabled: false},
			},
		},
	}

	created, err := s.Create(ctx, r)
	require.NoError(t, err)

	got, err := s.Get(ctx, "local", "chan-A", "json-test")
	require.NoError(t, err)

	assert.JSONEq(t, `{"event_type":"follow"}`, string(got.TriggerFilter))

	require.Equal(t, ConditionModeAll, got.Conditions.Mode)
	require.Len(t, got.Conditions.Conditions, 1)
	assert.Equal(t, "builtin:cooldown", got.Conditions.Conditions[0].TypeID)
	assert.JSONEq(t, `{"seconds":30}`, string(got.Conditions.Conditions[0].Config))

	assert.Equal(t, "my-queue", got.Actions.QueueID)
	require.Len(t, got.Actions.Actions, 2)
	assert.Equal(t, "builtin:log", got.Actions.Actions[0].TypeID)
	assert.JSONEq(t, `{"level":"info"}`, string(got.Actions.Actions[0].Config))
	assert.True(t, got.Actions.Actions[0].Enabled)
	assert.Equal(t, "builtin:chat", got.Actions.Actions[1].TypeID)
	assert.JSONEq(t, `{"message":"hello"}`, string(got.Actions.Actions[1].Config))
	assert.False(t, got.Actions.Actions[1].Enabled)

	assert.Equal(t, created.ID, got.ID)
}

func TestStore_ValidationEmptyNameReturnsErrInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := sampleRule("chan-A", "")
	_, err := s.Create(ctx, r)
	assert.True(t, errors.Is(err, ErrInvalid), "expected ErrInvalid, got %v", err)
}

func TestStore_ValidationNoActionsReturnsErrInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := sampleRule("chan-A", "no-actions")
	r.Actions = ActionList{Actions: []ActionInstance{}}
	_, err := s.Create(ctx, r)
	assert.True(t, errors.Is(err, ErrInvalid), "expected ErrInvalid, got %v", err)
}

func TestStore_ValidationInvalidTriggerKindReturnsErrInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := sampleRule("chan-A", "bad-trigger")
	r.TriggerKind = "unknown"
	_, err := s.Create(ctx, r)
	assert.True(t, errors.Is(err, ErrInvalid), "expected ErrInvalid, got %v", err)
}

func TestStore_ValidationNameTooLongReturnsErrInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	longName := ""
	for i := 0; i < 65; i++ {
		longName += "x"
	}
	r := sampleRule("chan-A", longName)
	_, err := s.Create(ctx, r)
	assert.True(t, errors.Is(err, ErrInvalid), "expected ErrInvalid, got %v", err)
}

func TestStore_NameNormalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r := sampleRule("chan-A", "  MyRule  ")
	_, err := s.Create(ctx, r)
	require.NoError(t, err)

	got, err := s.Get(ctx, "local", "chan-A", "myrule")
	require.NoError(t, err)
	assert.Equal(t, "myrule", got.Name)

	got, err = s.Get(ctx, "local", "chan-A", "MYRULE")
	require.NoError(t, err)
	assert.Equal(t, "myrule", got.Name)
}

func TestStore_ListEmptyChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rules, err := s.List(ctx, "local", "empty-chan")
	require.NoError(t, err)
	assert.Empty(t, rules)
}

func TestStore_ListEnabledEmptyChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rules, err := s.ListEnabled(ctx, "local", "empty-chan")
	require.NoError(t, err)
	assert.Empty(t, rules)
}

func TestStore_ErrorsAreDistinct(t *testing.T) {
	assert.NotEqual(t, ErrNotFound, ErrAlreadyExists)
	assert.NotEqual(t, ErrNotFound, ErrInvalid)
	assert.NotEqual(t, ErrAlreadyExists, ErrInvalid)
}
