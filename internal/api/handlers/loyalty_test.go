package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/Luca-Pelzer/engelos/internal/loyalty"
)

type earnCall struct {
	viewerID string
	username string
	amount   int64
}

type fakeLoyaltyStore struct {
	earns  []earnCall
	spends []earnCall
}

func (f *fakeLoyaltyStore) Balance(context.Context, string, string, string) (loyalty.Account, error) {
	return loyalty.Account{}, loyalty.ErrNotFound
}

func (f *fakeLoyaltyStore) Earn(_ context.Context, _, _, viewerID, username string, amount int64) (loyalty.Account, error) {
	f.earns = append(f.earns, earnCall{viewerID: viewerID, username: username, amount: amount})
	return loyalty.Account{ViewerID: viewerID, Username: username, Balance: amount}, nil
}

func (f *fakeLoyaltyStore) Spend(_ context.Context, _, _, viewerID string, amount int64) (loyalty.Account, error) {
	f.spends = append(f.spends, earnCall{viewerID: viewerID, amount: amount})
	return loyalty.Account{ViewerID: viewerID, Balance: 0}, nil
}

func (f *fakeLoyaltyStore) Transfer(context.Context, string, string, string, string, string, int64) (loyalty.Account, loyalty.Account, error) {
	return loyalty.Account{}, loyalty.Account{}, nil
}

func (f *fakeLoyaltyStore) Leaderboard(context.Context, string, string, int) ([]loyalty.Account, error) {
	return nil, nil
}

func (f *fakeLoyaltyStore) Close() error { return nil }

func adjustRequest(t *testing.T, h *Loyalty, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/loyalty/adjust", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Adjust(rec, req)
	return rec
}

func TestLoyaltyAdjust_ResolvesUsernameToNumericID(t *testing.T) {
	store := &fakeLoyaltyStore{}
	resolver := func(_ context.Context, login string) (commands.UserProfile, error) {
		if login != "bob" {
			t.Fatalf("unexpected resolve login %q", login)
		}
		return commands.UserProfile{ID: "123", Login: "bob", DisplayName: "Bob"}, nil
	}
	h := NewLoyalty(store, "local", resolver, nil)

	rec := adjustRequest(t, h, `{"channel":"chan-A","username":"Bob","amount":50}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(store.earns) != 1 {
		t.Fatalf("Earn called %d times, want 1", len(store.earns))
	}
	got := store.earns[0]
	if got.viewerID != "123" {
		t.Fatalf("Earn viewerID = %q, want numeric id 123", got.viewerID)
	}
	if got.username == "123" {
		t.Fatalf("username arg must be the login/display, not the id")
	}
	if got.amount != 50 {
		t.Fatalf("Earn amount = %d, want 50", got.amount)
	}
}

func TestLoyaltyAdjust_NilResolverFailsClosed(t *testing.T) {
	store := &fakeLoyaltyStore{}
	h := NewLoyalty(store, "local", nil, nil)

	rec := adjustRequest(t, h, `{"channel":"chan-A","username":"Bob","amount":50}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if len(store.earns) != 0 || len(store.spends) != 0 {
		t.Fatalf("store written despite nil resolver: earns=%d spends=%d", len(store.earns), len(store.spends))
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp["error"] == "" {
		t.Fatalf("expected an error message, got %v", resp)
	}
}

func TestLoyaltyAdjust_FailedResolveFailsClosed(t *testing.T) {
	store := &fakeLoyaltyStore{}
	resolver := func(context.Context, string) (commands.UserProfile, error) {
		return commands.UserProfile{}, errors.New("no such user")
	}
	h := NewLoyalty(store, "local", resolver, nil)

	rec := adjustRequest(t, h, `{"channel":"chan-A","username":"ghost","amount":50}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if len(store.earns) != 0 {
		t.Fatalf("phantom row written on failed resolve: earns=%d", len(store.earns))
	}
}
