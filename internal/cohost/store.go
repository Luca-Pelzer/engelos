package cohost

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("cohost: not found")

// ErrInvalid is returned when a Config fails validation before persistence.
var ErrInvalid = errors.New("cohost: invalid config")

// defaultBotName is how viewers address the co-host when none is configured.
const defaultBotName = "bot"

// defaultPersona is the fallback style instruction folded into the system prompt.
const defaultPersona = "a friendly, concise stream co-host"

// defaultMaxReplyLen caps the characters of a co-host reply posted to chat.
const defaultMaxReplyLen = 280

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Config is the per-channel AI co-host configuration for a tenant.
//
// A tenant is a streamer account; a channel is a single chat surface owned by
// that tenant. Exactly one Config row exists per (tenant, channel) pair.
type Config struct {
	// TenantID is the owning tenant identifier. Required.
	TenantID string
	// Channel is the normalised channel name. Required.
	Channel string
	// Enabled reports whether the co-host answers in this channel. Default
	// false: it spends the streamer's Claude subscription, so it is opt-in.
	Enabled bool
	// BotName is how viewers address the co-host (for example "engel").
	// Defaults to "bot" when empty.
	BotName string
	// Persona is a short style instruction folded into the system prompt.
	// Defaults to a friendly, concise co-host when empty.
	Persona string
	// MaxReplyLen caps the runes of a reply posted to chat. 0 means use the
	// default (280); must be >= 0.
	MaxReplyLen int
	// UpdatedAt is the UTC timestamp of the last write.
	UpdatedAt time.Time
}

// Store is the persistence boundary for AI co-host configuration.
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
		return nil, fmt.Errorf("cohost: open: %w", err)
	}
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
			return fmt.Errorf("cohost: pragma %q: %w", p, err)
		}
	}
	return nil
}

// migrate applies all embedded SQL migrations in lexicographic order.
func migrate(ctx context.Context, db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("cohost: read migrations: %w", err)
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
			return fmt.Errorf("cohost: read migration %s: %w", name, err)
		}
		if _, err := db.ExecContext(ctx, string(b)); err != nil {
			return fmt.Errorf("cohost: exec migration %s: %w", name, err)
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
	c.BotName = strings.TrimSpace(c.BotName)
	if c.BotName == "" {
		c.BotName = defaultBotName
	}
	c.Persona = strings.TrimSpace(c.Persona)
	if c.Persona == "" {
		c.Persona = defaultPersona
	}
	if c.MaxReplyLen < 0 {
		return Config{}, fmt.Errorf("%w: max_reply_len must be >= 0", ErrInvalid)
	}
	if c.MaxReplyLen == 0 {
		c.MaxReplyLen = defaultMaxReplyLen
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
SELECT tenant_id, channel, enabled, bot_name, persona, max_reply_len, updated_at
FROM cohost_config
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
		TenantID:    strings.TrimSpace(tenantID),
		Channel:     normalizeChannel(channel),
		Enabled:     false,
		BotName:     defaultBotName,
		Persona:     defaultPersona,
		MaxReplyLen: defaultMaxReplyLen,
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
INSERT INTO cohost_config (id, tenant_id, channel, enabled, bot_name, persona, max_reply_len, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel) DO UPDATE SET
	enabled = excluded.enabled,
	bot_name = excluded.bot_name,
	persona = excluded.persona,
	max_reply_len = excluded.max_reply_len,
	updated_at = excluded.updated_at;`
	_, err = s.db.ExecContext(ctx, q,
		newID(),
		c.TenantID,
		c.Channel,
		boolToInt(c.Enabled),
		c.BotName,
		c.Persona,
		int64(c.MaxReplyLen),
		now.UnixNano(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Config{}, fmt.Errorf("%w: duplicate (tenant, channel)", ErrInvalid)
		}
		return Config{}, fmt.Errorf("cohost: set: %w", err)
	}
	return c, nil
}

// List implements Store.
func (s *sqliteStore) List(ctx context.Context, tenantID string) ([]Config, error) {
	tenantID = strings.TrimSpace(tenantID)
	const q = `
SELECT tenant_id, channel, enabled, bot_name, persona, max_reply_len, updated_at
FROM cohost_config
WHERE tenant_id = ?
ORDER BY channel ASC;`
	rows, err := s.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("cohost: list: %w", err)
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
		return nil, fmt.Errorf("cohost: list rows: %w", err)
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
	err := row.Scan(&c.TenantID, &c.Channel, &enabled, &c.BotName, &c.Persona, &c.MaxReplyLen, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{}, ErrNotFound
	}
	if err != nil {
		return Config{}, fmt.Errorf("cohost: scan: %w", err)
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
	err := rows.Scan(&c.TenantID, &c.Channel, &enabled, &c.BotName, &c.Persona, &c.MaxReplyLen, &updated)
	if err != nil {
		return Config{}, fmt.Errorf("cohost: scan row: %w", err)
	}
	c.Enabled = enabled != 0
	c.UpdatedAt = time.Unix(0, updated).UTC()
	return c, nil
}
