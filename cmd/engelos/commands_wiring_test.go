package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
)

// TestBuildCommandRouter_EndToEnd exercises the real wiring built in
// buildCommandRouter against real pity/streak systems on an in-memory event
// store. It guards the type-mapping seam (pity.Status -> commands.PityStatus
// etc.): a swapped field would compile cleanly but produce a wrong reply, so
// only an end-to-end assertion can catch it.
func TestBuildCommandRouter_EndToEnd(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	store, err := eventsourcing.OpenSQLite(ctx, "file::memory:?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	pitySys, err := pity.New(pity.DefaultConfig(), store, logger)
	require.NoError(t, err)
	streakSys, err := streak.New(streak.DefaultConfig(), store, logger)
	require.NoError(t, err)

	const (
		tenant  = "default"
		channel = "engelswtf"
		viewer  = "viewer-1"
		user    = "alice"
	)

	_, err = pitySys.GrantPoints(ctx, tenant, channel, viewer, user, "chat:test", 5)
	require.NoError(t, err)
	_, err = streakSys.Tick(ctx, tenant, channel, viewer, user)
	require.NoError(t, err)

	router := buildCommandRouter(tenant, pitySys, streakSys, logger)

	pityReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!pity",
	})
	require.True(t, handled)
	require.Contains(t, pityReply.Text, "@alice")
	require.Contains(t, pityReply.Text, "5 pity points")

	streakReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!streak",
	})
	require.True(t, handled)
	require.Contains(t, streakReply.Text, "@alice")
	require.Contains(t, streakReply.Text, "1-day streak")

	helpReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!commands",
	})
	require.True(t, handled)
	for _, want := range []string{"!pity", "!streak", "!commands"} {
		require.True(t, strings.Contains(helpReply.Text, want),
			"help reply %q missing %q", helpReply.Text, want)
	}

	_, handled = router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "not a command",
	})
	require.False(t, handled)
}
