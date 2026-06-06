package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/Luca-Pelzer/engelos/internal/loyalty"
	"github.com/Luca-Pelzer/engelos/internal/wrapped"
)

// wrappedPeriodAll is the all-time bucket key shared by the recorder and the
// report builder.
const wrappedPeriodAll = "all"

// monthPeriod returns the "YYYY-MM" bucket for t (UTC), matching the period
// format the wrapped store validates.
func monthPeriod(t time.Time) string {
	return t.UTC().Format("2006-01")
}

// wrappedRecorder adapts the wrapped.Store to runtime.WrappedRecorder. It owns
// the period bucketing: every event is recorded twice, once into the all-time
// bucket and once into the current calendar month, so both Wrapped horizons
// stay current without a backfill job. Recording is best-effort; a store error
// is logged and swallowed so it never blocks chat processing.
type wrappedRecorder struct {
	store    wrapped.Store
	tenantID string
	now      func() time.Time
	logger   *slog.Logger
}

func newWrappedRecorder(store wrapped.Store, tenantID string, logger *slog.Logger) wrappedRecorder {
	return wrappedRecorder{store: store, tenantID: tenantID, now: time.Now, logger: logger}
}

// periods returns the two buckets every event is counted into.
func (w wrappedRecorder) periods() []string {
	return []string{wrappedPeriodAll, monthPeriod(w.now())}
}

func (w wrappedRecorder) RecordMessage(ctx context.Context, channel, viewerID, username string) {
	for _, p := range w.periods() {
		if err := w.store.IncrementMessage(ctx, w.tenantID, channel, viewerID, username, p); err != nil {
			w.logger.WarnContext(ctx, "wrapped: record message failed", "channel", channel, "period", p, "err", err)
		}
	}
}

func (w wrappedRecorder) RecordSub(ctx context.Context, channel, viewerID, username string, giftCount int) {
	for _, p := range w.periods() {
		if err := w.store.IncrementSub(ctx, w.tenantID, channel, viewerID, username, p); err != nil {
			w.logger.WarnContext(ctx, "wrapped: record sub failed", "channel", channel, "period", p, "err", err)
		}
		if giftCount > 0 {
			if err := w.store.IncrementSubGift(ctx, w.tenantID, channel, viewerID, username, p, int64(giftCount)); err != nil {
				w.logger.WarnContext(ctx, "wrapped: record sub gift failed", "channel", channel, "period", p, "err", err)
			}
		}
	}
}

func (w wrappedRecorder) RecordRaid(ctx context.Context, channel, fromUsername string) {
	// Raids carry only a from-username (no stable user id), so the username
	// doubles as the viewer key for the raider's counter.
	for _, p := range w.periods() {
		if err := w.store.IncrementRaidStarted(ctx, w.tenantID, channel, fromUsername, fromUsername, p); err != nil {
			w.logger.WarnContext(ctx, "wrapped: record raid failed", "channel", channel, "period", p, "err", err)
		}
	}
}

// wrappedRankerAdapter supplies the loyalty + streak extras shown on a viewer
// Wrapped card, satisfying handlers.WrappedRanker. It reads the loyalty
// balance and the streak read-model's all-time longest streak, staying
// best-effort: any error yields a zero value rather than failing the card.
type wrappedRankerAdapter struct {
	loyalty  loyalty.Store
	streak   *streak.System
	tenantID string
}

func (a wrappedRankerAdapter) Points(ctx context.Context, channel, viewerID string) int64 {
	if a.loyalty == nil {
		return 0
	}
	acct, err := a.loyalty.Balance(ctx, a.tenantID, channel, viewerID)
	if err != nil {
		return 0
	}
	return acct.Balance
}

func (a wrappedRankerAdapter) LongestStreak(_ context.Context, channel, viewerID string) int {
	if a.streak == nil {
		return 0
	}
	return a.streak.ReadModel().Get(a.tenantID, channel, viewerID).DaysLongest
}
