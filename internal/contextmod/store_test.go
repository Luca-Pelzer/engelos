package contextmod

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := fmt.Sprintf("file:cmod_%d?mode=memory&cache=shared", time.Now().UnixNano())
	st, err := OpenSQLiteStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestStore_GetOrDefault_UnknownChannelDisabled(t *testing.T) {
	st := newTestStore(t)
	c, err := st.GetOrDefault(context.Background(), "t1", "#SmallCh")
	if err != nil {
		t.Fatalf("get or default: %v", err)
	}
	if c.Channel != "smallch" {
		t.Fatalf("channel not normalised: %q", c.Channel)
	}
	if c.Enabled || c.Rules != "" {
		t.Fatalf("default must be disabled with empty rules, got %+v", c)
	}
}

func TestStore_SetThenGetRoundtrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	in := Config{
		TenantID: "t1",
		Channel:  "engelswtf",
		Enabled:  true,
		Rules:    "No slurs, no threats, no doxxing. Keep it friendly.",
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
	if got.Enabled != true || got.Rules != in.Rules {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestStore_SetIsUpsert(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	base := Config{TenantID: "t1", Channel: "ch", Enabled: true, Rules: "rule one"}
	if _, err := st.Set(ctx, base); err != nil {
		t.Fatalf("first set: %v", err)
	}
	base.Rules = "rule two"
	base.Enabled = false
	if _, err := st.Set(ctx, base); err != nil {
		t.Fatalf("second set: %v", err)
	}
	got, err := st.Get(ctx, "t1", "ch")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Rules != "rule two" || got.Enabled {
		t.Fatalf("upsert did not update: %+v", got)
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
		if _, err := st.Set(ctx, Config{TenantID: "t1", Channel: ch, Enabled: true, Rules: "x"}); err != nil {
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
	long := Config{TenantID: "t1", Channel: "ch", Rules: strings.Repeat("a", maxRulesLen+1)}
	if _, err := st.Set(ctx, long); !errors.Is(err, ErrInvalid) {
		t.Fatalf("over-long rules should be ErrInvalid, got %v", err)
	}
}

func TestStore_GetNotFound(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Get(context.Background(), "t1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
