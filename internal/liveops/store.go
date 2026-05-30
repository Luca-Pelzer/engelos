package liveops

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
	// ErrNotFound is returned when a (tenant, channel, number) lookup
	// matches no row, or Next finds no upcoming event.
	ErrNotFound = errors.New("liveops: event not found")

	// ErrInvalid is returned when event fields fail validation. The
	// wrapped detail says why.
	ErrInvalid = errors.New("liveops: invalid event")
)

// Field length bounds.
const (
	// maxNameLen caps an event name at 80 characters.
	maxNameLen = 80
	// maxDescriptionLen caps an event description at 200 characters.
	maxDescriptionLen = 200
)

// defaultUpcomingLimit is the cap applied by Upcoming when the caller
// passes limit <= 0.
const defaultUpcomingLimit = 5

// Event is a scheduled Live-Ops item scoped to a (tenant, channel) — e.g.
// a "Double Points Weekend" or "Season 3 starts" the bot counts down to.
// Number is a per-channel 1-based sequence shown to users; it is NOT a
// global primary key. ID is the internal ULID primary key. EndsAt is nil
// for an instantaneous milestone with no defined end.
type Event struct {
	ID          string
	TenantID    string
	Channel     string
	Number      int
	Name        string
	Description string
	StartsAt    time.Time
	EndsAt      *time.Time
	CreatedAt   time.Time
}

