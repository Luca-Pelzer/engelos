package commands

import (
	"context"
	"fmt"
	"math"
	"regexp"
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
//	"@alice you have 47 pity points - 23% win chance (soft pity hit!)"
//	"@alice you have 89 pity points - guaranteed win incoming!"
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
			return Reply{Text: fmt.Sprintf("%s you have %d pity points - %s",
				mention, status.Points, tail)}
		},
	}
}

// NewStreakCommand returns the "!streak" command. The handler reads the
// viewer's status via q and renders a single-line, chat-friendly reply,
// e.g.:
//
//	"@alice 🔥 12-day streak (longest 30) - 3 freezes - next milestone: 30"
//	"@alice you have no active streak - chat today to start one!"
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
					"%s you have no active streak - chat today to start one!", mention)}
			}
			freezeWord := "freezes"
			if status.FreezesAvailable == 1 {
				freezeWord = "freeze"
			}
			milestone := "-"
			if status.NextMilestone > 0 {
				milestone = fmt.Sprintf("next milestone: %d", status.NextMilestone)
			}
			return Reply{Text: fmt.Sprintf(
				"%s 🔥 %d-day streak (longest %d) - %d %s - %s",
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
// metric - pity points or current streak days, depending on which board
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
		Help:     "Show the top viewers - !leaderboard [pity|streak].",
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
				return Reply{Text: "No leaderboard data yet - start chatting!"}
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
// custom command is always "everyone" - per-command role for custom
// commands is future work.
//
// A nil store yields a one-line "custom commands are unavailable"
// reply so the streamer sees something happened rather than the
// silence of a denied invocation.
func NewAddCommand(store CustomCommandStore) Command {
	return Command{
		Name:         "addcom",
		Aliases:      []string{"addcmd"},
		Help:         "Add a custom command - !addcom !name response with $user.",
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
		Help:         "Edit a custom command - !editcom !name new response.",
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
		Help:         "Delete a custom command - !delcom !name.",
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
// by !addtimer. It is 0 - the chat command does NOT expose the activity
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
		Help:         "Add an auto-announcement - !addtimer name seconds message.",
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
		Help:         "Delete an auto-announcement - !deltimer name.",
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

// defaultQuoteCooldown throttles the channel-wide read-only !quote
// command. 5s keeps chat tidy when several viewers ask back-to-back
// without making quotes feel stale.
const defaultQuoteCooldown = 5 * time.Second

// QuoteView is the read shape rendered to chat by the !quote built-in.
type QuoteView struct {
	Number int
	Text   string
}

// QuoteStore is the narrow surface the quote built-ins need. An adapter
// over [github.com/Luca-Pelzer/engelos/internal/quotes.Store] is wired in
// main; this interface lives HERE so internal/commands does NOT import
// internal/quotes (mirrors [TimerStore] and [CustomCommandStore]).
//
// Get and Random return ok=false for not-found/empty so the built-ins
// render a friendly "no quote" line without depending on the quotes
// package's sentinel errors. Delete returns an opaque error; the built-in
// renders a generic friendly reply on any non-nil error. main's adapter
// chooses the tenant_id.
type QuoteStore interface {
	Add(ctx context.Context, channel, text, createdBy string) (number int, err error)
	Get(ctx context.Context, channel string, number int) (QuoteView, bool)
	Random(ctx context.Context, channel string) (QuoteView, bool)
	Delete(ctx context.Context, channel string, number int) error
}

// NewAddQuoteCommand returns "!addquote". Mods-only.
//
// Usage: "!addquote <text>". On success replies "@mod added quote #4".
// Empty text yields a usage reply; a store error yields a friendly error
// reply. A nil store yields a one-line "quotes are unavailable" reply.
func NewAddQuoteCommand(store QuoteStore) Command {
	return Command{
		Name:         "addquote",
		Help:         "Save a memorable line - !addquote <text>.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%squotes are unavailable",
					mentionPrefix(msg))}
			}
			text := strings.TrimSpace(strings.Join(args, " "))
			if text == "" {
				return Reply{Text: fmt.Sprintf("%susage: !addquote <text>",
					mentionPrefix(msg))}
			}
			number, err := store.Add(ctx, msg.Channel, text, msg.UserID)
			if err != nil {
				return Reply{Text: fmt.Sprintf("%scouldn't add quote (text empty or too long)",
					mentionPrefix(msg))}
			}
			return Reply{Text: fmt.Sprintf("%sadded quote #%d", mentionPrefix(msg), number)}
		},
	}
}

