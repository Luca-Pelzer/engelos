package tts

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeSynth struct {
	mu     sync.Mutex
	calls  []string
	audio  []byte
	err    error
	gotKey string
}

func (f *fakeSynth) Synthesize(_ context.Context, voiceID, text string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, voiceID+"|"+text)
	if f.err != nil {
		return nil, f.err
	}
	return f.audio, nil
}

func (f *fakeSynth) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fakeSecrets struct {
	key string
	err error
}

func (f fakeSecrets) DecryptString(blob []byte) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.key, nil
}

type captureBroadcaster struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	typ     string
	payload any
}

func (b *captureBroadcaster) Broadcast(eventType string, payload any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, capturedEvent{typ: eventType, payload: payload})
}

func (b *captureBroadcaster) last() (capturedEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == 0 {
		return capturedEvent{}, false
	}
	return b.events[len(b.events)-1], true
}

func (b *captureBroadcaster) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

func newServiceWith(t *testing.T, synth SynthClient, secrets Secrets, bc Broadcaster) (*Service, Store) {
	t.Helper()
	st := newTestStore(t)
	svc := NewService(st, secrets, bc, "tenant1", nil)
	svc.newClient = func(apiKey, model string) SynthClient { return synth }
	return svc, st
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

func TestServiceOnSubscribeSynthesizesAndBroadcasts(t *testing.T) {
	synth := &fakeSynth{audio: []byte{0xaa, 0xbb}}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "real-key"}, bc)

	if _, err := st.Set(context.Background(), Config{
		TenantID: "tenant1", Channel: "chan", Enabled: true,
		VoiceID: "v1", APIKeyCiphertext: []byte{0x01},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.OnSubscribe("chan", "alice")

	waitFor(t, func() bool { return bc.count() == 1 })

	ev, _ := bc.last()
	if ev.typ != "tts.play" {
		t.Fatalf("bad event type: %s", ev.typ)
	}
	pp, ok := ev.payload.(playPayload)
	if !ok {
		t.Fatalf("bad payload type: %T", ev.payload)
	}
	if pp.Format != "mp3" {
		t.Fatalf("bad format: %s", pp.Format)
	}
	want := base64.StdEncoding.EncodeToString([]byte{0xaa, 0xbb})
	if pp.Audio != want {
		t.Fatalf("bad audio b64: %s", pp.Audio)
	}
	if synth.calls[0] != "v1|alice just subscribed!" {
		t.Fatalf("bad synth call: %s", synth.calls[0])
	}
}

func TestServiceSkipsWhenDisabled(t *testing.T) {
	synth := &fakeSynth{audio: []byte{0x01}}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "k"}, bc)

	if _, err := st.Set(context.Background(), Config{
		TenantID: "tenant1", Channel: "chan", Enabled: false, VoiceID: "v1",
		APIKeyCiphertext: []byte{0x01},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.OnSubscribe("chan", "bob")
	time.Sleep(100 * time.Millisecond)
	if synth.callCount() != 0 {
		t.Fatal("synth should not be called when disabled")
	}
	if bc.count() != 0 {
		t.Fatal("no broadcast expected when disabled")
	}
}

func TestServiceSkipsWhenNoVoice(t *testing.T) {
	synth := &fakeSynth{audio: []byte{0x01}}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "k"}, bc)
	if _, err := st.Set(context.Background(), Config{
		TenantID: "tenant1", Channel: "chan", Enabled: true, VoiceID: "",
		APIKeyCiphertext: []byte{0x01},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.OnSubscribe("chan", "bob")
	time.Sleep(100 * time.Millisecond)
	if synth.callCount() != 0 {
		t.Fatal("synth should not run without a voice id")
	}
}

func TestServiceSwallowsSynthError(t *testing.T) {
	synth := &fakeSynth{err: errors.New("boom")}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "k"}, bc)
	if _, err := st.Set(context.Background(), Config{
		TenantID: "tenant1", Channel: "chan", Enabled: true, VoiceID: "v1",
		APIKeyCiphertext: []byte{0x01},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.OnSubscribe("chan", "bob")
	waitFor(t, func() bool { return synth.callCount() == 1 })
	time.Sleep(50 * time.Millisecond)
	if bc.count() != 0 {
		t.Fatal("no broadcast on synth error")
	}
}

func TestServiceNilSafeOnSubscribe(t *testing.T) {
	var svc *Service
	svc.OnSubscribe("chan", "x")
}

func TestServiceStopIdempotent(t *testing.T) {
	synth := &fakeSynth{audio: []byte{0x01}}
	svc, _ := newServiceWith(t, synth, fakeSecrets{key: "k"}, &captureBroadcaster{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	svc.Stop()
	svc.Stop()
}

func seedEnabled(t *testing.T, st Store) {
	t.Helper()
	if _, err := st.Set(context.Background(), Config{
		TenantID: "tenant1", Channel: "chan", Enabled: true, VoiceID: "v1",
		APIKeyCiphertext: []byte{0x01},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestServiceOnResubscribeWithMonths(t *testing.T) {
	synth := &fakeSynth{audio: []byte{0x01}}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "k"}, bc)
	seedEnabled(t, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.OnResubscribe("chan", "alice", 5)
	waitFor(t, func() bool { return synth.callCount() == 1 })
	if synth.calls[0] != "v1|alice just resubscribed for 5 months!" {
		t.Fatalf("bad resub text: %s", synth.calls[0])
	}
}

func TestServiceOnGiftSubscribe(t *testing.T) {
	synth := &fakeSynth{audio: []byte{0x01}}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "k"}, bc)
	seedEnabled(t, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.OnGiftSubscribe("chan", "bob")
	waitFor(t, func() bool { return synth.callCount() == 1 })
	if synth.calls[0] != "v1|bob just gifted a subscription!" {
		t.Fatalf("bad gift text: %s", synth.calls[0])
	}
}

func TestServiceOnGiftSubscribeAnonymous(t *testing.T) {
	synth := &fakeSynth{audio: []byte{0x01}}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "k"}, bc)
	seedEnabled(t, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.OnGiftSubscribe("chan", "")
	waitFor(t, func() bool { return synth.callCount() == 1 })
	if synth.calls[0] != "v1|Someone just gifted a subscription!" {
		t.Fatalf("bad anon gift text: %s", synth.calls[0])
	}
}

func TestServiceOnRaidWithViewers(t *testing.T) {
	synth := &fakeSynth{audio: []byte{0x01}}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "k"}, bc)
	seedEnabled(t, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.OnRaid("chan", "carol", 42)
	waitFor(t, func() bool { return synth.callCount() == 1 })
	if synth.calls[0] != "v1|carol is raiding with 42 viewers!" {
		t.Fatalf("bad raid text: %s", synth.calls[0])
	}
}

func TestServiceNewTriggersNilSafe(t *testing.T) {
	var svc *Service
	svc.OnResubscribe("c", "u", 3)
	svc.OnGiftSubscribe("c", "g")
	svc.OnRaid("c", "r", 10)
	svc.Speak("c", "hi")
}

func TestServiceSpeakRawText(t *testing.T) {
	synth := &fakeSynth{audio: []byte{0x01}}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "k"}, bc)
	seedEnabled(t, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.Speak("chan", "read this exactly")
	waitFor(t, func() bool { return synth.callCount() == 1 })
	if synth.calls[0] != "v1|read this exactly" {
		t.Fatalf("bad speak text: %s", synth.calls[0])
	}
}

func TestServiceSequentialProcessing(t *testing.T) {
	synth := &fakeSynth{audio: []byte{0x01}}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "k"}, bc)
	if _, err := st.Set(context.Background(), Config{
		TenantID: "tenant1", Channel: "chan", Enabled: true, VoiceID: "v1",
		APIKeyCiphertext: []byte{0x01},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.OnSubscribe("chan", "a")
	svc.OnSubscribe("chan", "b")
	svc.OnSubscribe("chan", "c")
	waitFor(t, func() bool { return bc.count() == 3 })
}
