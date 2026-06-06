package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/Luca-Pelzer/engelos/internal/moments"
)

// momentBroadcaster is the narrow overlay-push surface the moment adapter
// needs; runtime.WSBroadcaster satisfies it. Declared here so the adapter
// stays free of a concrete websocket dependency.
type momentBroadcaster interface {
	Broadcast(eventType string, payload any)
}

// momentController adapts the moments.Store to commands.MomentController. It
// maps the store's sentinel errors onto the command-facing MomentOutcome enum
// (so internal/commands never imports the moments package) and broadcasts the
// moment.opened / moment.closed overlay alerts on the WS hub. Broadcasting is
// best-effort: a nil broadcaster simply skips the overlay push.
type momentController struct {
	store    moments.Store
	bc       momentBroadcaster
	tenantID string
	now      func() time.Time
	logger   *slog.Logger
}

func newMomentController(store moments.Store, bc momentBroadcaster, tenantID string, logger *slog.Logger) momentController {
	return momentController{store: store, bc: bc, tenantID: tenantID, now: time.Now, logger: logger}
}

func (c momentController) Open(ctx context.Context, channel, title, openedBy string, window time.Duration) commands.MomentOutcome {
	if _, err := c.store.Open(ctx, c.tenantID, channel, title, openedBy, window); err != nil {
		switch {
		case errors.Is(err, moments.ErrActiveExists):
			return commands.MomentActiveExists
		case errors.Is(err, moments.ErrInvalid):
			return commands.MomentInvalid
		default:
			c.logger.WarnContext(ctx, "moment: open failed", "channel", channel, "err", err)
			return commands.MomentUnavailable
		}
	}
	if c.bc != nil {
		c.bc.Broadcast("moment.opened", map[string]any{
			"title":      title,
			"window_sec": int(window.Seconds()),
		})
	}
	return commands.MomentOK
}

func (c momentController) Join(ctx context.Context, channel, viewerID, username string) (commands.MomentResult, commands.MomentOutcome) {
	count, err := c.store.Join(ctx, c.tenantID, channel, viewerID, username, c.now())
	if err != nil {
		switch {
		case errors.Is(err, moments.ErrNoActive):
			return commands.MomentResult{}, commands.MomentNone
		case errors.Is(err, moments.ErrClosed):
			return commands.MomentResult{}, commands.MomentClosedWindow
		case errors.Is(err, moments.ErrAlreadyJoined):
			return commands.MomentResult{}, commands.MomentAlreadyJoined
		default:
			c.logger.WarnContext(ctx, "moment: join failed", "channel", channel, "err", err)
			return commands.MomentResult{}, commands.MomentUnavailable
		}
	}
	return commands.MomentResult{Participants: count}, commands.MomentOK
}

func (c momentController) End(ctx context.Context, channel string) (commands.MomentResult, commands.MomentOutcome) {
	m, err := c.store.End(ctx, c.tenantID, channel, c.now())
	if err != nil {
		if errors.Is(err, moments.ErrNoActive) {
			return commands.MomentResult{}, commands.MomentNone
		}
		c.logger.WarnContext(ctx, "moment: end failed", "channel", channel, "err", err)
		return commands.MomentResult{}, commands.MomentUnavailable
	}
	res := commands.MomentResult{Title: m.Title, Rarity: string(m.Rarity), Participants: m.Participants}
	if c.bc != nil {
		c.bc.Broadcast("moment.closed", map[string]any{
			"title":        m.Title,
			"rarity":       string(m.Rarity),
			"participants": m.Participants,
		})
	}
	return res, commands.MomentOK
}

// History renders the most recent closed moments as a compact one-line
// summary like "GG (legendary, 73), close call (common, 4)".
func (c momentController) History(ctx context.Context, channel string, limit int) (string, commands.MomentOutcome) {
	list, err := c.store.History(ctx, c.tenantID, channel, limit)
	if err != nil {
		c.logger.WarnContext(ctx, "moment: history failed", "channel", channel, "err", err)
		return "", commands.MomentUnavailable
	}
	parts := make([]string, 0, len(list))
	for _, m := range list {
		parts = append(parts, fmt.Sprintf("%s (%s, %d)", m.Title, m.Rarity, m.Participants))
	}
	return strings.Join(parts, ", "), commands.MomentOK
}
