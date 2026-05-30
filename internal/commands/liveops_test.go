package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type addEventCall struct {
	channel     string
	name        string
	description string
	startsAt    time.Time
	endsAt      *time.Time
}

type fakeEventStore struct {
	next     ScheduledEvent
	nextOK   bool
	nextErr  error
	upcoming []ScheduledEvent
	upErr    error
	addNum   int
	addErr   error
	delErr   error
	addCalls []addEventCall
	delCalls []int
}

func (f *fakeEventStore) Next(_ context.Context, _ string) (ScheduledEvent, bool, error) {
	return f.next, f.nextOK, f.nextErr
}

func (f *fakeEventStore) Upcoming(_ context.Context, _ string, _ int) ([]ScheduledEvent, error) {
	return f.upcoming, f.upErr
}

func (f *fakeEventStore) Add(_ context.Context, channel, name, description string, startsAt time.Time, endsAt *time.Time) (int, error) {
	f.addCalls = append(f.addCalls, addEventCall{channel, name, description, startsAt, endsAt})
	return f.addNum, f.addErr
}

func (f *fakeEventStore) Delete(_ context.Context, _ string, number int) error {
	f.delCalls = append(f.delCalls, number)
	return f.delErr
}

func eventMsg(text string) Message {
	return Message{Platform: "twitch", Channel: "engelswtf", Username: "alice", Text: text}
}

func TestNextEvent(t *testing.T) {
	soon := time.Now().Add(2*time.Hour + 13*time.Minute)
	cases := []struct {
		name     string
		store    *fakeEventStore
		nilStore bool
		want     string
		notWant  string
	}{
		{name: "nil store", nilStore: true, want: "events unavailable"},
		{name: "none", store: &fakeEventStore{nextOK: false}, want: "no upcoming events"},
		{name: "error", store: &fakeEventStore{nextErr: errors.New("boom")}, want: "couldn't check events"},
		{
			name:  "active",
			store: &fakeEventStore{nextOK: true, next: ScheduledEvent{Name: "Double Points", Active: true}},
			want:  "Double Points is happening now!",
		},
		{
			name:    "upcoming",
			store:   &fakeEventStore{nextOK: true, next: ScheduledEvent{Name: "Season 3", StartsAt: soon}},
			want:    "Next: Season 3",
			notWant: "now!",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var cmd Command
			if c.nilStore {
				cmd = NewNextEventCommand(nil)
			} else {
				cmd = NewNextEventCommand(c.store)
			}
			assert.Equal(t, RoleEveryone, cmd.MinRole)
			reply := cmd.Handler(context.Background(), eventMsg("!nextevent"), nil)
			assert.Contains(t, reply.Text, c.want)
			if c.notWant != "" {
				assert.NotContains(t, reply.Text, c.notWant)
			}
		})
	}
}

func TestNextEvent_Countdown(t *testing.T) {
	store := &fakeEventStore{nextOK: true, next: ScheduledEvent{
		Name: "Season 3", StartsAt: time.Now().Add(2*time.Hour + 13*time.Minute)}}
	reply := NewNextEventCommand(store).Handler(context.Background(), eventMsg("!nextevent"), nil)
	assert.Contains(t, reply.Text, "in 2h")
}

func TestSchedule(t *testing.T) {
	in2h := time.Now().Add(2*time.Hour + time.Minute)
	in3h := time.Now().Add(3*time.Hour + time.Minute)

	cases := []struct {
		name     string
		store    *fakeEventStore
		nilStore bool
		want     string
	}{
		{name: "nil store", nilStore: true, want: "events unavailable"},
		{name: "empty", store: &fakeEventStore{upcoming: nil}, want: "no upcoming events"},
		{name: "error", store: &fakeEventStore{upErr: errors.New("boom")}, want: "couldn't check the schedule"},
		{
			name: "list with active",
			store: &fakeEventStore{upcoming: []ScheduledEvent{
				{Name: "Live Now", Active: true},
				{Name: "Season 3", StartsAt: in2h},
				{Name: "Finale", StartsAt: in3h},
			}},
			want: "Schedule: Live Now (now) | Season 3 (in 2h",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var cmd Command
			if c.nilStore {
				cmd = NewScheduleCommand(nil)
			} else {
				cmd = NewScheduleCommand(c.store)
			}
			assert.Equal(t, RoleEveryone, cmd.MinRole)
			reply := cmd.Handler(context.Background(), eventMsg("!schedule"), nil)
			assert.Contains(t, reply.Text, c.want)
			assert.NotContains(t, reply.Text, "\n")
		})
	}
}

