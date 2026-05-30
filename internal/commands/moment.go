package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// defaultMomentWindow is the default !here window when a mod gives no duration.
const defaultMomentWindow = 60 * time.Second

// momentWindowMin and momentWindowMax bound the mod-supplied window so a typo
// cannot open a moment that never closes or closes instantly.
const (
	momentWindowMin = 10 * time.Second
	momentWindowMax = 10 * time.Minute
)

// MomentOutcome is a sentinel enum the moment commands react to without
// importing the moments store, mirroring [PredictionOutcome] and
// [LoyaltyError]. main maps the store's results onto these.
type MomentOutcome int

const (
	// MomentOK signals success.
	MomentOK MomentOutcome = iota
	// MomentUnavailable means the controller is nil or the store call failed.
	MomentUnavailable
	// MomentActiveExists means a moment is already open (on Open).
	MomentActiveExists
	// MomentNone means no moment is open (on Join/End).
	MomentNone
	// MomentClosedWindow means the !here window has elapsed (on Join).
	MomentClosedWindow
	// MomentAlreadyJoined means the viewer already reacted to this moment.
	MomentAlreadyJoined
	// MomentInvalid means the request was malformed (e.g. empty title).
	MomentInvalid
)

// MomentResult is the read view the End/Join replies render.
type MomentResult struct {
	Title        string
	Rarity       string // "common" | "rare" | "legendary" (End only)
	Participants int
}

// MomentController is the narrow surface the moment commands need. main wires a
// moments-store adapter onto it (which also broadcasts the overlay alerts), so
// internal/commands never imports the moments/store or websocket packages,
// mirroring [PredictionController].
type MomentController interface {
	// Open starts a moment for the channel with the given title and window.
	Open(ctx context.Context, channel, title, openedBy string, window time.Duration) MomentOutcome
	// Join records the viewer's !here reaction, returning the running count.
	Join(ctx context.Context, channel, viewerID, username string) (MomentResult, MomentOutcome)
	// End closes the channel's moment and returns its rarity + final count.
	End(ctx context.Context, channel string) (MomentResult, MomentOutcome)
	// History renders the most recent closed moments as a one-line summary.
	History(ctx context.Context, channel string, limit int) (string, MomentOutcome)
}

// NewMomentCommand returns "!moment" (mods). Subcommands:
//
//	!moment <title> [window]   open a moment (window like "90" or "90s", clamped 10s-10m)
//	!moment end                close it and announce the rarity
//	!moment history            list recent moments
//
// A bare "!moment" with no args shows usage. A nil controller replies
// "moments are unavailable".
func NewMomentCommand(ctrl MomentController) Command {
	return Command{
		Name:         "moment",
		Help:         "Run a BeReal-style moment - !moment <title> [secs] | end | history.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if ctrl == nil {
				return Reply{Text: fmt.Sprintf("%smoments are unavailable", mentionPrefix(msg))}
			}
			if len(args) == 0 {
				return Reply{Text: fmt.Sprintf("%susage: !moment <title> [secs] | end | history", mentionPrefix(msg))}
			}
			switch strings.ToLower(args[0]) {
			case "end", "close", "stop":
				return momentEndReply(ctx, ctrl, msg)
			case "history", "list", "recent":
				return momentHistoryReply(ctx, ctrl, msg)
			default:
				return momentOpenReply(ctx, ctrl, msg, args)
			}
		},
	}
}

