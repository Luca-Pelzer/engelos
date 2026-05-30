package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/automod"
	"github.com/Luca-Pelzer/engelos/internal/moderation"
)

// AutoMod exposes the chat-moderation engine over HTTP: read/replace the filter
// configuration and read the enforcement audit log. All endpoints are
// session-protected at the router layer. A nil service makes every endpoint
// return 501 so the router boots without the feature.
type AutoMod struct {
	svc    *moderation.Service
	logger *slog.Logger
}

// NewAutoMod constructs the AutoMod handler bundle.
func NewAutoMod(svc *moderation.Service, logger *slog.Logger) *AutoMod {
	if logger == nil {
		logger = slog.Default()
	}
	return &AutoMod{svc: svc, logger: logger}
}

// GetConfig handles GET /api/v1/automod/config, returning the live filter
// configuration as JSON.
func (a *AutoMod) GetConfig(w http.ResponseWriter, r *http.Request) {
	if a.svc == nil {
		a.notImplemented(w)
		return
	}
	writeJSON(w, http.StatusOK, a.svc.Config())
}

// PutConfig handles PUT /api/v1/automod/config, replacing the filter
// configuration wholesale. A bad banned-word regex yields 400.
func (a *AutoMod) PutConfig(w http.ResponseWriter, r *http.Request) {
	if a.svc == nil {
		a.notImplemented(w)
		return
	}
	var cfg automod.Config
	dec := json.NewDecoder(io.LimitReader(r.Body, 256*1024))
	if err := dec.Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if err := a.svc.SetConfig(cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	a.logger.InfoContext(r.Context(), "automod config updated", slog.Int("mode", int(cfg.Mode)))
	writeJSON(w, http.StatusOK, a.svc.Config())
}

// Audit handles GET /api/v1/automod/audit?channel=...&limit=100, returning the
// most recent enforcement actions for a channel, newest first.
func (a *AutoMod) Audit(w http.ResponseWriter, r *http.Request) {
	if a.svc == nil {
		a.notImplemented(w)
		return
	}
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel query param is required"})
		return
	}
	limit := 100
	if ls := strings.TrimSpace(r.URL.Query().Get("limit")); ls != "" {
		n, err := strconv.Atoi(ls)
		if err != nil || n <= 0 || n > 500 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be an integer between 1 and 500"})
			return
		}
		limit = n
	}
	rows, err := a.svc.AuditList(r.Context(), channel, limit)
	if err != nil {
		a.logger.WarnContext(r.Context(), "automod audit list failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "audit_unavailable"})
		return
	}
	out := make([]map[string]any, len(rows))
	for i, e := range rows {
		out[i] = map[string]any{
			"id":           e.ID,
			"channel":      e.Channel,
			"username":     e.Username,
			"message_text": e.MessageText,
			"filter_name":  e.FilterName,
			"reason":       e.Reason,
			"matched_text": e.MatchedText,
			"action":       e.Action,
			"duration_sec": e.DurationSec,
			"dry_run":      e.DryRun,
			"created_at":   e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel": channel,
		"limit":   limit,
		"actions": out,
	})
}

func (a *AutoMod) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "automod_not_enabled"})
}
