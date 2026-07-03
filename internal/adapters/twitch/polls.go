package twitch

import (
	"context"
	"fmt"

	"github.com/nicklaw5/helix/v2"
)

// PollView is a neutral, helix-free snapshot of a created poll.
type PollView struct {
	ID    string
	Title string
}

// CreatePoll starts a poll on the channel. The caller is responsible for
// validating title/choice/duration bounds (the action node does); this method
// is a thin Helix pass-through. Requires the channel:manage:polls scope;
// returns [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) CreatePoll(ctx context.Context, login, title string, choices []string, durationSeconds int) (PollView, error) {
	if err := ctx.Err(); err != nil {
		return PollView{}, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return PollView{}, err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return PollView{}, err
	}
	choiceParams := make([]helix.PollChoiceParam, 0, len(choices))
	for _, c := range choices {
		choiceParams = append(choiceParams, helix.PollChoiceParam{Title: c})
	}
	resp, err := hx.CreatePoll(&helix.CreatePollParams{
		BroadcasterID: bid,
		Title:         title,
		Choices:       choiceParams,
		Duration:      durationSeconds,
	})
	if err != nil {
		return PollView{}, fmt.Errorf("twitch: create poll on %q: %w", login, err)
	}
	if err := helixStatusError("create poll", resp.StatusCode, resp.ErrorMessage); err != nil {
		return PollView{}, err
	}
	if len(resp.Data.Polls) == 0 {
		return PollView{}, fmt.Errorf("twitch: create poll on %q: empty response", login)
	}
	p := resp.Data.Polls[0]
	return PollView{ID: p.ID, Title: p.Title}, nil
}