// momentOpenReply parses an optional trailing window token and opens a moment.
func momentOpenReply(ctx context.Context, ctrl MomentController, msg Message, args []string) Reply {
	title := strings.TrimSpace(strings.Join(args, " "))
	window := defaultMomentWindow
	// A trailing integer (optionally suffixed "s") is treated as the window.
	if len(args) >= 2 {
		if w, ok := parseWindowToken(args[len(args)-1]); ok {
			window = w
			title = strings.TrimSpace(strings.Join(args[:len(args)-1], " "))
		}
	}
	if title == "" {
		return Reply{Text: fmt.Sprintf("%susage: !moment <title> [secs]", mentionPrefix(msg))}
	}
	switch ctrl.Open(ctx, msg.Channel, title, msg.Username, window) {
	case MomentOK:
		return Reply{Text: fmt.Sprintf("\U0001F6A8 MOMENT: %s - type !here in the next %ds!",
			title, int(window.Seconds()))}
	case MomentActiveExists:
		return Reply{Text: fmt.Sprintf("%sa moment is already running - !moment end it first", mentionPrefix(msg))}
	case MomentInvalid:
		return Reply{Text: fmt.Sprintf("%susage: !moment <title> [secs]", mentionPrefix(msg))}
	default:
		return Reply{Text: fmt.Sprintf("%scouldn't start the moment right now", mentionPrefix(msg))}
	}
}

func momentEndReply(ctx context.Context, ctrl MomentController, msg Message) Reply {
	res, outcome := ctrl.End(ctx, msg.Channel)
	switch outcome {
	case MomentOK:
		return Reply{Text: fmt.Sprintf("Moment over: \"%s\" was %s - %d were there \U0001F4F8",
			res.Title, res.Rarity, res.Participants)}
	case MomentNone:
		return Reply{Text: fmt.Sprintf("%sno moment is running right now", mentionPrefix(msg))}
	default:
		return Reply{Text: fmt.Sprintf("%scouldn't end the moment right now", mentionPrefix(msg))}
	}
}

func momentHistoryReply(ctx context.Context, ctrl MomentController, msg Message) Reply {
	summary, outcome := ctrl.History(ctx, msg.Channel, 5)
	switch outcome {
	case MomentOK:
		if strings.TrimSpace(summary) == "" {
			return Reply{Text: fmt.Sprintf("%sno moments yet", mentionPrefix(msg))}
		}
		return Reply{Text: "Recent moments: " + summary}
	default:
		return Reply{Text: fmt.Sprintf("%scouldn't load moments right now", mentionPrefix(msg))}
	}
}

// NewHereCommand returns "!here" (everyone): the viewer's reaction to the
// active moment. A nil controller replies "moments are unavailable".
func NewHereCommand(ctrl MomentController) Command {
	return Command{
		Name:         "here",
		Help:         "React to the running moment - !here.",
		UserCooldown: 2 * time.Second,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if ctrl == nil {
				return Reply{Text: fmt.Sprintf("%smoments are unavailable", mentionPrefix(msg))}
			}
			res, outcome := ctrl.Join(ctx, msg.Channel, msg.UserID, msg.Username)
			switch outcome {
			case MomentOK:
				return Reply{Text: fmt.Sprintf("%syou were here! \u2705 (%d so far)", mentionPrefix(msg), res.Participants)}
			case MomentAlreadyJoined:
				return Reply{Text: fmt.Sprintf("%syou're already counted for this moment", mentionPrefix(msg))}
			case MomentClosedWindow:
				return Reply{Text: fmt.Sprintf("%stoo late - that moment just closed", mentionPrefix(msg))}
			case MomentNone:
				return Reply{Text: fmt.Sprintf("%sthere's no moment running right now", mentionPrefix(msg))}
			default:
				return Reply{Text: fmt.Sprintf("%scouldn't record that right now", mentionPrefix(msg))}
			}
		},
	}
}

// parseWindowToken parses a window like "90" or "90s" into a clamped duration.
// ok is false when the token is not a bare (optionally "s"-suffixed) integer,
// so a non-numeric last word stays part of the title.
func parseWindowToken(tok string) (time.Duration, bool) {
	t := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(tok)), "s")
	n, err := strconv.Atoi(t)
	if err != nil || n <= 0 {
		return 0, false
	}
	d := time.Duration(n) * time.Second
	if d < momentWindowMin {
		d = momentWindowMin
	}
	if d > momentWindowMax {
		d = momentWindowMax
	}
	return d, true
}
