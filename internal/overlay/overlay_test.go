package overlay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/overlay/", Handler(nil))
	return httptest.NewServer(mux)
}

func get(t *testing.T, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp, string(body)
}

func TestEventsOverlay(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, body := get(t, srv, "/overlay/events")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if !strings.Contains(body, "engelOS / events") {
		t.Errorf("body missing overlay title marker")
	}
	if !strings.Contains(body, "/api/v1/ws") {
		t.Errorf("body missing /api/v1/ws reference")
	}
	if !strings.Contains(body, "message.created") {
		t.Errorf("body missing message.created handler")
	}
}

func TestLeaderboardOverlay(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, body := get(t, srv, "/overlay/leaderboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "/api/v1/streak/leaderboard") {
		t.Errorf("body missing /api/v1/streak/leaderboard reference")
	}
	if !strings.Contains(body, "feature.streak.milestone") {
		t.Errorf("body missing milestone refresh handler")
	}
}

func TestAlertsOverlay(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, body := get(t, srv, "/overlay/alerts")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "engelOS / alerts") {
		t.Errorf("body missing overlay title marker")
	}
	if !strings.Contains(body, "/api/v1/ws") {
		t.Errorf("body missing /api/v1/ws reference")
	}
	if !strings.Contains(body, "channel.raided") {
		t.Errorf("body missing channel.raided handler")
	}
}

func TestIndexOverlay(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, body := get(t, srv, "/overlay/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	for _, want := range []string{
		"/overlay/events",
		"/overlay/alerts",
		"/overlay/leaderboard",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing link %q", want)
		}
	}
}

func TestUnknownOverlayReturns404HTML(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, body := get(t, srv, "/overlay/doesnotexist")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html (NOT json)", ct)
	}
	if strings.Contains(ct, "json") {
		t.Errorf("404 must not be json, got %q", ct)
	}
	if !strings.Contains(body, "<html") {
		t.Errorf("404 body should be html, got %q", body)
	}
}

func TestServedOverlayCacheControl(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	for _, p := range []string{"/overlay/events", "/overlay/alerts", "/overlay/leaderboard", "/overlay/"} {
		resp, _ := get(t, srv, p)
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s Cache-Control = %q, want no-store", p, cc)
		}
	}
}

func TestRejectsNonGET(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/overlay/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST status = %d, want 404", resp.StatusCode)
	}
}
