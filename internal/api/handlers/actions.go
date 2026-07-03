package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

// RuleRunner runs a single already-loaded rule once, applying its conditions and
// actions. *actions.Engine satisfies it; the manual fire endpoint uses it to
// test-fire a named rule regardless of its trigger kind.
type RuleRunner interface {
	RunRule(rule actions.Rule, t actions.Trigger)
}

// SchedulerReloader re-arms the timer scheduler after a rule mutation so a timer
// create/update/delete takes effect without a restart. *actions.Scheduler
// satisfies it; a nil reloader means the feature runs without live re-arming.
type SchedulerReloader interface {
	Reload(ctx context.Context) error
}

// Actions exposes CRUD over the per-channel Action-Engine rule store plus a
// read-only catalog of the registered condition and action plugin types so the
// dashboard can render the rule builder. Endpoints are session-protected at the
// router layer and channel-scoped by the lower-cased channel login.
type Actions struct {
	store     actions.Store
	registry  *actions.Registry
	runner    RuleRunner
	scheduler SchedulerReloader
	tenantID  string
	logger    *slog.Logger
}

// NewActions constructs the Actions handler. When store is nil every endpoint
// short-circuits to 501 so the router boots without the feature. runner backs
// the manual fire endpoint and scheduler, when non-nil, is re-armed after each
// rule mutation; both may be nil (fire then degrades to 501, reload is skipped).
func NewActions(store actions.Store, registry *actions.Registry, runner RuleRunner, scheduler SchedulerReloader, tenantID string, logger *slog.Logger) *Actions {
	if logger == nil {
		logger = slog.Default()
	}
	return &Actions{
		store:     store,
		registry:  registry,
		runner:    runner,
		scheduler: scheduler,
		tenantID:  strings.TrimSpace(tenantID),
		logger:    logger,
	}
}

// Catalog handles GET /api/v1/actions/catalog, returning the registered
// condition and action plugin definitions for the rule builder.
func (h *Actions) Catalog(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		notImplemented(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conditions": h.registry.Conditions(),
		"actions":    h.registry.Actions(),
		"triggers":   h.registry.Triggers(),
	})
}

// List handles GET /api/v1/actions?channel=...
func (h *Actions) List(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	list, err := h.store.List(r.Context(), h.tenantID, channel)
	if err != nil {
		h.logger.Error("actions list failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, ru := range list {
		out = append(out, ruleJSON(ru))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": channel, "rules": out})
}

type ruleWriteRequest struct {
	Channel       string                `json:"channel"`
	Name          string                `json:"name"`
	Enabled       bool                  `json:"enabled"`
	TriggerKind   string                `json:"trigger_kind"`
	TriggerFilter json.RawMessage       `json:"trigger_filter"`
	Conditions    actions.ConditionList `json:"conditions"`
	Actions       actions.ActionList    `json:"actions"`
}

func (req ruleWriteRequest) toRule(tenantID, channel, name string) actions.Rule {
	return actions.Rule{
		TenantID:      tenantID,
		Channel:       channel,
		Name:          name,
		Enabled:       req.Enabled,
		TriggerKind:   actions.TriggerKind(strings.TrimSpace(req.TriggerKind)),
		TriggerFilter: req.TriggerFilter,
		Conditions:    req.Conditions,
		Actions:       req.Actions,
	}
}

// Create handles POST /api/v1/actions.
func (h *Actions) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req ruleWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	channel := channelFromRequest(r, req.Channel)
	if channel == "" || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel and name are required"})
		return
	}
	rule, err := ensureWebhookSecret(req.toRule(h.tenantID, channel, strings.TrimSpace(req.Name)), "")
	if err != nil {
		h.logger.Error("actions create failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "secret_error"})
		return
	}
	ru, err := h.store.Create(r.Context(), rule)
	if err != nil {
		h.writeWriteError(w, err, "actions create failed")
		return
	}
	writeJSON(w, http.StatusCreated, ruleJSON(ru))
	h.reloadScheduler(r.Context())
}

// Update handles PUT /api/v1/actions/{name}.
func (h *Actions) Update(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req ruleWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	channel := channelFromRequest(r, req.Channel)
	name := chi.URLParam(r, "name")
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	prior := ""
	if existing, gerr := h.store.Get(r.Context(), h.tenantID, channel, name); gerr == nil {
		prior = webhookSecretFrom(existing.TriggerFilter)
	}
	rule, err := ensureWebhookSecret(req.toRule(h.tenantID, channel, name), prior)
	if err != nil {
		h.logger.Error("actions update failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "secret_error"})
		return
	}
	ru, err := h.store.Update(r.Context(), rule)
	if err != nil {
		h.writeWriteError(w, err, "actions update failed")
		return
	}
	writeJSON(w, http.StatusOK, ruleJSON(ru))
	h.reloadScheduler(r.Context())
}

// Delete handles DELETE /api/v1/actions/{name}?channel=...
func (h *Actions) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	name := chi.URLParam(r, "name")
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if err := h.store.Delete(r.Context(), h.tenantID, channel, name); err != nil {
		if errors.Is(err, actions.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		h.logger.Error("actions delete failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	h.reloadScheduler(r.Context())
}

// Fire handles POST /api/v1/channels/{channelSlug}/actions/{name}/fire: it
// loads the named rule and test-runs it once via the engine. Any trigger kind
// can be fired this way because it is an explicit operator action; the rule's
// own conditions and actions still gate and run exactly as RunRule applies them.
func (h *Actions) Fire(w http.ResponseWriter, r *http.Request) {
	if h.store == nil || h.runner == nil {
		notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	name := chi.URLParam(r, "name")
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	rule, err := h.store.Get(r.Context(), h.tenantID, channel, name)
	if err != nil {
		if errors.Is(err, actions.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		h.logger.Error("actions fire load failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	// Pre-allocate the run id so the caller can poll the run history for this
	// firing. The id resolves once recorded; if recording is disabled or the
	// rule's conditions fail, no run is stored and the id stays dangling.
	runID := actions.NewRunID()
	h.runner.RunRule(rule, actions.Trigger{
		Kind:      actions.TriggerManual,
		Channel:   channel,
		EventType: "manual",
		Username:  "dashboard",
		RunID:     runID,
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "fired", "run_id": runID})
}

// reloadScheduler re-arms the timer scheduler after a rule mutation so timer
// changes apply live. A nil scheduler (feature wired without one) is a no-op; a
// reload error is logged but never fails the mutation that already succeeded.
func (h *Actions) reloadScheduler(ctx context.Context) {
	if h.scheduler == nil {
		return
	}
	if err := h.scheduler.Reload(ctx); err != nil {
		h.logger.Warn("actions scheduler reload failed", "err", err)
	}
}

func (h *Actions) writeWriteError(w http.ResponseWriter, err error, logMsg string) {
	switch {
	case errors.Is(err, actions.ErrAlreadyExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already_exists"})
	case errors.Is(err, actions.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid"})
	case errors.Is(err, actions.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	default:
		h.logger.Error(logMsg, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
	}
}

func ruleJSON(ru actions.Rule) map[string]any {
	return map[string]any{
		"name":           ru.Name,
		"enabled":        ru.Enabled,
		"trigger_kind":   string(ru.TriggerKind),
		"trigger_filter": maskTriggerFilter(ru.TriggerKind, ru.TriggerFilter),
		"conditions":     ru.Conditions,
		"actions":        ru.Actions,
	}
}

func rawOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}
