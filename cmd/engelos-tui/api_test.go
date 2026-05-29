package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestNewClient(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{"valid", "http://127.0.0.1:8080", false},
		{"trims trailing slash", "http://127.0.0.1:8080/", false},
		{"empty errors", "  ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewClient(tc.baseURL, false)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.BaseURL() == "" {
				t.Fatalf("BaseURL empty")
			}
			if c.HTTPClient() == nil {
				t.Fatalf("HTTPClient nil")
			}
		})
	}
}

func TestLogin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		statusCode int
		body       string
		setCookie  bool
		wantUser   string
		wantErr    error
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			body:       `{"user":{"id":"u1","email":"a@b.c","username":"luca"}}`,
			setCookie:  true,
			wantUser:   "luca",
		},
		{
			name:       "invalid credentials",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":"invalid_credentials"}`,
			wantErr:    ErrInvalidCredentials,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":"oops"}`,
			wantErr:    errors.New("generic"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST got %s", r.Method)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("missing Content-Type header")
				}
				body, _ := readAll(r.Body)
				var p map[string]string
				_ = json.Unmarshal(body, &p)
				if p["email"] == "" || p["password"] == "" {
					t.Errorf("missing fields in body: %s", body)
				}
				if tc.setCookie {
					http.SetCookie(w, &http.Cookie{Name: "engelos_session", Value: "sess-token", Path: "/"})
				}
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			})
			ts := httptest.NewServer(mux)
			defer ts.Close()

			c, err := NewClient(ts.URL, false)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			user, err := c.Login(ctx, "a@b.c", "secret")
			switch {
			case tc.wantErr != nil && errors.Is(tc.wantErr, ErrInvalidCredentials):
				if !errors.Is(err, ErrInvalidCredentials) {
					t.Fatalf("want ErrInvalidCredentials, got %v", err)
				}
			case tc.wantErr != nil:
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if user.Username != tc.wantUser {
					t.Fatalf("user=%q want %q", user.Username, tc.wantUser)
				}
				if !cookiePresent(c, ts.URL, "engelos_session") {
					t.Fatalf("session cookie not stored in jar")
				}
			}
		})
	}
}

func TestStatsAndStatus(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/stats", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"version":"0.1.0","phase":"alpha",
			"dispatcher":{"messages":12,"subscriptions":3,"raids":1,"pity_grant_errors":0,"streak_tick_errors":0,"last_event_at":"2025-01-02T03:04:05Z"}
		}`))
	})
	mux.HandleFunc("/api/v1/streak/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("channel") != "engelswtf" {
			t.Errorf("expected channel query, got %q", r.URL.Query().Get("channel"))
		}
		_, _ = w.Write([]byte(`{
			"channel":"engelswtf","limit":10,
			"entries":[{"viewer_id":"v1","username":"luca","days_current":7}]
		}`))
	})
	mux.HandleFunc("/api/v1/streak/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"days_current":5,"days_longest":12,"freezes_available":1,"next_milestone":7}`))
	})
	mux.HandleFunc("/api/v1/pity/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("channel") != "engelswtf" {
			t.Errorf("expected channel query, got %q", r.URL.Query().Get("channel"))
		}
		_, _ = w.Write([]byte(`{
			"channel":"engelswtf","limit":10,
			"entries":[{"viewer_id":"v1","username":"luca","points":42}]
		}`))
	})
	mux.HandleFunc("/api/v1/pity/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"points":42,"hard_pity_threshold":90,"effective_chance":0.06}`))
	})
	mux.HandleFunc("/api/v1/users/me", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"u1","email":"a@b.c","username":"luca"}`))
	})
	mux.HandleFunc("/api/v1/auth/logout", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()

	s, err := c.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Version != "0.1.0" || s.Phase != "alpha" {
		t.Fatalf("unexpected stats payload: %+v", s)
	}
	if s.Dispatcher.Messages != 12 || s.Dispatcher.Subscriptions != 3 || s.Dispatcher.Raids != 1 {
		t.Fatalf("dispatcher counters wrong: %+v", s.Dispatcher)
	}

	board, err := c.StreakLeaderboard(ctx, "engelswtf", 10)
	if err != nil {
		t.Fatalf("StreakLeaderboard: %v", err)
	}
	if len(board) != 1 || board[0].Username != "luca" || board[0].DaysCurrent != 7 {
		t.Fatalf("leaderboard wrong: %+v", board)
	}

	pityBoard, err := c.PityLeaderboard(ctx, "engelswtf", 10)
	if err != nil {
		t.Fatalf("PityLeaderboard: %v", err)
	}
	if len(pityBoard) != 1 || pityBoard[0].Username != "luca" || pityBoard[0].Points != 42 {
		t.Fatalf("pity leaderboard wrong: %+v", pityBoard)
	}

	streakSt, err := c.StreakStatus(ctx, "engelswtf", "v1")
	if err != nil {
		t.Fatalf("StreakStatus: %v", err)
	}
	if streakSt.DaysCurrent != 5 || streakSt.DaysLongest != 12 {
		t.Fatalf("streak status wrong: %+v", streakSt)
	}

	pitySt, err := c.PityStatus(ctx, "engelswtf", "v1")
	if err != nil {
		t.Fatalf("PityStatus: %v", err)
	}
	if pitySt.Points != 42 || pitySt.HardPityThreshold != 90 {
		t.Fatalf("pity status wrong: %+v", pitySt)
	}

	me, err := c.Me(ctx)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.Email != "a@b.c" {
		t.Fatalf("Me wrong: %+v", me)
	}

	if err := c.Logout(ctx); err != nil {
		t.Fatalf("Logout: %v", err)
	}
}

func TestUnauthorizedSurfacesAsErrInvalidCredentials(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer ts.Close()
	c, _ := NewClient(ts.URL, false)
	_, err := c.Me(context.Background())
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}
}

func TestStatsServerError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer ts.Close()
	c, _ := NewClient(ts.URL, false)
	_, err := c.Stats(context.Background())
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should mention status: %v", err)
	}
}

func TestStreamWebSocket(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") }()
		_ = conn.Write(r.Context(), websocket.MessageText,
			[]byte(`{"type":"message.created","data":{"platform":"twitch","channel":"x","message":{"username":"u","content":"hi"}}}`))
		time.Sleep(50 * time.Millisecond)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, _ := NewClient(ts.URL, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := c.StreamWebSocket(ctx)
	if err != nil {
		t.Fatalf("StreamWebSocket: %v", err)
	}
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatalf("channel closed without event")
		}
		if ev.Type != "message.created" {
			t.Fatalf("event type=%q want message.created", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for event")
	}
	cancel()
	for range ch {
	}
}

func TestBuildWSURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		base, path, want string
		wantErr          bool
	}{
		{"http://127.0.0.1:8080", "/api/v1/ws", "ws://127.0.0.1:8080/api/v1/ws", false},
		{"https://example.com", "/api/v1/ws", "wss://example.com/api/v1/ws", false},
		{"ftp://nope", "/x", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			got, err := buildWSURL(tc.base, tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected err")
				}
				return
			}
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func readAll(r interface{ Read(p []byte) (int, error) }) ([]byte, error) {
	buf := make([]byte, 0, 256)
	tmp := make([]byte, 256)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, err
		}
	}
}

func cookiePresent(c *Client, base, name string) bool {
	jar := c.HTTPClient().Jar
	if jar == nil {
		return false
	}
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	for _, ck := range jar.Cookies(u) {
		if ck.Name == name && ck.Value != "" {
			return true
		}
	}
	return false
}
