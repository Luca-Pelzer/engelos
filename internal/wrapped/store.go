// Package wrapped is a live-accumulating per-viewer statistics store for
// the "Stream Wrapped" (Spotify-Wrapped-style) recap feature. The chat
// dispatcher calls the Increment* methods as events happen; a report
// builder later reads the aggregates to render monthly and all-time
// Wrapped cards.
//
// Counters are bucketed by a period string so the same table backs both
// horizons: "all" for all-time and "YYYY-MM" (e.g. "2026-05") for a single
// month. Every row is scoped to a (tenant, channel, viewer, period) tuple.
//
// The package mirrors the internal/customcommands and internal/featureflags
// store conventions exactly: an embedded migrations directory applied on
// open, WAL + foreign-keys + busy_timeout pragmas, a single open
// connection, lower-cased ULID identifiers, Unix-nanosecond timestamps, and
// a sync.Mutex guarding the writes.
package wrapped

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Sentinel errors. Callers should compare with errors.Is.
var (
	// ErrNotFound is returned by ViewerStat when a (tenant, channel,
	// viewer, period) lookup matches no row.
	ErrNotFound = errors.New("wrapped: stat not found")

	// ErrInvalid is returned when a tenant, channel, viewer, period or
	// gift count fails validation. The wrapped detail says which field was
	// bad. Callers should compare with errors.Is.
	ErrInvalid = errors.New("wrapped: invalid stat")
)

// periodRE matches a monthly period bucket "YYYY-MM". The literal "all"
// bucket is accepted separately (see validatePeriod), so this regexp only
// has to recognise the calendar form.
var periodRE = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// maxTopChatters caps the number of rows TopChatters will return; the
// requested limit is clamped into [1, maxTopChatters].
const maxTopChatters = 100

// Stat is one viewer's accumulated counters for a (channel, period). The
// counters only ever increase. Username is refreshed on every increment so
// it tracks the viewer's most recent display name. FirstSeen is fixed at
// row creation; LastSeen and UpdatedAt move forward with each increment.
type Stat struct {
	TenantID     string
	Channel      string
	ViewerID     string
	Username     string
	Period       string // "all" | "YYYY-MM"
	Messages     int64
	SubsGiven    int64 // gift subs this viewer gave
	SubsTotal    int64 // this viewer's own subs/resubs
	RaidsStarted int64 // raids this viewer started toward the channel
	FirstSeen    time.Time
	LastSeen     time.Time
	UpdatedAt    time.Time
}

// ChannelSummary is the aggregate of every viewer's Stat for a
// (channel, period). It powers the channel-wide Wrapped card. TotalViewers
// is the number of DISTINCT viewers with at least one row in the bucket.
type ChannelSummary struct {
	Channel       string
	Period        string
	TotalMessages int64
	TotalSubs     int64
	TotalSubGifts int64
	TotalRaids    int64
	TotalViewers  int64 // distinct viewers with >=1 row
}

