package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/tts"
	"github.com/Luca-Pelzer/engelos/internal/tts/elevenlabs"
)

// TTSSecrets is the encryption boundary the handler needs to store and read the
// ElevenLabs key. *secrets.Box satisfies it.
type TTSSecrets interface {
	EncryptString(s string) ([]byte, error)
	DecryptString(blob []byte) (string, error)
}

// TTS exposes per-channel text-to-speech configuration over HTTP. The API key
// is write-only: callers may set it but the plaintext is never returned, only a
// has_api_key flag. All endpoints are session-protected at the router. When the
// store or secrets box is nil every endpoint returns 501.
type TTS struct {
	store    tts.Store
	secrets  TTSSecrets
	tenantID string
	logger   *slog.Logger
}

// NewTTS constructs the handler bundle.
func NewTTS(store tts.Store, secrets TTSSecrets, tenantID string, logger *slog.Logger) *TTS {
	if logger == nil {
		logger = slog.Default()
	}
	return &TTS{store: store, secrets: secrets, tenantID: tenantID, logger: logger}
}

func (h *TTS) disabled() bool { return h.store == nil || h.secrets == nil }

// Get handles GET /api/v1/tts?channel=...
// Without a channel it lists every configured channel for the tenant; with a
// channel it returns that channel's config (or the disabled default).
func (h *TTS) Get(w http.ResponseWriter, r *http.Request) {
	if h.disabled() {
		h.notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	if channel == "" {
		configs, err := h.store.List(r.Context(), h.tenantID)
		if err != nil {
			h.writeStoreError(w, r, "tts list failed", err)
			return
		}
		out := make([]map[string]any, 0, len(configs))
		for _, c := range configs {
			out = append(out, ttsJSON(c))
		}
		writeJSON(w, http.StatusOK, map[string]any{"configs": out})
		return
	}
	c, err := h.store.GetOrDefault(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "tts get failed", err)
		return
	}
	writeJSON(w, http.StatusOK, ttsJSON(c))
}

// Set handles PUT /api/v1/tts.
// Body: {channel, enabled, voice_id, model, api_key}. api_key, when a non-empty
// string, is encrypted and stored; omitting it (or sending null) keeps the
// existing key. Sending an explicit empty string clears the stored key.
func (h *TTS) Set(w http.ResponseWriter, r *http.Request) {
	if h.disabled() {
		h.notImplemented(w)
		return
	}
	var req struct {
		Channel string  `json:"channel"`
		Enabled *bool   `json:"enabled"`
		VoiceID *string `json:"voice_id"`
		Model   *string `json:"model"`
		APIKey  *string `json:"api_key"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	channel := channelFromRequest(r, req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	current, err := h.store.GetOrDefault(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "tts get failed", err)
		return
	}
	if req.Enabled != nil {
		current.Enabled = *req.Enabled
	}
	if req.VoiceID != nil {
		current.VoiceID = *req.VoiceID
	}
	if req.Model != nil {
		current.Model = *req.Model
	}
	if req.APIKey != nil {
		if *req.APIKey == "" {
			current.APIKeyCiphertext = nil
		} else {
			cipher, err := h.secrets.EncryptString(*req.APIKey)
			if err != nil {
				h.logger.WarnContext(r.Context(), "tts key encrypt failed", slog.Any("err", err))
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
			current.APIKeyCiphertext = cipher
		}
	}
	saved, err := h.store.Set(r.Context(), current)
	if err != nil {
		h.writeStoreError(w, r, "tts set failed", err)
		return
	}
	writeJSON(w, http.StatusOK, ttsJSON(saved))
}

// Voices handles GET /api/v1/tts/voices?channel=...
// It uses the channel's stored key to list the voices available on that
// ElevenLabs account so the dashboard can offer a picker.
func (h *TTS) Voices(w http.ResponseWriter, r *http.Request) {
	if h.disabled() {
		h.notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	c, err := h.store.GetOrDefault(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "tts get failed", err)
		return
	}
	if len(c.APIKeyCiphertext) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no_api_key"})
		return
	}
	key, err := h.secrets.DecryptString(c.APIKeyCiphertext)
	if err != nil {
		h.logger.WarnContext(r.Context(), "tts key decrypt failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	client := elevenlabs.New(elevenlabs.WithAPIKey(key), elevenlabs.WithLogger(h.logger))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	voices, err := client.Voices(ctx)
	if err != nil {
		h.logger.WarnContext(r.Context(), "tts voices fetch failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "voices_fetch_failed"})
		return
	}
	out := make([]map[string]any, 0, len(voices))
	for _, v := range voices {
		out = append(out, map[string]any{"voice_id": v.ID, "name": v.Name, "category": v.Category})
	}
	writeJSON(w, http.StatusOK, map[string]any{"voices": out})
}

// writeStoreError maps store sentinel errors to HTTP status codes.
func (h *TTS) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, tts.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func (h *TTS) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "tts_not_enabled"})
}

// ttsJSON renders a Config into the wire shape. The API key is never included;
// only has_api_key reports whether one is stored.
func ttsJSON(c tts.Config) map[string]any {
	return map[string]any{
		"channel":     c.Channel,
		"enabled":     c.Enabled,
		"voice_id":    c.VoiceID,
		"model":       c.Model,
		"has_api_key": len(c.APIKeyCiphertext) > 0,
		"updated_at":  c.UpdatedAt.Format(time.RFC3339),
	}
}