// NewQuoteCommand returns "!quote". Open to everyone, with a global
// Cooldown to curb spam.
//
// Usage: "!quote" shows a random quote; "!quote <n>" shows quote #n.
// Renders "#4: <text>". An empty channel replies "no quotes yet"; a
// missing number replies "no quote #<n>"; a non-numeric arg replies
// "usage: !quote [number]". A nil store yields "quotes are unavailable".
func NewQuoteCommand(store QuoteStore) Command {
	return Command{
		Name:     "quote",
		Help:     "Show a saved quote - !quote [number] (random if omitted).",
		Cooldown: defaultQuoteCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%squotes are unavailable",
					mentionPrefix(msg))}
			}
			if len(args) == 0 {
				view, ok := store.Random(ctx, msg.Channel)
				if !ok {
					return Reply{Text: fmt.Sprintf("%sno quotes yet", mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("#%d: %s", view.Number, view.Text)}
			}
			number, err := strconv.Atoi(args[0])
			if err != nil || number <= 0 {
				return Reply{Text: fmt.Sprintf("%susage: !quote [number]",
					mentionPrefix(msg))}
			}
			view, ok := store.Get(ctx, msg.Channel, number)
			if !ok {
				return Reply{Text: fmt.Sprintf("%sno quote #%d", mentionPrefix(msg), number)}
			}
			return Reply{Text: fmt.Sprintf("#%d: %s", view.Number, view.Text)}
		},
	}
}

// NewDeleteQuoteCommand returns "!delquote". Mods-only.
//
// Usage: "!delquote <n>". On success replies "@mod deleted quote #4". A
// missing or bad number yields a usage reply; a store error yields a
// friendly reply. A nil store yields "quotes are unavailable".
func NewDeleteQuoteCommand(store QuoteStore) Command {
	return Command{
		Name:         "delquote",
		Help:         "Delete a saved quote - !delquote <number>.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%squotes are unavailable",
					mentionPrefix(msg))}
			}
			if len(args) == 0 {
				return Reply{Text: fmt.Sprintf("%susage: !delquote <number>",
					mentionPrefix(msg))}
			}
			number, err := strconv.Atoi(args[0])
			if err != nil || number <= 0 {
				return Reply{Text: fmt.Sprintf("%susage: !delquote <number>",
					mentionPrefix(msg))}
			}
			if derr := store.Delete(ctx, msg.Channel, number); derr != nil {
				return Reply{Text: fmt.Sprintf("%scouldn't delete quote #%d (missing or invalid)",
					mentionPrefix(msg), number)}
			}
			return Reply{Text: fmt.Sprintf("%sdeleted quote #%d", mentionPrefix(msg), number)}
		},
	}
}

// defaultCounterCooldown throttles the channel-wide read-only !counter
// command. 3s keeps chat tidy when several viewers ask back-to-back.
const defaultCounterCooldown = 3 * time.Second

// CounterStore is the narrow surface the counter built-ins need. An adapter
// over [github.com/Luca-Pelzer/engelos/internal/counters.Store] is wired in
// main; this interface lives HERE so internal/commands does NOT import
// internal/counters (mirrors [QuoteStore] and [TimerStore]). main's adapter
// chooses the tenant_id.
type CounterStore interface {
	// Value returns the current value and ok=false if the named counter
	// does not exist.
	Value(ctx context.Context, channel, name string) (value int64, ok bool)
	// Add increments by delta (creating at 0 if absent) and returns the new value.
	Add(ctx context.Context, channel, name string, delta int64) (int64, error)
	// Set assigns an absolute value (creating if absent) and returns it.
	Set(ctx context.Context, channel, name string, value int64) (int64, error)
}

