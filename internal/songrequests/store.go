package songrequests

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
	// ErrNotFound is returned by Get when a (tenant, channel) lookup
	// matches no stored configuration row.
	ErrNotFound = errors.New("songrequests: config not found")

	// ErrInvalid is returned when TenantID, Channel, Provider or
	// MaxDurationSec fail validation. The wrapped detail says which field
	// was bad.
	ErrInvalid = errors.New("songrequests: invalid config")
)

// validProviders is the closed set of Provider strings accepted by the
// store. The empty string means song requests are disabled / no provider
// selected; "spotify" and "youtube" are the two supported music backends.
var validProviders = map[string]struct{}{
	"":        {},
	"spotify": {},
	"youtube": {},
}

// Config is the per-channel song-request configuration scoped to a
// (tenant, channel). Provider selects the active music backend
// ("spotify", "youtube", or "" when disabled). SpotifyPlaylistID is the
// playlist requested tracks are queued into (only meaningful when
// Provider == "spotify"). MaxDurationSec caps an individual track's length
// in seconds (0 means no limit). Enabled gates the feature as a whole.
// UpdatedAt is the last write time (UTC).
type Config struct {
	TenantID          string
	Channel           string
	Provider          string // "spotify" | "youtube" | "" (disabled)
	SpotifyPlaylistID string
	MaxDurationSec    int // 0 = no limit
	Enabled           bool
	UpdatedAt         time.Time
}

