package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/songrequests"
)

// SongRequests exposes per-channel song-request configuration (provider
// choice, Spotify playlist, max track duration) over HTTP for the dashboard.
// All endpoints are session-protected at the router layer. When the store is
// nil every endpoint returns 501 so the router still boots with the feature
// off.
type SongRequests struct {
	store    songrequests.Store
	tenantID string
	logger   *slog.Logger
}

// NewSongRequests constructs the handler bundle.
func NewSongRequests(store songrequests.Store, tenantID string, logger *slog.Logger) *SongRequests {
	if logger == nil {
		logger = slog.Default()
	}
	return &SongRequests{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// Get handles GET /api/v1/songrequests?channel=...
// Without a channel it lists every configured channel for the tenant; with a
// channel it returns that channel's config (or the disabled default).
func (h *SongRequests) Get(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		configs, err := h.store.List(r.Context(), h.tenantID)
		if err != nil {
			h.writeStoreError(w, r, "songrequests list failed", err)
			return
		}
		out := make([]map[string]any, 0, len(configs))
		for _, c := range configs {
			out = append(out, songRequestJSON(c))
		}
		writeJSON(w, http.StatusOK, map[string]any{"configs": out})
		return
	}
	c, err := h.store.GetOrDefault(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "songrequests get failed", err)
		return
	}
	writeJSON(w, http.StatusOK, songRequestJSON(c))
}

// Set handles PUT /api/v1/songrequests.
// Body: {channel, provider, spotify_playlist_id, max_duration_sec, enabled}.
func (h *SongRequests) Set(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	var req struct {
		Channel           string `json:"channel"`
		Provider          string `json:"provider"`
		SpotifyPlaylistID string `json:"spotify_playlist_id"`
		MaxDurationSec    int    `json:"max_duration_sec"`
		Enabled           *bool  `json:"enabled"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	saved, err := h.store.Set(r.Context(), songrequests.Config{
		TenantID:          h.tenantID,
		Channel:           channel,
		Provider:          req.Provider,
		SpotifyPlaylistID: req.SpotifyPlaylistID,
		MaxDurationSec:    req.MaxDurationSec,
		Enabled:           enabled,
	})
	if err != nil {
		h.writeStoreError(w, r, "songrequests set failed", err)
		return
	}
	writeJSON(w, http.StatusOK, songRequestJSON(saved))
}

// writeStoreError maps store sentinel errors to HTTP status codes.
func (h *SongRequests) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, songrequests.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func (h *SongRequests) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "songrequests_not_enabled"})
}

// songRequestJSON renders a Config into the wire shape with an RFC3339
// timestamp.
func songRequestJSON(c songrequests.Config) map[string]any {
	return map[string]any{
		"channel":             c.Channel,
		"provider":            c.Provider,
		"spotify_playlist_id": c.SpotifyPlaylistID,
		"max_duration_sec":    c.MaxDurationSec,
		"enabled":             c.Enabled,
		"updated_at":          c.UpdatedAt.Format(time.RFC3339),
	}
}
