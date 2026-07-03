package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/aibackend/usage"
)

// DefaultBaseURL targets Anthropic's public API. Supply your own key with
// [WithAPIKey]. Override the endpoint via [WithBaseURL] to point at any
// Anthropic-compatible server (a compatible proxy or an httptest server in
// tests). This client is provider-neutral: the concrete endpoint for a given
// deployment is supplied at wiring time (via the aibackend selector), never
// hardcoded beyond this public default.
const DefaultBaseURL = "https://api.anthropic.com"

// DefaultModel is the cheapest, fastest Claude model suitable for the
// short-text completions this client is used for (translation, co-host replies,
// clip titles). Override it with [WithModel].
const DefaultModel = "claude-haiku-4-5"

// defaultTimeout bounds a single request. These calls sit on the hot path of
// message handling, so the timeout is deliberately tight.
const defaultTimeout = 10 * time.Second

// maxOutputTokens caps the model's reply. A translated chat line is short, so a
// small cap keeps latency and cost down while leaving room for languages that
// expand under translation.
const maxOutputTokens = 256

// Sentinel errors returned by [Client]. Compare with [errors.Is]. They mirror
// the OpenAI client's sentinels so this client is a drop-in behind the same
// backend interfaces: consumers that only check err != nil and fail open keep
// working unchanged.
var (
	// ErrUnauthorized maps an HTTP 401: a missing or invalid API key. The
	// caller cannot fix this per request; it should surface the failure and
	// let the operator refresh credentials.
	ErrUnauthorized = errors.New("anthropic: unauthorized")
	// ErrAPI is the generic error for any other non-2xx response. It wraps the
	// upstream error envelope's message when present.
	ErrAPI = errors.New("anthropic: api error")
)

// Client is a thin Anthropic /v1/messages client. Construct it with [New]. It
// sends no per-call credentials by default; supply a key with [WithAPIKey] for
// the bring-your-own-key path.
type Client struct {
	httpClient *http.Client
	baseURL    string
	model      string
	apiKey     string
	logger     *slog.Logger

	// promptCaching requests cache_control on the static system prefix;
	// cacheDisabled latches on when a server rejects it, so the process stops
	// sending it.
	promptCaching bool
	cacheDisabled atomic.Bool
}

// Option configures a [Client] in [New].
type Option func(*Client)

// WithHTTPClient sets the underlying *http.Client. A nil client is ignored. The
// default is &http.Client{Timeout: 10 * time.Second}.
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

// WithAPIKey sets the Anthropic API key, sent as the x-api-key header. When
// unset (the default) no key header is sent. An empty value is ignored.
func WithAPIKey(key string) Option {
	return func(c *Client) {
		if key != "" {
			c.apiKey = key
		}
	}
}

// WithLogger sets the structured logger. A nil logger is ignored; the default
// is [slog.Default].
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithPromptCaching toggles Anthropic prompt caching of the static system-prompt
// prefix (marked with cache_control: ephemeral). It defaults to true. A server
// that rejects cache_control makes the client retry once without it and disable
// caching for the rest of the process, so leaving this on is safe even behind an
// older proxy.
func WithPromptCaching(on bool) Option {
	return func(c *Client) {
		c.promptCaching = on
	}
}

