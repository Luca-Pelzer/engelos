package claude

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

func TestTranslate_PostsExpectedRequestAndReturnsText(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		// Proxy mode: no x-api-key header is sent.
		assert.Empty(t, r.Header.Get("x-api-key"))

		var req wireRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, DefaultModel, req.Model)
		assert.Equal(t, maxOutputTokens, req.MaxTokens)
		assert.Equal(t, float64(0), req.Temperature)
		assert.Contains(t, req.System, "English")
		assert.Contains(t, req.System, "Output ONLY")
		require.Len(t, req.Messages, 1)
		assert.Equal(t, "user", req.Messages[0].Role)
		assert.Equal(t, "Hola, como estas?", req.Messages[0].Content)

		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"Hello, how are you?"}]}`)
	}))

	got, err := c.Translate(context.Background(), "Hola, como estas?", "English")
	require.NoError(t, err)
	assert.Equal(t, "Hello, how are you?", got)
}

func TestTranslate_TrimsWhitespaceAndJoinsTextBlocks(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"content":[
			{"type":"text","text":"  Hello"},
			{"type":"text","text":", world  "}
		]}`)
	}))
	got, err := c.Translate(context.Background(), "Hallo, Welt", "en")
	require.NoError(t, err)
	assert.Equal(t, "Hello, world", got)
}

func TestTranslate_EmptyInputSkipsRequest(t *testing.T) {
	called := false
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"x"}]}`)
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
		assert.Contains(t, req.System, "en")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	_, err := c.Translate(context.Background(), "texte", "")
	require.NoError(t, err)
}

func TestTranslate_SendsAPIKeyWhenConfigured(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "byok-123", r.Header.Get("x-api-key"))
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}), WithAPIKey("byok-123"))
	_, err := c.Translate(context.Background(), "hallo", "en")
	require.NoError(t, err)
}

func TestTranslate_HonoursModelOverride(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wireRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "claude-custom-1", req.Model)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}), WithModel("claude-custom-1"))
	_, err := c.Translate(context.Background(), "hallo", "en")
	require.NoError(t, err)
}

func TestTranslate_Unauthorized(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"type":"authentication_error","message":"token expired"}}`)
	}))
	_, err := c.Translate(context.Background(), "hallo", "en")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestTranslate_APIErrorWrapsMessage(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"type":"not_found_error","message":"model: bogus"}}`)
	}))
	_, err := c.Translate(context.Background(), "hallo", "en")
	require.ErrorIs(t, err, ErrAPI)
	assert.Contains(t, err.Error(), "model: bogus")
}

func TestTranslate_APIErrorWithoutEnvelope(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `upstream exploded`)
	}))
	_, err := c.Translate(context.Background(), "hallo", "en")
	require.ErrorIs(t, err, ErrAPI)
	assert.Contains(t, err.Error(), "status 500")
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
