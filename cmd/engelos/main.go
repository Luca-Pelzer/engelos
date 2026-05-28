// Package main is the entry point of the engelOS daemon.
//
// engelOS is an open-source streaming bot. This binary starts the core daemon,
// which connects to streaming platforms (Twitch, Discord, YouTube, Kick) and
// exposes an HTTP/WebSocket API on :8080 for the TUI, web dashboard, and
// native GUI to talk to.
//
// License: AGPL-3.0 (see LICENSE in repo root).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/engelswtf/engelos/internal/api"
	"github.com/engelswtf/engelos/internal/api/handlers"
	"github.com/engelswtf/engelos/internal/api/ws"
	"github.com/engelswtf/engelos/internal/server"
)

// Version is set at build time via -ldflags "-X main.Version=...".
var Version = "0.0.0-dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("engelOS starting",
		"version", Version,
		"phase", "0 — skeleton",
	)

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt, syscall.SIGTERM,
	)
	defer cancel()

	if err := run(ctx, logger); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
	slog.Info("engelOS stopped cleanly")
}

func run(ctx context.Context, logger *slog.Logger) error {
	hub := ws.NewHub(logger)
	go hub.Run(ctx)

	router := api.NewRouter(api.Deps{
		Logger: logger,
		Version: handlers.Version{
			Version: Version,
			Phase:   "0",
		},
		WS: hub,
	})

	srv := server.New(server.Config{
		Addr:     "127.0.0.1:8080",
		AllowLAN: false,
		Logger:   logger,
	}, router)

	return srv.Run(ctx)
}
