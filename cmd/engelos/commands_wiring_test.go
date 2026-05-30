package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/counters"
	"github.com/Luca-Pelzer/engelos/internal/customcommands"
	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/Luca-Pelzer/engelos/internal/liveops"
	"github.com/Luca-Pelzer/engelos/internal/quotes"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
	"github.com/Luca-Pelzer/engelos/internal/timers"
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

	customStore, err := customcommands.OpenSQLiteStore(ctx, "file:cc?mode=memory&cache=shared", logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = customStore.Close() })

	timerStore, err := timers.OpenSQLiteStore(ctx, "file:tm?mode=memory&cache=shared", logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = timerStore.Close() })

	quoteStore, err := quotes.OpenSQLiteStore(ctx, "file:qt?mode=memory&cache=shared", logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = quoteStore.Close() })

	counterStore, err := counters.OpenSQLiteStore(ctx, "file:ct?mode=memory&cache=shared", logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = counterStore.Close() })

	liveopsStore, err := liveops.OpenSQLiteStore(ctx, "file:lo?mode=memory&cache=shared", logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = liveopsStore.Close() })

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

	router := buildCommandRouter(tenant, pitySys, streakSys, customStore, timerStore, quoteStore, counterStore, liveopsStore, nil, nil, nil, logger)

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

	lbReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!leaderboard",
	})
	require.True(t, handled)
	require.Contains(t, lbReply.Text, "alice")

	helpReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!commands",
	})
	require.True(t, handled)
	for _, want := range []string{"!pity", "!streak", "!leaderboard", "!commands"} {
		require.True(t, strings.Contains(helpReply.Text, want),
			"help reply %q missing %q", helpReply.Text, want)
	}

	_, handled = router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "not a command",
	})
	require.False(t, handled)

	addTimerReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: "mod-1", Username: "modder",
		Text: "!addtimer rules 600 Follow the channel rules!", IsModerator: true,
	})
	require.True(t, handled)
	require.Contains(t, addTimerReply.Text, "rules")

	listReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: "mod-1", Username: "modder",
		Text: "!timers", IsModerator: true,
	})
	require.True(t, handled)
	require.Contains(t, listReply.Text, "rules")
	require.Contains(t, listReply.Text, "600")

	addQuoteReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: "mod-1", Username: "modder",
		Text: "!addquote engelOS is live", IsModerator: true,
	})
	require.True(t, handled)
	require.Contains(t, addQuoteReply.Text, "#1")

	quoteReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!quote 1",
	})
	require.True(t, handled)
	require.Contains(t, quoteReply.Text, "engelOS is live")

	addCounterReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: "mod-1", Username: "modder",
		Text: "!counter+ deaths", IsModerator: true,
	})
	require.True(t, handled)
	require.Contains(t, addCounterReply.Text, "1")

	counterReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!counter deaths",
	})
	require.True(t, handled)
	require.Contains(t, counterReply.Text, "deaths")
	require.Contains(t, counterReply.Text, "1")

	uptimeReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!uptime",
	})
	require.True(t, handled)
	require.NotEmpty(t, uptimeReply.Text)

	gameReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!game",
	})
	require.True(t, handled)
	require.NotEmpty(t, gameReply.Text)

	titleReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!title",
	})
	require.True(t, handled)
	require.NotEmpty(t, titleReply.Text)

	addEventReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: "mod-1", Username: "modder",
		Text: "!addevent 2d Double Points Weekend", IsModerator: true,
	})
	require.True(t, handled)
	require.Contains(t, addEventReply.Text, "Double Points Weekend")
	require.Contains(t, addEventReply.Text, "#1")

	nextEventReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!nextevent",
	})
	require.True(t, handled)
	require.Contains(t, nextEventReply.Text, "Double Points Weekend")

	scheduleReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: viewer, Username: user, Text: "!schedule",
	})
	require.True(t, handled)
	require.Contains(t, scheduleReply.Text, "Double Points Weekend")

	delEventReply, handled := router.Route(ctx, runtime.CommandInvocation{
		Platform: "twitch", Channel: channel, UserID: "mod-1", Username: "modder",
		Text: "!delevent 1", IsModerator: true,
	})
	require.True(t, handled)
	require.Contains(t, delEventReply.Text, "deleted event #1")
}