func TestAddEvent(t *testing.T) {
	t.Run("nil store", func(t *testing.T) {
		reply := NewAddEventCommand(nil).Handler(context.Background(), eventMsg("!addevent 2d gg"), []string{"2d", "gg"})
		assert.Contains(t, reply.Text, "events unavailable")
	})

	t.Run("missing args", func(t *testing.T) {
		store := &fakeEventStore{}
		reply := NewAddEventCommand(store).Handler(context.Background(), eventMsg("!addevent 2d"), []string{"2d"})
		assert.Contains(t, reply.Text, "usage:")
		assert.Empty(t, store.addCalls)
	})

	t.Run("bad when", func(t *testing.T) {
		store := &fakeEventStore{}
		reply := NewAddEventCommand(store).Handler(context.Background(),
			eventMsg("!addevent notawhen Season"), []string{"notawhen", "Season"})
		assert.Contains(t, reply.Text, "usage:")
		assert.Empty(t, store.addCalls)
	})

	t.Run("success", func(t *testing.T) {
		store := &fakeEventStore{addNum: 7}
		cmd := NewAddEventCommand(store)
		assert.Equal(t, RoleModerator, cmd.MinRole)
		reply := cmd.Handler(context.Background(),
			eventMsg("!addevent 2d Double Points Weekend"),
			[]string{"2d", "Double", "Points", "Weekend"})
		require.Len(t, store.addCalls, 1)
		assert.Equal(t, "engelswtf", store.addCalls[0].channel)
		assert.Equal(t, "Double Points Weekend", store.addCalls[0].name)
		assert.Equal(t, "", store.addCalls[0].description)
		assert.Nil(t, store.addCalls[0].endsAt)
		assert.Contains(t, reply.Text, "added 'Double Points Weekend' (#7)")
		assert.Contains(t, reply.Text, "in 1d")
	})

	t.Run("store error", func(t *testing.T) {
		store := &fakeEventStore{addErr: errors.New("boom")}
		reply := NewAddEventCommand(store).Handler(context.Background(),
			eventMsg("!addevent 4h Raid"), []string{"4h", "Raid"})
		assert.Contains(t, reply.Text, "couldn't add that event")
	})
}

func TestDelEvent(t *testing.T) {
	t.Run("nil store", func(t *testing.T) {
		reply := NewDelEventCommand(nil).Handler(context.Background(), eventMsg("!delevent 1"), []string{"1"})
		assert.Contains(t, reply.Text, "events unavailable")
	})

	t.Run("missing arg", func(t *testing.T) {
		store := &fakeEventStore{}
		reply := NewDelEventCommand(store).Handler(context.Background(), eventMsg("!delevent"), nil)
		assert.Contains(t, reply.Text, "usage:")
		assert.Empty(t, store.delCalls)
	})

	t.Run("bad arg", func(t *testing.T) {
		store := &fakeEventStore{}
		reply := NewDelEventCommand(store).Handler(context.Background(), eventMsg("!delevent abc"), []string{"abc"})
		assert.Contains(t, reply.Text, "usage:")
		assert.Empty(t, store.delCalls)
	})

	t.Run("success", func(t *testing.T) {
		store := &fakeEventStore{}
		cmd := NewDelEventCommand(store)
		assert.Equal(t, RoleModerator, cmd.MinRole)
		reply := cmd.Handler(context.Background(), eventMsg("!delevent 3"), []string{"3"})
		require.Len(t, store.delCalls, 1)
		assert.Equal(t, 3, store.delCalls[0])
		assert.Contains(t, reply.Text, "deleted event #3")
	})

	t.Run("not found", func(t *testing.T) {
		store := &fakeEventStore{delErr: errors.New("missing")}
		reply := NewDelEventCommand(store).Handler(context.Background(), eventMsg("!delevent 9"), []string{"9"})
		assert.Contains(t, reply.Text, "no event #9")
	})
}

func TestParseWhen(t *testing.T) {
	t.Run("relative combos", func(t *testing.T) {
		cases := []struct {
			token string
			want  time.Duration
		}{
			{"2d", 2 * 24 * time.Hour},
			{"4h", 4 * time.Hour},
			{"90m", 90 * time.Minute},
			{"1d12h", 24*time.Hour + 12*time.Hour},
			{"2d4h30m", 2*24*time.Hour + 4*time.Hour + 30*time.Minute},
		}
		for _, c := range cases {
			before := time.Now().UTC()
			got, err := parseWhen(c.token)
			require.NoError(t, err, c.token)
			after := time.Now().UTC()
			assert.False(t, got.Before(before.Add(c.want)), "token %s too early", c.token)
			assert.False(t, got.After(after.Add(c.want)), "token %s too late", c.token)
		}
	})

	t.Run("absolute date", func(t *testing.T) {
		got, err := parseWhen("2026-06-15")
		require.NoError(t, err)
		assert.Equal(t, 2026, got.Year())
		assert.Equal(t, time.June, got.Month())
		assert.Equal(t, 15, got.Day())
		assert.Equal(t, 0, got.Hour())
		assert.Equal(t, time.UTC, got.Location())
	})

	t.Run("absolute datetime", func(t *testing.T) {
		got, err := parseWhen("2026-06-15T18:30")
		require.NoError(t, err)
		assert.Equal(t, 18, got.Hour())
		assert.Equal(t, 30, got.Minute())
		assert.Equal(t, time.UTC, got.Location())
	})

	t.Run("invalid", func(t *testing.T) {
		for _, bad := range []string{"", "   ", "tomorrow", "2d3", "5x", "2026-13-40", "abc"} {
			_, err := parseWhen(strings.TrimSpace(bad))
			assert.Error(t, err, "token %q must be invalid", bad)
		}
	})
}

func TestFormatCountdown(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"sub-minute", 30 * time.Second, "soon"},
		{"minutes only", 45 * time.Minute, "in 45m"},
		{"hours and minutes", 2*time.Hour + 13*time.Minute, "in 2h 13m"},
		{"days and hours", 2*24*time.Hour + 4*time.Hour, "in 2d 4h"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, formatCountdown(c.in))
		})
	}
}
