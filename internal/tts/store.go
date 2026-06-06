package tts

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
var ErrNotFound = errors.New("tts: not found")

// ErrInvalid is returned when a Config fails validation before persistence.
var ErrInvalid = errors.New("tts: invalid config")

// defaultModel matches the elevenlabs client default and the migration default.
const defaultModel = "eleven_flash_v2_5"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Config is the per-channel text-to-speech configuration for a tenant.
//
// Exactly one Config row exists per (tenant, channel) pair. APIKeyCiphertext
// holds the ElevenLabs key already encrypted by the caller; the store never
// sees or produces the plaintext.
type Config struct {
	// TenantID is the owning tenant identifier. Required.
	TenantID string
	// Channel is the normalised channel name. Required.
	Channel string
	// Enabled reports whether TTS is performed for this channel.
	Enabled bool
	// VoiceID is the ElevenLabs voice the bot speaks with. Empty disables
	// synthesis even when Enabled is true.
	VoiceID string
	// Model is the ElevenLabs model id. Defaults to eleven_flash_v2_5.
	Model string
	// APIKeyCiphertext is the bring-your-own-key encrypted at rest by the
	// caller (secrets.Box). The store treats it as opaque bytes.
	APIKeyCiphertext []byte
	// UpdatedAt is the UTC timestamp of the last write.
	UpdatedAt time.Time
}

// Store is the persistence boundary for text-to-speech configuration.
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
	// under SetMaxOpenConns(1).
	mu sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn, applies
// migrations, and returns a ready Store.
func OpenSQLiteStore(ctx context.Context, dsn string) (Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("tts: open: %w", err)
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

func applyPragmas(ctx context.Context, db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("tts: pragma %q: %w", p, err)
		}
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("tts: read migrations: %w", err)
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
			return fmt.Errorf("tts: read migration %s: %w", name, err)
		}
		if _, err := db.ExecContext(ctx, string(b)); err != nil {
			return fmt.Errorf("tts: exec migration %s: %w", name, err)
		}
	}
	return nil
}

func newID() string {
	id := ulid.MustNew(ulid.Now(), ulidEntropy())
	return strings.ToLower(id.String())
}

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

func normalizeChannel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimSpace(s)
	return strings.ToLower(s)
}

func validate(c Config) (Config, error) {
	c.TenantID = strings.TrimSpace(c.TenantID)
	if c.TenantID == "" {
		return Config{}, fmt.Errorf("%w: tenant_id required", ErrInvalid)
	}
	c.Channel = normalizeChannel(c.Channel)
	if c.Channel == "" {
		return Config{}, fmt.Errorf("%w: channel required", ErrInvalid)
	}
	c.VoiceID = strings.TrimSpace(c.VoiceID)
	c.Model = strings.TrimSpace(c.Model)
	if c.Model == "" {
		c.Model = defaultModel
	}
	return c, nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Get implements Store.
func (s *sqliteStore) Get(ctx context.Context, tenantID, channel string) (Config, error) {
	tenantID = strings.TrimSpace(tenantID)
	channel = normalizeChannel(channel)
	const q = `
SELECT tenant_id, channel, enabled, voice_id, model, api_key_ciphertext, updated_at
FROM tts_config
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
		TenantID: strings.TrimSpace(tenantID),
		Channel:  normalizeChannel(channel),
		Enabled:  false,
		Model:    defaultModel,
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
INSERT INTO tts_config (id, tenant_id, channel, enabled, voice_id, model, api_key_ciphertext, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel) DO UPDATE SET
	enabled = excluded.enabled,
	voice_id = excluded.voice_id,
	model = excluded.model,
	api_key_ciphertext = excluded.api_key_ciphertext,
	updated_at = excluded.updated_at;`
	_, err = s.db.ExecContext(ctx, q,
		newID(),
		c.TenantID,
		c.Channel,
		boolToInt(c.Enabled),
		c.VoiceID,
		c.Model,
		c.APIKeyCiphertext,
		now.UnixNano(),
	)
	if err != nil {
		return Config{}, fmt.Errorf("tts: set: %w", err)
	}
	return c, nil
}

// List implements Store.
func (s *sqliteStore) List(ctx context.Context, tenantID string) ([]Config, error) {
	tenantID = strings.TrimSpace(tenantID)
	const q = `
SELECT tenant_id, channel, enabled, voice_id, model, api_key_ciphertext, updated_at
FROM tts_config
WHERE tenant_id = ?
ORDER BY channel ASC;`
	rows, err := s.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("tts: list: %w", err)
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
		return nil, fmt.Errorf("tts: list rows: %w", err)
	}
	return out, nil
}

// Close implements Store.
func (s *sqliteStore) Close() error {
	return s.db.Close()
}

func scanConfig(row *sql.Row) (Config, error) {
	var (
		c       Config
		enabled int64
		cipher  []byte
		updated int64
	)
	err := row.Scan(&c.TenantID, &c.Channel, &enabled, &c.VoiceID, &c.Model, &cipher, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{}, ErrNotFound
	}
	if err != nil {
		return Config{}, fmt.Errorf("tts: scan: %w", err)
	}
	c.Enabled = enabled != 0
	c.APIKeyCiphertext = cipher
	c.UpdatedAt = time.Unix(0, updated).UTC()
	return c, nil
}

func scanConfigRows(rows *sql.Rows) (Config, error) {
	var (
		c       Config
		enabled int64
		cipher  []byte
		updated int64
	)
	err := rows.Scan(&c.TenantID, &c.Channel, &enabled, &c.VoiceID, &c.Model, &cipher, &updated)
	if err != nil {
		return Config{}, fmt.Errorf("tts: scan row: %w", err)
	}
	c.Enabled = enabled != 0
	c.APIKeyCiphertext = cipher
	c.UpdatedAt = time.Unix(0, updated).UTC()
	return c, nil
}
