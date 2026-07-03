package avatar

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ErrInvalid is returned when a tenant or channel argument is empty.
var ErrInvalid = errors.New("avatar: invalid argument")

//go:embed migrations/*.sql
var migrationsFS embed.FS

// TokenStore persists the per-channel overlay bearer token used to authenticate
// avatar WebSocket connections. It is the authentication boundary for the
// overlay: the WS handler calls Verify, the owner endpoint calls GetOrCreate.
type TokenStore interface {
	// GetOrCreate returns the overlay token for (tenantID, channel), minting
	// and persisting a fresh one when none exists yet.
	GetOrCreate(ctx context.Context, tenantID, channel string) (string, error)
	// Verify reports whether token matches the stored token for
	// (tenantID, channel). An empty or unknown token returns false, nil.
	Verify(ctx context.Context, tenantID, channel, token string) (bool, error)
	// Close releases underlying resources.
	Close() error
}

// sqliteStore is the modernc.org/sqlite-backed TokenStore.
type sqliteStore struct {
	db *sql.DB
	// mu serialises GetOrCreate so the read-then-insert stays atomic under
	// SetMaxOpenConns(1).
	mu sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn, applies
// migrations, and returns a ready TokenStore.
func OpenSQLiteStore(ctx context.Context, dsn string) (TokenStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("avatar: open: %w", err)
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
			return fmt.Errorf("avatar: pragma %q: %w", p, err)
		}
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("avatar: read migrations: %w", err)
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
			return fmt.Errorf("avatar: read migration %s: %w", name, err)
		}
		if _, err := db.ExecContext(ctx, string(b)); err != nil {
			return fmt.Errorf("avatar: exec migration %s: %w", name, err)
		}
	}
	return nil
}

// GetOrCreate implements TokenStore.
func (s *sqliteStore) GetOrCreate(ctx context.Context, tenantID, channel string) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	channel = normalizeChannel(channel)
	if tenantID == "" || channel == "" {
		return "", ErrInvalid
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if tok, err := s.read(ctx, tenantID, channel); err != nil {
		return "", err
	} else if tok != "" {
		return tok, nil
	}

	tok, err := generateToken()
	if err != nil {
		return "", err
	}
	const q = `
INSERT INTO avatar_overlay_token (id, tenant_id, channel, token, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel) DO NOTHING;`
	if _, err := s.db.ExecContext(ctx, q, newID(), tenantID, channel, tok, time.Now().UTC().UnixNano()); err != nil {
		return "", fmt.Errorf("avatar: insert token: %w", err)
	}
	// Re-read so a concurrent writer's winning row is the one returned.
	stored, err := s.read(ctx, tenantID, channel)
	if err != nil {
		return "", err
	}
	if stored == "" {
		return "", fmt.Errorf("avatar: token vanished after insert")
	}
	return stored, nil
}

// Verify implements TokenStore.
func (s *sqliteStore) Verify(ctx context.Context, tenantID, channel, token string) (bool, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return false, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	channel = normalizeChannel(channel)
	if tenantID == "" || channel == "" {
		return false, nil
	}
	stored, err := s.read(ctx, tenantID, channel)
	if err != nil {
		return false, err
	}
	if stored == "" {
		return false, nil
	}
	// Constant-time compare so a mismatched token cannot be timed byte by byte.
	if subtle.ConstantTimeCompare([]byte(stored), []byte(token)) == 1 {
		return true, nil
	}
	return false, nil
}

// read returns the stored token for (tenantID, channel), or "" when absent.
func (s *sqliteStore) read(ctx context.Context, tenantID, channel string) (string, error) {
	const q = `SELECT token FROM avatar_overlay_token WHERE tenant_id = ? AND channel = ?;`
	var tok string
	err := s.db.QueryRowContext(ctx, q, tenantID, channel).Scan(&tok)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("avatar: read token: %w", err)
	}
	return tok, nil
}

// Close implements TokenStore.
func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// normalizeChannel lower-cases and strips a leading '#' so channel keys match
// the tts store's normalization and the overlay's ?channel= query value.
func normalizeChannel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimSpace(s)
	return strings.ToLower(s)
}

// newID returns a random 128-bit hex identifier for the primary key.
func newID() string {
	return randomHex(16)
}

// generateToken returns a 256-bit URL-safe hex bearer token. A failure of the
// system CSPRNG is surfaced rather than falling back to a weak source.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("avatar: generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// randomHex returns n random bytes hex-encoded. It panics only if the system
// CSPRNG fails, which is treated as unrecoverable (matches crypto/rand usage
// elsewhere for non-secret identifiers).
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read never returns an error on supported platforms; if
		// it somehow does, fall back to a time-seeded value so id generation
		// stays total. IDs are not secrets (the token is generated separately
		// and its generation error is surfaced).
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return hex.EncodeToString(b)
}
