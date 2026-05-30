package featureflags

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

// ErrInvalid is returned when a tenant, channel or feature fails
// validation (empty, or a feature outside [a-z0-9_]+ / too long). The
// wrapped detail says why. Callers should compare with errors.Is.
//
// There is deliberately no ErrNotFound: Get reports absence through its
// found return value, since an unset flag is a normal, expected state.
var ErrInvalid = errors.New("featureflags: invalid flag")

// featureRE constrains feature keys to a tight charset so they can be
// pasted into chat or config without quoting and never collide with
// whitespace tokenisation.
var featureRE = regexp.MustCompile(`^[a-z0-9_]+$`)

// maxFeatureLen caps a feature key at 40 characters.
const maxFeatureLen = 40

// Flag is a single explicit feature override scoped to a (tenant,
// channel). Feature is lower-cased and unique per (tenant, channel).
// Enabled is the stored boolean; UpdatedAt is the last write time
// (UTC, second resolution).
type Flag struct {
	TenantID  string
	Channel   string
	Feature   string
	Enabled   bool
	UpdatedAt time.Time
}

// Store is the persistence contract for per-channel feature toggles. All
// methods are safe for concurrent use; Set is an atomic upsert so
// concurrent writers of the same flag never collide. Only EXPLICIT
// overrides are stored — an unset flag has no row and the caller supplies
// its own default.
type Store interface {
	// Set explicitly enables or disables a feature for a channel (upsert).
	Set(ctx context.Context, tenantID, channel, feature string, enabled bool) error
	// Get returns the explicit setting for a feature. found=false means no
	// explicit override exists (the caller should use its own default).
	Get(ctx context.Context, tenantID, channel, feature string) (enabled bool, found bool, err error)
	// GetOrDefault returns the stored value, or def when none is stored.
	GetOrDefault(ctx context.Context, tenantID, channel, feature string, def bool) (bool, error)
	// List returns all explicit flags for a channel, ordered by feature ASC.
	List(ctx context.Context, tenantID, channel string) ([]Flag, error)
	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring counters.newID.
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

// validateFeature lower-cases and trims feature and enforces the charset
// and length bounds, returning the canonical form or an error wrapping
// ErrInvalid.
func validateFeature(feature string) (string, error) {
	f := strings.ToLower(strings.TrimSpace(feature))
	if f == "" {
		return "", fmt.Errorf("%w: feature is required", ErrInvalid)
	}
	if len(f) > maxFeatureLen {
		return "", fmt.Errorf("%w: feature length %d exceeds %d", ErrInvalid, len(f), maxFeatureLen)
	}
	if !featureRE.MatchString(f) {
		return "", fmt.Errorf("%w: feature %q must match [a-z0-9_]+", ErrInvalid, f)
	}
	return f, nil
}

// validate normalises and checks the full key triple, returning the
// canonical tenant, channel and feature or an error wrapping ErrInvalid.
func validate(tenantID, channel, feature string) (t, c, f string, err error) {
	t = strings.TrimSpace(tenantID)
	if t == "" {
		return "", "", "", fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c = normalizeChannel(channel)
	if c == "" {
		return "", "", "", fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	f, err = validateFeature(feature)
	if err != nil {
		return "", "", "", err
	}
	return t, c, f, nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/counters.sqliteStore conventions:
// WAL, foreign-keys, busy_timeout, SetMaxOpenConns(1) and a sync.Mutex
// around the upsert that Set performs.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger

	// mu serialises Set. The upsert itself is a single atomic ON CONFLICT
	// statement so it cannot leave a duplicate even without the lock, but
	// mu is held for consistency with the rest of the codebase.
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
		return nil, fmt.Errorf("featureflags: open sqlite: %w", err)
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
			return nil, fmt.Errorf("featureflags: %s: %w", p, err)
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
		return fmt.Errorf("featureflags: read migrations dir: %w", err)
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
			return fmt.Errorf("featureflags: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("featureflags: apply migration %s: %w", name, err)
		}
		s.log.Debug("featureflags: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

// Set explicitly enables or disables a feature for a channel, creating the
// row if absent and overwriting it otherwise (upsert).
func (s *sqliteStore) Set(ctx context.Context, tenantID, channel, feature string, enabled bool) error {
	t, c, f, err := validate(tenantID, channel, feature)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Unix()
	// The write is a single atomic upsert: a brand-new flag is INSERTed,
	// while an existing one has enabled/updated_at overwritten in the same
	// statement. The UNIQUE (tenant_id, channel, feature) constraint is the
	// conflict target, so concurrent writers cannot leave a duplicate row.
	const up = `
INSERT INTO feature_flags (id, tenant_id, channel, feature, enabled, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel, feature)
DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, up,
		newID(), t, c, f, boolToInt(enabled), now); err != nil {
		return fmt.Errorf("featureflags: set: %w", err)
	}
	return nil
}

// Get returns the explicit setting for a feature. found=false means no
// explicit override exists, and the caller should fall back to its own
// default (see GetOrDefault).
func (s *sqliteStore) Get(ctx context.Context, tenantID, channel, feature string) (enabled bool, found bool, err error) {
	t, c, f, err := validate(tenantID, channel, feature)
	if err != nil {
		return false, false, err
	}
	var i int64
	row := s.db.QueryRowContext(ctx,
		`SELECT enabled FROM feature_flags
         WHERE tenant_id = ? AND channel = ? AND feature = ?`,
		t, c, f)
	switch err := row.Scan(&i); {
	case errors.Is(err, sql.ErrNoRows):
		return false, false, nil
	case err != nil:
		return false, false, fmt.Errorf("featureflags: get: %w", err)
	default:
		return i != 0, true, nil
	}
}

// GetOrDefault returns the stored value, or def when no explicit override
// is stored.
func (s *sqliteStore) GetOrDefault(ctx context.Context, tenantID, channel, feature string, def bool) (bool, error) {
	enabled, found, err := s.Get(ctx, tenantID, channel, feature)
	if err != nil {
		return false, err
	}
	if !found {
		return def, nil
	}
	return enabled, nil
}

// List returns all explicit flags for a channel, ordered by feature ASC.
// An empty channel yields an empty (nil) slice and no error.
func (s *sqliteStore) List(ctx context.Context, tenantID, channel string) ([]Flag, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c := normalizeChannel(channel)
	if c == "" {
		return nil, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT tenant_id, channel, feature, enabled, updated_at
         FROM feature_flags
         WHERE tenant_id = ? AND channel = ? ORDER BY feature ASC`,
		t, c)
	if err != nil {
		return nil, fmt.Errorf("featureflags: list: %w", err)
	}
	defer rows.Close()
	var out []Flag
	for rows.Next() {
		var (
			fl      Flag
			i       int64
			updated int64
		)
		if err := rows.Scan(&fl.TenantID, &fl.Channel, &fl.Feature, &i, &updated); err != nil {
			return nil, fmt.Errorf("featureflags: scan: %w", err)
		}
		fl.Enabled = i != 0
		fl.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, fl)
	}
	return out, rows.Err()
}

// boolToInt maps a Go bool to the 0/1 SQLite stores in the enabled column.
func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
