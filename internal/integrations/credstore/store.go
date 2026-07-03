// Package credstore is the generic encrypted credential store for the
// integrations framework: per (tenant, integration) it holds a map of named
// credentials, each encrypted at rest with the same secrets.Box crypto the
// aiconfig store uses. It never persists, returns, or logs a plaintext value.
package credstore

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Crypto encrypts and decrypts a credential value at rest. *secrets.Box
// satisfies it directly. A nil Crypto disables writes (so a plaintext value can
// never reach disk) and yields empty reads, matching aiconfig's no-key case.
type Crypto interface {
	EncryptString(s string) ([]byte, error)
	DecryptString(blob []byte) (string, error)
}

var (
	// ErrNotFound is returned by Get when no credential is stored for the key.
	ErrNotFound = errors.New("credstore: credential not found")
	// ErrNoCrypto is returned by Put when no encryption key is configured, so a
	// plaintext secret is never written to disk.
	ErrNoCrypto = errors.New("credstore: no encryption key configured")
)

// Store is the SQLite-backed encrypted credential store.
type Store struct {
	db  *sql.DB
	box Crypto
	log *slog.Logger
}

// OpenStore opens (or creates) the credential database at dsn with WAL,
// foreign-keys ON, synchronous NORMAL and a 5s busy timeout, applies the
// embedded migrations, and binds the encryption box (which may be nil).
func OpenStore(ctx context.Context, dsn string, box Crypto, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("credstore: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("credstore: %s: %w", p, err)
		}
	}
	s := &Store{db: db, box: box, log: logger}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("credstore: read migrations dir: %w", err)
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
			return fmt.Errorf("credstore: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("credstore: apply migration %s: %w", name, err)
		}
	}
	return nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error { return s.db.Close() }

// Put encrypts plaintext and upserts it under (tenant, integration, key). With
// no encryption key configured it refuses with ErrNoCrypto so a plaintext secret
// never reaches disk.
func (s *Store) Put(ctx context.Context, tenantID, integrationID, key, plaintext string) error {
	tenantID, integrationID, key = strings.TrimSpace(tenantID), strings.TrimSpace(integrationID), strings.TrimSpace(key)
	if tenantID == "" || integrationID == "" || key == "" {
		return fmt.Errorf("credstore: tenant, integration and key are required")
	}
	if s.box == nil {
		return ErrNoCrypto
	}
	ct, err := s.box.EncryptString(plaintext)
	if err != nil {
		return fmt.Errorf("credstore: encrypt: %w", err)
	}
	const q = `
INSERT INTO integration_credentials (tenant_id, integration_id, cred_key, ciphertext, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, integration_id, cred_key) DO UPDATE SET
    ciphertext = excluded.ciphertext,
    updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, q, tenantID, integrationID, key, ct, time.Now().UTC().UnixNano()); err != nil {
		return fmt.Errorf("credstore: put: %w", err)
	}
	return nil
}

// Get decrypts and returns the stored credential, or ErrNotFound. With no
// encryption key configured a stored value cannot be read and returns "".
func (s *Store) Get(ctx context.Context, tenantID, integrationID, key string) (string, error) {
	const q = `SELECT ciphertext FROM integration_credentials WHERE tenant_id = ? AND integration_id = ? AND cred_key = ?`
	var ct []byte
	err := s.db.QueryRowContext(ctx, q,
		strings.TrimSpace(tenantID), strings.TrimSpace(integrationID), strings.TrimSpace(key)).Scan(&ct)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("credstore: get: %w", err)
	}
	if s.box == nil {
		s.log.Warn("credstore: stored credential but no encryption key configured; value unavailable")
		return "", nil
	}
	pt, err := s.box.DecryptString(ct)
	if err != nil {
		return "", fmt.Errorf("credstore: decrypt: %w", err)
	}
	return pt, nil
}

// ListKeys returns the credential key NAMES stored for (tenant, integration) in
// sorted order - never their values.
func (s *Store) ListKeys(ctx context.Context, tenantID, integrationID string) ([]string, error) {
	const q = `SELECT cred_key FROM integration_credentials WHERE tenant_id = ? AND integration_id = ? ORDER BY cred_key ASC`
	rows, err := s.db.QueryContext(ctx, q, strings.TrimSpace(tenantID), strings.TrimSpace(integrationID))
	if err != nil {
		return nil, fmt.Errorf("credstore: list keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("credstore: scan key: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Delete revokes every credential stored for (tenant, integration).
func (s *Store) Delete(ctx context.Context, tenantID, integrationID string) error {
	const q = `DELETE FROM integration_credentials WHERE tenant_id = ? AND integration_id = ?`
	if _, err := s.db.ExecContext(ctx, q, strings.TrimSpace(tenantID), strings.TrimSpace(integrationID)); err != nil {
		return fmt.Errorf("credstore: delete: %w", err)
	}
	return nil
}
