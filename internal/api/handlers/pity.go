package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
)

// Pity exposes the pity feature over HTTP. All endpoints are session-protected
// at the router layer; Pity itself trusts the user injected by SessionAuth.
type Pity struct {
	system   *pity.System
	tenantID string
	logger   *slog.Logger
}

// NewPity constructs the Pity handler bundle. When system is nil every
// endpoint short-circuits to 501 so the router boots without the feature.
func NewPity(system *pity.System, tenantID string, logger *slog.Logger) *Pity {
	if logger == nil {
		logger = slog.Default()
	}
	return &Pity{system: system, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// pityViewerRequest is the shared input shape across endpoints. Channel and
// ViewerID are required; Username is optional but recommended for analytics.
type pityViewerRequest struct {
	Channel  string `json:"channel"`
	ViewerID string `json:"viewer_id"`
	Username string `json:"username,omitempty"`
}

func (r pityViewerRequest) validate() error {
	if strings.TrimSpace(r.Channel) == "" {
		return errors.New("channel is required")
	}
	if strings.TrimSpace(r.ViewerID) == "" {
		return errors.New("viewer_id is required")
	}
	return nil
}

// Grant handles POST /api/v1/pity/grant.
// Body: {channel, viewer_id, username?, amount?, reason?}.
// amount defaults to Config.PointsPerMessage when zero or missing.
func (p *Pity) Grant(w http.ResponseWriter, r *http.Request) {
	if p.system == nil {
		p.notImplemented(w)
		return
	}
	var req struct {
		pityViewerRequest
		Amount int    `json:"amount"`
		Reason string `json:"reason"`
	}
	if !p.decode(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	amount := req.Amount
	if amount <= 0 {
		amount = p.system.Config().PointsPerMessage
	}
	reason := req.Reason
	if reason == "" {
		reason = "api"
	}
	total, err := p.system.GrantPoints(r.Context(), p.tenantID,
		req.Channel, req.ViewerID, req.Username, reason, amount)
	if err != nil {
		p.logger.WarnContext(r.Context(), "pity grant failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel":   req.Channel,
		"viewer_id": req.ViewerID,
		"granted":   amount,
		"total":     total,
	})
}

// Roll handles POST /api/v1/pity/roll.
// Body: {channel, viewer_id, username?}.
func (p *Pity) Roll(w http.ResponseWriter, r *http.Request) {
	if p.system == nil {
		p.notImplemented(w)
		return
	}
	var req pityViewerRequest
	if !p.decode(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result, err := p.system.Roll(r.Context(), p.tenantID,
		req.Channel, req.ViewerID, req.Username)
	if err != nil {
		p.logger.WarnContext(r.Context(), "pity roll failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"won":              result.Won,
		"was_guaranteed":   result.WasGuaranteed,
		"points_before":    result.PointsBefore,
		"points_after":     result.PointsAfter,
		"effective_chance": result.EffectiveChance,
	})
}

// Status handles GET /api/v1/pity/status?channel=...&viewer_id=...
func (p *Pity) Status(w http.ResponseWriter, r *http.Request) {
	if p.system == nil {
		p.notImplemented(w)
		return
	}
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	viewerID := strings.TrimSpace(r.URL.Query().Get("viewer_id"))
	if channel == "" || viewerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "channel and viewer_id query params are required",
		})
		return
	}
	status := p.system.Status(p.tenantID, channel, viewerID)
	cfg := p.system.Config()
	writeJSON(w, http.StatusOK, map[string]any{
		"channel":             channel,
		"viewer_id":           viewerID,
		"points":              status.Points,
		"soft_pity_hit":       status.SoftPityHit,
		"near_guaranteed":     status.NearGuaranteed,
		"effective_chance":    status.EffectiveChance,
		"hard_pity_threshold": cfg.HardPityThreshold,
		"soft_pity_fraction":  cfg.SoftPityFraction,
	})
}

// Leaderboard handles GET /api/v1/pity/leaderboard?channel=...&limit=10.
// Empty channel returns all channels in the tenant.
func (p *Pity) Leaderboard(w http.ResponseWriter, r *http.Request) {
	if p.system == nil {
		p.notImplemented(w)
		return
	}
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	limitStr := strings.TrimSpace(r.URL.Query().Get("limit"))
	limit := 10
	if limitStr != "" {
		n, err := strconv.Atoi(limitStr)
		if err != nil || n <= 0 || n > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "limit must be an integer between 1 and 100",
			})
			return
		}
		limit = n
	}
	rows := p.system.Leaderboard(p.tenantID, channel, limit)
	out := make([]map[string]any, len(rows))
	for i, e := range rows {
		out[i] = map[string]any{
			"channel":   e.Channel,
			"viewer_id": e.ViewerID,
			"username":  e.Username,
			"points":    e.Points,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel": channel,
		"limit":   limit,
		"entries": out,
	})
}

// Reset handles POST /api/v1/pity/reset.
// Body: {channel, viewer_id, reason?}.
// The injected user (via SessionAuth) is recorded as the moderator.
func (p *Pity) Reset(w http.ResponseWriter, r *http.Request) {
	if p.system == nil {
		p.notImplemented(w)
		return
	}
	var req struct {
		pityViewerRequest
		Reason string `json:"reason"`
	}
	if !p.decode(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	reason := req.Reason
	if reason == "" {
		reason = "admin"
		if u, ok := middleware.UserFromContext(r.Context()); ok && u.Username != "" {
			reason = "admin:" + u.Username
		}
	}
	if err := p.system.Reset(r.Context(), p.tenantID,
		req.Channel, req.ViewerID, reason); err != nil {
		p.logger.WarnContext(r.Context(), "pity reset failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Pity) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func (p *Pity) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "pity_not_enabled"})
}
