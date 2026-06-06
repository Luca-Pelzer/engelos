package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeStreamStatusProvider struct {
	status StreamStatus
	err    error
}

func (f fakeStreamStatusProvider) Status(_ context.Context, _ string) (StreamStatus, error) {
	return f.status, f.err
}

func TestNewGameCommand_Live(t *testing.T) {
	p := fakeStreamStatusProvider{status: StreamStatus{Live: true, GameName: "Elden Ring"}}
	cmd := NewGameCommand(p)
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "Elden Ring")
	assert.Contains(t, reply.Text, "engelswtf")
}

func TestNewGameCommand_LiveNoCategory(t *testing.T) {
	cmd := NewGameCommand(fakeStreamStatusProvider{status: StreamStatus{Live: true}})
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "no category")
}

func TestNewGameCommand_Offline(t *testing.T) {
	cmd := NewGameCommand(fakeStreamStatusProvider{status: StreamStatus{Live: false}})
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "offline")
	assert.Contains(t, reply.Text, "engelswtf")
}

func TestNewGameCommand_Error(t *testing.T) {
	cmd := NewGameCommand(fakeStreamStatusProvider{err: errors.New("boom")})
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, strings.ToLower(reply.Text), "couldn't check the game")
}

func TestNewGameCommand_NilProvider(t *testing.T) {
	cmd := NewGameCommand(nil)
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "unavailable")
}

func TestNewGameCommand_MinRoleEveryone(t *testing.T) {
	cmd := NewGameCommand(fakeStreamStatusProvider{})
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestNewTitleCommand_Live(t *testing.T) {
	p := fakeStreamStatusProvider{status: StreamStatus{Live: true, Title: "blind run"}}
	cmd := NewTitleCommand(p)
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "blind run")
}

func TestNewTitleCommand_LiveNoTitle(t *testing.T) {
	cmd := NewTitleCommand(fakeStreamStatusProvider{status: StreamStatus{Live: true}})
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "no title")
}

func TestNewTitleCommand_Offline(t *testing.T) {
	cmd := NewTitleCommand(fakeStreamStatusProvider{status: StreamStatus{Live: false}})
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "offline")
	assert.Contains(t, reply.Text, "engelswtf")
}

func TestNewTitleCommand_Error(t *testing.T) {
	cmd := NewTitleCommand(fakeStreamStatusProvider{err: errors.New("boom")})
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, strings.ToLower(reply.Text), "couldn't check the title")
}

func TestNewTitleCommand_NilProvider(t *testing.T) {
	cmd := NewTitleCommand(nil)
	reply := cmd.Handler(context.Background(), Message{Channel: "engelswtf"}, nil)
	assert.Contains(t, reply.Text, "unavailable")
}

func TestNewTitleCommand_MinRoleEveryone(t *testing.T) {
	cmd := NewTitleCommand(fakeStreamStatusProvider{})
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}
