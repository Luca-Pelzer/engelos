package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type fakeProfileProvider struct {
	profile UserProfile
	err     error
}

func (f fakeProfileProvider) UserProfile(_ context.Context, _ string) (UserProfile, error) {
	return f.profile, f.err
}

type fakeStreamStatus struct {
	status StreamStatus
	err    error
}

func (f fakeStreamStatus) Status(_ context.Context, _ string) (StreamStatus, error) {
	return f.status, f.err
}

func TestAccountAge_Self(t *testing.T) {
	created := time.Now().Add(-(3*365 + 40) * 24 * time.Hour)
	cmd := NewAccountAgeCommand(fakeProfileProvider{profile: UserProfile{Login: "bob", DisplayName: "Bob", CreatedAt: created}})
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Contains(t, reply.Text, "your account is")
	assert.Contains(t, reply.Text, "3y")
}

func TestAccountAge_Other(t *testing.T) {
	created := time.Now().Add(-200 * 24 * time.Hour)
	cmd := NewAccountAgeCommand(fakeProfileProvider{profile: UserProfile{Login: "alice", DisplayName: "Alice", CreatedAt: created}})
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"@alice"})
	assert.Contains(t, reply.Text, "Alice's account is")
}

func TestAccountAge_Error(t *testing.T) {
	cmd := NewAccountAgeCommand(fakeProfileProvider{err: errors.New("boom")})
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Contains(t, strings.ToLower(reply.Text), "couldn't look that up")
}

func TestAccountAge_NilProvider(t *testing.T) {
	cmd := NewAccountAgeCommand(nil)
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Contains(t, reply.Text, "unavailable")
}

func TestAccountAge_MinRoleEveryone(t *testing.T) {
	assert.Equal(t, RoleEveryone, NewAccountAgeCommand(nil).MinRole)
}

func TestShoutout_WithGame(t *testing.T) {
	cmd := NewShoutoutCommand(
		fakeProfileProvider{profile: UserProfile{Login: "alice", DisplayName: "Alice"}},
		fakeStreamStatus{status: StreamStatus{GameName: "Elden Ring"}},
	)
	reply := cmd.Handler(context.Background(), Message{Username: "mod", IsModerator: true}, []string{"@alice"})
	assert.Contains(t, reply.Text, "twitch.tv/alice")
	assert.Contains(t, reply.Text, "Alice")
	assert.Contains(t, reply.Text, "Elden Ring")
}

func TestShoutout_NoGame(t *testing.T) {
	cmd := NewShoutoutCommand(
		fakeProfileProvider{profile: UserProfile{Login: "alice", DisplayName: "Alice"}},
		fakeStreamStatus{status: StreamStatus{}},
	)
	reply := cmd.Handler(context.Background(), Message{Username: "mod"}, []string{"alice"})
	assert.Contains(t, reply.Text, "twitch.tv/alice")
	assert.NotContains(t, reply.Text, "last seen playing")
}

func TestShoutout_Usage(t *testing.T) {
	cmd := NewShoutoutCommand(nil, nil)
	reply := cmd.Handler(context.Background(), Message{Username: "mod"}, nil)
	assert.Contains(t, reply.Text, "usage")
}

func TestShoutout_MinRoleModerator(t *testing.T) {
	assert.Equal(t, RoleModerator, NewShoutoutCommand(nil, nil).MinRole)
}

func TestShoutout_ProfileFallbackOnError(t *testing.T) {
	// When the profile lookup fails, the command still shouts out using the
	// raw target as both login and display name.
	cmd := NewShoutoutCommand(
		fakeProfileProvider{err: errors.New("boom")},
		fakeStreamStatus{err: errors.New("boom")},
	)
	reply := cmd.Handler(context.Background(), Message{Username: "mod"}, []string{"@SomeStreamer"})
	assert.Contains(t, reply.Text, "twitch.tv/SomeStreamer")
}

func TestFormatAge(t *testing.T) {
	cases := map[time.Duration]string{
		12 * time.Hour:              "less than a day",
		5 * 24 * time.Hour:          "5d",
		40 * 24 * time.Hour:         "1m 10d",
		(365 + 60) * 24 * time.Hour: "1y 2m",
	}
	for d, want := range cases {
		assert.Equal(t, want, formatAge(d), "duration %v", d)
	}
}
