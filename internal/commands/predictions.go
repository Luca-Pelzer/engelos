package commands

import (
	"context"
	"fmt"
	"strings"
)

// defaultPredictionWindow is the betting window, in seconds, opened by
// !prediction. Twitch allows 30–1800s; 120s is a sensible default that keeps
// a round snappy. The chat command does not expose a configurable window for
// v1 — a fixed 120s keeps parsing unambiguous (every pipe-section is an
// outcome, never a stray number).
const defaultPredictionWindow = 120

// maxPredictionTitle is Twitch's hard limit on a prediction title (45 chars).
// A longer title yields a usage reply rather than a silent truncation, since
// the mod likely fat-fingered the "|" separator.
const maxPredictionTitle = 45

// maxPredictionOutcome is Twitch's hard limit on an outcome title (25 chars).
// Unlike the title, an over-long outcome is capped (not rejected) so a verbose
// option still opens the prediction instead of erroring the whole command.
const maxPredictionOutcome = 25

// minPredictionOutcomes / maxPredictionOutcomes bound the outcome count Twitch
// accepts (2–10). Fewer than two has nothing to bet on; more than ten is
// rejected by the API.
const (
	minPredictionOutcomes = 2
	maxPredictionOutcomes = 10
)

// PredictionOutcome mirrors the [LoyaltyError] pattern: a sentinel enum the
// commands react to without importing the twitch/helix packages. main wraps a
// Twitch adapter and maps its results onto these so internal/commands stays
// decoupled from the platform SDK.
type PredictionOutcome int

const (
	// PredictionOK signals a successful operation.
	PredictionOK PredictionOutcome = iota
	// PredictionUnavailable means the controller was nil or the Helix call failed.
	PredictionUnavailable
	// PredictionNotAffiliate means the channel is not affiliate/partner —
	// predictions are an affiliate-only feature.
	PredictionNotAffiliate
	// PredictionActiveExists means a prediction is already running, so a new
	// one cannot be created until it is resolved or canceled.
	PredictionActiveExists
	// PredictionNone means there is no active prediction to lock, resolve, or
	// cancel.
	PredictionNone
	// PredictionInvalid means the request was malformed (e.g. an unknown
	// winning outcome, or fewer than two outcomes).
	PredictionInvalid
)

// PredictionInfo is the read view a reply renders. Outcomes lists the outcome
// titles in their submitted order; Status is the platform's lifecycle label
// ("ACTIVE", "LOCKED", "RESOLVED", or "CANCELED").
type PredictionInfo struct {
	Title    string
	Outcomes []string
	Status   string
}

// PredictionController is the narrow surface the prediction commands need.
// main wires a Twitch adapter over internal/adapters/twitch onto it. The
// interface lives HERE (not imported from the twitch package) so
// internal/commands never depends on the twitch/helix SDK — mirroring
// [LoyaltyProvider] and the *Store interfaces in this package.
//
// channel is passed through from msg.Channel; the adapter is responsible for
// resolving it to a broadcaster ID. Every method returns the resulting
// [PredictionInfo] read view alongside a [PredictionOutcome] sentinel.
type PredictionController interface {
	Create(ctx context.Context, channel, title string, outcomes []string, windowSeconds int) (PredictionInfo, PredictionOutcome)
	Lock(ctx context.Context, channel string) (PredictionInfo, PredictionOutcome)
	Resolve(ctx context.Context, channel, winningOutcome string) (PredictionInfo, PredictionOutcome)
	Cancel(ctx context.Context, channel string) (PredictionInfo, PredictionOutcome)
}