// parseCounterAmount parses an optional integer amount token, defaulting to
// def when absent. ok=false signals an unparseable token (usage reply).
func parseCounterAmount(args []string, def int64) (amount int64, ok bool) {
	if len(args) < 2 {
		return def, true
	}
	v, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// NewCounterCommand returns the "!counter" command. Open to everyone, with a
// small global Cooldown.
//
// Usage: "!counter <name>" → "<name>: 42". A missing name yields a usage
// reply; an unknown counter yields a friendly "no counter '<name>' yet"
// (not an error). A nil store yields "counters unavailable".
func NewCounterCommand(store CounterStore) Command {
	return Command{
		Name:     "counter",
		Help:     "Show a counter's value - !counter <name>.",
		Cooldown: defaultCounterCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%scounters unavailable", mentionPrefix(msg))}
			}
			name := parseAdminTarget(args)
			if name == "" {
				return Reply{Text: fmt.Sprintf("%susage: !counter <name>", mentionPrefix(msg))}
			}
			value, ok := store.Value(ctx, msg.Channel, name)
			if !ok {
				return Reply{Text: fmt.Sprintf("%sno counter '%s' yet", mentionPrefix(msg), name)}
			}
			return Reply{Text: fmt.Sprintf("%s: %d", name, value)}
		},
	}
}

// NewCounterAddCommand returns the "!counter+" command. Mods-only.
//
// Usage: "!counter+ <name> [amount]" (amount defaults to 1) → "<name>: 43".
// Creates the counter if absent. A negative amount is allowed. A bad amount
// yields a usage reply; a nil store yields "counters unavailable".
func NewCounterAddCommand(store CounterStore) Command {
	return Command{
		Name:         "counter+",
		Help:         "Increment a counter - !counter+ <name> [amount].",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%scounters unavailable", mentionPrefix(msg))}
			}
			name := parseAdminTarget(args)
			amount, ok := parseCounterAmount(args, 1)
			if name == "" || !ok {
				return Reply{Text: fmt.Sprintf("%susage: !counter+ <name> [amount]", mentionPrefix(msg))}
			}
			value, err := store.Add(ctx, msg.Channel, name, amount)
			if err != nil {
				return Reply{Text: fmt.Sprintf("%scouldn't update counter '%s'", mentionPrefix(msg), name)}
			}
			return Reply{Text: fmt.Sprintf("%s: %d", name, value)}
		},
	}
}

// NewCounterSubCommand returns the "!counter-" command. Mods-only.
//
// Usage: "!counter- <name> [amount]" (amount defaults to 1) → "<name>: 41".
// The decrement applies Add with the negated amount. A bad amount yields a
// usage reply; a nil store yields "counters unavailable".
func NewCounterSubCommand(store CounterStore) Command {
	return Command{
		Name:         "counter-",
		Help:         "Decrement a counter - !counter- <name> [amount].",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%scounters unavailable", mentionPrefix(msg))}
			}
			name := parseAdminTarget(args)
			amount, ok := parseCounterAmount(args, 1)
			if name == "" || !ok {
				return Reply{Text: fmt.Sprintf("%susage: !counter- <name> [amount]", mentionPrefix(msg))}
			}
			value, err := store.Add(ctx, msg.Channel, name, -amount)
			if err != nil {
				return Reply{Text: fmt.Sprintf("%scouldn't update counter '%s'", mentionPrefix(msg), name)}
			}
			return Reply{Text: fmt.Sprintf("%s: %d", name, value)}
		},
	}
}

// NewSetCounterCommand returns the "!setcounter" command. Mods-only.
//
// Usage: "!setcounter <name> <value>" → "<name> set to <value>". A missing
// name or bad value yields a usage reply; a nil store yields "counters
// unavailable".
func NewSetCounterCommand(store CounterStore) Command {
	return Command{
		Name:         "setcounter",
		Help:         "Set a counter's value - !setcounter <name> <value>.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%scounters unavailable", mentionPrefix(msg))}
			}
			name := parseAdminTarget(args)
			if name == "" || len(args) < 2 {
				return Reply{Text: fmt.Sprintf("%susage: !setcounter <name> <value>", mentionPrefix(msg))}
			}
			value, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return Reply{Text: fmt.Sprintf("%susage: !setcounter <name> <value>", mentionPrefix(msg))}
			}
			set, serr := store.Set(ctx, msg.Channel, name, value)
			if serr != nil {
				return Reply{Text: fmt.Sprintf("%scouldn't set counter '%s'", mentionPrefix(msg), name)}
			}
			return Reply{Text: fmt.Sprintf("%s%s set to %d", mentionPrefix(msg), name, set)}
		},
	}
}

