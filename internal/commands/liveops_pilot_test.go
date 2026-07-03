package commands_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/commands"
)

// pilotEventStore is a minimal commands.EventStore for the liveops pilot test.
type pilotEventStore struct{}

func (pilotEventStore) Next(context.Context, string) (commands.ScheduledEvent, bool, error) {
	return commands.ScheduledEvent{}, false, nil
}
func (pilotEventStore) Upcoming(context.Context, string, int) ([]commands.ScheduledEvent, error) {
	return nil, nil
}
func (pilotEventStore) Add(context.Context, string, string, string, time.Time, *time.Time) (int, error) {
	return 1, nil
}
func (pilotEventStore) Delete(context.Context, string, int) error { return nil }

// buildLiveopsPilotRouter mirrors cmd/engelos.buildCommandRouter's liveops
// gating: the event commands register only when the store is non-nil.
func buildLiveopsPilotRouter(t *testing.T, store commands.EventStore) *commands.Engine {
	t.Helper()
	eng := commands.New(commands.Config{Logger: silentLogger()})
	if store != nil {
		require.NoError(t, eng.Register(commands.NewNextEventCommand(store)))
		require.NoError(t, eng.Register(commands.NewScheduleCommand(store)))
		require.NoError(t, eng.Register(commands.NewAddEventCommand(store)))
		require.NoError(t, eng.Register(commands.NewDelEventCommand(store)))
	}
	return eng
}

func TestLiveopsPilot_EnabledRegistersCommands(t *testing.T) {
	eng := buildLiveopsPilotRouter(t, pilotEventStore{})

	_, handled := eng.Handle(context.Background(), msgText("!nextevent"))
	assert.True(t, handled, "liveops enabled: !nextevent is registered and handled")
}

func TestLiveopsPilot_DisabledUnregistersCommands(t *testing.T) {
	eng := buildLiveopsPilotRouter(t, nil) // plugin disabled => nil store => no registration

	for _, cmd := range []string{"!nextevent", "!schedule", "!addevent x in 2h", "!delevent 1"} {
		_, handled := eng.Handle(context.Background(), modMsg(cmd))
		assert.False(t, handled,
			"liveops disabled: %q must be unregistered so the router reports it unhandled", cmd)
	}
}
