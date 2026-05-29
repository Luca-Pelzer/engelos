package auth

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/secrets"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store is the persistence contract for the auth package. It is
// transactional only at the row level (every method is atomic), which
// is sufficient for the auth domain. Multi-row workflows (e.g. "create
// owner during setup") are the caller's responsibility.
//
// All methods are safe for concurrent use. Errors are returned wrapped
// such that errors.Is matches one of: ErrUserNotFound,
// ErrUserAlreadyExists, ErrSessionNotFound, ErrAPIKeyNotFound,
// ErrInvalidUser, ErrInvalidSession, ErrInvalidAPIKey.
type Store interface {
	CreateUser(ctx context.Context, u User) (User, error)
	GetUserByID(ctx context.Context, tenantID, userID string) (User, error)
	GetUserByEmail(ctx context.Context, tenantID, email string) (User, error)
	UpdateUser(ctx context.Context, u User) error
	DeleteUser(ctx context.Context, tenantID, userID string) error
	ListUsers(ctx context.Context, tenantID string) ([]User, error)

	CreateSession(ctx context.Context, s Session) error
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context) (int, error)

	CreateAPIKey(ctx context.Context, k APIKey) error
	GetAPIKeyByHash(ctx context.Context, keyHash string) (APIKey, error)
	ListAPIKeys(ctx context.Context, tenantID string) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, keyID string) error
	UpdateAPIKeyLastUsed(ctx context.Context, keyID string, when time.Time) error

	// CreateOAuthIdentity inserts a new linked external identity or, on
	// conflict over (provider, provider_user_id), updates the existing
	// row with the new tokens/scopes/login/expiry/user/purpose
	// (upsert semantics). Token fields are encrypted at rest. Returns
	// ErrCryptoRequired if the Store was opened without WithCrypto.
	CreateOAuthIdentity(ctx context.Context, o OAuthIdentity) (OAuthIdentity, error)

	// GetOAuthIdentityByProviderUserID looks up an identity by its
	// external provider+account-id. Returns ErrOAuthIdentityNotFound
	// when absent.
	GetOAuthIdentityByProviderUserID(ctx context.Context, provider, providerUserID string) (OAuthIdentity, error)

	// GetOAuthIdentitiesByUser returns every identity linked to the
	// given local user, across providers.
	GetOAuthIdentitiesByUser(ctx context.Context, tenantID, userID string) ([]OAuthIdentity, error)

	// GetBotIdentity returns the single purpose="bot" identity for
	// (tenant, provider). Returns ErrOAuthIdentityNotFound when none.
	GetBotIdentity(ctx context.Context, tenantID, provider string) (OAuthIdentity, error)

	// UpdateOAuthTokens replaces the encrypted access/refresh tokens
	// and the expiry of an existing identity. An empty refresh token
	// stores SQL NULL. A zero expiresAt stores SQL NULL.
	UpdateOAuthTokens(ctx context.Context, id string, accessToken, refreshToken string, expiresAt time.Time) error

	// ListOAuthIdentitiesExpiringBefore returns every identity whose
	// expires_at is non-NULL and <= cutoff, across all tenants and
	// providers, ordered by expires_at ascending. Identities with a
	// NULL expires_at are EXCLUDED (we cannot know when they expire).
	// Token fields are decrypted. Returns ErrCryptoRequired if the
	// Store was opened without a WithCrypto option.
	ListOAuthIdentitiesExpiringBefore(ctx context.Context, cutoff time.Time) ([]OAuthIdentity, error)

	// DeleteOAuthIdentity removes an identity by its ULID.
	DeleteOAuthIdentity(ctx context.Context, id string) error

	Close() error
}

// StoreOption configures a sqliteStore at construction time.
type StoreOption func(*sqliteStore)

// WithCrypto wires an authenticated-encryption Box into the Store so
// that OAuth token fields are encrypted at rest. Required for any of
// the OAuth identity methods.
func WithCrypto(box *secrets.Box) StoreOption {
	return func(s *sqliteStore) { s.crypto = box }
}