// NewResetCounterCommand returns the "!resetcounter" command. Mods-only.
//
// Usage: "!resetcounter <name>" → "<name> reset to 0". A missing name yields
// a usage reply; a nil store yields "counters unavailable".
func NewResetCounterCommand(store CounterStore) Command {
	return Command{
		Name:         "resetcounter",
		Help:         "Reset a counter to 0 - !resetcounter <name>.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%scounters unavailable", mentionPrefix(msg))}
			}
			name := parseAdminTarget(args)
			if name == "" {
				return Reply{Text: fmt.Sprintf("%susage: !resetcounter <name>", mentionPrefix(msg))}
			}
			if _, err := store.Set(ctx, msg.Channel, name, 0); err != nil {
				return Reply{Text: fmt.Sprintf("%scouldn't reset counter '%s'", mentionPrefix(msg), name)}
			}
			return Reply{Text: fmt.Sprintf("%s%s reset to 0", mentionPrefix(msg), name)}
		},
	}
}

// defaultUptimeCooldown throttles the channel-wide read-only !uptime
// command. 5s keeps chat tidy when several viewers ask back-to-back while
// the underlying provider's own TTL cache absorbs the rest.
const defaultUptimeCooldown = 5 * time.Second

// UptimeProvider reports a channel's live status for the !uptime command.
// An adapter over the Twitch adapter is wired in main.
type UptimeProvider interface {
	// Uptime returns since (stream start) and live=true when the channel is
	// currently live; live=false means offline. err is non-nil only on a
	// lookup failure (e.g. the platform API is unavailable).
	Uptime(ctx context.Context, channel string) (since time.Time, live bool, err error)
}

// NewUptimeCommand returns "!uptime" (MinRole RoleEveryone, ~5s global
// Cooldown). Replies "<channel> has been live for 2h 13m" when live,
// "<channel> is offline" when not, and a friendly "couldn't check uptime
// right now" on error. A nil provider yields "uptime is unavailable".
func NewUptimeCommand(provider UptimeProvider) Command {
	return Command{
		Name:     "uptime",
		Help:     "Show how long the stream has been live.",
		Cooldown: defaultUptimeCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if provider == nil {
				return Reply{Text: "uptime is unavailable"}
			}
			channel := strings.TrimPrefix(strings.TrimSpace(msg.Channel), "#")
			since, live, err := provider.Uptime(ctx, channel)
			if err != nil {
				return Reply{Text: "couldn't check uptime right now"}
			}
			if !live {
				return Reply{Text: fmt.Sprintf("%s is offline", channel)}
			}
			return Reply{Text: fmt.Sprintf("%s has been live for %s",
				channel, formatDuration(time.Since(since)))}
		},
	}
}

// defaultStreamStatusCooldown throttles the channel-wide read-only !game
// and !title commands. 5s keeps chat tidy while the underlying provider's
// own TTL cache absorbs the rest.
const defaultStreamStatusCooldown = 5 * time.Second

// StreamStatus is a point-in-time view of a channel's stream used by the
// !game and !title commands.
type StreamStatus struct {
	Live        bool
	GameName    string
	Title       string
	ViewerCount int
}

// StreamStatusProvider reports a channel's current stream metadata. An
// adapter over the Twitch adapter is wired in main.
type StreamStatusProvider interface {
	// Status returns the channel's current stream status. err is non-nil
	// only on a lookup failure (e.g. the platform API is unavailable).
	Status(ctx context.Context, channel string) (StreamStatus, error)
}

