package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/Luca-Pelzer/engelos/internal/loyalty"
)

func newTestLoyaltyStore(t *testing.T) loyalty.Store {
	t.Helper()
	dsn := "file:economy-transfer-test?mode=memory&cache=shared"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := loyalty.OpenSQLiteStore(context.Background(), dsn, logger)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestEconomyTransfer_CreditsRecipientByNumericID(t *testing.T) {
	ctx := context.Background()
	store := newTestLoyaltyStore(t)

	const channel = "chan-A"
	const senderID = "999sender"
	const recipientID = "123"
	const recipientLogin = "bob"

	if _, err := store.Earn(ctx, "local", channel, senderID, "Sender", 100); err != nil {
		t.Fatalf("seed sender: %v", err)
	}

	resolver := func(_ context.Context, login string) (commands.UserProfile, error) {
		if login != recipientLogin {
			t.Fatalf("unexpected resolve login %q", login)
		}
		return commands.UserProfile{ID: recipientID, Login: recipientLogin, DisplayName: "Bob"}, nil
	}

	econ := newEconomyAdapter(store, "local", 5, time.Minute).withResolver(resolver)

	loyErr, display := econ.Transfer(ctx, channel, senderID, recipientLogin, 40)
	if loyErr != commands.LoyaltyOK {
		t.Fatalf("Transfer returned %v, want LoyaltyOK", loyErr)
	}
	if display != "Bob" {
		t.Fatalf("display name = %q, want Bob", display)
	}

	got, err := store.Balance(ctx, "local", channel, recipientID)
	if err != nil {
		t.Fatalf("balance by numeric id: %v", err)
	}
	if got.Balance != 40 {
		t.Fatalf("recipient balance under numeric id = %d, want 40", got.Balance)
	}

	if _, err := store.Balance(ctx, "local", channel, recipientLogin); err == nil {
		t.Fatalf("expected no phantom login-keyed account for %q, but one exists", recipientLogin)
	}

	from, err := store.Balance(ctx, "local", channel, senderID)
	if err != nil {
		t.Fatalf("balance sender: %v", err)
	}
	if from.Balance != 60 {
		t.Fatalf("sender balance = %d, want 60", from.Balance)
	}
}

func TestEconomyTransfer_RefusesWhenNoNumericID(t *testing.T) {
	ctx := context.Background()
	store := newTestLoyaltyStore(t)

	const channel = "chan-A"
	const senderID = "999sender"

	if _, err := store.Earn(ctx, "local", channel, senderID, "Sender", 100); err != nil {
		t.Fatalf("seed sender: %v", err)
	}

	resolver := func(_ context.Context, _ string) (commands.UserProfile, error) {
		return commands.UserProfile{ID: "", Login: "bob", DisplayName: "Bob"}, nil
	}

	econ := newEconomyAdapter(store, "local", 5, time.Minute).withResolver(resolver)

	loyErr, _ := econ.Transfer(ctx, channel, senderID, "bob", 40)
	if loyErr != commands.LoyaltyInvalid {
		t.Fatalf("Transfer returned %v, want LoyaltyInvalid", loyErr)
	}

	from, err := store.Balance(ctx, "local", channel, senderID)
	if err != nil {
		t.Fatalf("balance sender: %v", err)
	}
	if from.Balance != 100 {
		t.Fatalf("sender debited despite refused transfer: balance = %d, want 100", from.Balance)
	}
}
