package usage

import (
	"context"
	"errors"
	"testing"
)

// fakeBackend simulates a wire client: it reports a fixed token usage to the
// context sink and returns a configurable error.
type fakeBackend struct {
	u   Usage
	err error
}

func (f *fakeBackend) Complete(ctx context.Context, _, _ string) (string, error) {
	Record(ctx, f.u)
	return "ok", f.err
}

func (f *fakeBackend) Translate(ctx context.Context, _, _ string) (string, error) {
	Record(ctx, f.u)
	return "ok", f.err
}

func TestRegistry_CountsPerConsumer(t *testing.T) {
	reg := NewRegistry()
	tr := Wrap(&fakeBackend{u: Usage{InputTokens: 10, OutputTokens: 3, CacheReadTokens: 4}}, reg, "translate")
	co := Wrap(&fakeBackend{u: Usage{InputTokens: 5, OutputTokens: 2}}, reg, "cohost")

	_, _ = tr.Complete(context.Background(), "s", "u")
	_, _ = tr.Complete(context.Background(), "s", "u")
	_, _ = co.Translate(context.Background(), "hi", "en")

	snap := reg.Snapshot()
	if snap.Calls != 3 || snap.OK != 3 || snap.Errors != 0 {
		t.Fatalf("total calls/ok/errors = %d/%d/%d, want 3/3/0", snap.Calls, snap.OK, snap.Errors)
	}
	if snap.TokensIn != 25 || snap.TokensOut != 8 || snap.CacheReadTokens != 8 {
		t.Fatalf("tokens in/out/cache = %d/%d/%d, want 25/8/8", snap.TokensIn, snap.TokensOut, snap.CacheReadTokens)
	}
	if got := snap.ByConsumer["translate"]; got.Calls != 2 || got.TokensIn != 20 || got.TokensOut != 6 {
		t.Fatalf("translate breakdown wrong: %+v", got)
	}
	if got := snap.ByConsumer["cohost"]; got.Calls != 1 || got.TokensIn != 5 || got.TokensOut != 2 {
		t.Fatalf("cohost breakdown wrong: %+v", got)
	}
}

func TestRegistry_CountsErrors(t *testing.T) {
	reg := NewRegistry()
	b := Wrap(&fakeBackend{err: errors.New("boom")}, reg, "contextmod")

	if _, err := b.Complete(context.Background(), "s", "u"); err == nil {
		t.Fatal("expected the inner error to propagate")
	}
	snap := reg.Snapshot()
	if snap.Calls != 1 || snap.OK != 0 || snap.Errors != 1 {
		t.Fatalf("calls/ok/errors = %d/%d/%d, want 1/0/1", snap.Calls, snap.OK, snap.Errors)
	}
	if snap.ByConsumer["contextmod"].Errors != 1 {
		t.Fatalf("per-consumer error not counted: %+v", snap.ByConsumer["contextmod"])
	}
}

func TestWrap_NilRegistryReturnsInner(t *testing.T) {
	inner := &fakeBackend{}
	if Wrap(inner, nil, "x") != inner {
		t.Fatal("Wrap with a nil registry must return inner unchanged")
	}
}

func TestRecord_NoSinkIsNoop(t *testing.T) {
	Record(context.Background(), Usage{InputTokens: 5}) // must not panic
}

func TestSnapshot_NilRegistry(t *testing.T) {
	var r *Registry
	snap := r.Snapshot()
	if snap.Calls != 0 || snap.ByConsumer == nil {
		t.Fatalf("nil registry snapshot should be zero with an empty map: %+v", snap)
	}
}
