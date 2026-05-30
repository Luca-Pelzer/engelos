package redemptions

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreate_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	b, err := s.Create(ctx, "local", "chan-A", Binding{
		RewardID:    "reward-1",
		RewardTitle: "Hydrate",
		ActionType:  ActionChatMessage,
		ActionParam: "drink some water",
		Enabled:     true,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, b.ID)
	assert.False(t, b.CreatedAt.IsZero())
	assert.False(t, b.UpdatedAt.IsZero())
	assert.Equal(t, "local", b.TenantID)
	assert.Equal(t, "chan-A", b.Channel)

	got, err := s.GetByReward(ctx, "local", "chan-A", "reward-1")
	require.NoError(t, err)
	assert.Equal(t, b.ID, got.ID)
	assert.Equal(t, "Hydrate", got.RewardTitle)
	assert.Equal(t, ActionChatMessage, got.ActionType)
	assert.Equal(t, "drink some water", got.ActionParam)
	assert.True(t, got.Enabled)
}

func TestCreate_DuplicateConflict(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, "local", "chan-A", Binding{RewardID: "reward-1", ActionType: ActionNone})
	require.NoError(t, err)

	_, err = s.Create(ctx, "local", "chan-A", Binding{RewardID: "reward-1", ActionType: ActionNone})
	assert.ErrorIs(t, err, ErrConflict)
}

func TestCreate_ScopedByTenantAndChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, "local", "chan-A", Binding{RewardID: "reward-1", ActionType: ActionNone})
	require.NoError(t, err)

	_, err = s.Create(ctx, "local", "chan-B", Binding{RewardID: "reward-1", ActionType: ActionNone})
	require.NoError(t, err, "same reward_id under a different channel is allowed")

	_, err = s.Create(ctx, "other-tenant", "chan-A", Binding{RewardID: "reward-1", ActionType: ActionNone})
	require.NoError(t, err, "same reward_id under a different tenant is allowed")
}

func TestGetByReward_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetByReward(context.Background(), "local", "chan-A", "absent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestList_OrderingAndScoping(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, "local", "chan-A", Binding{RewardID: "r-z", RewardTitle: "Zebra", ActionType: ActionNone})
	require.NoError(t, err)
	_, err = s.Create(ctx, "local", "chan-A", Binding{RewardID: "r-a", RewardTitle: "Apple", ActionType: ActionNone})
	require.NoError(t, err)
	_, err = s.Create(ctx, "local", "chan-A", Binding{RewardID: "r-2", RewardTitle: "", ActionType: ActionNone})
	require.NoError(t, err)
	_, err = s.Create(ctx, "local", "chan-A", Binding{RewardID: "r-1", RewardTitle: "", ActionType: ActionNone})
	require.NoError(t, err)

	_, err = s.Create(ctx, "local", "chan-B", Binding{RewardID: "r-other", ActionType: ActionNone})
	require.NoError(t, err)

	got, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, "r-1", got[0].RewardID)
	assert.Equal(t, "r-2", got[1].RewardID)
	assert.Equal(t, "Apple", got[2].RewardTitle)
	assert.Equal(t, "Zebra", got[3].RewardTitle)

	gotB, err := s.List(ctx, "local", "chan-B")
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	assert.Equal(t, "r-other", gotB[0].RewardID)
}

func TestList_EmptyChannel(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List(context.Background(), "local", "chan-empty")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestUpdate_MutatesFieldsAndBumpsUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, "local", "chan-A", Binding{
		RewardID:    "reward-1",
		RewardTitle: "Old",
		ActionType:  ActionNone,
		Enabled:     true,
	})
	require.NoError(t, err)

	updated, err := s.Update(ctx, "local", "chan-A", "reward-1", Binding{
		RewardTitle: "New",
		ActionType:  ActionCounterIncr,
		ActionParam: "deaths",
		Enabled:     false,
		AutoFulfill: true,
	})
	require.NoError(t, err)
	assert.Equal(t, created.ID, updated.ID, "ID is immutable")
	assert.Equal(t, created.CreatedAt.UnixNano(), updated.CreatedAt.UnixNano(), "CreatedAt is immutable")
	assert.False(t, updated.UpdatedAt.Before(created.UpdatedAt), "UpdatedAt is bumped")
	assert.Equal(t, "New", updated.RewardTitle)
	assert.Equal(t, ActionCounterIncr, updated.ActionType)
	assert.Equal(t, "deaths", updated.ActionParam)
	assert.False(t, updated.Enabled)
	assert.True(t, updated.AutoFulfill)
}

