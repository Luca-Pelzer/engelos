package tts

import (
	"context"
	"errors"
	"testing"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	st, err := OpenSQLiteStore(context.Background(), "file:tts_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestStoreGetNotFound(t *testing.T) {
	st := newTestStore(t)
	_, err := st.Get(context.Background(), "tenant1", "chan")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v want ErrNotFound", err)
	}
}

func TestStoreGetOrDefault(t *testing.T) {
	st := newTestStore(t)
	cfg, err := st.GetOrDefault(context.Background(), "tenant1", "#ChanName")
	if err != nil {
		t.Fatalf("GetOrDefault: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("default should be disabled")
	}
	if cfg.Channel != "channame" {
		t.Fatalf("channel not normalized: %q", cfg.Channel)
	}
	if cfg.Model != defaultModel {
		t.Fatalf("default model wrong: %q", cfg.Model)
	}
}

func TestStoreSetAndGetRoundTrip(t *testing.T) {
	st := newTestStore(t)
	cipher := []byte{0x01, 0x02, 0x03}
	saved, err := st.Set(context.Background(), Config{
		TenantID:         "tenant1",
		Channel:          "MyChannel",
		Enabled:          true,
		VoiceID:          "voiceABC",
		APIKeyCiphertext: cipher,
	})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if saved.Channel != "mychannel" {
		t.Fatalf("channel not normalized on save: %q", saved.Channel)
	}
	if saved.Model != defaultModel {
		t.Fatalf("model default not applied: %q", saved.Model)
	}

	got, err := st.Get(context.Background(), "tenant1", "mychannel")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enabled || got.VoiceID != "voiceABC" {
		t.Fatalf("bad round trip: %+v", got)
	}
	if string(got.APIKeyCiphertext) != string(cipher) {
		t.Fatalf("ciphertext not preserved: %v", got.APIKeyCiphertext)
	}
}

func TestStoreSetUpsert(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.Set(ctx, Config{TenantID: "t", Channel: "c", Enabled: true, VoiceID: "v1"}); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if _, err := st.Set(ctx, Config{TenantID: "t", Channel: "c", Enabled: false, VoiceID: "v2"}); err != nil {
		t.Fatalf("second set: %v", err)
	}
	got, err := st.Get(ctx, "t", "c")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Enabled || got.VoiceID != "v2" {
		t.Fatalf("upsert did not overwrite: %+v", got)
	}
	all, err := st.List(ctx, "t")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", len(all))
	}
}

func TestStoreSetValidation(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Set(context.Background(), Config{Channel: "c"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing tenant: got %v want ErrInvalid", err)
	}
	if _, err := st.Set(context.Background(), Config{TenantID: "t"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing channel: got %v want ErrInvalid", err)
	}
}