// sqliteStore is a pure-Go SQLite implementation of Store backed by
// modernc.org/sqlite. It enables WAL and foreign-key enforcement and
// runs all embedded migrations on open.
type sqliteStore struct {
	db     *sql.DB
	log    *slog.Logger
	crypto *secrets.Box

	// mu serialises writes that need to atomically check uniqueness and
	// insert. SQLite already serialises writers at the engine level,
	// but holding mu lets us produce ErrUserAlreadyExists from a
	// separate SELECT without races between the SELECT and the INSERT.
	mu sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn and
// returns a ready-to-use Store. dsn may be a file path or a full
// modernc.org/sqlite DSN. Use "file::memory:?cache=shared" for tests.
//
// The returned Store has WAL journal mode, foreign-keys ON, and
// synchronous=NORMAL.
func OpenSQLiteStore(ctx context.Context, dsn string, logger *slog.Logger, opts ...StoreOption) (Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("auth: open sqlite: %w", err)
	}
	// modernc.org/sqlite uses a single connection well; keep the pool
	// small to avoid SQLITE_BUSY storms under -race tests.
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
			return nil, fmt.Errorf("auth: %s: %w", p, err)
		}
	}

	s := &sqliteStore{db: db, log: logger}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *sqliteStore) migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("auth: read migrations dir: %w", err)
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
			return fmt.Errorf("auth: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("auth: apply migration %s: %w", name, err)
		}
		s.log.Debug("auth: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

// ----------------------------------------------------------------------
// Users
// ----------------------------------------------------------------------

func (s *sqliteStore) CreateUser(ctx context.Context, u User) (User, error) {
	if strings.TrimSpace(u.ID) == "" {
		u.ID = NewUserID()
	}
	u.Email = NormalizeEmail(u.Email)
	u.Username = NormalizeUsername(u.Username)
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = now
	}
	if err := u.Validate(); err != nil {
		return User{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	const dup = `SELECT 1 FROM users WHERE tenant_id = ? AND (email = ? OR username = ?) LIMIT 1`
	var x int
	switch err := s.db.QueryRowContext(ctx, dup, u.TenantID, u.Email, u.Username).Scan(&x); {
	case err == nil:
		return User{}, ErrUserAlreadyExists
	case errors.Is(err, sql.ErrNoRows):
		// not a duplicate; continue
	default:
		return User{}, fmt.Errorf("auth: check duplicate user: %w", err)
	}

	const ins = `
INSERT INTO users (id, tenant_id, email, username, password_hash, role, totp_secret,
                   created_at, updated_at, last_login_at, disabled)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	disabled := 0
	if u.Disabled {
		disabled = 1
	}
	if _, err := s.db.ExecContext(ctx, ins,
		u.ID, u.TenantID, u.Email, u.Username, u.PasswordHash, string(u.Role),
		u.TOTPSecret, u.CreatedAt.UnixNano(), u.UpdatedAt.UnixNano(),
		u.LastLoginAt.UnixNano(), disabled,
	); err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrUserAlreadyExists
		}
		return User{}, fmt.Errorf("auth: insert user: %w", err)
	}
	return u, nil
}

const userSelect = `SELECT id, tenant_id, email, username, password_hash, role,
                           totp_secret, created_at, updated_at, last_login_at, disabled
                    FROM users `

func (s *sqliteStore) scanUser(row interface{ Scan(...any) error }) (User, error) {
	var (
		u                           User
		created, updated, lastLogin int64
		disabled                    int
		roleStr                     string
		totpSecret                  []byte
	)
	err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.Username, &u.PasswordHash,
		&roleStr, &totpSecret, &created, &updated, &lastLogin, &disabled)
	if err != nil {
		return User{}, err
	}
	u.Role = Role(roleStr)
	u.TOTPSecret = totpSecret
	u.CreatedAt = time.Unix(0, created).UTC()
	u.UpdatedAt = time.Unix(0, updated).UTC()
	if lastLogin != 0 {
		u.LastLoginAt = time.Unix(0, lastLogin).UTC()
	}
	u.Disabled = disabled != 0
	return u, nil
}

func (s *sqliteStore) GetUserByID(ctx context.Context, tenantID, userID string) (User, error) {
	row := s.db.QueryRowContext(ctx,
		userSelect+`WHERE tenant_id = ? AND id = ?`, tenantID, userID)
	u, err := s.scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: get user by id: %w", err)
	}
	return u, nil
}

func (s *sqliteStore) GetUserByEmail(ctx context.Context, tenantID, email string) (User, error) {
	email = NormalizeEmail(email)
	row := s.db.QueryRowContext(ctx,
		userSelect+`WHERE tenant_id = ? AND email = ?`, tenantID, email)
	u, err := s.scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: get user by email: %w", err)
	}
	return u, nil
}

func (s *sqliteStore) UpdateUser(ctx context.Context, u User) error {
	u.Email = NormalizeEmail(u.Email)
	u.Username = NormalizeUsername(u.Username)
	u.UpdatedAt = time.Now().UTC()
	if err := u.Validate(); err != nil {
		return err
	}
	disabled := 0
	if u.Disabled {
		disabled = 1
	}
	const q = `
UPDATE users SET email=?, username=?, password_hash=?, role=?, totp_secret=?,
                 updated_at=?, last_login_at=?, disabled=?
WHERE tenant_id=? AND id=?`
	res, err := s.db.ExecContext(ctx, q,
		u.Email, u.Username, u.PasswordHash, string(u.Role), u.TOTPSecret,
		u.UpdatedAt.UnixNano(), u.LastLoginAt.UnixNano(), disabled,
		u.TenantID, u.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrUserAlreadyExists
		}
		return fmt.Errorf("auth: update user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *sqliteStore) DeleteUser(ctx context.Context, tenantID, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM users WHERE tenant_id = ? AND id = ?`, tenantID, userID)
	if err != nil {
		return fmt.Errorf("auth: delete user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *sqliteStore) ListUsers(ctx context.Context, tenantID string) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		userSelect+`WHERE tenant_id = ? ORDER BY created_at ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("auth: list users: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := s.scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("auth: scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ----------------------------------------------------------------------
// Sessions
// ----------------------------------------------------------------------

func (s *sqliteStore) CreateSession(ctx context.Context, sess Session) error {
	if err := sess.Validate(); err != nil {
		return err
	}
	const q = `
INSERT INTO sessions (id, tenant_id, user_id, token_hash, created_at, expires_at,
                      last_used_at, user_agent, remote_ip)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, q,
		sess.ID, sess.TenantID, sess.UserID, sess.TokenHash,
		sess.CreatedAt.UnixNano(), sess.ExpiresAt.UnixNano(),
		sess.LastUsedAt.UnixNano(), sess.UserAgent, sess.RemoteIP,
	)
	if err != nil {
		return fmt.Errorf("auth: insert session: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	const q = `SELECT id, tenant_id, user_id, token_hash, created_at, expires_at,
                      last_used_at, user_agent, remote_ip
               FROM sessions WHERE token_hash = ?`
	var (
		sess                       Session
		created, expires, lastUsed int64
	)
	err := s.db.QueryRowContext(ctx, q, tokenHash).Scan(
		&sess.ID, &sess.TenantID, &sess.UserID, &sess.TokenHash,
		&created, &expires, &lastUsed, &sess.UserAgent, &sess.RemoteIP,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("auth: get session: %w", err)
	}
	sess.CreatedAt = time.Unix(0, created).UTC()
	sess.ExpiresAt = time.Unix(0, expires).UTC()
	sess.LastUsedAt = time.Unix(0, lastUsed).UTC()
	return sess, nil
}

func (s *sqliteStore) DeleteSession(ctx context.Context, tokenHash string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	if err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *sqliteStore) DeleteExpiredSessions(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC().UnixNano())
	if err != nil {
		return 0, fmt.Errorf("auth: delete expired sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ----------------------------------------------------------------------
// API keys
// ----------------------------------------------------------------------

func encodeScopes(ps []Permission) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = string(p)
	}
	return strings.Join(parts, ",")
}

func decodeScopes(s string) []Permission {
	if s == "" {
		return nil
	}
	raw := strings.Split(s, ",")
	out := make([]Permission, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		out = append(out, Permission(r))
	}
	return out
}

func encodePrefixes(ps []netip.Prefix) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.String()
	}
	return strings.Join(parts, ",")
}

func decodePrefixes(s string) ([]netip.Prefix, error) {
	if s == "" {
		return nil, nil
	}
	raw := strings.Split(s, ",")
	out := make([]netip.Prefix, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		p, err := netip.ParsePrefix(r)
		if err != nil {
			return nil, fmt.Errorf("auth: decode ip prefix %q: %w", r, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *sqliteStore) CreateAPIKey(ctx context.Context, k APIKey) error {
	if err := k.Validate(); err != nil {
		return err
	}
	const q = `
INSERT INTO api_keys (id, tenant_id, name, description, key_hash, prefix, scopes,
                      ip_whitelist, rate_limit, created_at, created_by,
                      last_used_at, expires_at, revoked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	var (
		expires *int64
		revoked *int64
	)
	if k.ExpiresAt != nil {
		ns := k.ExpiresAt.UnixNano()
		expires = &ns
	}
	if k.RevokedAt != nil {
		ns := k.RevokedAt.UnixNano()
		revoked = &ns
	}
	_, err := s.db.ExecContext(ctx, q,
		k.ID, k.TenantID, k.Name, k.Description, k.KeyHash, k.Prefix,
		encodeScopes(k.Scopes), encodePrefixes(k.IPWhitelist), k.RateLimit,
		k.CreatedAt.UnixNano(), k.CreatedBy, k.LastUsedAt.UnixNano(),
		expires, revoked,
	)
	if err != nil {
		return fmt.Errorf("auth: insert api key: %w", err)
	}
	return nil
}

const apiKeySelect = `
SELECT id, tenant_id, name, description, key_hash, prefix, scopes, ip_whitelist,
       rate_limit, created_at, created_by, last_used_at, expires_at, revoked_at
FROM api_keys `

func (s *sqliteStore) scanAPIKey(row interface{ Scan(...any) error }) (APIKey, error) {
	var (
		k                 APIKey
		scopes, ipList    string
		created, lastUsed int64
		expires, revoked  sql.NullInt64
	)
	err := row.Scan(&k.ID, &k.TenantID, &k.Name, &k.Description, &k.KeyHash,
		&k.Prefix, &scopes, &ipList, &k.RateLimit, &created, &k.CreatedBy,
		&lastUsed, &expires, &revoked)
	if err != nil {
		return APIKey{}, err
	}
	k.Scopes = decodeScopes(scopes)
	prefixes, err := decodePrefixes(ipList)
	if err != nil {
		return APIKey{}, err
	}
	k.IPWhitelist = prefixes
	k.CreatedAt = time.Unix(0, created).UTC()
	if lastUsed != 0 {
		k.LastUsedAt = time.Unix(0, lastUsed).UTC()
	}
	if expires.Valid {
		t := time.Unix(0, expires.Int64).UTC()
		k.ExpiresAt = &t
	}
	if revoked.Valid {
		t := time.Unix(0, revoked.Int64).UTC()
		k.RevokedAt = &t
	}
	return k, nil
}

func (s *sqliteStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (APIKey, error) {
	row := s.db.QueryRowContext(ctx, apiKeySelect+`WHERE key_hash = ?`, keyHash)
	k, err := s.scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrAPIKeyNotFound
	}
	if err != nil {
		return APIKey{}, fmt.Errorf("auth: get api key: %w", err)
	}
	return k, nil
}

func (s *sqliteStore) ListAPIKeys(ctx context.Context, tenantID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		apiKeySelect+`WHERE tenant_id = ? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("auth: list api keys: %w", err)
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		k, err := s.scanAPIKey(rows)
		if err != nil {
			return nil, fmt.Errorf("auth: scan api key: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *sqliteStore) RevokeAPIKey(ctx context.Context, keyID string) error {
	now := time.Now().UTC().UnixNano()
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		now, keyID)
	if err != nil {
		return fmt.Errorf("auth: revoke api key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (s *sqliteStore) UpdateAPIKeyLastUsed(ctx context.Context, keyID string, when time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		when.UTC().UnixNano(), keyID)
	if err != nil {
		return fmt.Errorf("auth: update api key last_used: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// isUniqueViolation recognises SQLite's UNIQUE constraint error in a
// driver-agnostic way (modernc.org/sqlite reports the message verbatim).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
