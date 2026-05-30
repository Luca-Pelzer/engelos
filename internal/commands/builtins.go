package commands

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// defaultBuiltinUserCooldown throttles per-user spam of the read-only
// !pity / !streak status commands. 5s is long enough to stop trivial
// double-taps but short enough that a viewer who legitimately mistyped
// is not noticeably blocked.
const defaultBuiltinUserCooldown = 5 * time.Second

// defaultLeaderboardCooldown throttles the channel-wide !leaderboard
// command. 10s keeps the chat tidy when several viewers ask back-to-back
// without making the board feel stale.
const defaultLeaderboardCooldown = 10 * time.Second

// defaultLeaderboardTopN is the number of viewers rendered on a board.
// Three keeps the reply on one line under ~400 chars for both pity and
// streak boards.
const defaultLeaderboardTopN = 3

// PityStatus is the read-only snapshot the !pity built-in needs. It is
// declared here (rather than imported from internal/features/pity) so this
// package stays decoupled from the feature implementation. main.go wires
// an adapter that translates pity.Status into PityStatus.
type PityStatus struct {
	Points          int
	SoftPityHit     bool
	NearGuaranteed  bool
	EffectiveChance float64
}

// PityQuerier is the narrow contract for reading a viewer's pity standing.
// internal/features/pity.System satisfies a thin adapter of this in main.
type PityQuerier interface {
	Status(tenantID, channel, viewerID string) PityStatus
}

// StreakStatus is the read-only snapshot the !streak built-in needs.
// See [PityStatus] for the decoupling rationale.
type StreakStatus struct {
	DaysCurrent      int
	DaysLongest      int
	FreezesAvailable int
	NextMilestone    int
}

// StreakQuerier is the narrow contract for reading a viewer's streak
// standing. internal/features/streak.System satisfies a thin adapter of
// this in main.
type StreakQuerier interface {
	Status(tenantID, channel, viewerID string) StreakStatus
}

// NewPityCommand returns the "!pity" command. The handler reads the
// viewer's status via q and renders a single-line, chat-friendly reply
// addressing the user by @username, e.g.:
//
//	"@alice you have 47 pity points — 23% win chance (soft pity hit!)"
//	"@alice you have 89 pity points — guaranteed win incoming!"
//
// EffectiveChance is rounded to an integer percentage. The constructor
// requires a non-nil q; the wiring layer is responsible for only
// registering the command when the pity feature is available. The
// handler defensively tolerates a zero-value [PityStatus].
func NewPityCommand(tenantID string, q PityQuerier) Command {
	return Command{
		Name:         "pity",
		Help:         "Show your current pity points and win chance.",
		UserCooldown: defaultBuiltinUserCooldown,
		Handler: func(_ context.Context, msg Message, _ []string) Reply {
			if q == nil {
				return Reply{}
			}
			status := q.Status(tenantID, msg.Channel, msg.UserID)
			mention := mentionOf(msg)
			pct := int(math.Round(status.EffectiveChance * 100))
			var tail string
			switch {
			case status.NearGuaranteed:
				tail = "guaranteed win incoming!"
			case status.SoftPityHit:
				tail = fmt.Sprintf("%d%% win chance (soft pity hit!)", pct)
			default:
				tail = fmt.Sprintf("%d%% win chance", pct)
			}
			return Reply{Text: fmt.Sprintf("%s you have %d pity points — %s",
				mention, status.Points, tail)}
		},
	}
}

