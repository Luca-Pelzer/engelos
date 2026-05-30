package claude

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
	"time"
)

// DefaultBaseURL is the local Anthropic OAuth proxy that fronts the shared
// Claude subscription. The proxy owns token refresh and the Claude Code
// identity injection, so the client only has to POST plain /v1/messages JSON.
// Override it in tests via [WithBaseURL] to point at an httptest server, or in
// production via the ENGELOS_TRANSLATE_BASE_URL env var wired in main.
const DefaultBaseURL = "http://10.10.10.1:3033"

// DefaultModel is the cheapest, fastest Claude model suitable for short-text
// translation. The proxy exposes it under this alias.
const DefaultModel = "claude-haiku-4-5"

// defaultTimeout bounds a single translation request. Chat translation is on
// the hot path of message handling, so the timeout is deliberately tight.
const defaultTimeout = 10 * time.Second

// maxOutputTokens caps the model's reply. A translated chat line is short, so a
// small cap keeps latency and (for paid backends) cost down while leaving room
// for languages that expand under translation.
const maxOutputTokens = 256

// Sentinel errors returned by [Client]. Compare with [errors.Is].
var (
	// ErrUnauthorized maps an HTTP 401: the proxy's subscription token is
	// stale, or a supplied bring-your-own-key is invalid. The caller cannot
	// fix this per request; it should surface the failure and let the proxy
	// (or the operator) refresh credentials.
	ErrUnauthorized = errors.New("claude: unauthorized")
	// ErrAPI is the generic error for any other non-2xx response. It wraps the
	// upstream error envelope's message when present.
	ErrAPI = errors.New("claude: api error")
)

// Client is a thin Claude translation client. Construct it with [New]. It holds
// no per-call credentials by default; the fronting proxy supplies the OAuth
// bearer. Use [WithAPIKey] only for the bring-your-own-key path.
type Client struct {
	httpClient *http.Client
	baseURL    string
	model      string
	apiKey     string
	logger     *slog.Logger
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

// WithAPIKey sets a bring-your-own-key sent as the x-api-key header. When unset
// (the default) no key header is sent, which is what the OAuth proxy expects.
// An empty value is ignored.
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

// New constructs a [Client]. With no options it targets [DefaultBaseURL] with
// [DefaultModel], a 10s HTTP timeout and [slog.Default], and sends no key
// header (proxy mode).
func New(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    DefaultBaseURL,
		model:      DefaultModel,
		logger:     slog.Default(),
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
	System      string        `json:"system"`
	Messages    []wireMessage `json:"messages"`
}

type wireContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireResponse struct {
	Content []wireContentBlock `json:"content"`
}

type wireError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// buildSystemPrompt returns the output-only translation instruction for the
// given target language code. The wording mirrors the prompt the proxy is
// validated against: emit only the translation, pass through text already in
// the target language, and leave untranslatable tokens (numbers, links, emote
// names) unchanged.
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

	reqBody := wireRequest{
		Model:       c.model,
		MaxTokens:   maxOutputTokens,
		Temperature: 0,
		System:      buildSystemPrompt(targetLang),
		Messages:    []wireMessage{{Role: "user", Content: text}},
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("claude: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("claude: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("claude: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", c.mapError(resp)
	}

	var wr wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return "", fmt.Errorf("claude: decode response: %w", err)
	}
	return joinText(wr.Content), nil
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