// Store is the persistence contract for Stream Wrapped statistics. All
// methods are safe for concurrent use; each Increment* is an atomic upsert
// on (tenant, channel, viewer, period) so concurrent writers never leave a
// duplicate row.
type Store interface {
	// IncrementMessage adds 1 to Messages for (tenant, channel, viewer,
	// period), upserting the row and refreshing Username/LastSeen (and
	// FirstSeen on create).
	IncrementMessage(ctx context.Context, tenantID, channel, viewerID, username, period string) error
	// IncrementSub adds 1 to SubsTotal for (tenant, channel, viewer,
	// period), upserting the row and refreshing Username/LastSeen.
	IncrementSub(ctx context.Context, tenantID, channel, viewerID, username, period string) error
	// IncrementSubGift adds n (>=1) to SubsGiven for (tenant, channel,
	// viewer, period), upserting the row and refreshing Username/LastSeen.
	IncrementSubGift(ctx context.Context, tenantID, channel, viewerID, username, period string, n int64) error
	// IncrementRaidStarted adds 1 to RaidsStarted for (tenant, channel,
	// viewer, period), upserting the row and refreshing Username/LastSeen.
	IncrementRaidStarted(ctx context.Context, tenantID, channel, viewerID, username, period string) error
	// TopChatters returns viewers ordered by Messages DESC (ties Username
	// ASC) for (tenant, channel, period). limit is clamped into [1, 100].
	TopChatters(ctx context.Context, tenantID, channel, period string, limit int) ([]Stat, error)
	// ViewerStat returns one viewer's Stat for (tenant, channel, viewer,
	// period); ErrNotFound if none exists.
	ViewerStat(ctx context.Context, tenantID, channel, viewerID, period string) (Stat, error)
	// ChannelTotals returns the summed Stat across all viewers for
	// (tenant, channel, period), including the DISTINCT viewer count in
	// TotalViewers. An empty bucket yields a zero-value ChannelSummary
	// (carrying Channel and Period) and no error.
	ChannelTotals(ctx context.Context, tenantID, channel, period string) (ChannelSummary, error)
	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring customcommands.newID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// normalizeChannel lower-cases, trims, and strips a leading "#" so callers
// may pass either "#channel" or "channel". Returns the canonical form.
func normalizeChannel(channel string) string {
	c := strings.ToLower(strings.TrimSpace(channel))
	c = strings.TrimPrefix(c, "#")
	return c
}

// validatePeriod checks that period is either the literal "all" bucket or a
// calendar month "YYYY-MM", returning the trimmed canonical form or an error
// wrapping ErrInvalid.
func validatePeriod(period string) (string, error) {
	p := strings.TrimSpace(period)
	if p == "" {
		return "", fmt.Errorf("%w: period is required", ErrInvalid)
	}
	if p == "all" {
		return p, nil
	}
	if !periodRE.MatchString(p) {
		return "", fmt.Errorf("%w: period %q must be \"all\" or YYYY-MM", ErrInvalid, p)
	}
	return p, nil
}

// validateKey normalises and checks the (tenant, channel, viewer, period)
// key, returning the canonical components or an error wrapping ErrInvalid.
// Username is intentionally not validated - it is stored verbatim.
func validateKey(tenantID, channel, viewerID, period string) (t, c, v, p string, err error) {
	t = strings.TrimSpace(tenantID)
	if t == "" {
		return "", "", "", "", fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c = normalizeChannel(channel)
	if c == "" {
		return "", "", "", "", fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	v = strings.TrimSpace(viewerID)
	if v == "" {
		return "", "", "", "", fmt.Errorf("%w: viewer_id is required", ErrInvalid)
	}
	p, err = validatePeriod(period)
	if err != nil {
		return "", "", "", "", err
	}
	return t, c, v, p, nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/featureflags.sqliteStore
// conventions: WAL, foreign-keys, busy_timeout, SetMaxOpenConns(1) and a
// sync.Mutex around the upserts the Increment* methods perform.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger

	// mu serialises the Increment* upserts. Each upsert is a single atomic
	// ON CONFLICT statement so it cannot leave a duplicate even without the
	// lock, but mu is held for consistency with the rest of the codebase.
	mu sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn and returns a
// ready-to-use Store. dsn may be a file path or a full modernc.org/sqlite
// DSN. Use "file::memory:?cache=shared" for tests.
//
// The returned Store has WAL journal mode, foreign-keys ON, and
// synchronous=NORMAL.
func OpenSQLiteStore(ctx context.Context, dsn string, logger *slog.Logger) (Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("wrapped: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("wrapped: %s: %w", p, err)
		}
	}

	s := &sqliteStore{db: db, log: logger}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *sqliteStore) migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("wrapped: read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("wrapped: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("wrapped: apply migration %s: %w", name, err)
		}
		s.log.Debug("wrapped: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

// increment performs the shared upsert for every Increment* method. column
// is the counter to bump and delta is the amount added to it. The first
// write creates the row with the counter set to delta and first_seen ==
// last_seen == updated_at == now; subsequent writes add delta to the
// counter and refresh username/last_seen/updated_at while leaving
// first_seen untouched (ON CONFLICT does not list it).
//
// column is supplied only from the fixed, package-internal call sites below
// and is never derived from caller input, so interpolating it into the SQL
// is safe.
func (s *sqliteStore) increment(ctx context.Context, tenantID, channel, viewerID, username, period, column string, delta int64) error {
	t, c, v, p, err := validateKey(tenantID, channel, viewerID, period)
	if err != nil {
		return err
	}
	u := strings.TrimSpace(username)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().UnixNano()
	// The write is a single atomic upsert: a brand-new viewer row is
	// INSERTed with the counter seeded to delta, while an existing row has
	// the counter incremented and username/last_seen/updated_at refreshed in
	// the same statement. The UNIQUE (tenant_id, channel, viewer_id, period)
	// constraint is the conflict target, so concurrent writers cannot leave
	// a duplicate row. first_seen is deliberately absent from the DO UPDATE
	// SET list so it is preserved at its original value.
	up := fmt.Sprintf(`
INSERT INTO wrapped_stats (id, tenant_id, channel, viewer_id, username, period,
                           %s, first_seen, last_seen, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel, viewer_id, period)
DO UPDATE SET %s = %s + excluded.%s,
              username = excluded.username,
              last_seen = excluded.last_seen,
              updated_at = excluded.updated_at`, column, column, column, column)
	if _, err := s.db.ExecContext(ctx, up,
		newID(), t, c, v, u, p, delta, now, now, now); err != nil {
		return fmt.Errorf("wrapped: increment %s: %w", column, err)
	}
	return nil
}

// IncrementMessage adds 1 to Messages for (tenant, channel, viewer,
// period), upserting the row and refreshing Username/LastSeen (and
// FirstSeen on create).
func (s *sqliteStore) IncrementMessage(ctx context.Context, tenantID, channel, viewerID, username, period string) error {
	return s.increment(ctx, tenantID, channel, viewerID, username, period, "messages", 1)
}

// IncrementSub adds 1 to SubsTotal for (tenant, channel, viewer, period),
// upserting the row and refreshing Username/LastSeen.
func (s *sqliteStore) IncrementSub(ctx context.Context, tenantID, channel, viewerID, username, period string) error {
	return s.increment(ctx, tenantID, channel, viewerID, username, period, "subs_total", 1)
}

// IncrementSubGift adds n (>=1) to SubsGiven for (tenant, channel, viewer,
// period), upserting the row and refreshing Username/LastSeen. A non-positive
// n returns ErrInvalid.
func (s *sqliteStore) IncrementSubGift(ctx context.Context, tenantID, channel, viewerID, username, period string, n int64) error {
	if n < 1 {
		return fmt.Errorf("%w: gift count %d must be >= 1", ErrInvalid, n)
	}
	return s.increment(ctx, tenantID, channel, viewerID, username, period, "subs_given", n)
}

// IncrementRaidStarted adds 1 to RaidsStarted for (tenant, channel, viewer,
// period), upserting the row and refreshing Username/LastSeen.
func (s *sqliteStore) IncrementRaidStarted(ctx context.Context, tenantID, channel, viewerID, username, period string) error {
	return s.increment(ctx, tenantID, channel, viewerID, username, period, "raids_started", 1)
}

const statSelect = `SELECT tenant_id, channel, viewer_id, username, period,
                           messages, subs_given, subs_total, raids_started,
                           first_seen, last_seen, updated_at
                    FROM wrapped_stats `

// scanStat reads one wrapped_stats row into a Stat, converting the
// Unix-nanosecond timestamps back into UTC time.Time values.
func scanStat(row interface{ Scan(...any) error }) (Stat, error) {
	var (
		st                   Stat
		first, last, updated int64
	)
	err := row.Scan(&st.TenantID, &st.Channel, &st.ViewerID, &st.Username, &st.Period,
		&st.Messages, &st.SubsGiven, &st.SubsTotal, &st.RaidsStarted,
		&first, &last, &updated)
	if err != nil {
		return Stat{}, err
	}
	st.FirstSeen = time.Unix(0, first).UTC()
	st.LastSeen = time.Unix(0, last).UTC()
	st.UpdatedAt = time.Unix(0, updated).UTC()
	return st, nil
}

// TopChatters returns viewers ordered by Messages DESC (ties broken by
// Username ASC) for (tenant, channel, period). limit is clamped into
// [1, 100]. An empty bucket yields a nil slice and no error.
func (s *sqliteStore) TopChatters(ctx context.Context, tenantID, channel, period string, limit int) ([]Stat, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c := normalizeChannel(channel)
	if c == "" {
		return nil, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	p, err := validatePeriod(period)
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxTopChatters {
		limit = maxTopChatters
	}
	rows, err := s.db.QueryContext(ctx,
		statSelect+`WHERE tenant_id = ? AND channel = ? AND period = ?
                    ORDER BY messages DESC, username ASC
                    LIMIT ?`,
		t, c, p, limit)
	if err != nil {
		return nil, fmt.Errorf("wrapped: top chatters: %w", err)
	}
	defer rows.Close()
	var out []Stat
	for rows.Next() {
		st, err := scanStat(rows)
		if err != nil {
			return nil, fmt.Errorf("wrapped: scan: %w", err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ViewerStat returns one viewer's Stat for (tenant, channel, viewer,
// period); ErrNotFound if no row exists.
func (s *sqliteStore) ViewerStat(ctx context.Context, tenantID, channel, viewerID, period string) (Stat, error) {
	t, c, v, p, err := validateKey(tenantID, channel, viewerID, period)
	if err != nil {
		return Stat{}, err
	}
	row := s.db.QueryRowContext(ctx,
		statSelect+`WHERE tenant_id = ? AND channel = ? AND viewer_id = ? AND period = ?`,
		t, c, v, p)
	st, err := scanStat(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Stat{}, ErrNotFound
	}
	if err != nil {
		return Stat{}, fmt.Errorf("wrapped: viewer stat: %w", err)
	}
	return st, nil
}

// ChannelTotals returns the summed Stat across all viewers for
// (tenant, channel, period), with the DISTINCT viewer count in
// TotalViewers. An empty bucket yields a zero-value ChannelSummary carrying
// only Channel and Period, and no error.
func (s *sqliteStore) ChannelTotals(ctx context.Context, tenantID, channel, period string) (ChannelSummary, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return ChannelSummary{}, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c := normalizeChannel(channel)
	if c == "" {
		return ChannelSummary{}, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	p, err := validatePeriod(period)
	if err != nil {
		return ChannelSummary{}, err
	}
	// COALESCE turns the all-NULL result of an empty aggregate into zeros so
	// an absent bucket maps cleanly to a zero-value summary. COUNT already
	// yields 0 on no rows. subs_total -> TotalSubs, subs_given ->
	// TotalSubGifts, raids_started -> TotalRaids.
	const q = `
SELECT COALESCE(SUM(messages), 0),
       COALESCE(SUM(subs_total), 0),
       COALESCE(SUM(subs_given), 0),
       COALESCE(SUM(raids_started), 0),
       COUNT(*)
FROM wrapped_stats
WHERE tenant_id = ? AND channel = ? AND period = ?`
	sum := ChannelSummary{Channel: c, Period: p}
	row := s.db.QueryRowContext(ctx, q, t, c, p)
	if err := row.Scan(&sum.TotalMessages, &sum.TotalSubs, &sum.TotalSubGifts,
		&sum.TotalRaids, &sum.TotalViewers); err != nil {
		return ChannelSummary{}, fmt.Errorf("wrapped: channel totals: %w", err)
	}
	return sum, nil
}

// isUniqueViolation recognises SQLite's UNIQUE constraint error in a
// driver-agnostic way (modernc.org/sqlite reports the message verbatim).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
