package openai

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, h http.Handler, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	base := []Option{
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}
	return New(append(base, opts...)...)
}

// okResponse is a minimal valid chat/completions body carrying content.
func okResponse(content string) string {
	b, _ := json.Marshal(wireResponse{
		Choices: []wireChoice{{Message: wireMessage{Role: "assistant", Content: content}}},
	})
	return string(b)
}

func TestComplete_PostsExpectedRequestAndReturnsText(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		// Proxy mode: no Authorization header is sent.
		assert.Empty(t, r.Header.Get("Authorization"))

		var req wireRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, DefaultModel, req.Model)
		assert.Equal(t, maxOutputTokens, req.MaxTokens)
		assert.Equal(t, float64(0), req.Temperature)
		// System then user, in that order.
		require.Len(t, req.Messages, 2)
		assert.Equal(t, "system", req.Messages[0].Role)
		assert.Equal(t, "You are a helpful assistant.", req.Messages[0].Content)
		assert.Equal(t, "user", req.Messages[1].Role)
		assert.Equal(t, "Ping?", req.Messages[1].Content)

		_, _ = io.WriteString(w, okResponse("Pong."))
	}))

	got, err := c.Complete(context.Background(), "You are a helpful assistant.", "Ping?")
	require.NoError(t, err)
	assert.Equal(t, "Pong.", got)
}

func TestComplete_TrimsWhitespace(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, okResponse("  Hello, world  "))
	}))
	got, err := c.Complete(context.Background(), "sys", "hi")
	require.NoError(t, err)
	assert.Equal(t, "Hello, world", got)
}

func TestComplete_EmptyInputSkipsRequest(t *testing.T) {
	called := false
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = io.WriteString(w, okResponse("x"))
	}))
	got, err := c.Complete(context.Background(), "sys", "   ")
	require.NoError(t, err)
	assert.Equal(t, "", got)
	assert.False(t, called, "no HTTP request should be made for empty input")
}

func TestTranslate_PostsSystemAndUserAndReturnsText(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)

		var req wireRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Len(t, req.Messages, 2)
		assert.Equal(t, "system", req.Messages[0].Role)
		assert.Contains(t, req.Messages[0].Content, "English")
		assert.Contains(t, req.Messages[0].Content, "Output ONLY")
		assert.Equal(t, "user", req.Messages[1].Role)
		assert.Equal(t, "Hola, como estas?", req.Messages[1].Content)

		_, _ = io.WriteString(w, okResponse("Hello, how are you?"))
	}))

	got, err := c.Translate(context.Background(), "Hola, como estas?", "English")
	require.NoError(t, err)
	assert.Equal(t, "Hello, how are you?", got)
}

func TestTranslate_EmptyInputSkipsRequest(t *testing.T) {
	called := false
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = io.WriteString(w, okResponse("x"))
	}))
	got, err := c.Translate(context.Background(), "   ", "en")
	require.NoError(t, err)
	assert.Equal(t, "", got)
	assert.False(t, called, "no HTTP request should be made for empty input")
}

func TestTranslate_DefaultsTargetLangToEnglish(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wireRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Len(t, req.Messages, 2)
		assert.Contains(t, req.Messages[0].Content, "en")
		_, _ = io.WriteString(w, okResponse("ok"))
	}))
	_, err := c.Translate(context.Background(), "texte", "")
	require.NoError(t, err)
}

func TestComplete_SendsBearerAuthWhenConfigured(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer byok-123", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, okResponse("ok"))
	}), WithAPIKey("byok-123"))
	_, err := c.Complete(context.Background(), "sys", "hallo")
	require.NoError(t, err)
}

func TestComplete_HonoursModelOverride(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wireRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "gpt-custom-1", req.Model)
		_, _ = io.WriteString(w, okResponse("ok"))
	}), WithModel("gpt-custom-1"))
	_, err := c.Complete(context.Background(), "sys", "hallo")
	require.NoError(t, err)
}

func TestComplete_Unauthorized(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"invalid api key"}}`)
	}))
	_, err := c.Complete(context.Background(), "sys", "hallo")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestTranslate_Unauthorized(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"invalid api key"}}`)
	}))
	_, err := c.Translate(context.Background(), "hallo", "en")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestComplete_APIErrorWrapsMessage(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"model: bogus"}}`)
	}))
	_, err := c.Complete(context.Background(), "sys", "hallo")
	require.ErrorIs(t, err, ErrAPI)
	assert.Contains(t, err.Error(), "model: bogus")
}

func TestComplete_APIErrorWithoutEnvelope(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `upstream exploded`)
	}))
	_, err := c.Complete(context.Background(), "sys", "hallo")
	require.ErrorIs(t, err, ErrAPI)
	assert.Contains(t, err.Error(), "status 500")
}

func TestComplete_EmptyChoicesReturnsEmpty(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	got, err := c.Complete(context.Background(), "sys", "hallo")
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

func TestNew_Defaults(t *testing.T) {
	c := New()
	assert.Equal(t, DefaultBaseURL, c.baseURL)
	assert.Equal(t, DefaultModel, c.model)
	assert.Empty(t, c.apiKey)
}

func TestWithBaseURL_TrimsTrailingSlash(t *testing.T) {
	c := New(WithBaseURL("http://example.test:9000/"))
	assert.Equal(t, "http://example.test:9000", c.baseURL)
	assert.False(t, strings.HasSuffix(c.baseURL, "/"))
}