// NewStreakCommand returns the "!streak" command. The handler reads the
// viewer's status via q and renders a single-line, chat-friendly reply,
// e.g.:
//
//	"@alice 🔥 12-day streak (longest 30) — 3 freezes — next milestone: 30"
//	"@alice you have no active streak — chat today to start one!"
//
// The constructor requires a non-nil q; the wiring layer is responsible
// for only registering the command when the streak feature is available.
// The handler defensively tolerates a zero-value [StreakStatus] by
// rendering the "no active streak" branch.
func NewStreakCommand(tenantID string, q StreakQuerier) Command {
	return Command{
		Name:         "streak",
		Help:         "Show your current activity streak.",
		UserCooldown: defaultBuiltinUserCooldown,
		Handler: func(_ context.Context, msg Message, _ []string) Reply {
			if q == nil {
				return Reply{}
			}
			status := q.Status(tenantID, msg.Channel, msg.UserID)
			mention := mentionOf(msg)
			if status.DaysCurrent <= 0 {
				return Reply{Text: fmt.Sprintf(
					"%s you have no active streak — chat today to start one!", mention)}
			}
			freezeWord := "freezes"
			if status.FreezesAvailable == 1 {
				freezeWord = "freeze"
			}
			milestone := "—"
			if status.NextMilestone > 0 {
				milestone = fmt.Sprintf("next milestone: %d", status.NextMilestone)
			}
			return Reply{Text: fmt.Sprintf(
				"%s 🔥 %d-day streak (longest %d) — %d %s — %s",
				mention,
				status.DaysCurrent,
				status.DaysLongest,
				status.FreezesAvailable,
				freezeWord,
				milestone,
			)}
		},
	}
}

// NewHelpCommand returns the "!commands" command (alias "!help"). The
// handler enumerates the engine's live registrations via [Engine.Commands]
// so newly-added commands are listed without rebuilding the help command.
// Names are prefixed with the engine's Prefix and joined with " ".
func NewHelpCommand(e *Engine) Command {
	return Command{
		Name:    "commands",
		Aliases: []string{"help"},
		Help:    "List all available commands.",
		Handler: func(_ context.Context, msg Message, _ []string) Reply {
			if e == nil {
				return Reply{}
			}
			cmds := e.Commands()
			if len(cmds) == 0 {
				return Reply{Text: fmt.Sprintf("%sno commands registered",
					mentionPrefix(msg))}
			}
			parts := make([]string, 0, len(cmds))
			for _, c := range cmds {
				parts = append(parts, e.Prefix()+c.Name)
			}
			return Reply{Text: fmt.Sprintf("%sAvailable commands: %s",
				mentionPrefix(msg), strings.Join(parts, " "))}
		},
	}
}

// LeaderboardEntry is one row on a leaderboard. Score is the ranking
// metric — pity points or current streak days, depending on which board
// produced the entry. The renderer formats Score per board (bare integer
// for pity, "Nd" for streak).
type LeaderboardEntry struct {
	Username string
	Score    int
}

// LeaderboardQuerier exposes the read-only top-N boards the !leaderboard
// built-in needs. It is declared here (not imported from features/*) to
// keep this package decoupled from the feature implementations; main.go
// wires a thin adapter that translates feature-side leaderboard rows into
// [LeaderboardEntry].
type LeaderboardQuerier interface {
	PityTop(tenantID, channel string, n int) []LeaderboardEntry
	StreakTop(tenantID, channel string, n int) []LeaderboardEntry
}

// NewLeaderboardCommand returns the "!leaderboard" command (alias "!top").
//
// Args:
//   - no arg or "pity": render the pity board.
//   - "streak":         render the streak board.
//
// Pity rows render as "1. alice (47)"; streak rows as "1. alice (30d)".
// An empty board returns the "no data yet" line; a nil querier yields an
// empty reply (consistent with !pity / !streak when wiring is missing).
// The returned Command has a 10s global Cooldown to throttle channel-wide
// spam; wiring code may override by constructing a Command with different
// values.
func NewLeaderboardCommand(tenantID string, q LeaderboardQuerier) Command {
	return Command{
		Name:     "leaderboard",
		Aliases:  []string{"top"},
		Help:     "Show the top viewers — !leaderboard [pity|streak].",
		Cooldown: defaultLeaderboardCooldown,
		Handler: func(_ context.Context, msg Message, args []string) Reply {
			if q == nil {
				return Reply{}
			}
			board := "pity"
			if len(args) > 0 {
				board = strings.ToLower(args[0])
			}

			var (
				entries []LeaderboardEntry
				header  string
				suffix  string
			)
			switch board {
			case "streak":
				entries = q.StreakTop(tenantID, msg.Channel, defaultLeaderboardTopN)
				header = "🔥 Streak leaders:"
				suffix = "d"
			default:
				entries = q.PityTop(tenantID, msg.Channel, defaultLeaderboardTopN)
				header = "🏆 Pity leaders:"
			}

			if len(entries) == 0 {
				return Reply{Text: "No leaderboard data yet — start chatting!"}
			}

			parts := make([]string, 0, len(entries))
			for i, e := range entries {
				name := strings.TrimSpace(e.Username)
				if name == "" {
					name = "viewer"
				}
				parts = append(parts, fmt.Sprintf("%d. %s (%d%s)", i+1, name, e.Score, suffix))
			}
			return Reply{Text: fmt.Sprintf("%s %s", header, strings.Join(parts, " "))}
		},
	}
}

