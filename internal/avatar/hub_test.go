package avatar

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func readMessage(t *testing.T, s *subscriber, within time.Duration) Message {
	t.Helper()
	select {
	case b, ok := <-s.send:
		require.True(t, ok, "subscriber send channel closed unexpectedly")
		var m Message
		require.NoError(t, json.Unmarshal(b, &m))
		return m
	case <-time.After(within):
		t.Fatal("timed out waiting for a directive")
		return Message{}
	}
}

func TestHubBroadcastsSpeakToSubscriber(t *testing.T) {
	h := NewHub(nil)
	sub := h.subscribe("channelA")

	h.Speak("channelA", "id1", "hello", "voiceX", "YXVkaW8=", json.RawMessage(`{"a":1}`))

	got := readMessage(t, sub, time.Second)
	require.Equal(t, TypeSpeak, got.Type)
	require.Equal(t, "id1", got.ID)
	require.Equal(t, "hello", got.Text)
	require.Equal(t, "voiceX", got.Voice)
	require.Equal(t, "YXVkaW8=", got.AudioB64)
	require.JSONEq(t, `{"a":1}`, string(got.Alignment))
}

func TestHubBroadcastsExpressionAndEnd(t *testing.T) {
	h := NewHub(nil)
	sub := h.subscribe("c")

	h.Expression("c", "e1", "talking", 5)
	got := readMessage(t, sub, time.Second)
	require.Equal(t, TypeExpression, got.Type)
	require.Equal(t, "talking", got.Expression)
	require.Equal(t, 5, got.HoldSeconds)

	h.End("c", "id1")
	got = readMessage(t, sub, time.Second)
	require.Equal(t, TypeEnd, got.Type)
	require.Equal(t, "id1", got.ID)
}

func TestHubDeliversToMultipleSubscribers(t *testing.T) {
	h := NewHub(nil)
	a := h.subscribe("c")
	b := h.subscribe("c")
	require.Equal(t, 2, h.channelSubscriberCount("c"))

	h.Expression("c", "e1", "happy", 0)

	require.Equal(t, "happy", readMessage(t, a, time.Second).Expression)
	require.Equal(t, "happy", readMessage(t, b, time.Second).Expression)
}

func TestHubPerChannelIsolation(t *testing.T) {
	h := NewHub(nil)
	a := h.subscribe("channelA")
	b := h.subscribe("channelB")

	h.Expression("channelA", "e1", "hype", 0)

	require.Equal(t, "hype", readMessage(t, a, time.Second).Expression)
	select {
	case _, ok := <-b.send:
		if ok {
			t.Fatal("subscriber on channelB received a channelA directive")
		}
	case <-time.After(100 * time.Millisecond):
		// expected: no cross-channel delivery
	}
}

func TestHubNormalizesChannel(t *testing.T) {
	h := NewHub(nil)
	sub := h.subscribe("mychannel")
	h.Expression("#MyChannel", "e1", "idle", 0)
	require.Equal(t, TypeExpression, readMessage(t, sub, time.Second).Type)
}

func TestHubDropsSlowSubscriber(t *testing.T) {
	h := NewHub(nil)
	sub := h.subscribe("c")
	require.Equal(t, 1, h.channelSubscriberCount("c"))

	// Never drain sub.send. Once the bounded buffer fills, the next publish
	// evicts the subscriber instead of blocking or growing memory.
	for i := 0; i < subscriberBuffer+5; i++ {
		h.Expression("c", fmt.Sprintf("e%d", i), "idle", 0)
	}

	require.Eventually(t, func() bool {
		return h.channelSubscriberCount("c") == 0
	}, time.Second, 5*time.Millisecond, "slow subscriber was not dropped")

	// The send channel is closed after eviction (drains buffered, then closed).
	drained := false
	for range subscriberBuffer + 5 {
		if _, ok := <-sub.send; !ok {
			drained = true
			break
		}
	}
	require.True(t, drained, "evicted subscriber's send channel should be closed")
}

func TestHubUnsubscribeIsIdempotent(t *testing.T) {
	h := NewHub(nil)
	sub := h.subscribe("c")
	h.unsubscribe("c", sub)
	require.Equal(t, 0, h.channelSubscriberCount("c"))
	// A second unsubscribe must not panic (double close) and stays a no-op.
	h.unsubscribe("c", sub)
	require.Equal(t, 0, h.SubscriberCount())
}

func TestHubPublishToEmptyChannelIsNoop(t *testing.T) {
	h := NewHub(nil)
	require.NotPanics(t, func() { h.Speak("ghost", "id", "x", "", "", nil) })
	require.Equal(t, 0, h.SubscriberCount())
}

func TestHubConcurrentAccess(t *testing.T) {
	h := NewHub(nil)
	var wg sync.WaitGroup

	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ch := fmt.Sprintf("c%d", n%4)
			sub := h.subscribe(ch)
			go func() {
				for range sub.send {
				}
			}()
			for i := 0; i < 50; i++ {
				h.Expression(ch, "e", "idle", 0)
			}
			h.unsubscribe(ch, sub)
		}(g)
	}
	wg.Wait()
	require.Equal(t, 0, h.SubscriberCount())
}
