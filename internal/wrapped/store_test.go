package wrapped

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := fmt.Sprintf("file:wrapped-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestIncrementMessage_Accumulates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", "all"))
	}

	st, err := s.ViewerStat(ctx, "local", "chan-A", "v1", "all")
	require.NoError(t, err)
	assert.Equal(t, int64(3), st.Messages)
	assert.Equal(t, int64(0), st.SubsTotal)
	assert.Equal(t, int64(0), st.SubsGiven)
	assert.Equal(t, int64(0), st.RaidsStarted)
	assert.Equal(t, "alice", st.Username)
	assert.Equal(t, "v1", st.ViewerID)
	assert.Equal(t, "chan-a", st.Channel)
	assert.Equal(t, "all", st.Period)
}

func TestIncrement_UsernameAndLastSeenRefresh_FirstSeenStable(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", "all"))
	first, err := s.ViewerStat(ctx, "local", "chan-A", "v1", "all")
	require.NoError(t, err)

	time.Sleep(2 * time.Millisecond)
	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice_renamed", "all"))
	second, err := s.ViewerStat(ctx, "local", "chan-A", "v1", "all")
	require.NoError(t, err)

	// Username and last_seen move forward; first_seen is pinned.
	assert.Equal(t, "alice_renamed", second.Username)
	assert.Equal(t, first.FirstSeen, second.FirstSeen, "first_seen must not change")
	assert.False(t, second.LastSeen.Before(first.LastSeen), "last_seen must not go backwards")
	assert.True(t, second.LastSeen.After(first.LastSeen), "last_seen should advance")
	assert.False(t, second.UpdatedAt.Before(first.UpdatedAt))
}

func TestIncrementSub_And_SubGift_And_Raid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.IncrementSub(ctx, "local", "chan-A", "v1", "alice", "all"))
	require.NoError(t, s.IncrementSub(ctx, "local", "chan-A", "v1", "alice", "all"))
	require.NoError(t, s.IncrementSubGift(ctx, "local", "chan-A", "v1", "alice", "all", 5))
	require.NoError(t, s.IncrementRaidStarted(ctx, "local", "chan-A", "v1", "alice", "all"))

	st, err := s.ViewerStat(ctx, "local", "chan-A", "v1", "all")
	require.NoError(t, err)
	assert.Equal(t, int64(2), st.SubsTotal)
	assert.Equal(t, int64(5), st.SubsGiven)
	assert.Equal(t, int64(1), st.RaidsStarted)
	assert.Equal(t, int64(0), st.Messages)
}

func TestTopChatters_OrderingAndLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// bob: 3 msgs, carol: 3 msgs (tie), alice: 5 msgs, dave: 1 msg.
	for i := 0; i < 5; i++ {
		require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v_alice", "alice", "all"))
	}
	for i := 0; i < 3; i++ {
		require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v_bob", "bob", "all"))
		require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v_carol", "carol", "all"))
	}
	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v_dave", "dave", "all"))

	got, err := s.TopChatters(ctx, "local", "chan-A", "all", 100)
	require.NoError(t, err)
	require.Len(t, got, 4)
	// messages DESC, ties username ASC: alice(5), bob(3), carol(3), dave(1).
	assert.Equal(t, "alice", got[0].Username)
	assert.Equal(t, int64(5), got[0].Messages)
	assert.Equal(t, "bob", got[1].Username)
	assert.Equal(t, "carol", got[2].Username)
	assert.Equal(t, "dave", got[3].Username)

	// Limit clamps the number of rows.
	top2, err := s.TopChatters(ctx, "local", "chan-A", "all", 2)
	require.NoError(t, err)
	require.Len(t, top2, 2)
	assert.Equal(t, "alice", top2[0].Username)
	assert.Equal(t, "bob", top2[1].Username)

	// limit < 1 clamps up to 1; limit > 100 clamps down to 100.
	top1, err := s.TopChatters(ctx, "local", "chan-A", "all", 0)
	require.NoError(t, err)
	require.Len(t, top1, 1)
	assert.Equal(t, "alice", top1[0].Username)

	big, err := s.TopChatters(ctx, "local", "chan-A", "all", 9999)
	require.NoError(t, err)
	assert.Len(t, big, 4)
}

