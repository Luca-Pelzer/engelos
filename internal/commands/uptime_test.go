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

type fakeUptimeProvider struct {
	since time.Time
	live  bool
	err   error
}

func (f fakeUptimeProvider) Uptime(_ context.Context, _ string) (time.Time, bool, error) {
	return f.since, f.live, f.err
}

func TestNewUptimeCommand_Live(t *testing.T) {
	p := fakeUptimeProvider{since: time.Now().Add(-(2*time.Hour + 13*time.Minute)), live: true}
	cmd := NewUptimeCommand(p)
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "engelswtf")
	assert.Contains(t, reply.Text, "2h")
	assert.Contains(t, reply.Text, "13m")
}

func TestNewUptimeCommand_Offline(t *testing.T) {
	cmd := NewUptimeCommand(fakeUptimeProvider{live: false})
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "offline")
	assert.Contains(t, reply.Text, "engelswtf")
}

func TestNewUptimeCommand_Error(t *testing.T) {
	cmd := NewUptimeCommand(fakeUptimeProvider{err: errors.New("boom")})
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, strings.ToLower(reply.Text), "couldn't check uptime")
}

func TestNewUptimeCommand_NilProvider(t *testing.T) {
	cmd := NewUptimeCommand(nil)
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "unavailable")
}

func TestNewUptimeCommand_MinRoleEveryone(t *testing.T) {
	cmd := NewUptimeCommand(fakeUptimeProvider{})
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"sub-minute", 30 * time.Second, "just went live"},
		{"minutes only", 45 * time.Minute, "45m"},
		{"hours and minutes", 2*time.Hour + 13*time.Minute, "2h 13m"},
		{"days and hours", 3*24*time.Hour + 4*time.Hour, "3d 4h"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, formatDuration(c.in))
		})
	}
}
