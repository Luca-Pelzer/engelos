package twitch

import (
	"context"
	"fmt"
	"strings"

	"github.com/nicklaw5/helix/v2"
)

// RewardSpec describes a Channel-Points custom reward to create.
type RewardSpec struct {
	Title           string
	Cost            int
	Prompt          string
	Enabled         bool
	UserInputNeeded bool
	BackgroundColor string // optional hex like "#00E5CB"; empty = Twitch default
}

// Reward is a neutral view of an existing Channel-Points custom reward,
// free of any helix types so the orchestrator never depends on the SDK.
type Reward struct {
	ID      string
	Title   string
	Cost    int
	Prompt  string
	Enabled bool
}

// BroadcasterType returns "affiliate", "partner", or "" (none) for the
// given channel login. It is used to gate Channel-Points features, which
// require an affiliate or partner channel. Returns [ErrHelixUnavailable]
// in anonymous mode. The type is never cached because affiliate status can
// change while the bot runs.
func (a *Adapter) BroadcasterType(ctx context.Context, login string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return "", err
	}
	resp, err := hx.GetUsers(&helix.UsersParams{Logins: []string{normalizeLogin(login)}})
	if err != nil {
		return "", fmt.Errorf("twitch: lookup broadcaster type for %q: %w", login, err)
	}
	if err := helixStatusError("broadcaster type", resp.StatusCode, resp.ErrorMessage); err != nil {
		return "", err
	}
	if len(resp.Data.Users) == 0 {
		return "", fmt.Errorf("twitch: broadcaster type: no user for login %q", login)
	}
	return resp.Data.Users[0].BroadcasterType, nil
}

// CreateReward creates a manageable Channel-Points custom reward on the
// channel and returns the neutral [Reward] including the Twitch-assigned
// id. Returns [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) CreateReward(ctx context.Context, login string, spec RewardSpec) (Reward, error) {
	if err := ctx.Err(); err != nil {
		return Reward{}, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return Reward{}, err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return Reward{}, err
	}
	resp, err := hx.CreateCustomReward(&helix.ChannelCustomRewardsParams{
		BroadcasterID:       bid,
		Title:               spec.Title,
		Cost:                spec.Cost,
		Prompt:              spec.Prompt,
		IsEnabled:           spec.Enabled,
		IsUserInputRequired: spec.UserInputNeeded,
		BackgroundColor:     spec.BackgroundColor,
	})
	if err != nil {
		return Reward{}, fmt.Errorf("twitch: create custom reward on %q: %w", login, err)
	}
	if err := helixStatusError("create custom reward", resp.StatusCode, resp.ErrorMessage); err != nil {
		return Reward{}, err
	}
	if len(resp.Data.ChannelCustomRewards) == 0 {
		return Reward{}, fmt.Errorf("twitch: create custom reward on %q: empty response", login)
	}
	return toReward(resp.Data.ChannelCustomRewards[0]), nil
}

// ListManageableRewards returns the custom rewards this application can
// manage (OnlyManageableRewards=true) on the channel. Returns
// [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) ListManageableRewards(ctx context.Context, login string) ([]Reward, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return nil, err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return nil, err
	}
	resp, err := hx.GetCustomRewards(&helix.GetCustomRewardsParams{
		BroadcasterID:         bid,
		OnlyManageableRewards: true,
	})
	if err != nil {
		return nil, fmt.Errorf("twitch: list custom rewards on %q: %w", login, err)
	}
	if err := helixStatusError("list custom rewards", resp.StatusCode, resp.ErrorMessage); err != nil {
		return nil, err
	}
	rewards := make([]Reward, 0, len(resp.Data.ChannelCustomRewards))
	for _, r := range resp.Data.ChannelCustomRewards {
		rewards = append(rewards, toReward(r))
	}
	return rewards, nil
}

// DeleteReward deletes a custom reward by id. Only rewards created by this
// application can be deleted. Returns [ErrHelixUnavailable] in anonymous
// mode.
func (a *Adapter) DeleteReward(ctx context.Context, login, rewardID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return err
	}
	resp, err := hx.DeleteCustomReward(&helix.DeleteCustomRewardsParams{
		BroadcasterID: bid,
		ID:            rewardID,
	})
	if err != nil {
		return fmt.Errorf("twitch: delete custom reward %q on %q: %w", rewardID, login, err)
	}
	return helixStatusError("delete custom reward", resp.StatusCode, resp.ErrorMessage)
}

