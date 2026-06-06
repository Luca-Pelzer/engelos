package channelpoints

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch/eventsub"
)

// RedemptionEventType is the stable Action-Engine event-type identifier a
// Channel-Points redemption fires under. A rule whose event trigger filters on
// this value runs its full action list (chat, OBS, delay, ...) for every
// redemption, complementing the fixed binding executor. The string is a
// persisted wire identifier shared with the dashboard and rule store, so it
// must never change once shipped.
const RedemptionEventType = "channel.points.redemption"

// RuleEngine fires an Action-Engine event trigger. The cmd-layer adapter over
// actions.Engine satisfies it; this narrow interface keeps channelpoints free
// of any internal/actions import. data carries the redemption-specific fields
// rule variables read via $(reward_title), $(reward_cost), $(user_input), etc.
type RuleEngine interface {
	FireEvent(platform, channel, eventType, userID, username string, data map[string]any)
}

// Bridge forwards each Channel-Points redemption into the Action-Engine as an
// event trigger, so dashboard rules can react to redemptions with the whole
// plugin catalog rather than only the fixed binding actions. It is one half of
// the redemption fan-out: the binding [Executor] still runs in parallel and
// owns auto-fulfilment, while the Bridge owns the rule path. Construct via
// [NewBridge].
type Bridge struct {
	engine RuleEngine
	logger *slog.Logger
}

// NewBridge constructs a [Bridge] forwarding redemptions to engine. A nil
// engine yields a Bridge whose Handle is a safe no-op, so a deployment without
// the Action-Engine wired still boots cleanly. A nil logger defaults to
// slog.Default.
func NewBridge(engine RuleEngine, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{
		engine: engine,
		logger: logger.With("component", "channelpoints.bridge"),
	}
}

// Handle maps one redemption event onto an Action-Engine event trigger and
// fires it. The redeeming viewer becomes the trigger user (so $(user) and
// $(userid) resolve), the broadcaster login becomes the channel, and the reward
// fields are exposed in the data map under snake_case keys for variable
// substitution. It matches the [eventsub.Config.Handler] signature so it can be
// composed alongside [Executor.Handle]. A redemption missing its broadcaster
// login is dropped, mirroring the executor, because the engine keys rules by
// channel.
func (b *Bridge) Handle(ctx context.Context, evt eventsub.RedemptionEvent) {
	if b.engine == nil {
		return
	}
	channel := strings.ToLower(strings.TrimSpace(evt.BroadcasterUserLogin))
	if channel == "" {
		b.logger.Warn("redemption missing broadcaster login; dropping", "reward_id", evt.RewardID)
		return
	}

	username := evt.UserName
	if username == "" {
		username = evt.UserLogin
	}

	data := map[string]any{
		"reward_id":     evt.RewardID,
		"reward_title":  evt.RewardTitle,
		"reward_cost":   strconv.Itoa(evt.RewardCost),
		"user_input":    evt.UserInput,
		"user_login":    evt.UserLogin,
		"redemption_id": evt.ID,
		"status":        evt.Status,
	}

	b.engine.FireEvent("twitch", channel, RedemptionEventType, evt.UserID, username, data)
}
