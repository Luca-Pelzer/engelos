package timers

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	mu      sync.Mutex
	enabled []Timer
}

func (f *fakeStore) set(ts []Timer) {
	f.mu.Lock()
	f.enabled = ts
	f.mu.Unlock()
}

func (f *fakeStore) Create(context.Context, Timer) (Timer, error) { return Timer{}, nil }
func (f *fakeStore) Update(context.Context, string, string, string, string, time.Duration, int, bool) (Timer, error) {
	return Timer{}, nil
}
func (f *fakeStore) Get(context.Context, string, string, string) (Timer, error) {
	return Timer{}, ErrNotFound
}
func (f *fakeStore) Delete(context.Context, string, string, string) error { return nil }
func (f *fakeStore) List(context.Context, string, string) ([]Timer, error) {
	return nil, nil
}
func (f *fakeStore) ListEnabled(context.Context, string) ([]Timer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Timer, len(f.enabled))
	copy(out, f.enabled)
	return out, nil
}
func (f *fakeStore) Close() error { return nil }

type sendCall struct {
	channel string
	message string
}

type fakeSender struct {
	mu    sync.Mutex
	calls []sendCall
	err   error
}

func (f *fakeSender) Send(_ context.Context, channel, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sendCall{channel, message})
	return f.err
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeSender) snapshot() []sendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sendCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newSchedFixture(t *testing.T, ts []Timer) (*Scheduler, *fakeStore, *fakeSender, *fakeClock) {
	t.Helper()
	store := &fakeStore{}
	store.set(ts)
	sender := &fakeSender{}
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0).UTC()}
	s, err := New(Config{
		Store:    store,
		Sender:   sender,
		TenantID: "local",
		Logger:   silentLogger(),
		Tick:     time.Millisecond,
		Now:      clock.now,
	})
	require.NoError(t, err)
	return s, store, sender, clock
}

func TestNew_NilStoreOrSender(t *testing.T) {
	_, err := New(Config{Sender: &fakeSender{}})
	assert.Error(t, err)
	_, err = New(Config{Store: &fakeStore{}})
	assert.Error(t, err)
}

func TestScheduler_FiresAfterInterval(t *testing.T) {
	timer := Timer{
		ID: "t1", TenantID: "local", Channel: "chan-A", Name: "rules",
		Message: "Follow the rules!", Interval: 60 * time.Second, Enabled: true,
	}
	s, _, sender, clock := newSchedFixture(t, []Timer{timer})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// Not yet due: lastFired initialised to load time.
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, sender.count(), "must not fire before interval elapses")

	clock.advance(60 * time.Second)
	require.Eventually(t, func() bool { return sender.count() == 1 },
		2*time.Second, 5*time.Millisecond, "timer must fire once interval elapses")

	calls := sender.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "chan-A", calls[0].channel)
	assert.Equal(t, "Follow the rules!", calls[0].message)
}

func TestScheduler_ActivityGate(t *testing.T) {
	timer := Timer{
		ID: "t1", TenantID: "local", Channel: "chan-A", Name: "rules",
		Message: "msg", Interval: 60 * time.Second, MinChatLines: 3, Enabled: true,
	}
	s, _, sender, clock := newSchedFixture(t, []Timer{timer})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)

	clock.advance(60 * time.Second)
	// Interval elapsed but activity gate (3 lines) not satisfied.
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, 0, sender.count(), "must not fire until MinChatLines reached")

	s.RecordChatActivity("chan-A")
	s.RecordChatActivity("chan-A")
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, sender.count(), "still under threshold")

	s.RecordChatActivity("chan-A")
	require.Eventually(t, func() bool { return sender.count() == 1 },
		2*time.Second, 5*time.Millisecond, "fires once activity gate satisfied")

	// After firing the channel counter resets; it must re-accumulate
	// before the next fire even though another interval elapses.
	clock.advance(60 * time.Second)
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, 1, sender.count(), "counter reset: must re-accumulate before next fire")

	s.RecordChatActivity("chan-A")
	s.RecordChatActivity("chan-A")
	s.RecordChatActivity("chan-A")
	require.Eventually(t, func() bool { return sender.count() == 2 },
		2*time.Second, 5*time.Millisecond, "fires again after re-accumulating activity")
}

func TestScheduler_DisabledNeverFires(t *testing.T) {
	// ListEnabled excludes disabled timers, so an empty enabled set means
	// the scheduler has nothing to fire.
	s, _, sender, clock := newSchedFixture(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)

	clock.advance(10 * time.Minute)
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, 0, sender.count(), "no enabled timers -> no fires")
}

func TestScheduler_SenderErrorDoesNotHotLoop(t *testing.T) {
	timer := Timer{
		ID: "t1", TenantID: "local", Channel: "chan-A", Name: "rules",
		Message: "msg", Interval: 60 * time.Second, Enabled: true,
	}
	s, _, sender, clock := newSchedFixture(t, []Timer{timer})
	sender.mu.Lock()
	sender.err = assertErr{}
	sender.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)

	clock.advance(60 * time.Second)
	require.Eventually(t, func() bool { return sender.count() >= 1 },
		2*time.Second, 5*time.Millisecond, "one attempt happens")

	// Many ticks pass at the same clock time: lastFired advanced on error,
	// so no further attempts within this interval window.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, sender.count(),
		"send error must advance lastFired -> at most one attempt per interval")
}

type assertErr struct{}

func (assertErr) Error() string { return "send failed" }

func TestScheduler_RunReturnsOnCancel(t *testing.T) {
	s, _, _, _ := newSchedFixture(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly on ctx cancel")
	}
}

func TestScheduler_RecordActivityRace(t *testing.T) {
	timer := Timer{
		ID: "t1", TenantID: "local", Channel: "chan-A", Name: "rules",
		Message: "msg", Interval: 60 * time.Second, MinChatLines: 1, Enabled: true,
	}
	s, _, _, clock := newSchedFixture(t, []Timer{timer})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.RecordChatActivity("chan-A")
				if j%50 == 0 {
					clock.advance(time.Second)
				}
			}
		}()
	}
	wg.Wait()
}