// defaultAdminUserCooldown throttles per-mod double-fires of the
// !addcom / !editcom / !delcom built-ins. 2s is short enough to feel
// instant but long enough to swallow an accidental Enter-spam.
const defaultAdminUserCooldown = 2 * time.Second

// CustomCommandStore is the narrow management surface the !addcom /
// !editcom / !delcom built-ins need. An adapter over
// [github.com/Luca-Pelzer/engelos/internal/customcommands.Store] is
// wired in main; this interface lives HERE so internal/commands does
// NOT import internal/customcommands (avoids an import cycle since
// internal/customcommands needs the persistence-side types and main
// already depends on both).
//
// All three methods take channel + name; main's adapter is responsible
// for choosing the tenant_id (typically the host's tenant for that
// channel) and any normalisation beyond what the built-in already does.
//
// Errors are returned as opaque error values: the built-in handlers
// render a single generic "couldn't <verb> !name (already exists,
// missing, or invalid)" reply on any non-nil error and log the detail
// at INFO. This keeps internal/commands free of the
// customcommands-package sentinel error symbols while still surfacing
// failures to mods in chat.
type CustomCommandStore interface {
	Add(ctx context.Context, channel, name, response, minRole, createdBy string) error
	Edit(ctx context.Context, channel, name, response string) error
	Remove(ctx context.Context, channel, name string) error
}

// parseAdminTarget normalises args[0] as a command-name target: lower
// case, trim, strip a leading "!". Returns "" when the slice is empty.
func parseAdminTarget(args []string) string {
	if len(args) == 0 {
		return ""
	}
	t := strings.ToLower(strings.TrimSpace(args[0]))
	t = strings.TrimPrefix(t, "!")
	return t
}

// NewAddCommand returns "!addcom" (alias "!addcmd"). Mods-only.
//
// Usage: "!addcom !name response text with $user". The response text
// is everything after the first whitespace-separated token; multiple
// spaces between args are collapsed to single spaces (the engine has
// already strings.Fields-split the input). MinRole on the created
// custom command is always "everyone" — per-command role for custom
// commands is future work.
//
// A nil store yields a one-line "custom commands are unavailable"
// reply so the streamer sees something happened rather than the
// silence of a denied invocation.
func NewAddCommand(store CustomCommandStore) Command {
	return Command{
		Name:         "addcom",
		Aliases:      []string{"addcmd"},
		Help:         "Add a custom command — !addcom !name response with $user.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%scustom commands are unavailable",
					mentionPrefix(msg))}
			}
			target := parseAdminTarget(args)
			if target == "" || len(args) < 2 {
				return Reply{Text: fmt.Sprintf("%susage: !addcom !name response text",
					mentionPrefix(msg))}
			}
			response := strings.Join(args[1:], " ")
			if err := store.Add(ctx, msg.Channel, target, response, "everyone", msg.UserID); err != nil {
				return Reply{Text: fmt.Sprintf(
					"%scouldn't add !%s (already exists, missing, or invalid)",
					mentionPrefix(msg), target)}
			}
			return Reply{Text: fmt.Sprintf("%sadded !%s", mentionPrefix(msg), target)}
		},
	}
}

