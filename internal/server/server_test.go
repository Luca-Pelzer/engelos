package server_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/engelos-bot/engelos/internal/server"
)

func TestServer_RunServesAndShutsDown(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	srv := server.New(server.Config{
		Addr:            "127.0.0.1:0",
		ShutdownTimeout: time.Second,
	}, mux)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	require.Eventually(t, func() bool {
		return srv.Addr() != "127.0.0.1:0" && srv.Addr() != ""
	}, 2*time.Second, 10*time.Millisecond)

	resp, err := http.Get("http://" + srv.Addr() + "/healthz")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", string(body))

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}

func TestServer_LoopbackBindingWhenLANDisallowed(t *testing.T) {
	t.Parallel()

	srv := server.New(server.Config{
		Addr:     "0.0.0.0:0",
		AllowLAN: false,
	}, http.NewServeMux())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	require.Eventually(t, func() bool { return srv.Addr() != "" && !strings.HasSuffix(srv.Addr(), ":0") }, 2*time.Second, 10*time.Millisecond)
	assert.True(t, strings.HasPrefix(srv.Addr(), "127.0.0.1:"),
		"expected loopback bind, got %s", srv.Addr())

	cancel()
	<-done
}

func TestServer_BadAddrReturnsError(t *testing.T) {
	t.Parallel()

	srv := server.New(server.Config{
		Addr: "127.0.0.1:not-a-port",
	}, http.NewServeMux())

	err := srv.Run(context.Background())
	require.Error(t, err)
}
