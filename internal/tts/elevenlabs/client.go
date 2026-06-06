package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the ElevenLabs API root. Override it in tests via
// [WithBaseURL] to point at an httptest server.
const DefaultBaseURL = "https://api.elevenlabs.io"

// DefaultModel is the lowest-latency multilingual model, the right default for
// short real-time alert lines.
const DefaultModel = "eleven_flash_v2_5"

// defaultOutputFormat is the broadest-compatibility audio format for browser
// playback; it carries no tier restriction.
const defaultOutputFormat = "mp3_44100_128"

// defaultTimeout bounds a single synthesis request. Audio synthesis is slower
// than a text completion, so the budget is wider than the Claude client's.
const defaultTimeout = 30 * time.Second

// Sentinel errors returned by [Client]. Compare with [errors.Is].
var (
	// ErrAPIKeyRequired is returned when a call needs the bring-your-own-key
	// but none was set via [WithAPIKey].
	ErrAPIKeyRequired = errors.New("elevenlabs: api key required")
	// ErrUnauthorized maps an HTTP 401: the supplied key is invalid.
	ErrUnauthorized = errors.New("elevenlabs: unauthorized")
	// ErrQuotaExceeded maps an HTTP 429: the key hit its concurrency or
	// character quota.
	ErrQuotaExceeded = errors.New("elevenlabs: quota or rate limit exceeded")
	// ErrAPI is the generic error for any other non-2xx response.
	ErrAPI = errors.New("elevenlabs: api error")
)

// VoiceSettings tunes a synthesis request. The zero value is not useful; use
// [DefaultVoiceSettings].
type VoiceSettings struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
	Style           float64 `json:"style"`
	UseSpeakerBoost bool    `json:"use_speaker_boost"`
}

// DefaultVoiceSettings returns the documented ElevenLabs defaults: balanced
// stability and similarity, no style exaggeration, speaker boost on.
func DefaultVoiceSettings() VoiceSettings {
	return VoiceSettings{
		Stability:       0.5,
		SimilarityBoost: 0.75,
		Style:           0.0,
		UseSpeakerBoost: true,
	}
}

