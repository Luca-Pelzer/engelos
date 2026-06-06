package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// Config configures the HTTP Server.
type Config struct {
	// Addr is the listen address (host:port). If AllowLAN is false, the host
	// portion is forced to 127.0.0.1.
	Addr string

	// AllowedOrigins is consumed by CORS middleware in internal/api; the
	// server itself does not use it but carries it for convenience.
	AllowedOrigins []string

	// AllowLAN, when false, forces the listener to bind 127.0.0.1 regardless
	// of the host in Addr. When true, Addr is honored as-is.
	AllowLAN bool

	// ShutdownTimeout is the maximum time to wait for in-flight requests on
	// graceful shutdown. Defaults to 10s when zero.
	ShutdownTimeout time.Duration

	// Logger receives lifecycle events. Defaults to slog.Default().
	Logger *slog.Logger
}

// Server wraps net/http.Server with engelOS-flavored defaults and lifecycle.
type Server struct {
	cfg    Config
	http   *http.Server
	logger *slog.Logger

	mu      sync.RWMutex
	bindStr string
}

// New constructs a Server. The handler is mounted as-is.
func New(cfg Config, handler http.Handler) *Server {
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 10 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	bind := resolveBind(cfg.Addr, cfg.AllowLAN)

	httpSrv := &http.Server{
		Addr:              bind,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	return &Server{
		cfg:     cfg,
		http:    httpSrv,
		logger:  logger,
		bindStr: bind,
	}
}

// Addr returns the resolved bind address (post AllowLAN normalization and
// post listener resolution).
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bindStr
}

// Run starts the HTTP server and blocks until ctx is cancelled or a
// non-recoverable error occurs. http.ErrServerClosed is returned as nil.
func (s *Server) Run(ctx context.Context) error {
	s.mu.RLock()
	initialBind := s.bindStr
	s.mu.RUnlock()

	ln, err := net.Listen("tcp", initialBind)
	if err != nil {
		return fmt.Errorf("listen %s: %w", initialBind, err)
	}

	resolved := ln.Addr().String()
	s.mu.Lock()
	s.bindStr = resolved
	s.http.Addr = resolved
	s.mu.Unlock()

	s.logger.Info("http listening",
		"addr", resolved,
		"allow_lan", s.cfg.AllowLAN,
	)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- s.http.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("http shutdown requested", "cause", context.Cause(ctx))
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		s.logger.Warn("http shutdown error", "err", err)
		return fmt.Errorf("shutdown: %w", err)
	}

	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve after shutdown: %w", err)
	}

	s.logger.Info("http stopped")
	return nil
}

// resolveBind enforces loopback-only binding unless AllowLAN is true.
// If Addr has no host part (":8080"), the host defaults to 127.0.0.1 when
// AllowLAN is false, or 0.0.0.0 when true.
func resolveBind(addr string, allowLAN bool) string {
	if addr == "" {
		if allowLAN {
			return "0.0.0.0:8080"
		}
		return "127.0.0.1:8080"
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if allowLAN {
			return "0.0.0.0" + addr
		}
		return "127.0.0.1" + addr
	}

	if !allowLAN {
		host = "127.0.0.1"
	} else if host == "" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, port)
}
