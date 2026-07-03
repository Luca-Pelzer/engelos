package contextmod

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// capturingBackend records the last system/user prompt it was handed so tests
// can assert on the exact text sent to the model.
type capturingBackend struct {
	mu     sync.Mutex
	system string
	user   string
	out    string
	calls  int
}

func (c *capturingBackend) Complete(_ context.Context, system, user string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.system, c.user, c.calls = system, user, c.calls+1
	return c.out, nil
}

func (c *capturingBackend) lastUser() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.user
}

// reviewText is a message that passes the needsAI pre-filter (long enough, has
// letters, not repeated-token spam) so the backend is actually consulted.
const reviewText = "a borderline message worth judging ZZREVIEW"

func TestClassifyInChannel_InjectsLastNInOrder(t *testing.T) {
	be := &capturingBackend{out: "ALLOW fine"}
	e := NewEscalator(be, Options{ContextSize: 5}) // no rate limit, no dedup

	for i := 0; i < 10; i++ {
		e.Observe("chan", fmt.Sprintf("user%d", i), fmt.Sprintf("history-line-%02d", i))
	}

	d := e.ClassifyInChannel(context.Background(), "chan", "no spam", "reviewer", reviewText)
	if !d.Consulted {
		t.Fatalf("expected the backend to be consulted: %+v", d)
	}
	up := be.lastUser()

	if !strings.Contains(up, "Recent chat") {
		t.Fatalf("prompt missing recent-chat block:\n%s", up)
	}

	// The last 5 messages (05..09) appear, in chronological order.
	prev := -1
	for i := 5; i < 10; i++ {
		marker := fmt.Sprintf("history-line-%02d", i)
		idx := strings.Index(up, marker)
		if idx < 0 {
			t.Fatalf("prompt missing %q:\n%s", marker, up)
		}
		if idx < prev {
			t.Fatalf("history out of chronological order at %q:\n%s", marker, up)
		}
		prev = idx
	}

	// The evicted older 5 (00..04) are absent.
	for i := 0; i < 5; i++ {
		marker := fmt.Sprintf("history-line-%02d", i)
		if strings.Contains(up, marker) {
			t.Fatalf("evicted message %q must be absent:\n%s", marker, up)
		}
	}

	// The reviewed message is clearly marked and comes after the history.
	if !strings.Contains(up, "Message under review") {
		t.Fatalf("prompt missing the review marker:\n%s", up)
	}
	idxRev := strings.Index(up, "ZZREVIEW")
	idxLastHist := strings.Index(up, "history-line-09")
	if idxRev < 0 || idxRev < idxLastHist {
		t.Fatalf("reviewed message must appear after the history block:\n%s", up)
	}
}

func TestClassifyInChannel_SizeZeroIsSingleTurnIdentical(t *testing.T) {
	be := &capturingBackend{out: "ALLOW fine"}
	e := NewEscalator(be, Options{ContextSize: 0}) // feature off

	// Observing is a no-op when disabled.
	e.Observe("chan", "someone", "prior message that must not appear")

	reviewed := "a borderline message worth judging"
	e.ClassifyInChannel(context.Background(), "chan", "no spam", "reviewer", reviewed)

	up := be.lastUser()
	if up != reviewed {
		t.Fatalf("size-0 prompt must equal the raw text.\n got: %q\nwant: %q", up, reviewed)
	}
	if strings.Contains(up, "Recent chat") {
		t.Fatalf("size-0 must carry no recent-chat block: %q", up)
	}
}

func TestClassifyInChannel_NoHistoryYetIsSingleTurn(t *testing.T) {
	be := &capturingBackend{out: "ALLOW"}
	e := NewEscalator(be, Options{ContextSize: 5})

	reviewed := "a borderline message worth judging"
	e.ClassifyInChannel(context.Background(), "freshchan", "no spam", "u", reviewed)

	if up := be.lastUser(); up != reviewed {
		t.Fatalf("first message with no history must be single-turn: %q", up)
	}
}

func TestClassifyInChannel_PerChannelPromptIsolation(t *testing.T) {
	be := &capturingBackend{out: "ALLOW"}
	e := NewEscalator(be, Options{ContextSize: 10})

	e.Observe("chanA", "a", "ALPHA-CONTEXT-LINE")
	e.Observe("chanB", "b", "BETA-CONTEXT-LINE")

	e.ClassifyInChannel(context.Background(), "chanA", "no spam", "u", "a borderline message worth judging")
	up := be.lastUser()

	if !strings.Contains(up, "ALPHA-CONTEXT-LINE") {
		t.Fatalf("chanA prompt should include chanA history:\n%s", up)
	}
	if strings.Contains(up, "BETA-CONTEXT-LINE") {
		t.Fatalf("chanA prompt must NOT include chanB history:\n%s", up)
	}
}

func TestClassifyInChannel_ConcurrentObserveAndClassify(t *testing.T) {
	be := &capturingBackend{out: "ALLOW"}
	e := NewEscalator(be, Options{ContextSize: 30})

	var wg sync.WaitGroup
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				e.Observe("chan", fmt.Sprintf("u%d", w), fmt.Sprintf("line-%d-%d", w, i))
			}
		}(w)
	}
	for c := 0; c < 4; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = e.ClassifyInChannel(context.Background(), "chan", "no spam", "u", "a borderline message worth judging")
			}
		}()
	}
	wg.Wait()
}
