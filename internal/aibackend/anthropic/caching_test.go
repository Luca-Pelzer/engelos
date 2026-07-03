package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/aibackend/usage"
)

func TestPromptCaching_EnabledSendsCacheControlBlock(t *testing.T) {
	var gotSystem json.RawMessage
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw struct {
			System json.RawMessage `json:"system"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		gotSystem = raw.System
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"OK"}]}`)
	}), WithPromptCaching(true))

	_, err := c.Complete(context.Background(), "SYSTEM-PREFIX", "hi")
	require.NoError(t, err)

	s := string(gotSystem)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(s), "["), "system should be a content-block array: %s", s)
	assert.Contains(t, s, "cache_control")
	assert.Contains(t, s, "ephemeral")
	assert.Contains(t, s, "SYSTEM-PREFIX")
}

func TestPromptCaching_DisabledSendsPlainStringSystem(t *testing.T) {
	var gotSystem json.RawMessage
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw struct {
			System json.RawMessage `json:"system"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		gotSystem = raw.System
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"OK"}]}`)
	}), WithPromptCaching(false))

	_, err := c.Complete(context.Background(), "SYSTEM-PREFIX", "hi")
	require.NoError(t, err)

	assert.NotContains(t, string(gotSystem), "cache_control")
	var sys string
	require.NoError(t, json.Unmarshal(gotSystem, &sys), "system must be a plain JSON string")
	assert.Equal(t, "SYSTEM-PREFIX", sys)
}

func TestPromptCaching_RejectionRetriesWithoutAndDisables(t *testing.T) {
	var bodies []string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if strings.Contains(string(b), "cache_control") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"cache_control: unsupported by this endpoint"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"OK"}]}`)
	}), WithPromptCaching(true))

	// First call: the cache attempt 400s, then the client retries without it.
	out, err := c.Complete(context.Background(), "SYS", "hi")
	require.NoError(t, err)
	assert.Equal(t, "OK", out)
	require.Len(t, bodies, 2, "expected one cache attempt + one cacheless retry")
	assert.Contains(t, bodies[0], "cache_control")
	assert.NotContains(t, bodies[1], "cache_control")

	// Second call: caching is latched off for the process, so a single request.
	bodies = nil
	out, err = c.Complete(context.Background(), "SYS", "hi2")
	require.NoError(t, err)
	assert.Equal(t, "OK", out)
	require.Len(t, bodies, 1, "expected a single request once caching is disabled")
	assert.NotContains(t, bodies[0], "cache_control")
}

func TestUsage_ExtractedFromResponse(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"OK"}],"usage":{"input_tokens":40,"output_tokens":12,"cache_read_input_tokens":30}}`)
	}))

	reg := usage.NewRegistry()
	wrapped := usage.Wrap(c, reg, "unit")
	_, err := wrapped.Complete(context.Background(), "sys", "hi")
	require.NoError(t, err)

	snap := reg.Snapshot()
	assert.Equal(t, int64(40), snap.TokensIn)
	assert.Equal(t, int64(12), snap.TokensOut)
	assert.Equal(t, int64(30), snap.CacheReadTokens)
	assert.Equal(t, int64(1), snap.ByConsumer["unit"].Calls)
}
