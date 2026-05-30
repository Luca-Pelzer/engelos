package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/songrequests/queue"
)

// SongQueue exposes the bot-managed YouTube song queue to the
// /overlay/song-player browser source. Its single endpoint advances the queue:
// it is called by the player when it is ready for the next song. When the store
// is nil every endpoint returns 501 so the router still boots with the feature
// off.
//
// Unlike the dashboard APIs this endpoint is NOT session-protected: the OBS
// browser source has no login, and the only capability it grants is "play the
// next public song request", which is not sensitive.
type SongQueue struct {
	store    queue.Store
	tenantID string
	logger   *slog.Logger
}

// NewSongQueue constructs the handler.
func NewSongQueue(store queue.Store, tenantID string, logger *slog.Logger) *SongQueue {
	if logger == nil {
		logger = slog.Default()
	}
	return &SongQueue{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// Next handles GET /api/v1/songqueue/next?channel=...
// It marks the currently-playing item played (if any), promotes the oldest
// queued item to playing, and returns it. 204 No Content when the queue is
// empty so the player idles and polls again.
func (h *SongQueue) Next(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "songqueue_not_enabled"})
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	ctx := r.Context()

	// Retire the current song so it is not replayed. ErrEmpty is fine: the
	// player may be fetching its very first song.
	if cur, err := h.store.Current(ctx, h.tenantID, channel); err == nil {
		if mErr := h.store.MarkPlayed(ctx, h.tenantID, channel, cur.ID); mErr != nil {
			h.logger.WarnContext(ctx, "songqueue: mark played failed", "channel", channel, "err", mErr)
		}
	} else if !errors.Is(err, queue.ErrEmpty) {
		h.logger.WarnContext(ctx, "songqueue: current lookup failed", "channel", channel, "err", err)
	}

	next, err := h.store.Next(ctx, h.tenantID, channel)
	if err != nil {
		if errors.Is(err, queue.ErrEmpty) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.logger.WarnContext(ctx, "songqueue: next failed", "channel", channel, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"video_id":     next.VideoID,
		"title":        next.Title,
		"artist":       next.Artist,
		"duration_ms":  next.DurationMS,
		"requested_by": next.RequestedBy,
	})
}