// NewEditCommand returns "!editcom" (alias "!editcmd"). Mods-only.
//
// Usage: "!editcom !name new response text". A nil store yields the
// "custom commands are unavailable" reply (see [NewAddCommand]).
func NewEditCommand(store CustomCommandStore) Command {
	return Command{
		Name:         "editcom",
		Aliases:      []string{"editcmd"},
		Help:         "Edit a custom command — !editcom !name new response.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%scustom commands are unavailable",
					mentionPrefix(msg))}
			}
			target := parseAdminTarget(args)
			if target == "" || len(args) < 2 {
				return Reply{Text: fmt.Sprintf("%susage: !editcom !name new response",
					mentionPrefix(msg))}
			}
			response := strings.Join(args[1:], " ")
			if err := store.Edit(ctx, msg.Channel, target, response); err != nil {
				return Reply{Text: fmt.Sprintf(
					"%scouldn't edit !%s (already exists, missing, or invalid)",
					mentionPrefix(msg), target)}
			}
			return Reply{Text: fmt.Sprintf("%sedited !%s", mentionPrefix(msg), target)}
		},
	}
}

// NewDeleteCommand returns "!delcom" (alias "!delcmd"). Mods-only.
//
// Usage: "!delcom !name". A nil store yields the "custom commands are
// unavailable" reply (see [NewAddCommand]).
func NewDeleteCommand(store CustomCommandStore) Command {
	return Command{
		Name:         "delcom",
		Aliases:      []string{"delcmd"},
		Help:         "Delete a custom command — !delcom !name.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%scustom commands are unavailable",
					mentionPrefix(msg))}
			}
			target := parseAdminTarget(args)
			if target == "" {
				return Reply{Text: fmt.Sprintf("%susage: !delcom !name",
					mentionPrefix(msg))}
			}
			if err := store.Remove(ctx, msg.Channel, target); err != nil {
				return Reply{Text: fmt.Sprintf(
					"%scouldn't delete !%s (already exists, missing, or invalid)",
					mentionPrefix(msg), target)}
			}
			return Reply{Text: fmt.Sprintf("%sdeleted !%s", mentionPrefix(msg), target)}
		},
	}
}

// defaultTimerMinChatLines is the activity-gate value passed to the store
// by !addtimer. It is 0 — the chat command does NOT expose the activity
// gate, so a timer added from chat fires purely on its interval. Gating a
// timer behind chat activity is configured out-of-band (admin/config),
// because defaulting to a non-zero gate here would silently prevent a mod
// from seeing their just-added timer fire while testing in quiet chat.
const defaultTimerMinChatLines = 0

// TimerInfo is a read view for the !timers listing.
type TimerInfo struct {
	Name            string
	IntervalSeconds int
	Enabled         bool
}

// TimerStore is the narrow management surface the !addtimer / !deltimer /
// !timers built-ins need. An adapter over
// [github.com/Luca-Pelzer/engelos/internal/timers.Store] is wired in main;
// this interface lives HERE so internal/commands does NOT import
// internal/timers (mirrors [CustomCommandStore]).
//
// main's adapter chooses the tenant_id and converts the interval-seconds
// and min-chat-lines ints into the store's richer types. Errors are opaque:
// the handlers render a single generic reply on any non-nil error.
type TimerStore interface {
	AddTimer(ctx context.Context, channel, name, message string, intervalSeconds, minChatLines int, createdBy string) error
	RemoveTimer(ctx context.Context, channel, name string) error
	ListTimers(ctx context.Context, channel string) ([]TimerInfo, error)
}