// NewGameCommand returns "!game" (MinRole RoleEveryone, ~5s global
// Cooldown). Replies "<channel> is playing <GameName>" when live (or
// "<channel> has no category set" when the category is empty), "<channel>
// is offline" when not, and "couldn't check the game right now" on error.
// A nil provider yields "that's unavailable".
func NewGameCommand(provider StreamStatusProvider) Command {
	return Command{
		Name:     "game",
		Help:     "Show the current category the stream is playing.",
		Cooldown: defaultStreamStatusCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if provider == nil {
				return Reply{Text: "that's unavailable"}
			}
			channel := strings.TrimPrefix(strings.TrimSpace(msg.Channel), "#")
			status, err := provider.Status(ctx, channel)
			if err != nil {
				return Reply{Text: "couldn't check the game right now"}
			}
			if !status.Live {
				return Reply{Text: fmt.Sprintf("%s is offline", channel)}
			}
			if strings.TrimSpace(status.GameName) == "" {
				return Reply{Text: fmt.Sprintf("%s has no category set", channel)}
			}
			return Reply{Text: fmt.Sprintf("%s is playing %s", channel, status.GameName)}
		},
	}
}

// NewTitleCommand returns "!title" (MinRole RoleEveryone, ~5s global
// Cooldown). Replies "Title: <Title>" when live (or "<channel> has no
// title set" when the title is empty), "<channel> is offline" when not,
// and "couldn't check the title right now" on error. A nil provider
// yields "that's unavailable".
func NewTitleCommand(provider StreamStatusProvider) Command {
	return Command{
		Name:     "title",
		Help:     "Show the current stream title.",
		Cooldown: defaultStreamStatusCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if provider == nil {
				return Reply{Text: "that's unavailable"}
			}
			channel := strings.TrimPrefix(strings.TrimSpace(msg.Channel), "#")
			status, err := provider.Status(ctx, channel)
			if err != nil {
				return Reply{Text: "couldn't check the title right now"}
			}
			if !status.Live {
				return Reply{Text: fmt.Sprintf("%s is offline", channel)}
			}
			if strings.TrimSpace(status.Title) == "" {
				return Reply{Text: fmt.Sprintf("%s has no title set", channel)}
			}
			return Reply{Text: fmt.Sprintf("Title: %s", status.Title)}
		},
	}
}

