package cohost

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeBackend struct {
	mu       sync.Mutex
	calls    int32
	out      string
	err      error
	lastUser string
	lastSys  string
}

func (f *fakeBackend) Complete(_ context.Context, systemPrompt, userText string) (string, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	f.lastUser = userText
	f.lastSys = systemPrompt
	f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	return f.out, nil
}

func (f *fakeBackend) count() int { return int(atomic.LoadInt32(&f.calls)) }

func cfg(botName string, maxLen int) Config {
	return Config{TenantID: "local", Channel: "c", Enabled: true, BotName: botName, Persona: "a helper", MaxReplyLen: maxLen}
}

func TestRespond_AddressedByName(t *testing.T) {
	b := &fakeBackend{out: "42 of course"}
	r := NewResponder(b, Options{})
	reply, ok, err := r.Respond(context.Background(), cfg("engel", 280), "u1", "u1", "engel, what is the answer?")
	if err != nil || !ok {
		t.Fatalf("want answered, got ok=%v err=%v", ok, err)
	}
	if reply != "42 of course" {
		t.Fatalf("unexpected reply %q", reply)
	}
	if b.lastUser != "what is the answer?" {
		t.Fatalf("prefix not stripped: %q", b.lastUser)
	}
}

func TestRespond_AddressedByAskPrefix(t *testing.T) {
	b := &fakeBackend{out: "yes"}
	r := NewResponder(b, Options{})
	_, ok, err := r.Respond(context.Background(), cfg("engel", 280), "u1", "u1", "!ask are you there")
	if err != nil || !ok {
		t.Fatalf("want answered, got ok=%v err=%v", ok, err)
	}
	if b.lastUser != "are you there" {
		t.Fatalf("ask prefix not stripped: %q", b.lastUser)
	}
}

func TestRespond_NotAddressed(t *testing.T) {
	b := &fakeBackend{out: "should not be used"}
	r := NewResponder(b, Options{})
	cases := []string{"just chatting", "botanist is a job", "hello world", "!points", ""}
	for _, text := range cases {
		_, ok, err := r.Respond(context.Background(), cfg("bot", 280), "u1", "u1", text)
		if err != nil || ok {
			t.Fatalf("text %q: want not-answered, got ok=%v err=%v", text, ok, err)
		}
	}
	if b.count() != 0 {
		t.Fatalf("backend must not be called, got %d", b.count())
	}
}

func TestRespond_TruncatesToMaxRunes(t *testing.T) {
	long := strings.Repeat("a", 500)
	b := &fakeBackend{out: long}
	r := NewResponder(b, Options{})
	reply, ok, err := r.Respond(context.Background(), cfg("bot", 50), "u1", "u1", "bot say something")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if len([]rune(reply)) != 50 {
		t.Fatalf("want 50 runes, got %d", len([]rune(reply)))
	}
}

func TestRespond_EmptyReplyNotAnswered(t *testing.T) {
	b := &fakeBackend{out: "   "}
	r := NewResponder(b, Options{})
	_, ok, err := r.Respond(context.Background(), cfg("bot", 280), "u1", "u1", "bot hello")
	if err != nil || ok {
		t.Fatalf("empty reply should not be answered: ok=%v err=%v", ok, err)
	}
}

func TestRespond_BackendErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	b := &fakeBackend{err: sentinel}
	r := NewResponder(b, Options{})
	_, ok, err := r.Respond(context.Background(), cfg("bot", 280), "u1", "u1", "bot hello there")
	if ok || !errors.Is(err, sentinel) {
		t.Fatalf("want backend error, got ok=%v err=%v", ok, err)
	}
}

func TestRespond_PerUserRateLimit(t *testing.T) {
	b := &fakeBackend{out: "hi"}
	r := NewResponder(b, Options{PerUserEvery: time.Hour, PerUserBurst: 1})
	_, ok1, _ := r.Respond(context.Background(), cfg("bot", 280), "u1", "u1", "bot one")
	_, ok2, _ := r.Respond(context.Background(), cfg("bot", 280), "u1", "u1", "bot two")
	if !ok1 || ok2 {
		t.Fatalf("first should answer, second rate-limited: %v %v", ok1, ok2)
	}
	_, ok3, _ := r.Respond(context.Background(), cfg("bot", 280), "u2", "u2", "bot three")
	if !ok3 {
		t.Fatalf("other user should answer")
	}
}

func TestRespond_GlobalRateLimit(t *testing.T) {
	b := &fakeBackend{out: "hi"}
	r := NewResponder(b, Options{GlobalPerSecond: 1})
	fixed := time.Now()
	r.nowFunc = func() time.Time { return fixed }
	_, ok1, _ := r.Respond(context.Background(), cfg("bot", 280), "u1", "u1", "bot one")
	_, ok2, _ := r.Respond(context.Background(), cfg("bot", 280), "u2", "u2", "bot two")
	if !ok1 || ok2 {
		t.Fatalf("first should answer, second hits global limit: %v %v", ok1, ok2)
	}
}

func TestNewResponder_NilBackendPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on nil backend")
		}
	}()
	_ = NewResponder(nil, DefaultOptions())
}

func TestBuildSystemPrompt_IncludesPersonaAndLimit(t *testing.T) {
	p := buildSystemPrompt(cfg("engel", 120))
	if !strings.Contains(p, "engel") || !strings.Contains(p, "a helper") || !strings.Contains(p, "120") {
		t.Fatalf("system prompt missing parts: %q", p)
	}
}
