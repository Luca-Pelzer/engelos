package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
)

// fakeSongRequester is a minimal commands.SongRequester used to prove the !sr
// family registers when a non-nil requester is wired.
type fakeSongRequester struct{}

func (fakeSongRequester) Request(context.Context, string, string) (commands.SongTrack, commands.SongOutcome) {
	return commands.SongTrack{Title: "Test", Artist: "Tester"}, commands.SongOK
}
func (fakeSongRequester) NowPlaying(context.Context, string) (commands.SongTrack, commands.SongOutcome) {
	return commands.SongTrack{Title: "Test", Artist: "Tester"}, commands.SongOK
}
func (fakeSongRequester) Skip(context.Context, string) commands.SongOutcome {
	return commands.SongOK
}

// buildRouterForGateTest wires buildCommandRouter with everything nil except the
// songRequester slot, isolating the RES-19 gate. songRequester is the 15th
// positional arg to buildCommandRouter.
func buildRouterForGateTest(t *testing.T, sr commands.SongRequester) runtime.CommandRouter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return buildCommandRouter(
		"default", // tenantID
		nil,       // pity
		nil,       // streak
		nil,       // custom
		nil,       // timerStore
		nil,       // quoteStore
		nil,       // counterStore
		nil,       // liveopsStore
		nil,       // twitchAdapter
		nil,       // loyaltyProvider
		nil,       // heistSender
		nil,       // rewardCatalog
		nil,       // featureToggle
		nil,       // predictions
		sr,        // songRequester (the RES-19 gate)
		nil,       // momentCtrl
		nil,       // translateConfig
		nil,       // cohostConfig
		logger,
	)
}

// TestBuildCommandRouter_SongRequestsGate proves the RES-19 Option-A quarantine
// at the command layer: with a nil songRequester (feature off) the !sr / !song
// / !skipsong commands are unregistered — Route reports them unhandled and they
// do not appear in !commands help. With a non-nil requester (feature on) they
// register and respond, proving the gate is reversible with no code removed.
func TestBuildCommandRouter_SongRequestsGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		channel = "engelswtf"
		viewer  = "viewer-1"
		user    = "alice"
	)

	inv := func(text string, mod bool) runtime.CommandInvocation {
		return runtime.CommandInvocation{
			Platform: "twitch", Channel: channel, UserID: viewer,
			Username: user, Text: text, IsModerator: mod,
		}
	}

	t.Run("off: sr commands unregistered", func(t *testing.T) {
		t.Parallel()
		router := buildRouterForGateTest(t, nil)

		for _, cmd := range []string{"!sr song name", "!song", "!skipsong"} {
			_, handled := router.Route(ctx, inv(cmd, true))
			require.Falsef(t, handled, "%q must be unhandled when feature off", cmd)
		}

		help, handled := router.Route(ctx, inv("!commands", false))
		require.True(t, handled)
		require.NotContains(t, help.Text, "!sr", "help must not advertise !sr when off")
		require.NotContains(t, help.Text, "!skipsong", "help must not advertise !skipsong when off")
	})

	t.Run("on: sr commands registered", func(t *testing.T) {
		t.Parallel()
		router := buildRouterForGateTest(t, fakeSongRequester{})

		reply, handled := router.Route(ctx, inv("!sr never gonna give you up", false))
		require.True(t, handled, "!sr must be handled when feature on")
		require.Contains(t, reply.Text, "queued")

		npReply, handled := router.Route(ctx, inv("!song", false))
		require.True(t, handled, "!song must be handled when feature on")
		require.Contains(t, npReply.Text, "now playing")

		skipReply, handled := router.Route(ctx, inv("!skipsong", true))
		require.True(t, handled, "!skipsong must be handled when feature on")
		require.Contains(t, skipReply.Text, "skipped")
	})
}

// TestSongRequestsActive proves the RES-19 flag/plugin interaction that decides
// whether songrequests runs (and therefore whether its stores open, routes
// mount, and commands register). The plugin toggle is the source of truth; the
// legacy ENGELOS_FEATURE_SONGREQUESTS env flag forces it on for backward compat.
// A box with neither stays default-off (the RES-19 quarantine).
func TestSongRequestsActive(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		pluginEnabled bool
		envFlag       bool
		want          bool
	}{
		{"neither: stays default-off (RES-19)", false, false, false},
		{"plugin on: enabled via dashboard toggle", true, false, true},
		{"env flag on: backward-compat forces on", false, true, true},
		{"both on", true, true, true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, c.want, songRequestsActive(c.pluginEnabled, c.envFlag))
		})
	}
}
