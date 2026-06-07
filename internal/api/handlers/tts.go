package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/tts"
	"github.com/Luca-Pelzer/engelos/internal/tts/elevenlabs"
)

// maxCloneUpload caps a voice-clone upload body so a client cannot stream an
// unbounded request into memory. A few minutes of audio fits well under this.
const maxCloneUpload = 25 << 20

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

// Clone handles POST /api/v1/tts/clone (multipart/form-data).
// Fields: name (required), files (one or more audio parts). It uses the
// channel's stored ElevenLabs key to create an Instant Voice Clone and returns
// the new voice_id so the dashboard can select it.
func (h *TTS) Clone(w http.ResponseWriter, r *http.Request) {
	if h.disabled() {
		h.notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCloneUpload)
	if err := r.ParseMultipartForm(maxCloneUpload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_upload"})
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	samples, err := readVoiceSamples(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(samples) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one audio file is required"})
		return
	}

	client, ok := h.clientForChannel(w, r, channel)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	voice, err := client.CreateVoiceClone(ctx, name, samples)
	if err != nil {
		h.logger.WarnContext(r.Context(), "tts voice clone failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "clone_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"voice_id":              voice.VoiceID,
		"requires_verification": voice.RequiresVerification,
	})
}

// DeleteVoice handles DELETE /api/v1/tts/voices/{voiceID}?channel=...
// It removes the voice from the channel's ElevenLabs account.
func (h *TTS) DeleteVoice(w http.ResponseWriter, r *http.Request) {
	if h.disabled() {
		h.notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	voiceID := strings.TrimSpace(chi.URLParam(r, "voiceID"))
	if voiceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "voice id is required"})
		return
	}
	client, ok := h.clientForChannel(w, r, channel)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := client.DeleteVoice(ctx, voiceID); err != nil {
		h.logger.WarnContext(r.Context(), "tts voice delete failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "delete_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// clientForChannel builds an ElevenLabs client from the channel's stored key,
// writing the appropriate error response and returning ok=false on any failure
// (no stored key, decrypt failure, store error).
func (h *TTS) clientForChannel(w http.ResponseWriter, r *http.Request, channel string) (*elevenlabs.Client, bool) {
	c, err := h.store.GetOrDefault(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "tts get failed", err)
		return nil, false
	}
	if len(c.APIKeyCiphertext) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no_api_key"})
		return nil, false
	}
	key, err := h.secrets.DecryptString(c.APIKeyCiphertext)
	if err != nil {
		h.logger.WarnContext(r.Context(), "tts key decrypt failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return nil, false
	}
	return elevenlabs.New(elevenlabs.WithAPIKey(key), elevenlabs.WithLogger(h.logger)), true
}

// readVoiceSamples reads every "files" part of the parsed multipart form into
// memory as voice-clone samples, skipping empty parts.
func readVoiceSamples(r *http.Request) ([]elevenlabs.VoiceSample, error) {
	if r.MultipartForm == nil {
		return nil, nil
	}
	headers := r.MultipartForm.File["files"]
	out := make([]elevenlabs.VoiceSample, 0, len(headers))
	for _, fh := range headers {
		f, err := fh.Open()
		if err != nil {
			return nil, errors.New("invalid_upload")
		}
		data, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			return nil, errors.New("invalid_upload")
		}
		if len(data) == 0 {
			continue
		}
		out = append(out, elevenlabs.VoiceSample{Filename: fh.Filename, Data: data})
	}
	return out, nil
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
