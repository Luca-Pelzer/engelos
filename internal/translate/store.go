package translate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

// errorsList is intentionally avoided; we expose typed sentinel errors below.

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("translate: not found")

// ErrInvalid is returned when a Config fails validation before persistence.
var ErrInvalid = errors.New("translate: invalid config")

//go:embed migrations/*.sql
var migrationsFS embed.FS

// targetLangRE matches an ISO 639-1 style code with an optional region suffix,
// for example "en" or "pt-br". Validation lowercases input before matching.
var targetLangRE = regexp.MustCompile(`^[a-z]{2}(-[a-z]{2,4})?$`)

// Config is the per-channel chat-translation configuration for a tenant.
//
// A tenant is an organisation/streamer account; a channel is a single chat
// surface (for example a Twitch channel) owned by that tenant. Exactly one
// Config row exists per (tenant, channel) pair.
type Config struct {
	// TenantID is the owning tenant identifier. Required.
	TenantID string
	// Channel is the normalised channel name. Required.
	Channel string
	// Enabled reports whether translation is performed for this channel.
	Enabled bool
	// TargetLang is the ISO 639-1 target language code (for example "en"),
	// lowercased and 2-5 characters of [a-z-]. Defaults to "en" when empty.
	TargetLang string
	// OutputMode selects how the translation is posted back: "chat" or
	// "reply". Defaults to "chat" when empty.
	OutputMode string
	// MinWordCount skips messages with fewer than N words. 0 means use the
	// caller-provided default; must be >= 0.
	MinWordCount int
	// UpdatedAt is the UTC timestamp of the last write.
	UpdatedAt time.Time
}

// Store is the persistence boundary for chat-translation configuration.
type Store interface {
	// Get returns the stored Config for (tenantID, channel) or ErrNotFound.
	Get(ctx context.Context, tenantID, channel string) (Config, error)
	// GetOrDefault returns the stored Config, or a disabled default carrying
	// the tenant and normalised channel when none is stored.
	GetOrDefault(ctx context.Context, tenantID, channel string) (Config, error)
	// Set validates and upserts c, returning the stored Config.
	Set(ctx context.Context, c Config) (Config, error)
	// List returns all Configs for a tenant ordered by channel ascending.
	List(ctx context.Context, tenantID string) ([]Config, error)
	// Close releases underlying resources.
	Close() error
}

// sqliteStore is the modernc.org/sqlite-backed Store implementation.
type sqliteStore struct {
	db *sql.DB
	// mu serialises Set so the read-modify-write upsert stays atomic even
	// under SetMaxOpenConns(1); cheap and keeps semantics obvious.
	mu sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn, applies
// migrations, and returns a ready Store.
//
// dsn is a modernc.org/sqlite DSN. For tests, use
// "file:foo?mode=memory&cache=shared" so multiple connections share state.
func OpenSQLiteStore(ctx context.Context, dsn string) (Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("translate: open: %w", err)
	}
	// SQLite writes serialise anyway; one connection keeps WAL + busy_timeout
	// behaviour predictable and avoids "database is locked" under load.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := applyPragmas(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqliteStore{db: db}, nil
}

// applyPragmas sets connection pragmas for durability and concurrency.
func applyPragmas(ctx context.Context, db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("translate: pragma %q: %w", p, err)
		}
	}
	return nil
}

// migrate applies all embedded SQL migrations in lexicographic order.
func migrate(ctx context.Context, db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("translate: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for _, name := range names {
		b, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("translate: read migration %s: %w", name, err)
		}
		if _, err := db.ExecContext(ctx, string(b)); err != nil {
			return fmt.Errorf("translate: exec migration %s: %w", name, err)
		}
	}
	return nil
}

// newID returns a lowercased ULID for use as a primary key.
func newID() string {
	id := ulid.MustNew(ulid.Now(), ulidEntropy())
	return strings.ToLower(id.String())
}

// ulidEntropy returns a process-seeded entropy source for ULID generation.
func ulidEntropy() *ulid.MonotonicEntropy {
	entropyOnce.Do(func() {
		seed := uint64(time.Now().UnixNano())
		src := rand.New(rand.NewSource(int64(seed)))
		entropy = ulid.Monotonic(src, 0)
	})
	return entropy
}

var (
	entropyOnce sync.Once
	entropy     *ulid.MonotonicEntropy
)

// normalizeChannel lowercases, trims spaces, and strips a single leading '#'.
func normalizeChannel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimSpace(s)
	return strings.ToLower(s)
}

