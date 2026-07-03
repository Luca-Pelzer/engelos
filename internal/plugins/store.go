package plugins

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

// ErrInvalid is returned when a tenant or plugin id fails validation (empty).
// There is deliberately no ErrNotFound: Get reports absence through its found
// return value, since an unset plugin state is a normal, expected state that
// falls back to the manifest default.
var ErrInvalid = errors.New("plugins: invalid state key")

// State is a single explicit plugin toggle scoped to a tenant. Enabled is the
// stored desired boolean; UpdatedAt is the last write time (UTC, second
// resolution).
type State struct {
	TenantID  string
	PluginID  string
	Enabled   bool
	UpdatedAt time.Time
}

// StateStore is the persistence contract for per-tenant plugin toggles. All
// methods are safe for concurrent use; Set is an atomic upsert. Only EXPLICIT
// overrides are stored - an unset plugin has no row and the caller supplies its
// manifest default (see GetOrDefault and Registry.Active).
type StateStore interface {
	// Set writes the desired enabled state for a plugin (upsert).
	Set(ctx context.Context, tenantID, pluginID string, enabled bool) error
	// Get returns the explicit desired state. found=false means no override.
	Get(ctx context.Context, tenantID, pluginID string) (enabled bool, found bool, err error)
	// GetOrDefault returns the stored value, or def when none is stored.
	GetOrDefault(ctx context.Context, tenantID, pluginID string, def bool) (bool, error)
	// List returns every explicit toggle for a tenant, ordered by plugin_id.
	List(ctx context.Context, tenantID string) ([]State, error)
	// Close releases the underlying database handle.
	Close() error
}

func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

func validate(tenantID, pluginID string) (t, p string, err error) {
	t = strings.TrimSpace(tenantID)
	if t == "" {
		return "", "", fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	p = strings.TrimSpace(pluginID)
	if p == "" {
		return "", "", fmt.Errorf("%w: plugin_id is required", ErrInvalid)
	}
	return t, p, nil
}

// sqliteStore is a pure-Go SQLite implementation backed by modernc.org/sqlite,
// mirroring internal/featureflags.sqliteStore conventions: WAL, foreign-keys,
// busy_timeout, SetMaxOpenConns(1) and a sync.Mutex around Set's upsert.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger
	mu  sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn and returns a
// ready-to-use StateStore with WAL journal mode, foreign-keys ON and
// synchronous=NORMAL. Use "file::memory:?cache=shared" for tests.
func OpenSQLiteStore(ctx context.Context, dsn string, logger *slog.Logger) (StateStore, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("plugins: open sqlite: %w", err)
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
			return nil, fmt.Errorf("plugins: %s: %w", p, err)
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
		return fmt.Errorf("plugins: read migrations dir: %w", err)
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
			return fmt.Errorf("plugins: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("plugins: apply migration %s: %w", name, err)
		}
		s.log.Debug("plugins: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

// Set writes the desired enabled state for a plugin, creating the row if absent
// and overwriting it otherwise (upsert).
func (s *sqliteStore) Set(ctx context.Context, tenantID, pluginID string, enabled bool) error {
	t, p, err := validate(tenantID, pluginID)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Unix()
	const up = `
INSERT INTO plugin_state (id, tenant_id, plugin_id, enabled, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, plugin_id)
DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, up,
		newID(), t, p, boolToInt(enabled), now); err != nil {
		return fmt.Errorf("plugins: set: %w", err)
	}
	return nil
}

// Get returns the explicit desired state for a plugin. found=false means no
// override exists and the caller should fall back to the manifest default.
func (s *sqliteStore) Get(ctx context.Context, tenantID, pluginID string) (enabled bool, found bool, err error) {
	t, p, err := validate(tenantID, pluginID)
	if err != nil {
		return false, false, err
	}
	var i int64
	row := s.db.QueryRowContext(ctx,
		`SELECT enabled FROM plugin_state WHERE tenant_id = ? AND plugin_id = ?`,
		t, p)
	switch err := row.Scan(&i); {
	case errors.Is(err, sql.ErrNoRows):
		return false, false, nil
	case err != nil:
		return false, false, fmt.Errorf("plugins: get: %w", err)
	default:
		return i != 0, true, nil
	}
}

// GetOrDefault returns the stored value, or def when no explicit override is
// stored.
func (s *sqliteStore) GetOrDefault(ctx context.Context, tenantID, pluginID string, def bool) (bool, error) {
	enabled, found, err := s.Get(ctx, tenantID, pluginID)
	if err != nil {
		return false, err
	}
	if !found {
		return def, nil
	}
	return enabled, nil
}

// List returns every explicit toggle for a tenant, ordered by plugin_id ASC.
func (s *sqliteStore) List(ctx context.Context, tenantID string) ([]State, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT tenant_id, plugin_id, enabled, updated_at
         FROM plugin_state WHERE tenant_id = ? ORDER BY plugin_id ASC`,
		t)
	if err != nil {
		return nil, fmt.Errorf("plugins: list: %w", err)
	}
	defer rows.Close()
	var out []State
	for rows.Next() {
		var (
			st      State
			i       int64
			updated int64
		)
		if err := rows.Scan(&st.TenantID, &st.PluginID, &i, &updated); err != nil {
			return nil, fmt.Errorf("plugins: scan: %w", err)
		}
		st.Enabled = i != 0
		st.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, st)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