// parsePredictionArgs splits a raw "!prediction" argument string on "|" and
// trims each section. The first section is the title; the rest are outcome
// titles. It returns ok=false when there is no non-empty title or fewer than
// [minPredictionOutcomes] outcomes, or when the title exceeds
// [maxPredictionTitle] — every such case maps to a usage reply. Over-long
// outcomes are capped to [maxPredictionOutcome] rather than rejected, and at
// most [maxPredictionOutcomes] outcomes are kept.
func parsePredictionArgs(args []string) (title string, outcomes []string, ok bool) {
	parts := strings.Split(strings.Join(args, " "), "|")
	if len(parts) == 0 {
		return "", nil, false
	}
	title = strings.TrimSpace(parts[0])
	if title == "" || len(title) > maxPredictionTitle {
		return "", nil, false
	}
	outcomes = make([]string, 0, len(parts)-1)
	for _, p := range parts[1:] {
		o := strings.TrimSpace(p)
		if o == "" {
			continue
		}
		if len(o) > maxPredictionOutcome {
			o = o[:maxPredictionOutcome]
		}
		outcomes = append(outcomes, o)
		if len(outcomes) == maxPredictionOutcomes {
			break
		}
	}
	if len(outcomes) < minPredictionOutcomes {
		return "", nil, false
	}
	return title, outcomes, true
}

// NewPredictionCommand returns "!prediction". Mods-only.
//
// Usage: "!prediction <title> | <outcome1> | <outcome2> [| <outcome3> ...]".
// The full argument string is split on "|": the first section is the title
// (required, ≤45 chars) and the rest are outcomes (2–10; each capped to 25
// chars). The betting window is a fixed [defaultPredictionWindow] seconds. On
// success replies "@mod prediction opened: <title> — options: a / b (120s to
// bet)". A nil controller yields a one-line "predictions are unavailable"
// reply (mirroring the nil-store guards in builtins.go).
func NewPredictionCommand(ctrl PredictionController) Command {
	return Command{
		Name:         "prediction",
		Help:         "Open a prediction — !prediction <title> | <outcome1> | <outcome2>.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if ctrl == nil {
				return Reply{Text: fmt.Sprintf("%spredictions are unavailable",
					mentionPrefix(msg))}
			}
			title, outcomes, ok := parsePredictionArgs(args)
			if !ok {
				return Reply{Text: fmt.Sprintf(
					"%susage: !prediction <title> | <outcome1> | <outcome2>",
					mentionPrefix(msg))}
			}
			info, status := ctrl.Create(ctx, msg.Channel, title, outcomes, defaultPredictionWindow)
			switch status {
			case PredictionOK:
				return Reply{Text: fmt.Sprintf("%sprediction opened: %s — options: %s (%ds to bet)",
					mentionPrefix(msg),
					predictionTitle(info, title),
					strings.Join(predictionOutcomes(info, outcomes), " / "),
					defaultPredictionWindow)}
			case PredictionActiveExists:
				return Reply{Text: fmt.Sprintf(
					"%sa prediction is already running — resolve or cancel it first",
					mentionPrefix(msg))}
			case PredictionNotAffiliate:
				return Reply{Text: fmt.Sprintf(
					"%spredictions need an affiliate/partner channel", mentionPrefix(msg))}
			case PredictionInvalid:
				return Reply{Text: fmt.Sprintf(
					"%susage: !prediction <title> | <outcome1> | <outcome2>",
					mentionPrefix(msg))}
			default:
				return Reply{Text: fmt.Sprintf(
					"%scouldn't open the prediction right now", mentionPrefix(msg))}
			}
		},
	}
}

// NewLockPredictionCommand returns "!lockprediction". Mods-only. It takes no
// arguments and locks betting on the active prediction. On success replies
// "@mod betting locked for: <title>"; with no active prediction replies "@mod
// no active prediction". A nil controller yields "predictions are
// unavailable".
func NewLockPredictionCommand(ctrl PredictionController) Command {
	return Command{
		Name:         "lockprediction",
		Help:         "Lock betting on the active prediction.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if ctrl == nil {
				return Reply{Text: fmt.Sprintf("%spredictions are unavailable",
					mentionPrefix(msg))}
			}
			info, status := ctrl.Lock(ctx, msg.Channel)
			switch status {
			case PredictionOK:
				return Reply{Text: fmt.Sprintf("%sbetting locked for: %s",
					mentionPrefix(msg), predictionTitle(info, "prediction"))}
			case PredictionNone:
				return Reply{Text: fmt.Sprintf("%sno active prediction", mentionPrefix(msg))}
			case PredictionNotAffiliate:
				return Reply{Text: fmt.Sprintf(
					"%spredictions need an affiliate/partner channel", mentionPrefix(msg))}
			default:
				return Reply{Text: fmt.Sprintf(
					"%scouldn't lock it right now", mentionPrefix(msg))}
			}
		},
	}
}