// validate normalises and checks c, returning ErrInvalid on failure.
func validate(c Config) (Config, error) {
	c.TenantID = strings.TrimSpace(c.TenantID)
	if c.TenantID == "" {
		return Config{}, fmt.Errorf("%w: tenant_id required", ErrInvalid)
	}
	c.Channel = normalizeChannel(c.Channel)
	if c.Channel == "" {
		return Config{}, fmt.Errorf("%w: channel required", ErrInvalid)
	}
	c.TargetLang = strings.ToLower(strings.TrimSpace(c.TargetLang))
	if c.TargetLang == "" {
		c.TargetLang = "en"
	}
	if !targetLangRE.MatchString(c.TargetLang) {
		return Config{}, fmt.Errorf("%w: target_lang %q invalid", ErrInvalid, c.TargetLang)
	}
	c.OutputMode = strings.ToLower(strings.TrimSpace(c.OutputMode))
	if c.OutputMode == "" {
		c.OutputMode = "chat"
	}
	if c.OutputMode != "chat" && c.OutputMode != "reply" {
		return Config{}, fmt.Errorf("%w: output_mode %q unsupported", ErrInvalid, c.OutputMode)
	}
	if c.MinWordCount < 0 {
		return Config{}, fmt.Errorf("%w: min_word_count must be >= 0", ErrInvalid)
	}
	return c, nil
}

// boolToInt maps a bool to the integer SQLite stores.
func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint error.
//
// Set uses an upsert so this should not fire in normal operation; it is kept
// for parity with other stores and to surface unexpected races clearly.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

// Get implements Store.
func (s *sqliteStore) Get(ctx context.Context, tenantID, channel string) (Config, error) {
	tenantID = strings.TrimSpace(tenantID)
	channel = normalizeChannel(channel)
	const q = `
SELECT tenant_id, channel, enabled, target_lang, output_mode, min_word_count, updated_at
FROM translate_config
WHERE tenant_id = ? AND channel = ?;`
	row := s.db.QueryRowContext(ctx, q, tenantID, channel)
	return scanConfig(row)
}

// GetOrDefault implements Store.
func (s *sqliteStore) GetOrDefault(ctx context.Context, tenantID, channel string) (Config, error) {
	c, err := s.Get(ctx, tenantID, channel)
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Config{}, err
	}
	return Config{
		TenantID:   strings.TrimSpace(tenantID),
		Channel:    normalizeChannel(channel),
		Enabled:    false,
		TargetLang: "en",
		OutputMode: "chat",
	}, nil
}

// Set implements Store.
func (s *sqliteStore) Set(ctx context.Context, c Config) (Config, error) {
	c, err := validate(c)
	if err != nil {
		return Config{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	c.UpdatedAt = now

	const q = `
INSERT INTO translate_config (id, tenant_id, channel, enabled, target_lang, output_mode, min_word_count, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel) DO UPDATE SET
	enabled = excluded.enabled,
	target_lang = excluded.target_lang,
	output_mode = excluded.output_mode,
	min_word_count = excluded.min_word_count,
	updated_at = excluded.updated_at;`
	_, err = s.db.ExecContext(ctx, q,
		newID(),
		c.TenantID,
		c.Channel,
		boolToInt(c.Enabled),
		c.TargetLang,
		c.OutputMode,
		int64(c.MinWordCount),
		now.UnixNano(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Config{}, fmt.Errorf("%w: duplicate (tenant, channel)", ErrInvalid)
		}
		return Config{}, fmt.Errorf("translate: set: %w", err)
	}
	return c, nil
}

// List implements Store.
func (s *sqliteStore) List(ctx context.Context, tenantID string) ([]Config, error) {
	tenantID = strings.TrimSpace(tenantID)
	const q = `
SELECT tenant_id, channel, enabled, target_lang, output_mode, min_word_count, updated_at
FROM translate_config
WHERE tenant_id = ?
ORDER BY channel ASC;`
	rows, err := s.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("translate: list: %w", err)
	}
	defer rows.Close()

	var out []Config
	for rows.Next() {
		c, err := scanConfigRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("translate: list rows: %w", err)
	}
	return out, nil
}

// Close implements Store.
func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// scanConfig scans a single-row query result into a Config.
func scanConfig(row *sql.Row) (Config, error) {
	var (
		c       Config
		enabled int64
		updated int64
	)
	err := row.Scan(&c.TenantID, &c.Channel, &enabled, &c.TargetLang, &c.OutputMode, &c.MinWordCount, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{}, ErrNotFound
	}
	if err != nil {
		return Config{}, fmt.Errorf("translate: scan: %w", err)
	}
	c.Enabled = enabled != 0
	c.UpdatedAt = time.Unix(0, updated).UTC()
	return c, nil
}

// scanConfigRows scans the current row of a multi-row query into a Config.
func scanConfigRows(rows *sql.Rows) (Config, error) {
	var (
		c       Config
		enabled int64
		updated int64
	)
	err := rows.Scan(&c.TenantID, &c.Channel, &enabled, &c.TargetLang, &c.OutputMode, &c.MinWordCount, &updated)
	if err != nil {
		return Config{}, fmt.Errorf("translate: scan row: %w", err)
	}
	c.Enabled = enabled != 0
	c.UpdatedAt = time.Unix(0, updated).UTC()
	return c, nil
}