// Store is the persistence contract for scheduled events. All methods are
// safe for concurrent use; per-channel Number assignment is serialised so
// concurrent Adds never collide.
type Store interface {
	// Add inserts a new event and returns it with its assigned per-channel
	// Number. Returns ErrInvalid for empty/over-long fields, a zero
	// startsAt, or an endsAt before startsAt.
	Add(ctx context.Context, tenantID, channel, name, description string, startsAt time.Time, endsAt *time.Time) (Event, error)
	// Delete removes the event with the given number. ErrNotFound if absent.
	// Numbers of OTHER events are NOT renumbered (gaps are expected).
	Delete(ctx context.Context, tenantID, channel string, number int) error
	// Next returns the earliest event whose StartsAt is strictly after now.
	// ErrNotFound if no such event exists.
	Next(ctx context.Context, tenantID, channel string, now time.Time) (Event, error)
	// Upcoming returns events that are upcoming (StartsAt > now) OR active
	// (StartsAt <= now AND EndsAt != nil AND EndsAt >= now), ordered by
	// StartsAt ASC and capped at limit. limit <= 0 defaults to 5.
	Upcoming(ctx context.Context, tenantID, channel string, now time.Time, limit int) ([]Event, error)
	// List returns all events for the channel ordered by StartsAt ASC.
	List(ctx context.Context, tenantID, channel string) ([]Event, error)
	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring counters.newID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// validate normalises and bounds-checks the user-supplied event fields,
// returning the trimmed name/description or an error wrapping ErrInvalid.
func validate(tenantID, channel, name, description string, startsAt time.Time, endsAt *time.Time) (string, string, error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", "", fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if strings.TrimSpace(channel) == "" {
		return "", "", fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	n := strings.TrimSpace(name)
	if n == "" {
		return "", "", fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(n) > maxNameLen {
		return "", "", fmt.Errorf("%w: name length %d exceeds %d", ErrInvalid, len(n), maxNameLen)
	}
	d := strings.TrimSpace(description)
	if len(d) > maxDescriptionLen {
		return "", "", fmt.Errorf("%w: description length %d exceeds %d", ErrInvalid, len(d), maxDescriptionLen)
	}
	if startsAt.IsZero() {
		return "", "", fmt.Errorf("%w: starts_at is required", ErrInvalid)
	}
	if endsAt != nil && endsAt.Before(startsAt) {
		return "", "", fmt.Errorf("%w: ends_at is before starts_at", ErrInvalid)
	}
	return n, d, nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/quotes.sqliteStore conventions:
// WAL, foreign-keys, busy_timeout, SetMaxOpenConns(1) and a sync.Mutex
// serialising the read-max-then-insert that assigns Number.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger

	// mu serialises the MAX(number)+1 read and the subsequent INSERT so
	// concurrent Adds to the same channel cannot be assigned the same
	// Number.
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
		return nil, fmt.Errorf("liveops: open sqlite: %w", err)
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
			return nil, fmt.Errorf("liveops: %s: %w", p, err)
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
		return fmt.Errorf("liveops: read migrations dir: %w", err)
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
			return fmt.Errorf("liveops: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("liveops: apply migration %s: %w", name, err)
		}
		s.log.Debug("liveops: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const eventSelect = `SELECT id, tenant_id, channel, number, name, description, starts_at, ends_at, created_at
                     FROM events `

func scanRow(row interface{ Scan(...any) error }) (Event, error) {
	var (
		e       Event
		starts  int64
		ends    sql.NullInt64
		created int64
	)
	err := row.Scan(&e.ID, &e.TenantID, &e.Channel, &e.Number, &e.Name,
		&e.Description, &starts, &ends, &created)
	if err != nil {
		return Event{}, err
	}
	e.StartsAt = time.Unix(0, starts).UTC()
	if ends.Valid {
		t := time.Unix(0, ends.Int64).UTC()
		e.EndsAt = &t
	}
	e.CreatedAt = time.Unix(0, created).UTC()
	return e, nil
}

func (s *sqliteStore) Add(ctx context.Context, tenantID, channel, name, description string, startsAt time.Time, endsAt *time.Time) (Event, error) {
	n, d, err := validate(tenantID, channel, name, description, startsAt, endsAt)
	if err != nil {
		return Event{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// The visible Number is assigned as MAX(number)+1 for this
	// (tenant, channel), computed under s.mu so two concurrent Adds can
	// never read the same MAX and collide on the unique index. The first
	// event in a channel gets Number 1.
	var maxNum sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(number) FROM events WHERE tenant_id = ? AND channel = ?`,
		tenantID, channel).Scan(&maxNum); err != nil {
		return Event{}, fmt.Errorf("liveops: max number: %w", err)
	}
	number := int(maxNum.Int64) + 1

	starts := startsAt.UTC()
	e := Event{
		ID:          newID(),
		TenantID:    tenantID,
		Channel:     channel,
		Number:      number,
		Name:        n,
		Description: d,
		StartsAt:    starts,
		CreatedAt:   time.Now().UTC(),
	}

	var endsArg any
	if endsAt != nil {
		ends := endsAt.UTC()
		e.EndsAt = &ends
		endsArg = ends.UnixNano()
	}

	const ins = `
INSERT INTO events (id, tenant_id, channel, number, name, description, starts_at, ends_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		e.ID, e.TenantID, e.Channel, e.Number, e.Name, e.Description,
		e.StartsAt.UnixNano(), endsArg, e.CreatedAt.UnixNano(),
	); err != nil {
		return Event{}, fmt.Errorf("liveops: insert: %w", err)
	}
	return e, nil
}

func (s *sqliteStore) Delete(ctx context.Context, tenantID, channel string, number int) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM events WHERE tenant_id = ? AND channel = ? AND number = ?`,
		tenantID, channel, number)
	if err != nil {
		return fmt.Errorf("liveops: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) Next(ctx context.Context, tenantID, channel string, now time.Time) (Event, error) {
	row := s.db.QueryRowContext(ctx,
		eventSelect+`WHERE tenant_id = ? AND channel = ? AND starts_at > ?
                     ORDER BY starts_at ASC LIMIT 1`,
		tenantID, channel, now.UTC().UnixNano())
	e, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("liveops: next: %w", err)
	}
	return e, nil
}

func (s *sqliteStore) Upcoming(ctx context.Context, tenantID, channel string, now time.Time, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = defaultUpcomingLimit
	}
	nowNanos := now.UTC().UnixNano()
	// An event qualifies when it is upcoming (starts in the future) OR
	// active (already started but has a defined end that has not yet
	// passed). Instantaneous milestones (ends_at NULL) drop out once they
	// start, so only their countdown phase appears here.
	rows, err := s.db.QueryContext(ctx,
		eventSelect+`WHERE tenant_id = ? AND channel = ?
                     AND (starts_at > ?
                          OR (starts_at <= ? AND ends_at IS NOT NULL AND ends_at >= ?))
                     ORDER BY starts_at ASC LIMIT ?`,
		tenantID, channel, nowNanos, nowNanos, nowNanos, limit)
	if err != nil {
		return nil, fmt.Errorf("liveops: upcoming: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("liveops: scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) List(ctx context.Context, tenantID, channel string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		eventSelect+`WHERE tenant_id = ? AND channel = ? ORDER BY starts_at ASC`,
		tenantID, channel)
	if err != nil {
		return nil, fmt.Errorf("liveops: list: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("liveops: scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
