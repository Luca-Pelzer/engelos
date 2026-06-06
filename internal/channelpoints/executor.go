package channelpoints

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch/eventsub"
	"github.com/Luca-Pelzer/engelos/internal/redemptions"
)

// maxChatMessageLen caps an expanded chat message, matching the
// codebase-wide 480-char convention.
const maxChatMessageLen = 480

// ChatSender posts a chat message to a channel. Satisfied by the bot's
// platform sender in main.
type ChatSender interface {
	Send(ctx context.Context, channel, text string) error
}

// CounterAdmin mutates named counters for a channel. Satisfied by main's
// counter adapter.
type CounterAdmin interface {
	Increment(ctx context.Context, channel, name string) (int64, error)
	Reset(ctx context.Context, channel, name string) (int64, error)
}

// Fulfiller completes or refunds a redemption. Satisfied by *twitch.Adapter.
type Fulfiller interface {
	FulfillRedemption(ctx context.Context, login, rewardID, redemptionID string) error
	CancelRedemption(ctx context.Context, login, rewardID, redemptionID string) error
}

// BindingStore reads the reward->action binding. Satisfied by
// *redemptions.Store.
type BindingStore interface {
	GetByReward(ctx context.Context, tenantID, channel, rewardID string) (redemptions.Binding, error)
}

// Config configures an [Executor]. Construct via [New].
type Config struct {
	TenantID  string
	Store     BindingStore
	Chat      ChatSender   // may be nil -> chat actions are no-ops with a logged warning
	Counters  CounterAdmin // may be nil -> counter actions error
	Fulfiller Fulfiller    // may be nil -> auto-fulfill skipped
	Logger    *slog.Logger
}

// Executor runs the bot action bound to a redeemed reward and optionally
// fulfils or refunds the redemption.
type Executor struct {
	tenantID  string
	store     BindingStore
	chat      ChatSender
	counters  CounterAdmin
	fulfiller Fulfiller
	logger    *slog.Logger
}

// New constructs an [Executor] from cfg, defaulting a nil Logger to
// slog.Default.
func New(cfg Config) *Executor {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		tenantID:  cfg.TenantID,
		store:     cfg.Store,
		chat:      cfg.Chat,
		counters:  cfg.Counters,
		fulfiller: cfg.Fulfiller,
		logger:    logger.With("component", "channelpoints"),
	}
}

// Handle processes one redemption event: it looks up the binding for the
// redeemed reward, runs the bound action, and (when the binding has
// AutoFulfill) marks the redemption FULFILLED on success or CANCELED on
// failure. It is safe to pass directly as the eventsub client's Handler.
func (e *Executor) Handle(ctx context.Context, evt eventsub.RedemptionEvent) {
	channel := strings.ToLower(strings.TrimSpace(evt.BroadcasterUserLogin))
	if channel == "" {
		e.logger.Warn("redemption missing broadcaster login; dropping", "reward_id", evt.RewardID)
		return
	}

	binding, err := e.store.GetByReward(ctx, e.tenantID, channel, evt.RewardID)
	if errors.Is(err, redemptions.ErrNotFound) {
		e.logger.Debug("no binding for reward; ignoring", "channel", channel, "reward_id", evt.RewardID)
		return
	}
	if err != nil {
		e.logger.Error("binding lookup failed", "channel", channel, "reward_id", evt.RewardID, "err", err)
		return
	}
	if !binding.Enabled {
		e.logger.Debug("binding disabled; ignoring", "channel", channel, "reward_id", evt.RewardID)
		return
	}

	actionErr := e.runAction(ctx, channel, binding, evt)

	if binding.AutoFulfill && e.fulfiller != nil {
		e.settle(ctx, channel, evt, actionErr)
	}

	e.logger.Info("redemption handled",
		"channel", channel,
		"reward_title", binding.RewardTitle,
		"action_type", binding.ActionType,
		"success", actionErr == nil)
}

// runAction dispatches the binding's action and returns nil on success or a
// non-nil error on failure (which drives a CANCELED refund when
// auto-fulfilling).
func (e *Executor) runAction(ctx context.Context, channel string, b redemptions.Binding, evt eventsub.RedemptionEvent) error {
	switch b.ActionType {
	case redemptions.ActionNone:
		return nil
	case redemptions.ActionChatMessage:
		if e.chat == nil {
			e.logger.Warn("chat action but no sender configured", "channel", channel, "reward_id", evt.RewardID)
			return errors.New("channelpoints: chat sender unavailable")
		}
		text := expandTemplate(b.ActionParam, evt)
		if text == "" {
			return nil
		}
		return e.chat.Send(ctx, channel, text)
	case redemptions.ActionCounterIncr:
		if e.counters == nil {
			return errors.New("channelpoints: counter admin unavailable")
		}
		_, err := e.counters.Increment(ctx, channel, b.ActionParam)
		return err
	case redemptions.ActionCounterReset:
		if e.counters == nil {
			return errors.New("channelpoints: counter admin unavailable")
		}
		_, err := e.counters.Reset(ctx, channel, b.ActionParam)
		return err
	default:
		e.logger.Error("unknown action type", "channel", channel, "action_type", b.ActionType)
		return errors.New("channelpoints: unknown action type")
	}
}

// settle marks the redemption FULFILLED when the action succeeded or
// CANCELED (refund) when it failed, logging any Twitch-side error.
func (e *Executor) settle(ctx context.Context, channel string, evt eventsub.RedemptionEvent, actionErr error) {
	if actionErr == nil {
		if err := e.fulfiller.FulfillRedemption(ctx, channel, evt.RewardID, evt.ID); err != nil {
			e.logger.Error("fulfill redemption failed", "channel", channel, "redemption_id", evt.ID, "err", err)
		}
		return
	}
	if err := e.fulfiller.CancelRedemption(ctx, channel, evt.RewardID, evt.ID); err != nil {
		e.logger.Error("cancel redemption failed", "channel", channel, "redemption_id", evt.ID, "err", err)
	}
}

// expandTemplate substitutes the supported $placeholders in a chat-message
// template with values from the redemption event, trims the result, and
// caps it at [maxChatMessageLen]. It uses a fixed replacer (no
// arbitrary expression evaluation) to avoid any template-injection risk.
func expandTemplate(tmpl string, evt eventsub.RedemptionEvent) string {
	user := evt.UserName
	if user == "" {
		user = evt.UserLogin
	}
	replacer := strings.NewReplacer(
		"$user", user,
		"$input", evt.UserInput,
		"$reward", evt.RewardTitle,
		"$cost", strconv.Itoa(evt.RewardCost),
	)
	out := strings.TrimSpace(replacer.Replace(tmpl))
	if len(out) > maxChatMessageLen {
		out = out[:maxChatMessageLen]
	}
	return out
}