func TestUpdate_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Update(context.Background(), "local", "chan-A", "absent", Binding{ActionType: ActionNone})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUpdate_BadActionType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, "local", "chan-A", Binding{RewardID: "reward-1", ActionType: ActionNone})
	require.NoError(t, err)

	_, err = s.Update(ctx, "local", "chan-A", "reward-1", Binding{ActionType: "bogus"})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestSetEnabled_TogglesAndNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, "local", "chan-A", Binding{RewardID: "reward-1", ActionType: ActionNone, Enabled: true})
	require.NoError(t, err)

	require.NoError(t, s.SetEnabled(ctx, "local", "chan-A", "reward-1", false))
	got, err := s.GetByReward(ctx, "local", "chan-A", "reward-1")
	require.NoError(t, err)
	assert.False(t, got.Enabled)

	require.NoError(t, s.SetEnabled(ctx, "local", "chan-A", "reward-1", true))
	got, err = s.GetByReward(ctx, "local", "chan-A", "reward-1")
	require.NoError(t, err)
	assert.True(t, got.Enabled)

	assert.ErrorIs(t, s.SetEnabled(ctx, "local", "chan-A", "absent", true), ErrNotFound)
}

func TestDelete_RemovesAndMissing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, "local", "chan-A", Binding{RewardID: "reward-1", ActionType: ActionNone})
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, "local", "chan-A", "reward-1"))
	_, err = s.GetByReward(ctx, "local", "chan-A", "reward-1")
	assert.ErrorIs(t, err, ErrNotFound)

	assert.ErrorIs(t, s.Delete(ctx, "local", "chan-A", "reward-1"), ErrNotFound)
}

func TestValidation_Errors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []struct {
		name     string
		tenantID string
		channel  string
		binding  Binding
	}{
		{"empty reward_id", "local", "chan-A", Binding{RewardID: "", ActionType: ActionNone}},
		{"whitespace reward_id", "local", "chan-A", Binding{RewardID: "   ", ActionType: ActionNone}},
		{"reward_id too long", "local", "chan-A", Binding{RewardID: strings.Repeat("x", maxRewardIDLen+1), ActionType: ActionNone}},
		{"reward_title too long", "local", "chan-A", Binding{RewardID: "r", RewardTitle: strings.Repeat("y", maxRewardTitleLen+1), ActionType: ActionNone}},
		{"action_param too long", "local", "chan-A", Binding{RewardID: "r", ActionType: ActionChatMessage, ActionParam: strings.Repeat("z", maxActionParamLen+1)}},
		{"unknown action_type", "local", "chan-A", Binding{RewardID: "r", ActionType: "bogus"}},
		{"empty action_type", "local", "chan-A", Binding{RewardID: "r", ActionType: ""}},
		{"empty tenant", "", "chan-A", Binding{RewardID: "r", ActionType: ActionNone}},
		{"empty channel", "local", "", Binding{RewardID: "r", ActionType: ActionNone}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.Create(ctx, c.tenantID, c.channel, c.binding)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestBool_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, "local", "chan-A", Binding{
		RewardID:    "reward-1",
		ActionType:  ActionNone,
		Enabled:     false,
		AutoFulfill: true,
	})
	require.NoError(t, err)

	got, err := s.GetByReward(ctx, "local", "chan-A", "reward-1")
	require.NoError(t, err)
	assert.False(t, got.Enabled)
	assert.True(t, got.AutoFulfill)
}

func TestErrors_AreDistinct(t *testing.T) {
	assert.NotEqual(t, ErrNotFound, ErrInvalid)
	assert.NotEqual(t, ErrNotFound, ErrConflict)
	assert.NotEqual(t, ErrInvalid, ErrConflict)
}

func TestConcurrent_CreateDistinctRewards(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Create(ctx, "local", "chan-A", Binding{
				RewardID:   "reward-" + strconv.Itoa(i),
				ActionType: ActionNone,
			})
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	list, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Len(t, list, n)
}

func TestConcurrent_CreateSameRewardOneWinner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 50
	var (
		wg        sync.WaitGroup
		successes atomic.Int64
		conflicts atomic.Int64
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Create(ctx, "local", "chan-A", Binding{
				RewardID:   "reward-shared",
				ActionType: ActionNone,
			})
			switch {
			case err == nil:
				successes.Add(1)
			case assert.ErrorIs(t, err, ErrConflict):
				conflicts.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(1), successes.Load(), "exactly one Create wins")
	assert.Equal(t, int64(n-1), conflicts.Load(), "the rest get ErrConflict")

	list, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Len(t, list, 1, "exactly one row persisted")
}