// NewAddTimerCommand returns "!addtimer". Mods-only.
//
// Usage: "!addtimer <name> <interval_seconds> <message...>", e.g.
// "!addtimer rules 600 Follow the rules!". The interval is parsed as
// integer seconds; the message is everything after it. A nil store yields
// a one-line "timers are unavailable" reply.
func NewAddTimerCommand(store TimerStore) Command {
	return Command{
		Name:         "addtimer",
		Help:         "Add an auto-announcement — !addtimer name seconds message.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%stimers are unavailable",
					mentionPrefix(msg))}
			}
			if len(args) < 3 {
				return Reply{Text: fmt.Sprintf("%susage: !addtimer name seconds message",
					mentionPrefix(msg))}
			}
			name := parseAdminTarget(args)
			seconds, err := strconv.Atoi(args[1])
			if err != nil || seconds <= 0 {
				return Reply{Text: fmt.Sprintf("%susage: !addtimer name seconds message",
					mentionPrefix(msg))}
			}
			message := strings.Join(args[2:], " ")
			if aerr := store.AddTimer(ctx, msg.Channel, name, message,
				seconds, defaultTimerMinChatLines, msg.UserID); aerr != nil {
				return Reply{Text: fmt.Sprintf(
					"%scouldn't add timer '%s' (already exists, missing, or invalid)",
					mentionPrefix(msg), name)}
			}
			return Reply{Text: fmt.Sprintf("%sadded timer '%s' (every %ds)",
				mentionPrefix(msg), name, seconds)}
		},
	}
}

// NewDeleteTimerCommand returns "!deltimer". Mods-only.
//
// Usage: "!deltimer <name>". A nil store yields the "timers are
// unavailable" reply (see [NewAddTimerCommand]).
func NewDeleteTimerCommand(store TimerStore) Command {
	return Command{
		Name:         "deltimer",
		Help:         "Delete an auto-announcement — !deltimer name.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%stimers are unavailable",
					mentionPrefix(msg))}
			}
			name := parseAdminTarget(args)
			if name == "" {
				return Reply{Text: fmt.Sprintf("%susage: !deltimer name",
					mentionPrefix(msg))}
			}
			if err := store.RemoveTimer(ctx, msg.Channel, name); err != nil {
				return Reply{Text: fmt.Sprintf(
					"%scouldn't delete timer '%s' (already exists, missing, or invalid)",
					mentionPrefix(msg), name)}
			}
			return Reply{Text: fmt.Sprintf("%sdeleted timer '%s'",
				mentionPrefix(msg), name)}
		},
	}
}

// NewListTimersCommand returns "!timers". Mods-only. It renders timer names
// and their intervals on one line, e.g. "@mod timers: rules (600s),
// discord (1800s)", or "no timers set" when empty. A nil store yields the
// "timers are unavailable" reply (see [NewAddTimerCommand]).
func NewListTimersCommand(store TimerStore) Command {
	return Command{
		Name:         "timers",
		Help:         "List the channel's auto-announcements.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%stimers are unavailable",
					mentionPrefix(msg))}
			}
			infos, err := store.ListTimers(ctx, msg.Channel)
			if err != nil {
				return Reply{Text: fmt.Sprintf("%scouldn't list timers",
					mentionPrefix(msg))}
			}
			if len(infos) == 0 {
				return Reply{Text: fmt.Sprintf("%sno timers set", mentionPrefix(msg))}
			}
			parts := make([]string, 0, len(infos))
			for _, info := range infos {
				parts = append(parts, fmt.Sprintf("%s (%ds)", info.Name, info.IntervalSeconds))
			}
			return Reply{Text: fmt.Sprintf("%stimers: %s",
				mentionPrefix(msg), strings.Join(parts, ", "))}
		},
	}
}

// mentionOf returns "@username" when Username is set, falling back to
// "@viewer" so replies never read as "you have X points".
func mentionOf(msg Message) string {
	name := strings.TrimSpace(msg.Username)
	if name == "" {
		name = "viewer"
	}
	return "@" + name
}

// mentionPrefix returns "@username " (with trailing space) when a username
// is available and the empty string otherwise — so the help text reads
// naturally either way.
func mentionPrefix(msg Message) string {
	name := strings.TrimSpace(msg.Username)
	if name == "" {
		return ""
	}
	return "@" + name + " "
}
