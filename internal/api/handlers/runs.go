package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

const (
	runsDefaultLimit = 50
	runsMaxLimit     = 200
)

// RunSource is the read/delete surface the run-history endpoints need. The
// actions/runs.Store satisfies it. A nil source makes every endpoint 501.
type RunSource interface {
	ListRuns(ctx context.Context, tenantID, channel, ruleName string, limit int) ([]actions.RunTrace, error)
	GetRun(ctx context.Context, tenantID, channel, ruleName, runID string) (actions.RunTrace, error)
	DeleteRuns(ctx context.Context, tenantID, channel, ruleName string) (int, error)
}

// Runs serves the per-rule workflow run history. It is channel-scoped by the
// resolved workspace slug and owner-gated at the router layer.
type Runs struct {
	source   RunSource
	tenantID string
	logger   *slog.Logger
}

// NewRuns constructs the Runs handler. A nil source short-circuits every
// endpoint to 501 so the router boots without run recording configured.
func NewRuns(source RunSource, tenantID string, logger *slog.Logger) *Runs {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runs{source: source, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// List handles GET /api/v1/channels/{channelSlug}/actions/{name}/runs?limit=50,
// returning the newest-first run headers (no node entries) for the rule.
func (h *Runs) List(w http.ResponseWriter, r *http.Request) {
	if h.source == nil {
		notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	name := chi.URLParam(r, "name")
	if channel == "" || strings.TrimSpace(name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel and name are required"})
		return
	}
	list, err := h.source.ListRuns(r.Context(), h.tenantID, channel, name, parseRunLimit(r.URL.Query().Get("limit")))
	if err != nil {
		h.logger.Error("runs list failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": channel, "rule": name, "runs": list})
}

// Detail handles GET …/actions/{name}/runs/{runID}, returning the run header
// plus its ordered condition/action node entries.
func (h *Runs) Detail(w http.ResponseWriter, r *http.Request) {
	if h.source == nil {
		notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	name := chi.URLParam(r, "name")
	runID := chi.URLParam(r, "runID")
	if channel == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(runID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel, name and run id are required"})
		return
	}
	run, err := h.source.GetRun(r.Context(), h.tenantID, channel, name, runID)
	if errors.Is(err, actions.ErrRunNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	if err != nil {
		h.logger.Error("runs detail failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// Delete handles DELETE …/actions/{name}/runs, clearing the rule's history.
func (h *Runs) Delete(w http.ResponseWriter, r *http.Request) {
	if h.source == nil {
		notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	name := chi.URLParam(r, "name")
	if channel == "" || strings.TrimSpace(name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel and name are required"})
		return
	}
	n, err := h.source.DeleteRuns(r.Context(), h.tenantID, channel, name)
	if err != nil {
		h.logger.Error("runs delete failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}

// parseRunLimit clamps the ?limit query to (0, runsMaxLimit], defaulting to
// runsDefaultLimit for a missing or invalid value.
func parseRunLimit(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return runsDefaultLimit
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return runsDefaultLimit
	}
	if n > runsMaxLimit {
		return runsMaxLimit
	}
	return n
}