func TestTopChatters_EmptyBucket(t *testing.T) {
	s := newTestStore(t)
	got, err := s.TopChatters(context.Background(), "local", "chan-empty", "all", 10)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestPeriodIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", "all"))
	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", "all"))
	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", "2026-05"))

	all, err := s.ViewerStat(ctx, "local", "chan-A", "v1", "all")
	require.NoError(t, err)
	assert.Equal(t, int64(2), all.Messages)

	month, err := s.ViewerStat(ctx, "local", "chan-A", "v1", "2026-05")
	require.NoError(t, err)
	assert.Equal(t, int64(1), month.Messages)

	// A period with no rows is ErrNotFound, independent of the others.
	_, err = s.ViewerStat(ctx, "local", "chan-A", "v1", "2026-06")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestChannelAndViewerIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", "all"))
	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-B", "v1", "alice", "all"))
	require.NoError(t, s.IncrementMessage(ctx, "other", "chan-A", "v1", "alice", "all"))
	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v2", "bob", "all"))

	// chan-A/v1/local has exactly one message; the others do not leak in.
	a, err := s.ViewerStat(ctx, "local", "chan-A", "v1", "all")
	require.NoError(t, err)
	assert.Equal(t, int64(1), a.Messages)

	// Different channel, viewer and tenant each have their own row.
	b, err := s.ViewerStat(ctx, "local", "chan-B", "v1", "all")
	require.NoError(t, err)
	assert.Equal(t, int64(1), b.Messages)

	o, err := s.ViewerStat(ctx, "other", "chan-A", "v1", "all")
	require.NoError(t, err)
	assert.Equal(t, int64(1), o.Messages)

	top, err := s.TopChatters(ctx, "local", "chan-A", "all", 100)
	require.NoError(t, err)
	require.Len(t, top, 2) // v1 and v2 only.
}

func TestChannelTotals_SumsAndDistinctViewers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// alice: 2 msgs, 1 sub, 3 gifts, 1 raid.
	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", "all"))
	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", "all"))
	require.NoError(t, s.IncrementSub(ctx, "local", "chan-A", "v1", "alice", "all"))
	require.NoError(t, s.IncrementSubGift(ctx, "local", "chan-A", "v1", "alice", "all", 3))
	require.NoError(t, s.IncrementRaidStarted(ctx, "local", "chan-A", "v1", "alice", "all"))
	// bob: 1 msg, 2 subs.
	require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v2", "bob", "all"))
	require.NoError(t, s.IncrementSub(ctx, "local", "chan-A", "v2", "bob", "all"))
	require.NoError(t, s.IncrementSub(ctx, "local", "chan-A", "v2", "bob", "all"))

	sum, err := s.ChannelTotals(ctx, "local", "chan-A", "all")
	require.NoError(t, err)
	assert.Equal(t, "chan-a", sum.Channel)
	assert.Equal(t, "all", sum.Period)
	assert.Equal(t, int64(3), sum.TotalMessages)
	assert.Equal(t, int64(3), sum.TotalSubs)     // subs_total: 1 + 2
	assert.Equal(t, int64(3), sum.TotalSubGifts) // subs_given: 3
	assert.Equal(t, int64(1), sum.TotalRaids)    // raids_started: 1
	assert.Equal(t, int64(2), sum.TotalViewers)  // distinct: alice, bob
}

func TestChannelTotals_EmptyIsZeroValueNotError(t *testing.T) {
	s := newTestStore(t)
	sum, err := s.ChannelTotals(context.Background(), "local", "chan-empty", "2026-05")
	require.NoError(t, err)
	assert.Equal(t, "chan-empty", sum.Channel)
	assert.Equal(t, "2026-05", sum.Period)
	assert.Equal(t, int64(0), sum.TotalMessages)
	assert.Equal(t, int64(0), sum.TotalSubs)
	assert.Equal(t, int64(0), sum.TotalSubGifts)
	assert.Equal(t, int64(0), sum.TotalRaids)
	assert.Equal(t, int64(0), sum.TotalViewers)
}