// Voice is the subset of an ElevenLabs voice object the dashboard needs to list
// and pick voices.
type Voice struct {
	ID       string `json:"voice_id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

// Subscription is the subset of the user endpoint used to validate a key and
// surface remaining character quota.
type Subscription struct {
	Tier           string `json:"tier"`
	CharacterCount int    `json:"character_count"`
	CharacterLimit int    `json:"character_limit"`
}

// Client is a thin ElevenLabs TTS client. Construct it with [New].
type Client struct {
	httpClient    *http.Client
	baseURL       string
	model         string
	apiKey        string
	voiceSettings VoiceSettings
	logger        *slog.Logger
}

// Option configures a [Client] in [New].
type Option func(*Client)

// WithHTTPClient sets the underlying *http.Client. A nil client is ignored.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithBaseURL overrides the endpoint root (default [DefaultBaseURL]). A trailing
// slash is trimmed. An empty value is ignored.
func WithBaseURL(base string) Option {
	return func(c *Client) {
		if base != "" {
			c.baseURL = strings.TrimRight(base, "/")
		}
	}
}

// WithModel overrides the model id (default [DefaultModel]). An empty value is
// ignored.
func WithModel(model string) Option {
	return func(c *Client) {
		if model != "" {
			c.model = model
		}
	}
}

// WithAPIKey sets the bring-your-own-key sent as the xi-api-key header. An empty
// value is ignored.
func WithAPIKey(key string) Option {
	return func(c *Client) {
		if key != "" {
			c.apiKey = key
		}
	}
}

// WithVoiceSettings overrides the synthesis voice settings (default
// [DefaultVoiceSettings]).
func WithVoiceSettings(vs VoiceSettings) Option {
	return func(c *Client) { c.voiceSettings = vs }
}

// WithLogger sets the structured logger. A nil logger is ignored.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.logger = l
		}
	}
}

// New constructs a [Client]. With no options it targets [DefaultBaseURL] with
// [DefaultModel], a 30s timeout, [DefaultVoiceSettings] and [slog.Default], and
// sends no key (so synthesis fails with [ErrAPIKeyRequired] until one is set).
func New(opts ...Option) *Client {
	c := &Client{
		httpClient:    &http.Client{Timeout: defaultTimeout},
		baseURL:       DefaultBaseURL,
		model:         DefaultModel,
		voiceSettings: DefaultVoiceSettings(),
		logger:        slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type synthesizeRequest struct {
	Text          string        `json:"text"`
	ModelID       string        `json:"model_id"`
	VoiceSettings VoiceSettings `json:"voice_settings"`
}

// Synthesize converts text to speech with the given voice and returns the raw
// MP3 bytes. Empty or whitespace-only text returns (nil, nil) with no request.
// A missing key returns [ErrAPIKeyRequired]; 401 maps to [ErrUnauthorized],
// 429 to [ErrQuotaExceeded], and any other non-2xx to [ErrAPI].
func (c *Client) Synthesize(ctx context.Context, voiceID, text string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	if c.apiKey == "" {
		return nil, ErrAPIKeyRequired
	}
	if strings.TrimSpace(voiceID) == "" {
		return nil, fmt.Errorf("%w: voice id required", ErrAPI)
	}

	body, err := json.Marshal(synthesizeRequest{
		Text:          text,
		ModelID:       c.model,
		VoiceSettings: c.voiceSettings,
	})
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: marshal body: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1/text-to-speech/%s?output_format=%s",
		c.baseURL, url.PathEscape(voiceID), defaultOutputFormat)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: build request: %w", err)
	}
	req.Header.Set("xi-api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.mapError(resp)
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: read audio: %w", err)
	}
	return audio, nil
}

type voicesResponse struct {
	Voices []Voice `json:"voices"`
}

// Voices lists the voices available to the key. It is used by the dashboard to
// let the operator pick a voice id.
func (c *Client) Voices(ctx context.Context) ([]Voice, error) {
	if c.apiKey == "" {
		return nil, ErrAPIKeyRequired
	}
	var out voicesResponse
	if err := c.getJSON(ctx, "/v2/voices", &out); err != nil {
		return nil, err
	}
	return out.Voices, nil
}

type userResponse struct {
	Subscription Subscription `json:"subscription"`
}

// VerifyKey validates the key and returns the account subscription, including
// the used and total character quota.
func (c *Client) VerifyKey(ctx context.Context) (Subscription, error) {
	if c.apiKey == "" {
		return Subscription{}, ErrAPIKeyRequired
	}
	var out userResponse
	if err := c.getJSON(ctx, "/v1/user", &out); err != nil {
		return Subscription{}, err
	}
	return out.Subscription, nil
}

// getJSON performs a GET against path, attaches the key, and decodes a JSON
// body into out. Non-2xx maps through [Client.mapError].
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("elevenlabs: build request: %w", err)
	}
	req.Header.Set("xi-api-key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("elevenlabs: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.mapError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("elevenlabs: decode response: %w", err)
	}
	return nil
}

// mapError converts a non-2xx response into a sentinel error, consuming the
// body. 401 becomes [ErrUnauthorized], 429 [ErrQuotaExceeded]; everything else
// becomes [ErrAPI] with the upstream message when one can be extracted.
func (c *Client) mapError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusTooManyRequests:
		return ErrQuotaExceeded
	}
	msg := readErrorMessage(resp)
	if msg != "" {
		return fmt.Errorf("%w: status %d: %s", ErrAPI, resp.StatusCode, msg)
	}
	return fmt.Errorf("%w: status %d", ErrAPI, resp.StatusCode)
}

// readErrorMessage best-effort extracts {"detail":...} or the raw body from an
// error response.
func readErrorMessage(resp *http.Response) string {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(raw) == 0 {
		return ""
	}
	var env struct {
		Detail any `json:"detail"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Detail != nil {
		if s, ok := env.Detail.(string); ok && s != "" {
			return s
		}
		if b, err := json.Marshal(env.Detail); err == nil {
			return string(b)
		}
	}
	return strings.TrimSpace(string(raw))
}
