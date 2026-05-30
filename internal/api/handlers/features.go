package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/featureflags"
)

// Features exposes the per-channel feature-flag overrides (currently the points
// "economy" toggle) over HTTP for the dashboard. All endpoints are
// session-protected at the router layer. Flags are keyed by the lower-cased
// channel login so dashboard edits match the chat engine's lookup key.
type Features struct {
	store    featureflags.Store
	tenantID string
	logger   *slog.Logger
}

// NewFeatures constructs the Features handler bundle. When store is nil every
// endpoint short-circuits to 501 so the router boots without the feature.
func NewFeatures(store featureflags.Store, tenantID string, logger *slog.Logger) *Features {
	if logger == nil {
		logger = slog.Default()
	}
	return &Features{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// List handles GET /api/v1/features?channel=... returning all explicit flag
// overrides for the channel.
func (h *Features) List(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	flags, err := h.store.List(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "features list failed", err)
		return
	}
	out := make([]map[string]any, 0, len(flags))
	for _, f := range flags {
		out = append(out, featureJSON(f))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel": channel,
		"flags":   out,
	})
}

// Set handles PUT /api/v1/features/{feature}.
// Body: {channel, enabled}. feature comes from the path, not the body.
func (h *Features) Set(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	feature := strings.TrimSpace(chi.URLParam(r, "feature"))
	if feature == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "feature is required"})
		return
	}
	var req struct {
		Channel string `json:"channel"`
		Enabled *bool  `json:"enabled"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if req.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled is required"})
		return
	}
	if err := h.store.Set(r.Context(), h.tenantID, channel, feature, *req.Enabled); err != nil {
		h.writeStoreError(w, r, "features set failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel": channel,
		"feature": strings.ToLower(feature),
		"enabled": *req.Enabled,
	})
}

// writeStoreError maps store sentinel errors to HTTP status codes. ErrInvalid
// and anything else map to 400; featureflags has no not-found sentinel because
// an absent flag is a normal state surfaced through List.
func (h *Features) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, featureflags.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func (h *Features) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func (h *Features) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "features_not_enabled"})
}

// featureJSON renders a Flag into the wire shape, with an RFC3339 timestamp.
func featureJSON(f featureflags.Flag) map[string]any {
	return map[string]any{
		"feature":    f.Feature,
		"enabled":    f.Enabled,
		"updated_at": f.UpdatedAt.Format(time.RFC3339),
	}
}