// New constructs a [Client]. With no options it targets [DefaultBaseURL] with
// [DefaultModel], a 10s HTTP timeout and [slog.Default], sends no key header,
// and enables prompt caching.
func New(opts ...Option) *Client {
	c := &Client{
		httpClient:    &http.Client{Timeout: defaultTimeout},
		baseURL:       DefaultBaseURL,
		model:         DefaultModel,
		logger:        slog.Default(),
		promptCaching: true,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// --- wire shapes (only the fields we need) ---

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type wireRequest struct {
	Model       string        `json:"model"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	System      any           `json:"system"` // string (no cache) or []systemBlock (cache)
	Messages    []wireMessage `json:"messages"`
}

// systemBlock is the content-block form of the system prompt used when prompt
// caching is enabled: a single static text block carrying a cache_control mark.
type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type cacheControl struct {
	Type string `json:"type"`
}

type wireContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireResponse struct {
	Content []wireContentBlock `json:"content"`
	Usage   wireUsage          `json:"usage"`
}

// wireUsage is the token accounting Anthropic returns. cache_read_input_tokens
// counts the input tokens served from the prompt cache.
type wireUsage struct {
	InputTokens          int `json:"input_tokens"`
	OutputTokens         int `json:"output_tokens"`
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
}

type wireError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// buildSystemPrompt returns the output-only translation instruction for the
// given target language code: emit only the translation, pass through text
// already in the target language, and leave untranslatable tokens (numbers,
// links, emote names) unchanged.
func buildSystemPrompt(targetLang string) string {
	return fmt.Sprintf(
		"You are a silent translation engine. Translate the user's message to %s. "+
			"Output ONLY the translated text, with no explanations, no preamble and no quotation marks. "+
			"If the message is already in %s, output it unchanged. "+
			"Leave untranslatable tokens such as numbers, URLs and emote names unchanged.",
		targetLang, targetLang)
}

// Translate translates text into the language named by targetLang (an ISO 639-1
// code such as "en") and returns only the translated string. It posts a single
// non-streaming /v1/messages request with temperature 0 for deterministic,
// cache-friendly output.
//
// An empty or whitespace-only text returns "" with no request made. A 401 maps
// to [ErrUnauthorized]; any other non-2xx maps to [ErrAPI].
func (c *Client) Translate(ctx context.Context, text, targetLang string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	if strings.TrimSpace(targetLang) == "" {
		targetLang = "en"
	}
	return c.complete(ctx, buildSystemPrompt(targetLang), text, 0)
}

// Complete is a general single-turn completion: it sends systemPrompt plus a
// single user message and returns the model's text reply. It is used by
// features other than translation (co-host, context moderation, clip titling).
// An empty userText returns "" with no request made.
//
// A 401 maps to [ErrUnauthorized]; any other non-2xx maps to [ErrAPI].
func (c *Client) Complete(ctx context.Context, systemPrompt, userText string) (string, error) {
	if strings.TrimSpace(userText) == "" {
		return "", nil
	}
	return c.complete(ctx, systemPrompt, userText, 0)
}

// cachingEnabled reports whether this call should send cache_control: on by
// default (see [WithPromptCaching]) and staying on unless a server rejected it
// once, which latches cacheDisabled for the rest of the process.
func (c *Client) cachingEnabled() bool {
	return c.promptCaching && !c.cacheDisabled.Load()
}

// buildRequest assembles the /v1/messages body. With caching the static system
// prompt is sent as a single content block carrying cache_control: ephemeral;
// without it the system prompt is a plain string, byte-for-byte the pre-caching
// wire.
func (c *Client) buildRequest(systemPrompt, userText string, temperature float64, useCache bool) wireRequest {
	req := wireRequest{
		Model:       c.model,
		MaxTokens:   maxOutputTokens,
		Temperature: temperature,
		Messages:    []wireMessage{{Role: "user", Content: userText}},
	}
	if useCache {
		req.System = []systemBlock{{Type: "text", Text: systemPrompt, CacheControl: &cacheControl{Type: "ephemeral"}}}
	} else {
		req.System = systemPrompt
	}
	return req
}

// complete posts a single-turn /v1/messages request and returns the joined text
// content. temperature is passed through (0 for deterministic output). When
// prompt caching is enabled and the server rejects cache_control with a 4xx, it
// disables caching for the process and retries the request once without it.
func (c *Client) complete(ctx context.Context, systemPrompt, userText string, temperature float64) (string, error) {
	useCache := c.cachingEnabled()
	out, status, err := c.attempt(ctx, systemPrompt, userText, temperature, useCache)
	if err != nil && useCache && status >= 400 && status < 500 &&
		strings.Contains(strings.ToLower(err.Error()), "cache_control") {
		c.cacheDisabled.Store(true)
		c.logger.WarnContext(ctx, "anthropic: server rejected cache_control; disabling prompt caching for this process")
		out, _, err = c.attempt(ctx, systemPrompt, userText, temperature, false)
	}
	return out, err
}

// attempt performs one /v1/messages request. It returns the reply text, the HTTP
// status code (0 when the request could not be sent), and any error. On success
// it reports token usage to the context sink (see internal/aibackend/usage).
func (c *Client) attempt(ctx context.Context, systemPrompt, userText string, temperature float64, useCache bool) (string, int, error) {
	b, err := json.Marshal(c.buildRequest(systemPrompt, userText, temperature, useCache))
	if err != nil {
		return "", 0, fmt.Errorf("anthropic: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return "", 0, fmt.Errorf("anthropic: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("anthropic: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, c.mapError(resp)
	}

	var wr wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return "", resp.StatusCode, fmt.Errorf("anthropic: decode response: %w", err)
	}
	usage.Record(ctx, usage.Usage{
		InputTokens:     wr.Usage.InputTokens,
		OutputTokens:    wr.Usage.OutputTokens,
		CacheReadTokens: wr.Usage.CacheReadInputTokens,
	})
	return joinText(wr.Content), resp.StatusCode, nil
}

// joinText concatenates the text of every text content block, trimming the
// surrounding whitespace the model occasionally emits.
func joinText(blocks []wireContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

// mapError converts a non-2xx response into a sentinel error, consuming the
// body. 401 becomes [ErrUnauthorized]; everything else becomes [ErrAPI] with
// the upstream message when one can be extracted.
func (c *Client) mapError(resp *http.Response) error {
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	msg := c.readErrorMessage(resp)
	if msg != "" {
		return fmt.Errorf("%w: status %d: %s", ErrAPI, resp.StatusCode, msg)
	}
	return fmt.Errorf("%w: status %d", ErrAPI, resp.StatusCode)
}

// readErrorMessage best-effort extracts {"error":{"message":...}} from the
// body, falling back to the raw (trimmed) body text.
func (c *Client) readErrorMessage(resp *http.Response) string {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(raw) == 0 {
		return ""
	}
	var we wireError
	if json.Unmarshal(raw, &we) == nil && we.Error.Message != "" {
		return we.Error.Message
	}
	return strings.TrimSpace(string(raw))
}
