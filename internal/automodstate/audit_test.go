package automodstate

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

var memCounter atomic.Int64

// newTestStore opens a fresh in-memory SQLite AuditStore with a unique shared
// cache name so parallel tests never collide.
func newTestStore(t *testing.T) *AuditStore {
	t.Helper()
	dsn := fmt.Sprintf("file:automodaudit-%d?mode=memory&cache=shared",
		time.Now().UnixNano()+memCounter.Add(1))
	s, err := OpenSQLiteStore(context.Background(), dsn, nil)
	if err != nil {
		t.Fatalf("OpenSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleAction() ModAction {
	return ModAction{
		TenantID:    "local",
		Channel:     "chan",
		UserID:      "u123",
		Username:    "spammer",
		MessageID:   "m1",
		MessageText: "BUY FOLLOWERS http://spam.example",
		FilterName:  "links",
		Reason:      "posted a link without permit",
		MatchedText: "http://spam.example",
		Action:      "timeout",
		DurationSec: 60,
	}
}

func TestAudit_LogAssignsIDAndTimestamp(t *testing.T) {
	s := newTestStore(t)
	stored, err := s.Log(context.Background(), sampleAction())
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if stored.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if stored.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt")
	}
}

func TestAudit_LogThenListNewestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Unix(1_700_000_000, 0).UTC()

	for i := 0; i < 3; i++ {
		a := sampleAction()
		a.MessageID = fmt.Sprintf("m%d", i)
		a.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		if _, err := s.Log(ctx, a); err != nil {
			t.Fatalf("Log %d: %v", i, err)
		}
	}

	got, err := s.List(ctx, "local", "chan", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List len got %d want 3", len(got))
	}
	// Newest first: m2, m1, m0.
	for i, want := range []string{"m2", "m1", "m0"} {
		if got[i].MessageID != want {
			t.Fatalf("List[%d].MessageID got %s want %s", i, got[i].MessageID, want)
		}
	}
}

func TestAudit_RoundTripAllFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleAction()
	in.CreatedAt = time.Unix(1_700_000_123, 0).UTC()
	stored, err := s.Log(ctx, in)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	got, err := s.List(ctx, "local", "chan", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List len got %d want 1", len(got))
	}
	r := got[0]
	if r.ID != stored.ID || r.TenantID != "local" || r.Channel != "chan" ||
		r.UserID != "u123" || r.Username != "spammer" || r.MessageID != "m1" ||
		r.MessageText != in.MessageText || r.FilterName != "links" ||
		r.Reason != in.Reason || r.MatchedText != in.MatchedText ||
		r.Action != "timeout" || r.DurationSec != 60 {
		t.Fatalf("round-trip mismatch: %+v", r)
	}
	if !r.CreatedAt.Equal(in.CreatedAt) {
		t.Fatalf("CreatedAt got %v want %v", r.CreatedAt, in.CreatedAt)
	}
}

func TestAudit_CreatedAtRoundTripsToSecond(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Sub-second precision must be truncated consistently on write and read.
	in := sampleAction()
	in.CreatedAt = time.Unix(1_700_000_000, 987_654_321).UTC()
	stored, err := s.Log(ctx, in)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	want := time.Unix(1_700_000_000, 0).UTC()
	if !stored.CreatedAt.Equal(want) {
		t.Fatalf("stored CreatedAt got %v want %v", stored.CreatedAt, want)
	}

	got, _ := s.List(ctx, "local", "chan", 1)
	if !got[0].CreatedAt.Equal(want) {
		t.Fatalf("listed CreatedAt got %v want %v", got[0].CreatedAt, want)
	}
}

func TestAudit_DryRunRoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a := sampleAction()
	a.DryRun = true
	if _, err := s.Log(ctx, a); err != nil {
		t.Fatalf("Log: %v", err)
	}
	got, err := s.List(ctx, "local", "chan", 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !got[0].DryRun {
		t.Fatal("DryRun did not round-trip as true")
	}
}

func TestAudit_ListByUserFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"alice", "bob", "alice"} {
		a := sampleAction()
		a.Username = name
		if _, err := s.Log(ctx, a); err != nil {
			t.Fatalf("Log %s: %v", name, err)
		}
	}

	got, err := s.ListByUser(ctx, "local", "chan", "alice", 0)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByUser len got %d want 2", len(got))
	}
	for _, r := range got {
		if r.Username != "alice" {
			t.Fatalf("ListByUser returned wrong user: %s", r.Username)
		}
	}
}

func TestAudit_ListFiltersByTenantAndChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mk := func(tenant, channel string) ModAction {
		a := sampleAction()
		a.TenantID = tenant
		a.Channel = channel
		return a
	}
	if _, err := s.Log(ctx, mk("local", "chan")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Log(ctx, mk("local", "other")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Log(ctx, mk("t2", "chan")); err != nil {
		t.Fatal(err)
	}

	got, err := s.List(ctx, "local", "chan", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List len got %d want 1 (tenant/channel isolation)", len(got))
	}
}

func TestAudit_LimitCapRespected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := s.Log(ctx, sampleAction()); err != nil {
			t.Fatalf("Log %d: %v", i, err)
		}
	}

	// Explicit small limit is honoured.
	got, err := s.List(ctx, "local", "chan", 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List with limit 2 got %d want 2", len(got))
	}

	// Over-cap limit clamps to maxListLimit (no error, returns all 5 rows).
	got, err = s.List(ctx, "local", "chan", 100_000)
	if err != nil {
		t.Fatalf("List over cap: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("List over cap got %d want 5", len(got))
	}
}

func TestAudit_EmptyChannelReturnsNoRows(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List(context.Background(), "local", "nope", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty channel got %d rows want 0", len(got))
	}
}