func TestViewerStat_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.ViewerStat(context.Background(), "local", "chan-A", "ghost", "all")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestIncrementSubGift_NonPositiveInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	assert.ErrorIs(t, s.IncrementSubGift(ctx, "local", "chan-A", "v1", "alice", "all", 0), ErrInvalid)
	assert.ErrorIs(t, s.IncrementSubGift(ctx, "local", "chan-A", "v1", "alice", "all", -3), ErrInvalid)

	// No row should have been created by the rejected calls.
	_, err := s.ViewerStat(ctx, "local", "chan-A", "v1", "all")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestInvalidPeriod(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	bad := []string{"", "  ", "2026", "2026-13", "2026-00", "2026-5", "2026-05-01", "ALL", "all-time", "26-05"}
	for _, p := range bad {
		t.Run(fmt.Sprintf("period=%q", p), func(t *testing.T) {
			assert.ErrorIs(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", p), ErrInvalid)
			_, err := s.ViewerStat(ctx, "local", "chan-A", "v1", p)
			assert.ErrorIs(t, err, ErrInvalid)
			_, err = s.TopChatters(ctx, "local", "chan-A", p, 10)
			assert.ErrorIs(t, err, ErrInvalid)
			_, err = s.ChannelTotals(ctx, "local", "chan-A", p)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestValidPeriods(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	good := []string{"all", "2026-01", "2026-12", "2000-06", "9999-11"}
	for _, p := range good {
		require.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", p), "period %q should be valid", p)
	}
}

func TestEmptyIDsInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []struct {
		name                    string
		tenant, channel, viewer string
	}{
		{"empty tenant", "", "chan-A", "v1"},
		{"empty channel", "local", "", "v1"},
		{"whitespace channel", "local", "   ", "v1"},
		{"hash-only channel", "local", "#", "v1"},
		{"empty viewer", "local", "chan-A", ""},
		{"whitespace viewer", "local", "chan-A", "  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, s.IncrementMessage(ctx, tc.tenant, tc.channel, tc.viewer, "alice", "all"), ErrInvalid)
			assert.ErrorIs(t, s.IncrementSub(ctx, tc.tenant, tc.channel, tc.viewer, "alice", "all"), ErrInvalid)
			assert.ErrorIs(t, s.IncrementSubGift(ctx, tc.tenant, tc.channel, tc.viewer, "alice", "all", 1), ErrInvalid)
			assert.ErrorIs(t, s.IncrementRaidStarted(ctx, tc.tenant, tc.channel, tc.viewer, "alice", "all"), ErrInvalid)
			_, err := s.ViewerStat(ctx, tc.tenant, tc.channel, tc.viewer, "all")
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}

	// TopChatters and ChannelTotals validate tenant/channel only.
	_, err := s.TopChatters(ctx, "", "chan-A", "all", 10)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.TopChatters(ctx, "local", "", "all", 10)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.ChannelTotals(ctx, "", "chan-A", "all")
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.ChannelTotals(ctx, "local", "", "all")
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestChannelNormalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.IncrementMessage(ctx, "local", "#Chan-A", "v1", "alice", "all"))

	st, err := s.ViewerStat(ctx, "local", "chan-a", "v1", "all")
	require.NoError(t, err)
	assert.Equal(t, int64(1), st.Messages)
	assert.Equal(t, "chan-a", st.Channel)
}

func TestConcurrent_IncrementNoRace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, s.IncrementMessage(ctx, "local", "chan-A", "v1", "alice", "all"))
		}()
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ViewerStat(ctx, "local", "chan-A", "v1", "all")
			if err != nil {
				assert.ErrorIs(t, err, ErrNotFound)
			}
		}()
	}
	wg.Wait()

	st, err := s.ViewerStat(ctx, "local", "chan-A", "v1", "all")
	require.NoError(t, err)
	assert.Equal(t, int64(n), st.Messages)
}

func TestIsUniqueViolation(t *testing.T) {
	assert.False(t, isUniqueViolation(nil))
	assert.True(t, isUniqueViolation(fmt.Errorf("UNIQUE constraint failed: wrapped_stats.id")))
	assert.False(t, isUniqueViolation(fmt.Errorf("some other error")))
}
