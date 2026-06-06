package oauthrefresh

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeStore struct {
	mu       sync.Mutex
	items    []Identity
	updates  []updateCall
	listErr  error
	updErrOn map[string]error
}

type updateCall struct {
	ID, Access, Refresh string
	Expiry              time.Time
}

func (f *fakeStore) ListExpiring(_ context.Context, cutoff time.Time) ([]Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []Identity
	for _, it := range f.items {
		if !it.ExpiresAt.IsZero() && !it.ExpiresAt.After(cutoff) {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeStore) UpdateTokens(_ context.Context, id, access, refresh string, expiry time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.updErrOn[id]; ok {
		return err
	}
	f.updates = append(f.updates, updateCall{ID: id, Access: access, Refresh: refresh, Expiry: expiry})
	return nil
}

func (f *fakeStore) snapshotUpdates() []updateCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]updateCall, len(f.updates))
	copy(out, f.updates)
	return out
}

type fakeTokens struct {
	mu         sync.Mutex
	resp       map[string]tokenResp
	defaultErr error
}

type tokenResp struct {
	access, refresh string
	expiry          time.Time
	err             error
}

func (f *fakeTokens) Refresh(_ context.Context, refreshToken string) (string, string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.resp[refreshToken]; ok {
		return r.access, r.refresh, r.expiry, r.err
	}
	if f.defaultErr != nil {
		return "", "", time.Time{}, f.defaultErr
	}
	return "default-access-" + refreshToken, "default-refresh-" + refreshToken, time.Now().Add(time.Hour), nil
}

func TestNewRejectsMissingDeps(t *testing.T) {
	_, err := New(Config{Tokens: &fakeTokens{}})
	require.ErrorIs(t, err, ErrInvalidConfig)

	_, err = New(Config{Store: &fakeStore{}})
	require.ErrorIs(t, err, ErrInvalidConfig)

	r, err := New(Config{Store: &fakeStore{}, Tokens: &fakeTokens{}})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, defaultInterval, r.interval)
	assert.Equal(t, defaultRefreshWindow, r.refreshWindow)
	assert.NotNil(t, r.log)
	assert.NotNil(t, r.now)
}

func TestRefreshNowHappyPath(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	newExp := now.Add(4 * time.Hour)

	store := &fakeStore{items: []Identity{{
		ID: "id-1", Provider: "twitch", ProviderLogin: "ninja", Purpose: "bot",
		RefreshToken: "rtk-1", ExpiresAt: now.Add(5 * time.Minute),
	}}}
	tokens := &fakeTokens{resp: map[string]tokenResp{
		"rtk-1": {access: "atk-new-1", refresh: "rtk-new-1", expiry: newExp},
	}}

	var hookCalls []RefreshEvent
	var hookMu sync.Mutex
	r, err := New(Config{
		Store:         store,
		Tokens:        tokens,
		Logger:        quietLogger(),
		RefreshWindow: 15 * time.Minute,
		Now:           func() time.Time { return now },
		OnRefresh: func(ev RefreshEvent) {
			hookMu.Lock()
			defer hookMu.Unlock()
			hookCalls = append(hookCalls, ev)
		},
	})
	require.NoError(t, err)

	require.NoError(t, r.RefreshNow(context.Background()))

	updates := store.snapshotUpdates()
	require.Len(t, updates, 1)
	assert.Equal(t, "id-1", updates[0].ID)
	assert.Equal(t, "atk-new-1", updates[0].Access)
	assert.Equal(t, "rtk-new-1", updates[0].Refresh)
	assert.True(t, updates[0].Expiry.Equal(newExp))

	hookMu.Lock()
	defer hookMu.Unlock()
	require.Len(t, hookCalls, 1)
	assert.Equal(t, "id-1", hookCalls[0].Identity.ID)
	assert.Equal(t, "twitch", hookCalls[0].Identity.Provider)
	assert.Equal(t, "bot", hookCalls[0].Identity.Purpose)
	assert.Equal(t, "atk-new-1", hookCalls[0].AccessToken)
	assert.True(t, hookCalls[0].ExpiresAt.Equal(newExp))
}

func TestRefreshSkipsOutsideWindow(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []Identity{{
		ID: "far", Provider: "twitch", RefreshToken: "rtk",
		ExpiresAt: now.Add(2 * time.Hour),
	}}}
	r, err := New(Config{
		Store:         store,
		Tokens:        &fakeTokens{},
		Logger:        quietLogger(),
		RefreshWindow: 15 * time.Minute,
		Now:           func() time.Time { return now },
	})
	require.NoError(t, err)

	require.NoError(t, r.RefreshNow(context.Background()))
	assert.Empty(t, store.snapshotUpdates(), "identity outside window must not be refreshed")
}