// FulfillRedemption marks a Channel-Points redemption FULFILLED. Returns
// [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) FulfillRedemption(ctx context.Context, login, rewardID, redemptionID string) error {
	return a.updateRedemption(ctx, login, rewardID, redemptionID, "FULFILLED")
}

// CancelRedemption marks a Channel-Points redemption CANCELED, refunding
// the viewer's points. Returns [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) CancelRedemption(ctx context.Context, login, rewardID, redemptionID string) error {
	return a.updateRedemption(ctx, login, rewardID, redemptionID, "CANCELED")
}

// SubscribeRedemptions registers a Channel-Points redemption EventSub
// subscription bound to the given WebSocket session for the channel. Called
// from the eventsub client's OnSession callback after the session_welcome.
// Returns [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) SubscribeRedemptions(ctx context.Context, login, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return err
	}
	resp, err := hx.CreateEventSubSubscription(&helix.EventSubSubscription{
		Type:      "channel.channel_points_custom_reward_redemption.add",
		Version:   "1",
		Condition: helix.EventSubCondition{BroadcasterUserID: bid},
		Transport: helix.EventSubTransport{Method: "websocket", SessionID: sessionID},
	})
	if err != nil {
		return fmt.Errorf("twitch: subscribe redemptions: %w", err)
	}
	return helixStatusError("subscribe redemptions", resp.StatusCode, resp.ErrorMessage)
}

func (a *Adapter) updateRedemption(ctx context.Context, login, rewardID, redemptionID, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return err
	}
	resp, err := hx.UpdateChannelCustomRewardsRedemptionStatus(&helix.UpdateChannelCustomRewardsRedemptionStatusParams{
		ID:            redemptionID,
		BroadcasterID: bid,
		RewardID:      rewardID,
		Status:        status,
	})
	if err != nil {
		return fmt.Errorf("twitch: update redemption %q to %s: %w", redemptionID, status, err)
	}
	return helixStatusError("update redemption status", resp.StatusCode, resp.ErrorMessage)
}

// helixClientOrErr returns the live Helix client or [ErrHelixUnavailable]
// when the adapter runs in anonymous mode, mirroring StreamInfo's guard.
func (a *Adapter) helixClientOrErr() (helixClient, error) {
	a.mu.Lock()
	hx := a.helix
	a.mu.Unlock()
	if hx == nil {
		return nil, ErrHelixUnavailable
	}
	return hx, nil
}

// rewardBroadcasterID resolves a channel login to its numeric broadcaster
// id via Helix GetUsers and caches the result forever (login→id is
// effectively immutable). The cache is consulted under rewardMu, which is
// released across the network call so a slow GetUsers never blocks other
// reward calls; on a miss the result is re-locked and stored.
func (a *Adapter) rewardBroadcasterID(ctx context.Context, login string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	login = normalizeLogin(login)

	a.rewardMu.Lock()
	if id, ok := a.idCache[login]; ok && id != "" {
		a.rewardMu.Unlock()
		return id, nil
	}
	a.rewardMu.Unlock()

	hx, err := a.helixClientOrErr()
	if err != nil {
		return "", err
	}
	resp, err := hx.GetUsers(&helix.UsersParams{Logins: []string{login}})
	if err != nil {
		return "", fmt.Errorf("twitch: resolve broadcaster id for %q: %w", login, err)
	}
	if err := helixStatusError("resolve broadcaster id", resp.StatusCode, resp.ErrorMessage); err != nil {
		return "", err
	}
	if len(resp.Data.Users) == 0 || resp.Data.Users[0].ID == "" {
		return "", fmt.Errorf("twitch: resolve broadcaster id: no user for login %q", login)
	}
	id := resp.Data.Users[0].ID

	a.rewardMu.Lock()
	a.idCache[login] = id
	a.rewardMu.Unlock()
	return id, nil
}

func toReward(r helix.ChannelCustomReward) Reward {
	return Reward{
		ID:      r.ID,
		Title:   r.Title,
		Cost:    r.Cost,
		Prompt:  r.Prompt,
		Enabled: r.IsEnabled,
	}
}

// normalizeLogin canonicalises a channel login the same way StreamInfo
// does: trim surrounding space, strip a leading '#', and lowercase.
func normalizeLogin(login string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(login)), "#")
}
