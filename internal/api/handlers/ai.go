package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/aibackend"
	"github.com/Luca-Pelzer/engelos/internal/aibackend/aiconfig"
	"github.com/Luca-Pelzer/engelos/internal/aibackend/usage"
)

// AIManager is the subset of *aiconfig.Manager the AI config handler needs. It
// is an interface so the handler stays testable, but *aiconfig.Manager is the
// only production implementation.
type AIManager interface {
	Snapshot() aiconfig.Snapshot
	UpdateConfig(ctx context.Context, up aiconfig.ConfigUpdate) error
	Test(ctx context.Context, override *aibackend.Config) aiconfig.TestResult
	UsageSnapshot() usage.Snapshot
}

// AI serves the runtime AI-backend configuration endpoints (config CRUD,
// connection test, provider catalog). A nil manager disables the mutating and
// status endpoints; the routes are only mounted by the router when a manager is
// wired, so the nil guard here is defence in depth.
type AI struct {
	mgr    AIManager
	logger *slog.Logger
}

// NewAI builds the AI handler. A nil manager makes the config/test endpoints
// report not-implemented; a nil logger falls back to [slog.Default].
func NewAI(mgr AIManager, logger *slog.Logger) *AI {
	if logger == nil {
		logger = slog.Default()
	}
	return &AI{mgr: mgr, logger: logger}
}

func (h *AI) disabled() bool { return h.mgr == nil }

func (h *AI) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "ai_not_enabled"})
}

// GetConfig handles GET /api/v1/ai/config. It returns the masked active config
// (provider, base URL, model, whether a key is set plus a short hint, and the
// source: db, env or default). It never returns key material.
func (h *AI) GetConfig(w http.ResponseWriter, _ *http.Request) {
	if h.disabled() {
		h.notImplemented(w)
		return
	}
	writeJSON(w, http.StatusOK, h.mgr.Snapshot())
}

// PutConfig handles PUT /api/v1/ai/config. Provider, base URL and model are
// overwritten. The api_key field is tri-state: absent or "" keeps the stored
// key; a non-empty string sets it; JSON null or "clear_api_key":true removes
// it. On success it persists, hot-swaps the backend and returns the new masked
// config.
func (h *AI) PutConfig(w http.ResponseWriter, r *http.Request) {
	if h.disabled() {
		h.notImplemented(w)
		return
	}
	var req struct {
		Provider    string          `json:"provider"`
		BaseURL     string          `json:"base_url"`
		Model       string          `json:"model"`
		APIKey      json.RawMessage `json:"api_key"`
		ClearAPIKey bool            `json:"clear_api_key"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}

	up := aiconfig.ConfigUpdate{Provider: req.Provider, BaseURL: req.BaseURL, Model: req.Model}
	switch {
	case req.ClearAPIKey || string(req.APIKey) == "null":
		up.ClearAPIKey = true
	case len(req.APIKey) > 0:
		var key string
		if err := json.Unmarshal(req.APIKey, &key); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		if strings.TrimSpace(key) != "" {
			up.SetAPIKey = true
			up.APIKey = key
		}
	}

	if err := h.mgr.UpdateConfig(r.Context(), up); err != nil {
		switch {
		case errors.Is(err, aiconfig.ErrNoSecretsKey):
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":  "secrets_key_required",
				"detail": "set ENGELOS_SECRETS_KEY to store an API key encrypted at rest",
			})
		case errors.Is(err, aiconfig.ErrNoStore):
			h.notImplemented(w)
		default:
			h.logger.WarnContext(r.Context(), "ai config update failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "update_failed"})
		}
		return
	}
	writeJSON(w, http.StatusOK, h.mgr.Snapshot())
}

// TestConfig handles POST /api/v1/ai/test. With a non-empty body it tests the
// submitted config (provider, base_url, model, api_key); with an empty body it
// tests the active config. It always responds 200 with a structured result
// ({ok, latency_ms, provider, model, error?}); a backend failure is reported in
// the error field, never as a 5xx.
func (h *AI) TestConfig(w http.ResponseWriter, r *http.Request) {
	if h.disabled() {
		h.notImplemented(w)
		return
	}
	var req struct {
		Provider *string `json:"provider"`
		BaseURL  *string `json:"base_url"`
		Model    *string `json:"model"`
		APIKey   *string `json:"api_key"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}

	var override *aibackend.Config
	if req.Provider != nil || req.BaseURL != nil || req.Model != nil || req.APIKey != nil {
		override = &aibackend.Config{
			Provider: derefString(req.Provider),
			BaseURL:  derefString(req.BaseURL),
			Model:    derefString(req.Model),
			APIKey:   derefString(req.APIKey),
		}
	}
	writeJSON(w, http.StatusOK, h.mgr.Test(r.Context(), override))
}

// Providers handles GET /api/v1/ai/providers. It returns the static provider
// catalog the dashboard renders to build a config. It carries no secrets and
// does not touch the manager.
func (h *AI) Providers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"providers": aiconfig.Providers()})
}

// Usage handles GET /api/v1/ai/usage. It returns the in-memory AI usage counters
// since process start (calls, ok/errors, tokens in/out, cache reads, and a
// per-consumer breakdown). The counters are never persisted and reset on
// restart.
func (h *AI) Usage(w http.ResponseWriter, _ *http.Request) {
	if h.disabled() {
		h.notImplemented(w)
		return
	}
	writeJSON(w, http.StatusOK, h.mgr.UsageSnapshot())
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
