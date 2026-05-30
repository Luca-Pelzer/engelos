package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/wrapped"
)

// WrappedRanker supplies the loyalty + streak extras a Wrapped card shows
// beyond the raw message/sub/raid counters. main wires a thin adapter over the
// loyalty store and streak read-model; defining it here keeps this handler
// decoupled from those packages. A nil ranker simply omits those fields.
type WrappedRanker interface {
	// Points returns the viewer's loyalty balance for the channel.
	Points(ctx context.Context, channel, viewerID string) int64
	// LongestStreak returns the viewer's all-time longest streak (days).
	LongestStreak(ctx context.Context, channel, viewerID string) int
}

// Wrapped serves Spotify-Wrapped-style recap cards from the accumulated
// per-viewer stats. A viewer card (channel+viewer) shows that viewer's
// numbers and rank; a channel card (channel only) shows channel-wide totals
// and the top chatters. When the store is nil every endpoint returns 501.
type Wrapped struct {
	store    wrapped.Store
	ranker   WrappedRanker
	tenantID string
	logger   *slog.Logger
}

// NewWrapped constructs the handler. ranker may be nil (loyalty/streak extras
// are then omitted from viewer cards).
func NewWrapped(store wrapped.Store, ranker WrappedRanker, tenantID string, logger *slog.Logger) *Wrapped {
	if logger == nil {
		logger = slog.Default()
	}
	return &Wrapped{store: store, ranker: ranker, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// Get handles GET /api/v1/wrapped?channel=X[&viewer=Y][&period=all|YYYY-MM].
// With a viewer it returns that viewer's card; without, the channel card.
func (h *Wrapped) Get(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "wrapped_not_enabled"})
		return
	}
	q := r.URL.Query()
	channel := normChannel(q.Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	period := strings.TrimSpace(q.Get("period"))
	if period == "" {
		period = "all"
	}
	viewer := strings.TrimSpace(q.Get("viewer"))
	if viewer == "" {
		h.channelCard(w, r, channel, period)
		return
	}
	h.viewerCard(w, r, channel, period, viewer)
}

func (h *Wrapped) viewerCard(w http.ResponseWriter, r *http.Request, channel, period, viewer string) {
	ctx := r.Context()
	stat, err := h.store.ViewerStat(ctx, h.tenantID, channel, viewer, period)
	if err != nil {
		if errors.Is(err, wrapped.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no_wrapped_data"})
			return
		}
		if errors.Is(err, wrapped.ErrInvalid) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		h.logger.WarnContext(ctx, "wrapped: viewer stat failed", "channel", channel, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	rank, percentile := h.rankOf(ctx, channel, period, viewer)
	out := map[string]any{
		"kind":       "viewer",
		"channel":    channel,
		"period":     period,
		"viewer":     stat.ViewerID,
		"username":   stat.Username,
		"messages":   stat.Messages,
		"subs":       stat.SubsTotal,
		"sub_gifts":  stat.SubsGiven,
		"rank":       rank,
		"percentile": percentile,
	}
	if h.ranker != nil {
		out["points"] = h.ranker.Points(ctx, channel, viewer)
		out["longest_streak"] = h.ranker.LongestStreak(ctx, channel, viewer)
	}
	writeJSON(w, http.StatusOK, out)
}

// rankOf computes the viewer's 1-based rank by message count and their top
// percentile (e.g. 3 = "top 3%"). It scans the channel's top chatters; a
// viewer outside the top 100 reports rank 0 (unranked).
func (h *Wrapped) rankOf(ctx context.Context, channel, period, viewer string) (int, int) {
	top, err := h.store.TopChatters(ctx, h.tenantID, channel, period, 100)
	if err != nil || len(top) == 0 {
		return 0, 0
	}
	summary, _ := h.store.ChannelTotals(ctx, h.tenantID, channel, period)
	total := summary.TotalViewers
	for i, s := range top {
		if s.ViewerID == viewer {
			rank := i + 1
			pct := 0
			if total > 0 {
				pct = int(float64(rank) / float64(total) * 100)
				if pct < 1 {
					pct = 1
				}
			}
			return rank, pct
		}
	}
	return 0, 0
}

func (h *Wrapped) channelCard(w http.ResponseWriter, r *http.Request, channel, period string) {
	ctx := r.Context()
	summary, err := h.store.ChannelTotals(ctx, h.tenantID, channel, period)
	if err != nil {
		if errors.Is(err, wrapped.ErrInvalid) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		h.logger.WarnContext(ctx, "wrapped: channel totals failed", "channel", channel, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	top, err := h.store.TopChatters(ctx, h.tenantID, channel, period, 5)
	if err != nil {
		h.logger.WarnContext(ctx, "wrapped: top chatters failed", "channel", channel, "err", err)
		top = nil
	}
	chatters := make([]map[string]any, 0, len(top))
	for _, s := range top {
		chatters = append(chatters, map[string]any{
			"username": s.Username,
			"messages": s.Messages,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":           "channel",
		"channel":        channel,
		"period":         period,
		"total_messages": summary.TotalMessages,
		"total_subs":     summary.TotalSubs,
		"total_raids":    summary.TotalRaids,
		"total_viewers":  summary.TotalViewers,
		"top_chatters":   chatters,
	})
}
