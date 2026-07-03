package contextmod

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestContextBuffer_CountEviction(t *testing.T) {
	b := newContextBuffer(30)
	for i := 0; i < 35; i++ {
		b.observe("t", "c", "u", fmt.Sprintf("msg-%02d", i))
	}
	got := b.recent("t", "c", 30)
	if len(got) != 30 {
		t.Fatalf("want 30 retained, got %d", len(got))
	}
	// Oldest 5 (msg-00..msg-04) evicted; window is msg-05..msg-34, in order.
	for i, e := range got {
		want := fmt.Sprintf("msg-%02d", i+5)
		if e.Text != want {
			t.Fatalf("entry %d = %q want %q", i, e.Text, want)
		}
	}
}

func TestContextBuffer_ByteCapEviction(t *testing.T) {
	// Capacity is huge so the byte cap governs. Each 400-byte message means the
	// ring can hold at most floor(16384/400)=40 before the cap evicts the
	// oldest.
	b := newContextBuffer(1_000_000)
	msg := strings.Repeat("a", 400)
	for i := 0; i < 100; i++ {
		b.observe("t", "c", "u", msg)
	}
	got := b.recent("t", "c", 1_000_000)

	total := 0
	for _, e := range got {
		total += len(e.Text)
	}
	if total > maxContextBytes {
		t.Fatalf("retained %d bytes exceeds cap %d", total, maxContextBytes)
	}
	if want := maxContextBytes / 400; len(got) != want {
		t.Fatalf("retained %d messages, want %d (byte cap)", len(got), want)
	}
}

func TestContextBuffer_TruncatesLongMessage(t *testing.T) {
	b := newContextBuffer(10)
	b.observe("t", "c", "u", strings.Repeat("x", 900))
	got := b.recent("t", "c", 10)
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if runes := len([]rune(got[0].Text)); runes != maxMessageRunes {
		t.Fatalf("stored text runes = %d want %d", runes, maxMessageRunes)
	}
}

func TestContextBuffer_PerChannelIsolation(t *testing.T) {
	b := newContextBuffer(30)
	b.observe("t", "chanA", "ua", "alpha-only")
	b.observe("t", "chanB", "ub", "beta-only")

	if a := b.recent("t", "chanA", 30); len(a) != 1 || a[0].Text != "alpha-only" {
		t.Fatalf("chanA leaked/missing: %+v", a)
	}
	if bb := b.recent("t", "chanB", 30); len(bb) != 1 || bb[0].Text != "beta-only" {
		t.Fatalf("chanB leaked/missing: %+v", bb)
	}
}

func TestContextBuffer_PerTenantIsolation(t *testing.T) {
	b := newContextBuffer(30)
	b.observe("t1", "c", "u", "tenant1-msg")
	b.observe("t2", "c", "u", "tenant2-msg")

	if one := b.recent("t1", "c", 30); len(one) != 1 || one[0].Text != "tenant1-msg" {
		t.Fatalf("t1 leaked/missing: %+v", one)
	}
	if two := b.recent("t2", "c", 30); len(two) != 1 || two[0].Text != "tenant2-msg" {
		t.Fatalf("t2 leaked/missing: %+v", two)
	}
}

func TestContextBuffer_ChannelCaseInsensitive(t *testing.T) {
	b := newContextBuffer(30)
	b.observe("t", "ChanX", "u", "m1")
	if got := b.recent("t", "chanx", 30); len(got) != 1 {
		t.Fatalf("channel casing should share one window, got %d", len(got))
	}
}

func TestContextBuffer_DisabledZeroCapacity(t *testing.T) {
	b := newContextBuffer(0)
	b.observe("t", "c", "u", "should-not-store")
	if got := b.recent("t", "c", 30); got != nil {
		t.Fatalf("disabled buffer must return nil, got %+v", got)
	}
}

func TestContextBuffer_SkipsBlank(t *testing.T) {
	b := newContextBuffer(30)
	b.observe("t", "c", "", "no username")
	b.observe("t", "c", "u", "   ")
	if got := b.recent("t", "c", 30); got != nil {
		t.Fatalf("blank username/text must be skipped, got %+v", got)
	}
}

func TestContextBuffer_ConcurrentObserveRecent(t *testing.T) {
	b := newContextBuffer(50)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			ch := fmt.Sprintf("c%d", w%3)
			for i := 0; i < 200; i++ {
				b.observe("t", ch, "u", fmt.Sprintf("m-%d-%d", w, i))
				_ = b.recent("t", ch, 50)
			}
		}(w)
	}
	wg.Wait()
	for c := 0; c < 3; c++ {
		if got := b.recent("t", fmt.Sprintf("c%d", c), 50); len(got) > 50 {
			t.Fatalf("channel c%d exceeded capacity: %d", c, len(got))
		}
	}
}
