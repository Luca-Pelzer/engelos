package main

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	c, err := NewClient("http://127.0.0.1:0", false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return NewModel(c, "", "")
}

func TestViewSwitchMutatesState(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.state = viewDashboard
	updated, _ := m.Update(ViewSwitchMsg{Target: viewLeaderboard})
	got := updated.(Model)
	if got.state != viewLeaderboard {
		t.Fatalf("state=%v want %v", got.state, viewLeaderboard)
	}
	if got.prevView != viewDashboard {
		t.Fatalf("prevView=%v want %v", got.prevView, viewDashboard)
	}
}

func TestStatsResultUpdatesDashboard(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.state = viewDashboard
	msg := StatsResultMsg{
		Stats: Stats{
			Version: "9.9.9",
			Phase:   "beta",
			Dispatcher: DispatcherStats{
				Messages: 100, Subscriptions: 5, Raids: 2,
				PityGrantErrors: 1, StreakTickErrors: 0,
				LastEventAt: time.Now(),
			},
		},
	}
	updated, _ := m.Update(msg)
	got := updated.(Model)
	if got.dashboard.version != "9.9.9" || got.dashboard.phase != "beta" {
		t.Fatalf("dashboard meta wrong: %+v", got.dashboard)
	}
	if got.dashboard.current.Messages != 100 {
		t.Fatalf("messages=%d want 100", got.dashboard.current.Messages)
	}

	deltaMsg := StatsResultMsg{
		Stats: Stats{Dispatcher: DispatcherStats{Messages: 150}},
	}
	updated2, _ := got.Update(deltaMsg)
	got2 := updated2.(Model)
	if got2.dashboard.previous.Messages != 100 {
		t.Fatalf("previous not retained: %+v", got2.dashboard)
	}
	if got2.dashboard.current.Messages != 150 {
		t.Fatalf("current not advanced: %+v", got2.dashboard)
	}
}

func TestStatsResultErrorIsRecorded(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.state = viewDashboard
	updated, _ := m.Update(StatsResultMsg{Err: errors.New("boom")})
	got := updated.(Model)
	if got.dashboard.lastErr == nil || got.dashboard.lastErr.Error() != "boom" {
		t.Fatalf("error not recorded: %+v", got.dashboard.lastErr)
	}
}

func TestLoginSuccessAdvancesState(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	if m.state != viewLogin {
		t.Fatalf("initial state %v want viewLogin", m.state)
	}
	updated, _ := m.Update(LoginSuccessMsg{User: User{Username: "luca", Email: "a@b.c"}})
	got := updated.(Model)
	if got.state != viewDashboard {
		t.Fatalf("state=%v want viewDashboard", got.state)
	}
	if got.user.Username != "luca" {
		t.Fatalf("user not stored: %+v", got.user)
	}
	if got.wsCancel != nil {
		got.wsCancel()
	}
}

func TestLoginFailedSurfacesError(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	updated, _ := m.Update(LoginFailedMsg{Err: ErrInvalidCredentials})
	got := updated.(Model)
	if got.login.err == nil {
		t.Fatalf("expected login.err set")
	}
	if got.login.submitting {
		t.Fatalf("submitting should reset")
	}
}

func TestHelpToggle(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	if m.showHelp {
		t.Fatalf("help should start closed")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	got := updated.(Model)
	if !got.showHelp {
		t.Fatalf("help should toggle on")
	}
	updated2, _ := got.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got2 := updated2.(Model)
	if got2.showHelp {
		t.Fatalf("esc should close help")
	}
}

func TestChatAppendBuffersLastN(t *testing.T) {
	t.Parallel()
	cm := newChatModel()
	for i := 0; i < chatBufferCap+50; i++ {
		payload, _ := json.Marshal(map[string]any{
			"platform": "twitch",
			"channel":  "engelswtf",
			"message":  map[string]any{"username": "user", "content": "hi"},
		})
		cm.append(WSEvent{Type: "message.created", Data: payload})
	}
	if len(cm.lines) != chatBufferCap {
		t.Fatalf("buffer len=%d want %d", len(cm.lines), chatBufferCap)
	}
}

func TestChatIgnoresNonMessageEvents(t *testing.T) {
	t.Parallel()
	cm := newChatModel()
	cm.append(WSEvent{Type: "user.subscribed", Data: []byte(`{}`)})
	if len(cm.lines) != 0 {
		t.Fatalf("non-chat event should be ignored, got %d lines", len(cm.lines))
	}
}

func TestLeaderboardApplyPopulatesRows(t *testing.T) {
	t.Parallel()
	lbm := newLeaderboardModel("engelswtf")
	lbm.apply(LeaderboardResultMsg{
		Pity: []LeaderboardEntry{
			{Username: "alice", Points: 80},
			{Username: "bob", Points: 60},
		},
		Streak: []LeaderboardEntry{
			{Username: "carol", DaysCurrent: 12},
		},
	})
	if got := len(lbm.pityTable.Rows()); got != 2 {
		t.Fatalf("pity rows=%d want 2", got)
	}
	if got := len(lbm.strkTable.Rows()); got != 1 {
		t.Fatalf("streak rows=%d want 1", got)
	}
}

func TestLeaderboardApplyErrorRecorded(t *testing.T) {
	t.Parallel()
	lbm := newLeaderboardModel("engelswtf")
	lbm.apply(LeaderboardResultMsg{Err: errors.New("nope")})
	if lbm.lastErr == nil {
		t.Fatalf("expected lastErr set")
	}
}

func TestNonEmpty(t *testing.T) {
	t.Parallel()
	if got := nonEmpty("", "fallback"); got != "fallback" {
		t.Fatalf("got %q want fallback", got)
	}
	if got := nonEmpty("real", "fallback"); got != "real" {
		t.Fatalf("got %q want real", got)
	}
}

func TestDefaultChannelFromEnv(t *testing.T) {
	t.Setenv("ENGELOS_TWITCH_CHANNELS", "chan1,chan2")
	if got := defaultChannel(); got != "chan1" {
		t.Fatalf("got %q want chan1", got)
	}
	t.Setenv("ENGELOS_TWITCH_CHANNELS", "")
	if got := defaultChannel(); got != "engelswtf" {
		t.Fatalf("got %q want engelswtf", got)
	}
}

func TestWSReadyStoresChannel(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	ch := make(chan WSEvent, 1)
	close(ch)
	updated, cmd := m.Update(WSReadyMsg{Ch: ch})
	got := updated.(Model)
	if got.wsCh == nil {
		t.Fatalf("wsCh should be set")
	}
	if cmd == nil {
		t.Fatalf("expected awaitWS cmd")
	}
}

func TestWSReadyErrorBubblesToChat(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	updated, _ := m.Update(WSReadyMsg{Err: errors.New("dial failed")})
	got := updated.(Model)
	if got.chat.connectionErr == nil {
		t.Fatalf("expected chat.connectionErr set")
	}
}

func TestKeyMatches(t *testing.T) {
	t.Parallel()
	if !keyMatches(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}, "r") {
		t.Fatalf("should match r")
	}
	if keyMatches(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}, "r") {
		t.Fatalf("should not match x against r")
	}
	if keyMatches(StatsTickMsg(time.Now()), "r") {
		t.Fatalf("non-KeyMsg should never match")
	}
}