// Store is the persistence contract for per-channel song-request
// configuration. All methods are safe for concurrent use; Set is an atomic
// upsert so concurrent writers of the same (tenant, channel) never collide.
type Store interface {
	// Get returns the channel's config. Returns ErrNotFound when none is stored.
	Get(ctx context.Context, tenantID, channel string) (Config, error)
	// GetOrDefault returns the stored config, or a zero-value disabled Config
	// (Enabled=false, Provider="") when none stored.
	GetOrDefault(ctx context.Context, tenantID, channel string) (Config, error)
	// Set upserts the channel's config (validates Provider/MaxDurationSec).
	Set(ctx context.Context, c Config) (Config, error)
	// List returns all configs for a tenant, ordered by channel ASC.
	List(ctx context.Context, tenantID string) ([]Config, error)
	// Close releases the db handle.
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

// validate normalises and enforces field-level invariants on a Config,
// returning the canonical Config or an error wrapping ErrInvalid so callers
// can errors.Is-match. The returned Config has TenantID/Channel/Provider
// trimmed/normalised; UpdatedAt is left untouched.
func validate(c Config) (Config, error) {
	c.TenantID = strings.TrimSpace(c.TenantID)
	if c.TenantID == "" {
		return Config{}, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c.Channel = normalizeChannel(c.Channel)
	if c.Channel == "" {
		return Config{}, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
	if _, ok := validProviders[c.Provider]; !ok {
		return Config{}, fmt.Errorf("%w: provider %q is not one of \"\"|spotify|youtube",
			ErrInvalid, c.Provider)
	}
	if c.MaxDurationSec < 0 {
		return Config{}, fmt.Errorf("%w: max_duration_sec %d must be >= 0",
			ErrInvalid, c.MaxDurationSec)
	}
	c.SpotifyPlaylistID = strings.TrimSpace(c.SpotifyPlaylistID)
	return c, nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/customcommands.sqliteStore
// conventions: WAL, foreign-keys, busy_timeout, SetMaxOpenConns(1) and a
// sync.Mutex around the upsert that Set performs.
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
		return nil, fmt.Errorf("songrequests: open sqlite: %w", err)
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
			return nil, fmt.Errorf("songrequests: %s: %w", p, err)
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
		return fmt.Errorf("songrequests: read migrations dir: %w", err)
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
			return fmt.Errorf("songrequests: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("songrequests: apply migration %s: %w", name, err)
		}
		s.log.Debug("songrequests: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const srSelect = `SELECT tenant_id, channel, provider, spotify_playlist_id,
                         max_duration_sec, enabled, updated_at
                  FROM song_request_config `

// scanConfig reads one song_request_config row into a Config, decoding the
// enabled INTEGER (0/1) back to bool and updated_at (UnixNano) back to a UTC
// time.
func scanConfig(row interface{ Scan(...any) error }) (Config, error) {
	var (
		c       Config
		enabled int64
		updated int64
	)
	err := row.Scan(&c.TenantID, &c.Channel, &c.Provider, &c.SpotifyPlaylistID,
		&c.MaxDurationSec, &enabled, &updated)
	if err != nil {
		return Config{}, err
	}
	c.Enabled = enabled != 0
	c.UpdatedAt = time.Unix(0, updated).UTC()
	return c, nil
}

// Set upserts the channel's config. The Provider must be one of "",
// "spotify" or "youtube" and MaxDurationSec must be >= 0; otherwise an error
// wrapping ErrInvalid is returned. The channel is normalised (lower-cased,
// trimmed, leading "#" stripped). UpdatedAt is set to the current time and
// the stored Config is returned.
func (s *sqliteStore) Set(ctx context.Context, c Config) (Config, error) {
	c, err := validate(c)
	if err != nil {
		return Config{}, err
	}
	c.UpdatedAt = time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	// The write is a single atomic upsert: a brand-new config is INSERTed,
	// while an existing one has every mutable column overwritten in the same
	// statement. The UNIQUE (tenant_id, channel) constraint is the conflict
	// target, so concurrent writers cannot leave a duplicate row.
	const up = `
INSERT INTO song_request_config (id, tenant_id, channel, provider,
                                 spotify_playlist_id, max_duration_sec,
                                 enabled, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel)
DO UPDATE SET provider = excluded.provider,
              spotify_playlist_id = excluded.spotify_playlist_id,
              max_duration_sec = excluded.max_duration_sec,
              enabled = excluded.enabled,
              updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, up,
		newID(), c.TenantID, c.Channel, c.Provider, c.SpotifyPlaylistID,
		c.MaxDurationSec, boolToInt(c.Enabled), c.UpdatedAt.UnixNano(),
	); err != nil {
		return Config{}, fmt.Errorf("songrequests: set: %w", err)
	}
	return c, nil
}

// Get returns the channel's config. The channel is normalised before
// lookup. Returns ErrNotFound when no row is stored for the
// (tenant, channel).
func (s *sqliteStore) Get(ctx context.Context, tenantID, channel string) (Config, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return Config{}, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c := normalizeChannel(channel)
	if c == "" {
		return Config{}, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	row := s.db.QueryRowContext(ctx,
		srSelect+`WHERE tenant_id = ? AND channel = ?`, t, c)
	cfg, err := scanConfig(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{}, ErrNotFound
	}
	if err != nil {
		return Config{}, fmt.Errorf("songrequests: get: %w", err)
	}
	return cfg, nil
}

// GetOrDefault returns the stored config, or a zero-value disabled Config
// (Enabled=false, Provider="") carrying the requested TenantID and
// normalised Channel when none is stored.
func (s *sqliteStore) GetOrDefault(ctx context.Context, tenantID, channel string) (Config, error) {
	cfg, err := s.Get(ctx, tenantID, channel)
	if errors.Is(err, ErrNotFound) {
		return Config{
			TenantID: strings.TrimSpace(tenantID),
			Channel:  normalizeChannel(channel),
			Provider: "",
			Enabled:  false,
		}, nil
	}
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// List returns all configs for a tenant, ordered by channel ASC. An unknown
// tenant yields an empty (nil) slice and no error.
func (s *sqliteStore) List(ctx context.Context, tenantID string) ([]Config, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx,
		srSelect+`WHERE tenant_id = ? ORDER BY channel ASC`, t)
	if err != nil {
		return nil, fmt.Errorf("songrequests: list: %w", err)
	}
	defer rows.Close()
	var out []Config
	for rows.Next() {
		c, err := scanConfig(rows)
		if err != nil {
			return nil, fmt.Errorf("songrequests: scan: %w", err)
		}
		out = append(out, c)
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

// isUniqueViolation recognises SQLite's UNIQUE constraint error in a
// driver-agnostic way (modernc.org/sqlite reports the message verbatim).
// Set relies on ON CONFLICT for upserts, so this is provided for parity with
// the customcommands template and any future check-then-insert paths.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
