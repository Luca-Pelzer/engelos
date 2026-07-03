package openai

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

	"github.com/Luca-Pelzer/engelos/internal/aibackend/usage"
)

// DefaultBaseURL targets OpenAI's public API. Supply your own key with
// [WithAPIKey]. Override the endpoint via [WithBaseURL] to point at any
// OpenAI-compatible server (a local model runner, a compatible proxy, or an
// httptest server in tests). This client is provider-neutral: the concrete
// endpoint for a given deployment is supplied at wiring time, never hardcoded
// beyond this public default.
const DefaultBaseURL = "https://api.openai.com"

// DefaultModel is a small, fast, inexpensive chat model suitable for the
// short-text completions this client is used for (translation, co-host replies,
// clip titles). Override it with [WithModel].
const DefaultModel = "gpt-4o-mini"

// defaultTimeout bounds a single completion request. These calls sit on the hot
// path of message handling, so the timeout is deliberately tight, matching the
// Claude client.
const defaultTimeout = 10 * time.Second

// maxOutputTokens caps the model's reply. The replies this client produces are
// short, so a small cap keeps latency and cost down while leaving room for
// languages that expand under translation.
const maxOutputTokens = 256

// Sentinel errors returned by [Client]. Compare with [errors.Is]. They mirror
// the Claude client's sentinels so this client is a drop-in replacement behind
// the same backend interfaces: consumers that today only check err != nil and
// fail open keep working unchanged.
var (
	// ErrUnauthorized maps an HTTP 401: a missing or invalid API key. The
	// caller cannot fix this per request; it should surface the failure and
	// let the operator refresh credentials.
	ErrUnauthorized = errors.New("openai: unauthorized")
	// ErrAPI is the generic error for any other non-2xx response. It wraps the
	// upstream error envelope's message when present.
	ErrAPI = errors.New("openai: api error")
)

// Client is a thin OpenAI-compatible chat-completions client. Construct it with
// [New]. It is a structural mirror of the Claude client and satisfies the same
// backend interfaces (Complete and Translate), so it is 1:1 interchangeable
// with it behind the co-host, clipper/titler and translator consumers.
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

// WithAPIKey sets the API key, sent as the Authorization: Bearer header. When
// unset no auth header is sent (proxy mode). An empty value is ignored.
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
// [DefaultModel], a 10s HTTP timeout and [slog.Default], and sends no auth
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
	Messages    []wireMessage `json:"messages"`
}

type wireChoice struct {
	Message wireMessage `json:"message"`
}

type wireResponse struct {
	Choices []wireChoice `json:"choices"`
	Usage   wireUsage    `json:"usage"`
}

// wireUsage is the token accounting OpenAI returns.
type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type wireError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// buildSystemPrompt returns the output-only translation instruction for the
// given target language code. The wording mirrors the Claude path so the two
// clients are behaviourally interchangeable: emit only the translation, pass
// through text already in the target language, and leave untranslatable tokens
// (numbers, links, emote names) unchanged.
func buildSystemPrompt(targetLang string) string {
	return fmt.Sprintf(
		"You are a silent translation engine. Translate the user's message to %s. "+
			"Output ONLY the translated text, with no explanations, no preamble and no quotation marks. "+
			"If the message is already in %s, output it unchanged. "+
			"Leave untranslatable tokens such as numbers, URLs and emote names unchanged.",
		targetLang, targetLang)
}

// Translate translates text into the language named by targetLang (an ISO 639-1
// code such as "en") and returns only the translated string. It is a thin
// prompt wrapper around [Client.Complete] using the same system/user prompt
// convention as the Claude path.
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
	return c.Complete(ctx, buildSystemPrompt(targetLang), text)
}

// Complete is a general single-turn completion: it sends systemPrompt plus a
// single user message and returns the model's text reply. It is used by the
// co-host, the clip titler and (via [Client.Translate]) translation. An empty
// userText returns "" with no request made.
//
// A 401 maps to [ErrUnauthorized]; any other non-2xx maps to [ErrAPI].
func (c *Client) Complete(ctx context.Context, systemPrompt, userText string) (string, error) {
	if strings.TrimSpace(userText) == "" {
		return "", nil
	}
	return c.complete(ctx, systemPrompt, userText, 0)
}

// complete posts a single-turn /v1/chat/completions request and returns the
// first choice's message content. temperature is passed through (0 for
// deterministic output).
func (c *Client) complete(ctx context.Context, systemPrompt, userText string, temperature float64) (string, error) {
	reqBody := wireRequest{
		Model:       c.model,
		MaxTokens:   maxOutputTokens,
		Temperature: temperature,
		Messages: []wireMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userText},
		},
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", c.mapError(resp)
	}

	var wr wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return "", fmt.Errorf("openai: decode response: %w", err)
	}
	usage.Record(ctx, usage.Usage{
		InputTokens:  wr.Usage.PromptTokens,
		OutputTokens: wr.Usage.CompletionTokens,
	})
	return firstContent(wr.Choices), nil
}

// firstContent returns the trimmed content of the first choice's message, or
// the empty string when the response carries no choices.
func firstContent(choices []wireChoice) string {
	if len(choices) == 0 {
		return ""
	}
	return strings.TrimSpace(choices[0].Message.Content)
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
