package channelpoints

import (
	"context"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch/eventsub"
)

// RedemptionAlert is the overlay-facing view of a redemption, broadcast as the
// data of a [RedemptionEventType] WebSocket envelope. Field names are the JSON
// keys the bundled OBS overlays read, so they are a wire contract and must not
// be renamed once shipped.
type RedemptionAlert struct {
	Username    string `json:"username"`
	RewardTitle string `json:"reward_title"`
	RewardCost  int    `json:"reward_cost"`
	UserInput   string `json:"user_input"`
}

// OverlayNotifier broadcasts a redemption alert to connected overlay clients.
// The cmd-layer adapter over the WebSocket hub satisfies it; this narrow
// interface keeps channelpoints free of any web/ws import. eventType is passed
// so the adapter need not know the constant.
type OverlayNotifier interface {
	Notify(eventType string, alert RedemptionAlert)
}

// Notifier forwards each Channel-Points redemption to the overlay layer so it
// surfaces as an on-stream alert, the third consumer in the redemption fan-out
// beside the binding [Executor] and the rule [Bridge]. Construct via
// [NewNotifier]; a nil [OverlayNotifier] yields a no-op Handle.
type Notifier struct {
	out OverlayNotifier
}

// NewNotifier constructs a [Notifier] forwarding redemptions to out. A nil out
// yields a Notifier whose Handle is a safe no-op, so a deployment without the
// overlay hub wired still boots cleanly.
func NewNotifier(out OverlayNotifier) *Notifier {
	return &Notifier{out: out}
}

// Handle maps one redemption onto a [RedemptionAlert] and broadcasts it. The
// redeeming viewer's display name (falling back to login) becomes the alert
// username. It matches the [eventsub.Config.Handler] signature so it composes
// alongside [Executor.Handle] and [Bridge.Handle]. The ctx argument is unused
// because a hub broadcast is fire-and-forget, but it keeps the Handler shape.
func (n *Notifier) Handle(_ context.Context, evt eventsub.RedemptionEvent) {
	if n.out == nil {
		return
	}
	username := evt.UserName
	if username == "" {
		username = evt.UserLogin
	}
	n.out.Notify(RedemptionEventType, RedemptionAlert{
		Username:    strings.TrimSpace(username),
		RewardTitle: evt.RewardTitle,
		RewardCost:  evt.RewardCost,
		UserInput:   evt.UserInput,
	})
}
