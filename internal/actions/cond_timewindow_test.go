package actions

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func TestTimeWindowCondition_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	_, ok := reg.Condition("cond:time-window")
	assert.True(t, ok)
}

func TestTimeWindowCondition_SameDayWindow(t *testing.T) {
	// 2026-01-02 12:00 UTC, window 09:00-17:00 -> inside.
	at := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	c := timeWindowCondition{now: fixedClock(at)}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"from": "09:00", "to": "17:00", "timezone": "UTC"}))
	require.NoError(t, err)
	assert.True(t, ok)

	// 08:00 -> before window.
	c = timeWindowCondition{now: fixedClock(time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC))}
	ok, err = c.Evaluate(ec, mustJSON(t, map[string]any{"from": "09:00", "to": "17:00", "timezone": "UTC"}))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestTimeWindowCondition_CrossesMidnight(t *testing.T) {
	cfg := map[string]any{"from": "22:00", "to": "02:00", "timezone": "UTC"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	cases := []struct {
		hour, min int
		want      bool
	}{
		{23, 30, true}, // after from
		{1, 0, true},   // before to
		{2, 0, false},  // at to (exclusive)
		{12, 0, false}, // midday, outside
		{22, 0, true},  // at from (inclusive)
	}
	for _, tc := range cases {
		c := timeWindowCondition{now: fixedClock(time.Date(2026, 1, 2, tc.hour, tc.min, 0, 0, time.UTC))}
		ok, err := c.Evaluate(ec, mustJSON(t, cfg))
		require.NoError(t, err)
		assert.Equal(t, tc.want, ok, "%02d:%02d", tc.hour, tc.min)
	}
}

func TestTimeWindowCondition_DaysFilter(t *testing.T) {
	// 2026-01-02 is a Friday.
	at := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	c := timeWindowCondition{now: fixedClock(at)}
	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"from": "00:00", "to": "23:59", "days": []string{"fri"}, "timezone": "UTC"}))
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = c.Evaluate(ec, mustJSON(t, map[string]any{"from": "00:00", "to": "23:59", "days": []string{"mon", "tue"}, "timezone": "UTC"}))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestTimeWindowCondition_TimezoneShiftsWindow(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata not available")
	}
	// 2026-01-02 00:30 UTC == 2026-01-01 19:30 in New York (UTC-5).
	at := time.Date(2026, 1, 2, 0, 30, 0, 0, time.UTC)
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	c := timeWindowCondition{now: fixedClock(at)}
	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"from": "19:00", "to": "20:00", "timezone": "America/New_York"}))
	require.NoError(t, err)
	assert.True(t, ok, "19:30 local is inside 19:00-20:00")
	_ = loc
}

func TestTimeWindowCondition_InvalidConfig(t *testing.T) {
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})
	c := timeWindowCondition{now: fixedClock(time.Now())}

	_, err := c.Evaluate(ec, mustJSON(t, map[string]any{"from": "9am", "to": "17:00"}))
	require.Error(t, err)

	_, err = c.Evaluate(ec, mustJSON(t, map[string]any{"from": "09:00", "to": "17:00", "timezone": "Not/AZone"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timezone")
}
