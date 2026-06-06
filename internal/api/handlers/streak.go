package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/features/streak"
)

// Streak exposes the streak feature over HTTP. All endpoints are protected
// by SessionAuth at the router layer; Streak itself trusts the caller.
type Streak struct {
	system   *streak.System
	tenantID string
	logger   *slog.Logger
}

// NewStreak constructs the Streak handler bundle. When system is nil every
// endpoint short-circuits to 501 so the router boots without the feature.
func NewStreak(system *streak.System, tenantID string, logger *slog.Logger) *Streak {
	if logger == nil {
		logger = slog.Default()
	}
	return &Streak{
		system:   system,
		tenantID: strings.TrimSpace(tenantID),
		logger:   logger,
	}
}

type streakViewerRequest struct {
	Channel  string `json:"channel"`
	ViewerID string `json:"viewer_id"`
	Username string `json:"username,omitempty"`
}

func (r streakViewerRequest) validate() error {
	if strings.TrimSpace(r.Channel) == "" {
		return errors.New("channel is required")
	}
	if strings.TrimSpace(r.ViewerID) == "" {
		return errors.New("viewer_id is required")
	}
	return nil
}

// Tick handles POST /api/v1/streak/tick.
// Body: {channel, viewer_id, username?}.
func (s *Streak) Tick(w http.ResponseWriter, r *http.Request) {
	if s.system == nil {
		s.notImplemented(w)
		return
	}
	var req streakViewerRequest
	if !s.decode(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	res, err := s.system.Tick(r.Context(), s.tenantID,
		req.Channel, req.ViewerID, req.Username)
	if err != nil {
		s.logger.WarnContext(r.Context(), "streak tick failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resultToMap(res))
}

// UseFreeze handles POST /api/v1/streak/freeze.
// Body: {channel, viewer_id, username?}.
func (s *Streak) UseFreeze(w http.ResponseWriter, r *http.Request) {
	if s.system == nil {
		s.notImplemented(w)
		return
	}
	var req streakViewerRequest
	if !s.decode(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	res, err := s.system.UseFreeze(r.Context(), s.tenantID,
		req.Channel, req.ViewerID, req.Username)
	if err != nil {
		s.logger.WarnContext(r.Context(), "streak useFreeze failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resultToMap(res))
}

// Status handles GET /api/v1/streak/status?channel=...&viewer_id=...
func (s *Streak) Status(w http.ResponseWriter, r *http.Request) {
	if s.system == nil {
		s.notImplemented(w)
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
	st := s.system.Status(s.tenantID, channel, viewerID)
	writeJSON(w, http.StatusOK, map[string]any{
		"channel":           channel,
		"viewer_id":         viewerID,
		"days_current":      st.DaysCurrent,
		"days_longest":      st.DaysLongest,
		"freezes_available": st.FreezesAvailable,
		"last_tick_at":      st.LastTickAt,
		"next_milestone":    st.NextMilestone,
	})
}

// Leaderboard handles GET /api/v1/streak/leaderboard?channel=...&limit=10.
// Empty channel returns all channels in the tenant.
func (s *Streak) Leaderboard(w http.ResponseWriter, r *http.Request) {
	if s.system == nil {
		s.notImplemented(w)
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
	rows := s.system.Leaderboard(s.tenantID, channel, limit)
	out := make([]map[string]any, len(rows))
	for i, e := range rows {
		out[i] = map[string]any{
			"channel":      e.Channel,
			"viewer_id":    e.ViewerID,
			"username":     e.Username,
			"days_current": e.DaysCurrent,
			"days_longest": e.DaysLongest,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel": channel,
		"limit":   limit,
		"entries": out,
	})
}

// Reset handles POST /api/v1/streak/reset. Body: {channel, viewer_id, reason?}.
func (s *Streak) Reset(w http.ResponseWriter, r *http.Request) {
	if s.system == nil {
		s.notImplemented(w)
		return
	}
	var req struct {
		streakViewerRequest
		Reason string `json:"reason"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	reason := req.Reason
	if reason == "" {
		reason = "admin"
	}
	if err := s.system.Reset(r.Context(), s.tenantID,
		req.Channel, req.ViewerID, reason); err != nil {
		s.logger.WarnContext(r.Context(), "streak reset failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func resultToMap(res streak.Result) map[string]any {
	return map[string]any{
		"days_current":      res.DaysCurrent,
		"days_longest":      res.DaysLongest,
		"freezes_available": res.FreezesAvailable,
		"milestone":         res.Milestone,
		"same_day_retick":   res.SameDayReTick,
		"used_freezes":      res.UsedFreezes,
		"broken_from_days":  res.BrokenFromDays,
	}
}

func (s *Streak) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func (s *Streak) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "streak_not_enabled"})
}
