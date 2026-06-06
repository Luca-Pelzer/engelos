package main

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/clipper"
)

// fakeClipStore is an in-memory clipSettingsStore for the adapter tests. It
// counts Get calls so the TTL cache can be asserted.
type fakeClipStore struct {
	cfg   map[string]clipper.Config
	calls atomic.Int64
}

func (f *fakeClipStore) Get(_ context.Context, _, channel string) (clipper.Config, error) {
	f.calls.Add(1)
	if c, ok := f.cfg[channel]; ok {
		return c, nil
	}
	return clipper.Config{}, clipper.ErrNotFound
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAutoClipper_EnvAllowListGates(t *testing.T) {
	a := newAutoClipper(nil, clipper.DefaultOptions(), "t1", nil, nil, nil, []string{"engelswtf"}, quietLogger())
	if _, enabled := a.resolve(context.Background(), "otherchannel"); enabled {
		t.Fatalf("channel outside env allow-list must be disabled")
	}
	if det, enabled := a.resolve(context.Background(), "engelswtf"); !enabled || det == nil {
		t.Fatalf("allow-listed channel must resolve enabled with a detector")
	}
}

func TestAutoClipper_StoredDisabledGate(t *testing.T) {
	store := &fakeClipStore{cfg: map[string]clipper.Config{
		"engelswtf": {TenantID: "t1", Channel: "engelswtf", Settings: clipper.Settings{Enabled: false}},
	}}
	a := newAutoClipper(store, clipper.DefaultOptions(), "t1", nil, nil, nil, nil, quietLogger())
	if _, enabled := a.resolve(context.Background(), "engelswtf"); enabled {
		t.Fatalf("stored disabled config must gate the channel off")
	}
}

func TestAutoClipper_LoweredThresholdAppliesPerChannel(t *testing.T) {
	store := &fakeClipStore{cfg: map[string]clipper.Config{
		"smallch": {TenantID: "t1", Channel: "smallch", Settings: clipper.Settings{Enabled: true, KeywordThreshold: 3}},
	}}
	a := newAutoClipper(store, clipper.DefaultOptions(), "t1", nil, nil, nil, nil, quietLogger())
	fixed := time.Now()
	a.now = func() time.Time { return fixed }

	det, enabled := a.resolve(context.Background(), "smallch")
	if !enabled || det == nil {
		t.Fatalf("expected enabled detector")
	}
	var fired bool
	for i := 0; i < 3; i++ {
		if f, _ := det.Message("smallch", string(rune('a'+i)), "clip it", fixed.Add(time.Duration(i)*time.Second)); f {
			fired = true
		}
	}
	if !fired {
		t.Fatalf("lowered per-channel threshold (3) should let a small channel fire")
	}
}

func TestAutoClipper_TTLCachesDetector(t *testing.T) {
	store := &fakeClipStore{cfg: map[string]clipper.Config{
		"engelswtf": {TenantID: "t1", Channel: "engelswtf", Settings: clipper.Settings{Enabled: true}},
	}}
	a := newAutoClipper(store, clipper.DefaultOptions(), "t1", nil, nil, nil, nil, quietLogger())
	base := time.Now()
	a.now = func() time.Time { return base }

	d1, _ := a.resolve(context.Background(), "engelswtf")
	d2, _ := a.resolve(context.Background(), "engelswtf")
	if d1 != d2 {
		t.Fatalf("detector should be cached within the TTL")
	}
	if store.calls.Load() != 1 {
		t.Fatalf("store should be read once within TTL, got %d", store.calls.Load())
	}

	a.now = func() time.Time { return base.Add(settingsTTL + time.Second) }
	if _, _ = a.resolve(context.Background(), "engelswtf"); store.calls.Load() != 2 {
		t.Fatalf("store should be re-read after TTL, got %d", store.calls.Load())
	}
}
