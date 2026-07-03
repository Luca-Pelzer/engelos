package openai

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/aibackend/usage"
)

func TestUsage_ExtractedFromResponse(t *testing.T) {
	var body string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}],"usage":{"prompt_tokens":22,"completion_tokens":7}}`)
	}))

	reg := usage.NewRegistry()
	wrapped := usage.Wrap(c, reg, "unit")
	_, err := wrapped.Complete(context.Background(), "sys", "hi")
	require.NoError(t, err)

	// The request wire is unchanged: no cache_control (that is anthropic-only).
	assert.NotContains(t, body, "cache_control")

	snap := reg.Snapshot()
	assert.Equal(t, int64(22), snap.TokensIn)
	assert.Equal(t, int64(7), snap.TokensOut)
	assert.Equal(t, int64(0), snap.CacheReadTokens)
	assert.Equal(t, int64(1), snap.ByConsumer["unit"].Calls)
}