// NewResolvePredictionCommand returns "!endprediction" (alias
// "!resolveprediction"). Mods-only.
//
// Usage: "!endprediction <winning outcome>" — the winning outcome is the full
// remaining text (trimmed). Empty yields a usage reply. On success replies
// "@mod prediction resolved: '<winningOutcome>' wins! 🎉"; an unknown outcome
// replies "@mod couldn't find that outcome — check the spelling"; no active
// prediction replies "@mod no active prediction". A nil controller yields
// "predictions are unavailable".
func NewResolvePredictionCommand(ctrl PredictionController) Command {
	return Command{
		Name:         "endprediction",
		Aliases:      []string{"resolveprediction"},
		Help:         "Resolve the active prediction — !endprediction <winning outcome>.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if ctrl == nil {
				return Reply{Text: fmt.Sprintf("%spredictions are unavailable",
					mentionPrefix(msg))}
			}
			winning := strings.TrimSpace(strings.Join(args, " "))
			if winning == "" {
				return Reply{Text: fmt.Sprintf(
					"%susage: !endprediction <winning outcome>", mentionPrefix(msg))}
			}
			_, status := ctrl.Resolve(ctx, msg.Channel, winning)
			switch status {
			case PredictionOK:
				return Reply{Text: fmt.Sprintf("%sprediction resolved: '%s' wins! 🎉",
					mentionPrefix(msg), winning)}
			case PredictionNone:
				return Reply{Text: fmt.Sprintf("%sno active prediction", mentionPrefix(msg))}
			case PredictionInvalid:
				return Reply{Text: fmt.Sprintf(
					"%scouldn't find that outcome — check the spelling", mentionPrefix(msg))}
			default:
				return Reply{Text: fmt.Sprintf(
					"%scouldn't resolve it right now", mentionPrefix(msg))}
			}
		},
	}
}

// NewCancelPredictionCommand returns "!cancelprediction". Mods-only. It takes
// no arguments and cancels the active prediction, refunding all bets. On
// success replies "@mod prediction canceled, all points refunded"; no active
// prediction replies "@mod no active prediction". A nil controller yields
// "predictions are unavailable".
func NewCancelPredictionCommand(ctrl PredictionController) Command {
	return Command{
		Name:         "cancelprediction",
		Help:         "Cancel the active prediction and refund all bets.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if ctrl == nil {
				return Reply{Text: fmt.Sprintf("%spredictions are unavailable",
					mentionPrefix(msg))}
			}
			_, status := ctrl.Cancel(ctx, msg.Channel)
			switch status {
			case PredictionOK:
				return Reply{Text: fmt.Sprintf("%sprediction canceled, all points refunded",
					mentionPrefix(msg))}
			case PredictionNone:
				return Reply{Text: fmt.Sprintf("%sno active prediction", mentionPrefix(msg))}
			default:
				return Reply{Text: fmt.Sprintf(
					"%scouldn't cancel it right now", mentionPrefix(msg))}
			}
		},
	}
}

// predictionTitle returns the title from the controller's read view when set,
// falling back to the locally-parsed value so a reply never renders an empty
// title even if the adapter returns a zero-value [PredictionInfo].
func predictionTitle(info PredictionInfo, fallback string) string {
	if t := strings.TrimSpace(info.Title); t != "" {
		return t
	}
	return fallback
}

// predictionOutcomes returns the outcome titles from the controller's read
// view when present, falling back to the locally-parsed slice (same
// zero-value tolerance as [predictionTitle]).
func predictionOutcomes(info PredictionInfo, fallback []string) []string {
	if len(info.Outcomes) > 0 {
		return info.Outcomes
	}
	return fallback
}
