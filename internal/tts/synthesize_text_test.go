package tts

import (
	"context"
	"errors"
	"testing"
)

func storeConfig(t *testing.T, st Store, c Config) {
	t.Helper()
	if _, err := st.Set(context.Background(), c); err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

func TestSynthesizeText_ReturnsAudioForConfiguredChannel(t *testing.T) {
	synth := &fakeSynth{audio: []byte("mp3")}
	bc := &captureBroadcaster{}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "sk-live"}, bc)
	storeConfig(t, st, Config{TenantID: "tenant1", Channel: "chan", Enabled: true, VoiceID: "voiceX", APIKeyCiphertext: []byte("cipher")})

	audio, err := svc.SynthesizeText(context.Background(), "chan", "", "hello")
	if err != nil {
		t.Fatalf("SynthesizeText: %v", err)
	}
	if string(audio) != "mp3" {
		t.Fatalf("audio = %q, want mp3", audio)
	}
	if got := synth.calls; len(got) != 1 || got[0] != "voiceX|hello" {
		t.Fatalf("synth calls = %v, want [voiceX|hello]", got)
	}
	// SynthesizeText must not touch the TTS overlay broadcast path.
	if bc.count() != 0 {
		t.Fatalf("broadcast count = %d, want 0 (SynthesizeText must not broadcast)", bc.count())
	}
}

func TestSynthesizeText_VoiceOverride(t *testing.T) {
	synth := &fakeSynth{audio: []byte("mp3")}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "sk"}, &captureBroadcaster{})
	storeConfig(t, st, Config{TenantID: "tenant1", Channel: "chan", Enabled: true, VoiceID: "configured", APIKeyCiphertext: []byte("cipher")})

	if _, err := svc.SynthesizeText(context.Background(), "chan", "override", "hi"); err != nil {
		t.Fatalf("SynthesizeText: %v", err)
	}
	if got := synth.calls; len(got) != 1 || got[0] != "override|hi" {
		t.Fatalf("synth calls = %v, want [override|hi]", got)
	}
}

func TestSynthesizeText_NoKeyReturnsNotConfigured(t *testing.T) {
	synth := &fakeSynth{audio: []byte("mp3")}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "sk"}, &captureBroadcaster{})
	storeConfig(t, st, Config{TenantID: "tenant1", Channel: "chan", Enabled: true, VoiceID: "voiceX"})

	_, err := svc.SynthesizeText(context.Background(), "chan", "", "hello")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	if synth.callCount() != 0 {
		t.Fatalf("synth must not be called when unconfigured")
	}
}

func TestSynthesizeText_UnknownChannelReturnsNotConfigured(t *testing.T) {
	synth := &fakeSynth{audio: []byte("mp3")}
	svc, _ := newServiceWith(t, synth, fakeSecrets{key: "sk"}, &captureBroadcaster{})

	_, err := svc.SynthesizeText(context.Background(), "nope", "", "hello")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestSynthesizeText_NoVoiceReturnsNotConfigured(t *testing.T) {
	synth := &fakeSynth{audio: []byte("mp3")}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "sk"}, &captureBroadcaster{})
	storeConfig(t, st, Config{TenantID: "tenant1", Channel: "chan", Enabled: true, APIKeyCiphertext: []byte("cipher")})

	_, err := svc.SynthesizeText(context.Background(), "chan", "", "hello")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestSynthesizeText_EmptyTextIsNoop(t *testing.T) {
	synth := &fakeSynth{audio: []byte("mp3")}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "sk"}, &captureBroadcaster{})
	storeConfig(t, st, Config{TenantID: "tenant1", Channel: "chan", Enabled: true, VoiceID: "voiceX", APIKeyCiphertext: []byte("cipher")})

	audio, err := svc.SynthesizeText(context.Background(), "chan", "", "   ")
	if err != nil || audio != nil {
		t.Fatalf("SynthesizeText(empty) = (%v, %v), want (nil, nil)", audio, err)
	}
	if synth.callCount() != 0 {
		t.Fatalf("synth must not be called for empty text")
	}
}

func TestSynthesizeText_SynthErrorPropagates(t *testing.T) {
	synth := &fakeSynth{err: errors.New("api down")}
	svc, st := newServiceWith(t, synth, fakeSecrets{key: "sk"}, &captureBroadcaster{})
	storeConfig(t, st, Config{TenantID: "tenant1", Channel: "chan", Enabled: true, VoiceID: "voiceX", APIKeyCiphertext: []byte("cipher")})

	if _, err := svc.SynthesizeText(context.Background(), "chan", "", "hello"); err == nil {
		t.Fatalf("expected error when synthesis fails")
	}
}

func TestSynthesizeText_NilServiceReturnsNotConfigured(t *testing.T) {
	var svc *Service
	if _, err := svc.SynthesizeText(context.Background(), "chan", "", "hello"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}
