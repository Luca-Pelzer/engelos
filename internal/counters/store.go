package counters

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
	// ErrNotFound is returned when a (tenant, channel, name) lookup
	// matches no row.
	ErrNotFound = errors.New("counters: counter not found")

	// ErrInvalid is returned when a counter name fails validation. The
	// wrapped detail says why.
	ErrInvalid = errors.New("counters: invalid counter")
)

// nameRE constrains counter names to a tight charset so triggers can be
// pasted into chat without quoting and never collide with whitespace
// tokenisation by the commands engine.
var nameRE = regexp.MustCompile(`^[a-z0-9_]+$`)

// maxCounterNameLen caps a counter name at 32 characters.
const maxCounterNameLen = 32

// Counter is a named integer counter scoped to a (tenant, channel). Name is
// lower-cased, unique per (tenant, channel). Value may be negative.
type Counter struct {
	ID        string
	TenantID  string
	Channel   string
	Name      string
	Value     int64
	UpdatedAt time.Time
}

// Store is the persistence contract for counters. All methods are safe for
// concurrent use; the read-modify-write performed by Add is atomic so
// concurrent increments of the same counter never lose an update.
type Store interface {
	// Get returns the counter by (tenant,channel,name). ErrNotFound if absent.
	Get(ctx context.Context, tenantID, channel, name string) (Counter, error)
	// Add increments the counter by delta (may be negative), creating it at
	// 0 first if it does not exist (upsert), and returns the NEW value.
	// delta of 0 is allowed (acts as "ensure exists / read").
	Add(ctx context.Context, tenantID, channel, name string, delta int64) (Counter, error)
	// Set assigns an absolute value, creating the counter if absent, and
	// returns it.
	Set(ctx context.Context, tenantID, channel, name string, value int64) (Counter, error)
	// Delete removes the counter. ErrNotFound if absent.
	Delete(ctx context.Context, tenantID, channel, name string) error
	// List returns all counters for (tenant,channel) ordered by Name ASC.
	List(ctx context.Context, tenantID, channel string) ([]Counter, error)
	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring quotes.newID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// normalizeName lower-cases, trims, and strips a leading "!" so callers
// may pass either "!deaths" or "deaths". Returns the canonical form.
func normalizeName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.TrimPrefix(n, "!")
	return n
}

// validateName normalises name and enforces the charset and length bounds,
// returning the canonical form or an error wrapping ErrInvalid.
func validateName(name string) (string, error) {
	n := normalizeName(name)
	if n == "" {
		return "", fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(n) > maxCounterNameLen {
		return "", fmt.Errorf("%w: name length %d exceeds %d", ErrInvalid, len(n), maxCounterNameLen)
	}
	if !nameRE.MatchString(n) {
		return "", fmt.Errorf("%w: name %q must match [a-z0-9_]+", ErrInvalid, n)
	}
	return n, nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/quotes.sqliteStore conventions:
// WAL, foreign-keys, busy_timeout, SetMaxOpenConns(1) and a sync.Mutex
// around the read-modify-write that Add/Set perform.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger

	// mu serialises the upsert-then-read-back in Add/Set. The upsert
	// itself is atomic (single ON CONFLICT statement) so it cannot lose
	// an increment even without the lock, but mu is held both for
	// consistency with the rest of the codebase and so the SELECT that
	// reads back the authoritative new value sees the row this call wrote
	// rather than a concurrent writer's interleaved value.
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
		return nil, fmt.Errorf("counters: open sqlite: %w", err)
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
			return nil, fmt.Errorf("counters: %s: %w", p, err)
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
		return fmt.Errorf("counters: read migrations dir: %w", err)
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
			return fmt.Errorf("counters: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("counters: apply migration %s: %w", name, err)
		}
		s.log.Debug("counters: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const counterSelect = `SELECT id, tenant_id, channel, name, value, updated_at
                       FROM counters `

func scanRow(row interface{ Scan(...any) error }) (Counter, error) {
	var (
		c       Counter
		updated int64
	)
	err := row.Scan(&c.ID, &c.TenantID, &c.Channel, &c.Name, &c.Value, &updated)
	if err != nil {
		return Counter{}, err
	}
	c.UpdatedAt = time.Unix(0, updated).UTC()
	return c, nil
}

func (s *sqliteStore) Get(ctx context.Context, tenantID, channel, name string) (Counter, error) {
	n, err := validateName(name)
	if err != nil {
		return Counter{}, err
	}
	row := s.db.QueryRowContext(ctx,
		counterSelect+`WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, n)
	c, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Counter{}, ErrNotFound
	}
	if err != nil {
		return Counter{}, fmt.Errorf("counters: get: %w", err)
	}
	return c, nil
}

func (s *sqliteStore) Add(ctx context.Context, tenantID, channel, name string, delta int64) (Counter, error) {
	n, err := validateName(name)
	if err != nil {
		return Counter{}, err
	}
	if strings.TrimSpace(tenantID) == "" {
		return Counter{}, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if strings.TrimSpace(channel) == "" {
		return Counter{}, fmt.Errorf("%w: channel is required", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().UnixNano()
	// The increment is a single atomic upsert: a brand-new counter is
	// INSERTed at the delta (from a base of 0), while an existing one has
	// "value = value + delta" applied in the same statement. Because the
	// read and write happen inside one SQL statement, two concurrent Adds
	// cannot read the same old value and lose an update.
	const up = `
INSERT INTO counters (id, tenant_id, channel, name, value, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel, name)
DO UPDATE SET value = value + excluded.value, updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, up,
		newID(), tenantID, channel, n, delta, now); err != nil {
		return Counter{}, fmt.Errorf("counters: add: %w", err)
	}
	return s.getLocked(ctx, tenantID, channel, n)
}

func (s *sqliteStore) Set(ctx context.Context, tenantID, channel, name string, value int64) (Counter, error) {
	n, err := validateName(name)
	if err != nil {
		return Counter{}, err
	}
	if strings.TrimSpace(tenantID) == "" {
		return Counter{}, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if strings.TrimSpace(channel) == "" {
		return Counter{}, fmt.Errorf("%w: channel is required", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().UnixNano()
	const up = `
INSERT INTO counters (id, tenant_id, channel, name, value, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel, name)
DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, up,
		newID(), tenantID, channel, n, value, now); err != nil {
		return Counter{}, fmt.Errorf("counters: set: %w", err)
	}
	return s.getLocked(ctx, tenantID, channel, n)
}

// getLocked reads a counter without re-validating the name and without
// taking s.mu; it is called from Add/Set which already hold the lock and
// have already normalised the name.
func (s *sqliteStore) getLocked(ctx context.Context, tenantID, channel, name string) (Counter, error) {
	row := s.db.QueryRowContext(ctx,
		counterSelect+`WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	c, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Counter{}, ErrNotFound
	}
	if err != nil {
		return Counter{}, fmt.Errorf("counters: get: %w", err)
	}
	return c, nil
}

func (s *sqliteStore) Delete(ctx context.Context, tenantID, channel, name string) error {
	n, err := validateName(name)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM counters WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, n)
	if err != nil {
		return fmt.Errorf("counters: delete: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) List(ctx context.Context, tenantID, channel string) ([]Counter, error) {
	rows, err := s.db.QueryContext(ctx,
		counterSelect+`WHERE tenant_id = ? AND channel = ? ORDER BY name ASC`,
		tenantID, channel)
	if err != nil {
		return nil, fmt.Errorf("counters: list: %w", err)
	}
	defer rows.Close()
	var out []Counter
	for rows.Next() {
		c, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("counters: scan: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
