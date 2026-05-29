package commands

import (
	"context"
	"fmt"
	"math"
	"strings"
)

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
		Name: "pity",
		Help: "Show your current pity points and win chance.",
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
		Name: "streak",
		Help: "Show your current activity streak.",
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
