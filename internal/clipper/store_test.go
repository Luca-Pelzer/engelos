package clipper

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	// A unique shared-cache in-memory DSN per test keeps the single-connection
	// store isolated while still surviving across the pool's one connection.
	dsn := fmt.Sprintf("file:clip_%d?mode=memory&cache=shared", time.Now().UnixNano())
	st, err := OpenSQLiteStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestStore_GetOrDefault_UnknownChannelIsDisabled(t *testing.T) {
	st := newTestStore(t)
	c, err := st.GetOrDefault(context.Background(), "t1", "#SmallCh")
	if err != nil {
		t.Fatalf("get or default: %v", err)
	}
	if c.Channel != "smallch" {
		t.Fatalf("channel not normalised: %q", c.Channel)
	}
	if c.Settings.Enabled {
		t.Fatalf("default for unknown channel must be disabled")
	}
}

func TestStore_SetThenGetRoundtrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	in := Config{
		TenantID: "t1",
		Channel:  "engelswtf",
		Settings: Settings{
			Enabled:            true,
			KeywordThreshold:   3,
			EmoteThreshold:     4,
			CopypastaThreshold: 3,
			MinMessages:        5,
			SpikeFactor:        2.5,
			CompositeThreshold: 0.5,
			CooldownSeconds:    45,
		},
	}
	saved, err := st.Set(ctx, in)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if saved.UpdatedAt.IsZero() {
		t.Fatalf("updated_at not set")
	}
	got, err := st.Get(ctx, "t1", "engelswtf")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Settings != in.Settings {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got.Settings, in.Settings)
	}
}

func TestStore_SetIsUpsert(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	base := Config{TenantID: "t1", Channel: "ch", Settings: Settings{Enabled: true, KeywordThreshold: 5}}
	if _, err := st.Set(ctx, base); err != nil {
		t.Fatalf("first set: %v", err)
	}
	base.Settings.KeywordThreshold = 3
	if _, err := st.Set(ctx, base); err != nil {
		t.Fatalf("second set: %v", err)
	}
	got, err := st.Get(ctx, "t1", "ch")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Settings.KeywordThreshold != 3 {
		t.Fatalf("upsert did not update: got %d", got.Settings.KeywordThreshold)
	}
	all, err := st.List(ctx, "t1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("upsert created duplicate: %d rows", len(all))
	}
}

func TestStore_ListOrdersByChannel(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for _, ch := range []string{"zeta", "alpha", "mid"} {
		if _, err := st.Set(ctx, Config{TenantID: "t1", Channel: ch, Settings: Settings{Enabled: true}}); err != nil {
			t.Fatalf("set %s: %v", ch, err)
		}
	}
	all, err := st.List(ctx, "t1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 || all[0].Channel != "alpha" || all[2].Channel != "zeta" {
		t.Fatalf("not ordered by channel asc: %+v", all)
	}
}

func TestStore_SetRejectsInvalid(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.Set(ctx, Config{TenantID: "", Channel: "ch"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty tenant should be ErrInvalid, got %v", err)
	}
	if _, err := st.Set(ctx, Config{TenantID: "t1", Channel: ""}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty channel should be ErrInvalid, got %v", err)
	}
	bad := Config{TenantID: "t1", Channel: "ch", Settings: Settings{KeywordThreshold: -1}}
	if _, err := st.Set(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative threshold should be ErrInvalid, got %v", err)
	}
}

func TestStore_GetNotFound(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Get(context.Background(), "t1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