func TestRefreshContinuesOnPerIdentityError(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []Identity{
		{ID: "bad", Provider: "twitch", RefreshToken: "rtk-bad", ExpiresAt: now.Add(time.Minute)},
		{ID: "good", Provider: "twitch", RefreshToken: "rtk-good", ExpiresAt: now.Add(2 * time.Minute)},
	}}
	tokens := &fakeTokens{resp: map[string]tokenResp{
		"rtk-bad":  {err: errors.New("upstream 500")},
		"rtk-good": {access: "atk-good", refresh: "rtk-good-new", expiry: now.Add(4 * time.Hour)},
	}}
	r, err := New(Config{
		Store: store, Tokens: tokens, Logger: quietLogger(),
		RefreshWindow: 15 * time.Minute,
		Now:           func() time.Time { return now },
	})
	require.NoError(t, err)

	require.NoError(t, r.RefreshNow(context.Background()))
	updates := store.snapshotUpdates()
	require.Len(t, updates, 1, "healthy identity must still be refreshed after a peer failed")
	assert.Equal(t, "good", updates[0].ID)
}

func TestRefreshSkipsEmptyRefreshToken(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []Identity{{
		ID: "no-rtk", Provider: "twitch", RefreshToken: "",
		ExpiresAt: now.Add(time.Minute),
	}}}
	tokens := &fakeTokens{defaultErr: errors.New("must not be called")}
	r, err := New(Config{
		Store: store, Tokens: tokens, Logger: quietLogger(),
		RefreshWindow: 15 * time.Minute,
		Now:           func() time.Time { return now },
	})
	require.NoError(t, err)

	require.NoError(t, r.RefreshNow(context.Background()))
	assert.Empty(t, store.snapshotUpdates())
}

func TestRefreshPreservesOriginalRefreshTokenWhenSourceReturnsEmpty(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []Identity{{
		ID: "id-keep", Provider: "twitch", RefreshToken: "rtk-keep",
		ExpiresAt: now.Add(time.Minute),
	}}}
	tokens := &fakeTokens{resp: map[string]tokenResp{
		"rtk-keep": {access: "atk-new", refresh: "", expiry: now.Add(4 * time.Hour)},
	}}
	r, err := New(Config{
		Store: store, Tokens: tokens, Logger: quietLogger(),
		RefreshWindow: 15 * time.Minute,
		Now:           func() time.Time { return now },
	})
	require.NoError(t, err)

	require.NoError(t, r.RefreshNow(context.Background()))
	updates := store.snapshotUpdates()
	require.Len(t, updates, 1)
	assert.Equal(t, "rtk-keep", updates[0].Refresh, "empty newRefresh must persist as original, never blank")
	assert.Equal(t, "atk-new", updates[0].Access)
}

func TestRunReturnsOnContextCancel(t *testing.T) {
	r, err := New(Config{
		Store:    &fakeStore{},
		Tokens:   &fakeTokens{},
		Logger:   quietLogger(),
		Interval: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly after context cancellation")
	}
}

func TestOnRefreshPanicDoesNotKillLoop(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []Identity{{
		ID: "id-panic", Provider: "twitch", RefreshToken: "rtk",
		ExpiresAt: now.Add(time.Minute),
	}}}
	tokens := &fakeTokens{resp: map[string]tokenResp{
		"rtk": {access: "atk", refresh: "rtk2", expiry: now.Add(time.Hour)},
	}}
	r, err := New(Config{
		Store: store, Tokens: tokens, Logger: quietLogger(),
		RefreshWindow: 15 * time.Minute,
		Now:           func() time.Time { return now },
		OnRefresh:     func(_ RefreshEvent) { panic("boom") },
	})
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		_ = r.RefreshNow(context.Background())
	})
	assert.Len(t, store.snapshotUpdates(), 1, "persist must have happened before the hook panic")
}

func TestNormalizeRefresh(t *testing.T) {
	assert.Equal(t, "orig", normalizeRefresh("orig", ""))
	assert.Equal(t, "new", normalizeRefresh("orig", "new"))
	assert.Equal(t, "", normalizeRefresh("", ""))
}
