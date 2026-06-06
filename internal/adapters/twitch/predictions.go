package twitch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nicklaw5/helix/v2"
)

// Prediction status strings as Twitch reports/expects them. ACTIVE accepts
// bets; LOCKED stops betting; RESOLVED pays the winning outcome; CANCELED
// refunds everyone.
const (
	PredictionStatusActive   = "ACTIVE"
	PredictionStatusLocked   = "LOCKED"
	PredictionStatusResolved = "RESOLVED"
	PredictionStatusCanceled = "CANCELED"
)

// ErrNoActivePrediction is returned when a lock/resolve/cancel is attempted
// but the channel has no ACTIVE or LOCKED prediction to act on.
var ErrNoActivePrediction = errors.New("twitch: no active prediction")

// ErrOutcomeNotFound is returned by ResolvePrediction when the supplied
// winning-outcome title does not match any outcome of the active prediction.
var ErrOutcomeNotFound = errors.New("twitch: prediction outcome not found")

// PredictionOutcomeView is a neutral, helix-free view of one prediction
// outcome.
type PredictionOutcomeView struct {
	ID    string
	Title string
}

// PredictionView is a neutral, helix-free snapshot of a prediction returned
// to the orchestrator so it never depends on the helix SDK.
type PredictionView struct {
	ID       string
	Title    string
	Status   string
	Outcomes []PredictionOutcomeView
}

// CreatePrediction opens a Channel-Points prediction on the channel with the
// given title, outcome titles (2-10), and betting window in seconds (1-1800).
// Twitch handles the channel-points pool and proportional payout itself.
// Returns [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) CreatePrediction(ctx context.Context, login, title string, outcomes []string, windowSeconds int) (PredictionView, error) {
	if err := ctx.Err(); err != nil {
		return PredictionView{}, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return PredictionView{}, err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return PredictionView{}, err
	}
	choices := make([]helix.PredictionChoiceParam, 0, len(outcomes))
	for _, o := range outcomes {
		choices = append(choices, helix.PredictionChoiceParam{Title: o})
	}
	resp, err := hx.CreatePrediction(&helix.CreatePredictionParams{
		BroadcasterID:    bid,
		Title:            title,
		Outcomes:         choices,
		PredictionWindow: windowSeconds,
	})
	if err != nil {
		return PredictionView{}, fmt.Errorf("twitch: create prediction on %q: %w", login, err)
	}
	if err := helixStatusError("create prediction", resp.StatusCode, resp.ErrorMessage); err != nil {
		return PredictionView{}, err
	}
	if len(resp.Data.Predictions) == 0 {
		return PredictionView{}, fmt.Errorf("twitch: create prediction on %q: empty response", login)
	}
	return toPredictionView(resp.Data.Predictions[0]), nil
}

// ActivePrediction returns the channel's current ACTIVE or LOCKED prediction.
// It returns [ErrNoActivePrediction] when none is open. The most recent
// prediction is the first element Twitch returns. Returns
// [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) ActivePrediction(ctx context.Context, login string) (PredictionView, error) {
	if err := ctx.Err(); err != nil {
		return PredictionView{}, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return PredictionView{}, err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return PredictionView{}, err
	}
	resp, err := hx.GetPredictions(&helix.PredictionsParams{BroadcasterID: bid})
	if err != nil {
		return PredictionView{}, fmt.Errorf("twitch: get predictions on %q: %w", login, err)
	}
	if err := helixStatusError("get predictions", resp.StatusCode, resp.ErrorMessage); err != nil {
		return PredictionView{}, err
	}
	for _, p := range resp.Data.Predictions {
		if p.Status == PredictionStatusActive || p.Status == PredictionStatusLocked {
			return toPredictionView(p), nil
		}
	}
	return PredictionView{}, ErrNoActivePrediction
}

// LockPrediction stops betting on the channel's active prediction without
// resolving it. Returns [ErrNoActivePrediction] when none is open.
func (a *Adapter) LockPrediction(ctx context.Context, login string) (PredictionView, error) {
	active, err := a.ActivePrediction(ctx, login)
	if err != nil {
		return PredictionView{}, err
	}
	return a.endPrediction(ctx, login, active.ID, PredictionStatusLocked, "")
}

// ResolvePrediction settles the channel's active prediction, paying out the
// outcome whose title matches winningOutcome (case-insensitively). Twitch
// computes each winner's proportional channel-points payout. Returns
// [ErrNoActivePrediction] when none is open, or [ErrOutcomeNotFound] when the
// title matches no outcome.
func (a *Adapter) ResolvePrediction(ctx context.Context, login, winningOutcome string) (PredictionView, error) {
	active, err := a.ActivePrediction(ctx, login)
	if err != nil {
		return PredictionView{}, err
	}
	want := strings.ToLower(strings.TrimSpace(winningOutcome))
	outcomeID := ""
	for _, o := range active.Outcomes {
		if strings.ToLower(strings.TrimSpace(o.Title)) == want {
			outcomeID = o.ID
			break
		}
	}
	if outcomeID == "" {
		return PredictionView{}, ErrOutcomeNotFound
	}
	return a.endPrediction(ctx, login, active.ID, PredictionStatusResolved, outcomeID)
}

// CancelPrediction cancels the channel's active prediction and refunds every
// bettor's channel points. Returns [ErrNoActivePrediction] when none is open.
func (a *Adapter) CancelPrediction(ctx context.Context, login string) (PredictionView, error) {
	active, err := a.ActivePrediction(ctx, login)
	if err != nil {
		return PredictionView{}, err
	}
	return a.endPrediction(ctx, login, active.ID, PredictionStatusCanceled, "")
}

// endPrediction is the shared EndPrediction call used by Lock/Resolve/Cancel.
// winningOutcomeID is required only for RESOLVED and ignored otherwise.
func (a *Adapter) endPrediction(ctx context.Context, login, predictionID, status, winningOutcomeID string) (PredictionView, error) {
	if err := ctx.Err(); err != nil {
		return PredictionView{}, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return PredictionView{}, err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return PredictionView{}, err
	}
	resp, err := hx.EndPrediction(&helix.EndPredictionParams{
		BroadcasterID:    bid,
		ID:               predictionID,
		Status:           status,
		WinningOutcomeID: winningOutcomeID,
	})
	if err != nil {
		return PredictionView{}, fmt.Errorf("twitch: end prediction %q (%s) on %q: %w", predictionID, status, login, err)
	}
	if err := helixStatusError("end prediction", resp.StatusCode, resp.ErrorMessage); err != nil {
		return PredictionView{}, err
	}
	if len(resp.Data.Predictions) == 0 {
		return PredictionView{ID: predictionID, Status: status}, nil
	}
	return toPredictionView(resp.Data.Predictions[0]), nil
}

func toPredictionView(p helix.Prediction) PredictionView {
	outcomes := make([]PredictionOutcomeView, 0, len(p.Outcomes))
	for _, o := range p.Outcomes {
		outcomes = append(outcomes, PredictionOutcomeView{ID: o.ID, Title: o.Title})
	}
	return PredictionView{
		ID:       p.ID,
		Title:    p.Title,
		Status:   p.Status,
		Outcomes: outcomes,
	}
}