// formatDuration renders a stream's elapsed live time as a compact human
// string: "3d 4h" once 24h+ (days + hours), "2h 13m" for an hour or more
// (hours + minutes), "45m" under an hour, and "just went live" under a
// minute. Sub-minute durations collapse to the friendly phrase rather than
// reading "0m".
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "just went live"
	}
	if d >= 24*time.Hour {
		days := d / (24 * time.Hour)
		hours := (d % (24 * time.Hour)) / time.Hour
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if d >= time.Hour {
		hours := d / time.Hour
		minutes := (d % time.Hour) / time.Minute
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", d/time.Minute)
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
// is available and the empty string otherwise - so the help text reads
// naturally either way.
func mentionPrefix(msg Message) string {
	name := strings.TrimSpace(msg.Username)
	if name == "" {
		return ""
	}
	return "@" + name + " "
}

// ScheduledEvent is the chat-facing view of a Live-Ops item used by the
// !nextevent / !schedule commands. Active reports whether the event is
// currently running (between its start and end), in which case a countdown
// would be meaningless and the command instead says it is happening now.
type ScheduledEvent struct {
	Number   int
	Name     string
	StartsAt time.Time
	Active   bool
}

// EventStore is the narrow surface the Live-Ops built-ins need. An adapter
// over [github.com/Luca-Pelzer/engelos/internal/liveops.Store] is wired in
// main; this interface lives HERE so internal/commands does NOT import
// internal/liveops (mirrors [CounterStore] and [QuoteStore]). main's
// adapter chooses the tenant_id and normalises the channel; the built-ins
// pass msg.Channel through raw (matching the counter admin commands).
type EventStore interface {
	// Next returns the next upcoming event; ok=false if none is upcoming.
	Next(ctx context.Context, channel string) (ScheduledEvent, bool, error)
	// Upcoming returns up to limit upcoming-or-active events, soonest first.
	Upcoming(ctx context.Context, channel string, limit int) ([]ScheduledEvent, error)
	// Add schedules a new event and returns its assigned per-channel number.
	Add(ctx context.Context, channel, name, description string, startsAt time.Time, endsAt *time.Time) (number int, err error)
	// Delete removes the event with the given per-channel number.
	Delete(ctx context.Context, channel string, number int) error
}

// whenOffsetRE matches a relative offset token combining optional days,
// hours, and minutes (e.g. "2d", "4h", "90m", "1d12h", "2d4h30m"). At least
// one group must be present; parseWhen rejects the all-empty match
// separately so a bare "" never parses.
var whenOffsetRE = regexp.MustCompile(`^(\d+d)?(\d+h)?(\d+m)?$`)

// parseWhen converts a single whitespace-free token into an absolute UTC
// start time relative to time.Now. Accepted forms:
//
//   - Relative offset combining days/hours/minutes, in that order, each
//     optional but at least one present: "2d", "4h", "90m", "1d12h",
//     "2d4h30m". The sum is added to the current time.
//   - Absolute date "2006-01-02", interpreted as 00:00 UTC on that day.
//   - Absolute datetime "2006-01-02T15:04", interpreted as UTC.
//
// Any other input yields a non-nil error.
func parseWhen(token string) (time.Time, error) {
	t := strings.TrimSpace(token)
	if t == "" {
		return time.Time{}, fmt.Errorf("empty when token")
	}

	if m := whenOffsetRE.FindStringSubmatch(t); m != nil && (m[1] != "" || m[2] != "" || m[3] != "") {
		var d time.Duration
		if m[1] != "" {
			days, _ := strconv.Atoi(strings.TrimSuffix(m[1], "d"))
			d += time.Duration(days) * 24 * time.Hour
		}
		if m[2] != "" {
			hours, _ := strconv.Atoi(strings.TrimSuffix(m[2], "h"))
			d += time.Duration(hours) * time.Hour
		}
		if m[3] != "" {
			mins, _ := strconv.Atoi(strings.TrimSuffix(m[3], "m"))
			d += time.Duration(mins) * time.Minute
		}
		return time.Now().UTC().Add(d), nil
	}

	if abs, err := time.ParseInLocation("2006-01-02T15:04", t, time.UTC); err == nil {
		return abs.UTC(), nil
	}
	if abs, err := time.ParseInLocation("2006-01-02", t, time.UTC); err == nil {
		return abs.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unparseable when token %q", t)
}

// formatCountdown renders a time-until-start as a compact human phrase:
// "in 2d 4h" once 24h+ (days + hours), "in 2h 13m" for an hour or more
// (hours + minutes), "in 45m" under an hour, and "soon" under a minute.
// Unlike formatDuration it is phrased as a forward-looking countdown.
func formatCountdown(d time.Duration) string {
	if d < time.Minute {
		return "soon"
	}
	if d >= 24*time.Hour {
		days := d / (24 * time.Hour)
		hours := (d % (24 * time.Hour)) / time.Hour
		return fmt.Sprintf("in %dd %dh", days, hours)
	}
	if d >= time.Hour {
		hours := d / time.Hour
		minutes := (d % time.Hour) / time.Minute
		return fmt.Sprintf("in %dh %dm", hours, minutes)
	}
	return fmt.Sprintf("in %dm", d/time.Minute)
}

// NewNextEventCommand returns "!nextevent" (MinRole RoleEveryone, ~5s
// global Cooldown). Replies "<Name> is happening now!" for an active
// event, "Next: <Name> <countdown>" for an upcoming one, "no upcoming
// events" when none, and "couldn't check events right now" on error. A nil
// store yields "events unavailable".
func NewNextEventCommand(store EventStore) Command {
	return Command{
		Name:     "nextevent",
		Help:     "Show the next scheduled event.",
		Cooldown: defaultStreamStatusCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if store == nil {
				return Reply{Text: "events unavailable"}
			}
			channel := strings.TrimPrefix(strings.TrimSpace(msg.Channel), "#")
			evt, ok, err := store.Next(ctx, channel)
			if err != nil {
				return Reply{Text: "couldn't check events right now"}
			}
			if !ok {
				return Reply{Text: "no upcoming events"}
			}
			if evt.Active {
				return Reply{Text: fmt.Sprintf("%s is happening now!", evt.Name)}
			}
			return Reply{Text: fmt.Sprintf("Next: %s %s",
				evt.Name, formatCountdown(time.Until(evt.StartsAt)))}
		},
	}
}

// NewScheduleCommand returns "!schedule" (MinRole RoleEveryone, ~5s global
// Cooldown). Lists up to three upcoming-or-active events as "Schedule:
// <Name> (<countdown or 'now'>) | ...". Replies "no upcoming events" when
// empty and "couldn't check the schedule right now" on error. A nil store
// yields "events unavailable".
func NewScheduleCommand(store EventStore) Command {
	return Command{
		Name:     "schedule",
		Help:     "Show the next few scheduled events.",
		Cooldown: defaultStreamStatusCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if store == nil {
				return Reply{Text: "events unavailable"}
			}
			channel := strings.TrimPrefix(strings.TrimSpace(msg.Channel), "#")
			events, err := store.Upcoming(ctx, channel, 3)
			if err != nil {
				return Reply{Text: "couldn't check the schedule right now"}
			}
			if len(events) == 0 {
				return Reply{Text: "no upcoming events"}
			}
			parts := make([]string, 0, len(events))
			for _, e := range events {
				when := formatCountdown(time.Until(e.StartsAt))
				if e.Active {
					when = "now"
				}
				parts = append(parts, fmt.Sprintf("%s (%s)", e.Name, when))
			}
			return Reply{Text: "Schedule: " + strings.Join(parts, " | ")}
		},
	}
}

// NewAddEventCommand returns "!addevent". Mods-only.
//
// Usage: "!addevent <when> <name...>" where the first arg is a when-token
// (see [parseWhen]) and the rest is the event name. A missing arg or an
// unparseable when yields a usage reply; on success replies "@mod added
// '<name>' (#<n>), <countdown>". A store error yields a friendly reply. A
// nil store yields "events unavailable".
func NewAddEventCommand(store EventStore) Command {
	return Command{
		Name:         "addevent",
		Help:         "Schedule an event - !addevent <when> <name>.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%sevents unavailable", mentionPrefix(msg))}
			}
			usage := fmt.Sprintf("%susage: !addevent <when> <name> - when = 2d / 4h / 90m / 1d12h / 2026-06-15",
				mentionPrefix(msg))
			if len(args) < 2 {
				return Reply{Text: usage}
			}
			startsAt, err := parseWhen(args[0])
			if err != nil {
				return Reply{Text: usage}
			}
			name := strings.TrimSpace(strings.Join(args[1:], " "))
			if name == "" {
				return Reply{Text: usage}
			}
			number, aerr := store.Add(ctx, msg.Channel, name, "", startsAt, nil)
			if aerr != nil {
				return Reply{Text: fmt.Sprintf("%scouldn't add that event", mentionPrefix(msg))}
			}
			return Reply{Text: fmt.Sprintf("%sadded '%s' (#%d), %s",
				mentionPrefix(msg), name, number, formatCountdown(time.Until(startsAt)))}
		},
	}
}

// NewDelEventCommand returns "!delevent". Mods-only.
//
// Usage: "!delevent <number>". On success replies "@mod deleted event
// #<n>". A missing or bad number yields a usage reply; any store error
// yields "@mod no event #<n>". A nil store yields "events unavailable".
func NewDelEventCommand(store EventStore) Command {
	return Command{
		Name:         "delevent",
		Help:         "Remove a scheduled event - !delevent <number>.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%sevents unavailable", mentionPrefix(msg))}
			}
			target := parseAdminTarget(args)
			number, err := strconv.Atoi(target)
			if target == "" || err != nil || number <= 0 {
				return Reply{Text: fmt.Sprintf("%susage: !delevent <number>", mentionPrefix(msg))}
			}
			if derr := store.Delete(ctx, msg.Channel, number); derr != nil {
				return Reply{Text: fmt.Sprintf("%sno event #%d", mentionPrefix(msg), number)}
			}
			return Reply{Text: fmt.Sprintf("%sdeleted event #%d", mentionPrefix(msg), number)}
		},
	}
}
