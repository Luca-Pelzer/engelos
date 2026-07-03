package aiconfig

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when no config row exists for a tenant.
var ErrNotFound = errors.New("aiconfig: not found")

// ErrInvalid is returned when a StoredConfig fails validation before persistence.
var ErrInvalid = errors.New("aiconfig: invalid config")

//go:embed migrations/*.sql
var migrationsFS embed.FS

// StoredConfig is the persisted AI backend configuration for a tenant.
//
// Exactly one StoredConfig row exists per tenant. APIKeyCiphertext holds the
// provider API key already encrypted by the caller (secrets.Box); the store
// treats it as opaque bytes and never sees or produces the plaintext.
type StoredConfig struct {
	// TenantID is the owning tenant identifier. Required.
	TenantID string
	// Provider names the backend provider (for example "anthropic" or
	// "openai"). Empty means "fall back to env/default at resolution time".
	Provider string
	// BaseURL overrides the provider endpoint root. Empty keeps the default.
	BaseURL string
	// Model overrides the provider model id. Empty keeps the default.
	Model string
	// APIKeyCiphertext is the provider key encrypted at rest by the caller.
	// Nil or empty means no key is stored.
	APIKeyCiphertext []byte
	// UpdatedAt is the UTC timestamp of the last write.
	UpdatedAt time.Time
}

// Store is the persistence boundary for AI backend configuration.
type Store interface {
	// Get returns the stored config for tenantID or ErrNotFound.
	Get(ctx context.Context, tenantID string) (StoredConfig, error)
	// Set validates and upserts c, returning the stored config.
	Set(ctx context.Context, c StoredConfig) (StoredConfig, error)
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
		return nil, fmt.Errorf("aiconfig: open: %w", err)
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
			return fmt.Errorf("aiconfig: pragma %q: %w", p, err)
		}
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("aiconfig: read migrations: %w", err)
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
			return fmt.Errorf("aiconfig: read migration %s: %w", name, err)
		}
		if _, err := db.ExecContext(ctx, string(b)); err != nil {
			return fmt.Errorf("aiconfig: exec migration %s: %w", name, err)
		}
	}
	return nil
}

func validate(c StoredConfig) (StoredConfig, error) {
	c.TenantID = strings.TrimSpace(c.TenantID)
	if c.TenantID == "" {
		return StoredConfig{}, fmt.Errorf("%w: tenant_id required", ErrInvalid)
	}
	c.Provider = strings.TrimSpace(c.Provider)
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.Model = strings.TrimSpace(c.Model)
	return c, nil
}

// Get implements Store.
func (s *sqliteStore) Get(ctx context.Context, tenantID string) (StoredConfig, error) {
	tenantID = strings.TrimSpace(tenantID)
	const q = `
SELECT tenant_id, provider, base_url, model, api_key_ciphertext, updated_at
FROM ai_config
WHERE tenant_id = ?;`
	row := s.db.QueryRowContext(ctx, q, tenantID)

	var (
		c       StoredConfig
		cipher  []byte
		updated int64
	)
	err := row.Scan(&c.TenantID, &c.Provider, &c.BaseURL, &c.Model, &cipher, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredConfig{}, ErrNotFound
	}
	if err != nil {
		return StoredConfig{}, fmt.Errorf("aiconfig: scan: %w", err)
	}
	c.APIKeyCiphertext = cipher
	c.UpdatedAt = time.Unix(0, updated).UTC()
	return c, nil
}

// Set implements Store.
func (s *sqliteStore) Set(ctx context.Context, c StoredConfig) (StoredConfig, error) {
	c, err := validate(c)
	if err != nil {
		return StoredConfig{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	c.UpdatedAt = now

	const q = `
INSERT INTO ai_config (tenant_id, provider, base_url, model, api_key_ciphertext, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id) DO UPDATE SET
	provider = excluded.provider,
	base_url = excluded.base_url,
	model = excluded.model,
	api_key_ciphertext = excluded.api_key_ciphertext,
	updated_at = excluded.updated_at;`
	_, err = s.db.ExecContext(ctx, q,
		c.TenantID,
		c.Provider,
		c.BaseURL,
		c.Model,
		c.APIKeyCiphertext,
		now.UnixNano(),
	)
	if err != nil {
		return StoredConfig{}, fmt.Errorf("aiconfig: set: %w", err)
	}
	return c, nil
}

// Close implements Store.
func (s *sqliteStore) Close() error {
	return s.db.Close()
}
