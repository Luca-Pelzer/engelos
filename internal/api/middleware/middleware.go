// Package middleware contains engelOS-specific HTTP middleware (CORS,
// security headers, structured logging, JSON content-type).
package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// CORSOptions controls the CORS middleware.
type CORSOptions struct {
	AllowedOrigins   []string
	AllowCredentials bool
	MaxAge           int
}

// CORS returns a middleware enforcing the given CORS policy.
// AllowedOrigins is a list of exact origins (scheme+host[+port]). The literal
// "*" matches every origin; it is incompatible with AllowCredentials per spec
// and will silently disable credentials in that case.
func CORS(opts CORSOptions) func(http.Handler) http.Handler {
	allowAll := false
	allowed := make(map[string]struct{}, len(opts.AllowedOrigins))
	for _, o := range opts.AllowedOrigins {
		o = strings.TrimSpace(o)
		if o == "*" {
			allowAll = true
			continue
		}
		if o != "" {
			allowed[o] = struct{}{}
		}
	}
	maxAge := opts.MaxAge
	if maxAge <= 0 {
		maxAge = 600
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				w.Header().Add("Vary", "Origin")
				if allowAll {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else if _, ok := allowed[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					if opts.AllowCredentials {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
				}
			}

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
				if reqHdrs := r.Header.Get("Access-Control-Request-Headers"); reqHdrs != "" {
					w.Header().Set("Access-Control-Allow-Headers", reqHdrs)
				} else {
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
				}
				w.Header().Set("Access-Control-Max-Age", itoa(maxAge))
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// JSONContentType sets Content-Type: application/json on responses for paths
// that begin with /api/. It does not override an explicit Content-Type set by
// the handler.
func JSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			jw := &headerSetterWriter{ResponseWriter: w, contentType: "application/json; charset=utf-8"}
			next.ServeHTTP(jw, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type headerSetterWriter struct {
	http.ResponseWriter
	contentType string
	wrote       bool
}

func (h *headerSetterWriter) WriteHeader(code int) {
	if !h.wrote {
		if h.Header().Get("Content-Type") == "" {
			h.Header().Set("Content-Type", h.contentType)
		}
		h.wrote = true
	}
	h.ResponseWriter.WriteHeader(code)
}

func (h *headerSetterWriter) Write(b []byte) (int, error) {
	if !h.wrote {
		if h.Header().Get("Content-Type") == "" {
			h.Header().Set("Content-Type", h.contentType)
		}
		h.wrote = true
	}
	return h.ResponseWriter.Write(b)
}

// Flush forwards Flusher calls (needed for SSE).
func (h *headerSetterWriter) Flush() {
	if f, ok := h.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// SecurityHeaders sets baseline security response headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"connect-src 'self' ws: wss:; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

// Logger emits a structured slog entry per request once the response is done.
// It also surfaces the chi RequestID on the response as X-Request-Id so
// clients can correlate.
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			reqID := chimw.GetReqID(r.Context())
			if reqID != "" {
				w.Header().Set("X-Request-Id", reqID)
			}
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", reqID),
				slog.String("remote", r.RemoteAddr),
			)
		})
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
