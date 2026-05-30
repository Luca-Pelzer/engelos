package middleware

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Memory note: buckets are created per client IP on first use and evicted
// opportunistically on access once idle past the TTL, so the map stays bounded
// without a background sweeper goroutine.
type rateLimiter struct {
	rps   float64
	burst float64
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

// RateLimitOption customizes a RateLimit middleware.
type RateLimitOption func(*rateLimiter)

// WithClock overrides the limiter's time source. Intended for tests so the
// refill schedule can be advanced deterministically.
func WithClock(now func() time.Time) RateLimitOption {
	return func(rl *rateLimiter) {
		if now != nil {
			rl.now = now
		}
	}
}

// RateLimit returns middleware allowing at most rps requests/sec per client
// IP with the given burst; over-limit requests get 429 + Retry-After + JSON
// {"error":"rate_limited"}. Keyed on r.RemoteAddr (chi RealIP normalizes it
// upstream).
func RateLimit(rps float64, burst int, opts ...RateLimitOption) func(http.Handler) http.Handler {
	rl := &rateLimiter{
		rps:     rps,
		burst:   float64(burst),
		ttl:     10 * refillWindow(rps),
		now:     time.Now,
		buckets: make(map[string]*bucket),
	}
	for _, opt := range opts {
		opt(rl)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rl.allow(clientKey(r)) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Retry-After", strconv.Itoa(rl.retryAfterSeconds()))
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
		})
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	rl.evictExpired(now)

	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{tokens: rl.burst, last: now}
		rl.buckets[key] = b
	} else {
		elapsed := now.Sub(b.last).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * rl.rps
			if b.tokens > rl.burst {
				b.tokens = rl.burst
			}
			b.last = now
		}
	}

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func (rl *rateLimiter) evictExpired(now time.Time) {
	for k, b := range rl.buckets {
		if now.Sub(b.last) > rl.ttl {
			delete(rl.buckets, k)
		}
	}
}

func (rl *rateLimiter) retryAfterSeconds() int {
	if rl.rps <= 0 {
		return 1
	}
	s := int(1.0/rl.rps + 0.999)
	if s < 1 {
		s = 1
	}
	return s
}

func refillWindow(rps float64) time.Duration {
	if rps <= 0 {
		return time.Second
	}
	return time.Duration(float64(time.Second) / rps)
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
