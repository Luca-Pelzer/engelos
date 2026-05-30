package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func doReq(h http.Handler, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestRateLimit_WithinBurstPasses(t *testing.T) {
	t.Parallel()
	h := apimw.RateLimit(1, 5)(okHandler())
	for i := 0; i < 5; i++ {
		w := doReq(h, "10.0.0.1:1234")
		assert.Equal(t, http.StatusOK, w.Code, "request %d should pass within burst", i)
	}
}

func TestRateLimit_OverLimitReturns429(t *testing.T) {
	t.Parallel()
	h := apimw.RateLimit(1, 5)(okHandler())
	for i := 0; i < 5; i++ {
		require.Equal(t, http.StatusOK, doReq(h, "10.0.0.2:9").Code)
	}

	w := doReq(h, "10.0.0.2:9")
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.JSONEq(t, `{"error":"rate_limited"}`, w.Body.String())
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

func TestRateLimit_RefillAfterClockAdvance(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(d)
	}

	h := apimw.RateLimit(1, 1, apimw.WithClock(clock))(okHandler())

	require.Equal(t, http.StatusOK, doReq(h, "10.0.0.3:1").Code)
	require.Equal(t, http.StatusTooManyRequests, doReq(h, "10.0.0.3:1").Code)

	advance(time.Second)
	assert.Equal(t, http.StatusOK, doReq(h, "10.0.0.3:1").Code,
		"a refilled token should allow the next request")
}

func TestRateLimit_DistinctIPsIndependent(t *testing.T) {
	t.Parallel()
	h := apimw.RateLimit(1, 1)(okHandler())

	require.Equal(t, http.StatusOK, doReq(h, "10.0.0.4:1").Code)
	require.Equal(t, http.StatusTooManyRequests, doReq(h, "10.0.0.4:1").Code)

	assert.Equal(t, http.StatusOK, doReq(h, "10.0.0.5:1").Code,
		"a distinct IP must have its own bucket")
}

func TestRateLimit_ConcurrentRaceSafe(t *testing.T) {
	t.Parallel()
	h := apimw.RateLimit(1000, 1000)(okHandler())

	const n = 50
	var ok, limited atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch doReq(h, "10.0.0.6:1").Code {
			case http.StatusOK:
				ok.Add(1)
			case http.StatusTooManyRequests:
				limited.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.EqualValues(t, n, ok.Load()+limited.Load())
}
