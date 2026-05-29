package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const oauthSelect = `
SELECT id, tenant_id, user_id, provider, provider_user_id, provider_login,
       purpose, access_token_enc, refresh_token_enc, scopes, expires_at,
       created_at, updated_at
FROM oauth_identities `

// scanOAuthIdentity reads one row from an *sql.Rows or *sql.Row,
// decrypting the access/refresh token blobs via s.crypto. The caller
// must hold a non-nil s.crypto; that invariant is enforced by the
// public method wrappers.
func (s *sqliteStore) scanOAuthIdentity(row interface{ Scan(...any) error }) (OAuthIdentity, error) {
	var (
		o                OAuthIdentity
		accessEnc        []byte
		refreshEnc       []byte
		scopes           string
		expires          sql.NullInt64
		created, updated int64
	)
	err := row.Scan(
		&o.ID, &o.TenantID, &o.UserID, &o.Provider, &o.ProviderUserID,
		&o.ProviderLogin, &o.Purpose, &accessEnc, &refreshEnc, &scopes,
		&expires, &created, &updated,
	)
	if err != nil {
		return OAuthIdentity{}, err
	}
	accessPT, err := s.crypto.Decrypt(accessEnc)
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("auth: decrypt access token: %w", err)
	}
	o.AccessToken = string(accessPT)
	if len(refreshEnc) > 0 {
		refreshPT, err := s.crypto.Decrypt(refreshEnc)
		if err != nil {
			return OAuthIdentity{}, fmt.Errorf("auth: decrypt refresh token: %w", err)
		}
		o.RefreshToken = string(refreshPT)
	}
	o.Scopes = decodeOAuthScopes(scopes)
	if expires.Valid {
		o.ExpiresAt = time.Unix(0, expires.Int64).UTC()
	}
	o.CreatedAt = time.Unix(0, created).UTC()
	o.UpdatedAt = time.Unix(0, updated).UTC()
	return o, nil
}

func (s *sqliteStore) CreateOAuthIdentity(ctx context.Context, o OAuthIdentity) (OAuthIdentity, error) {
	if s.crypto == nil {
		return OAuthIdentity{}, ErrCryptoRequired
	}
	if err := o.Validate(); err != nil {
		return OAuthIdentity{}, err
	}
	now := time.Now().UTC()
	if o.ID == "" {
		o.ID = NewOAuthIdentityID()
	}
	if o.CreatedAt.IsZero() {
		o.CreatedAt = now
	}
	o.UpdatedAt = now

	accessEnc, err := s.crypto.EncryptString(o.AccessToken)
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("auth: encrypt access token: %w", err)
	}
	var refreshEnc []byte
	if o.RefreshToken != "" {
		refreshEnc, err = s.crypto.EncryptString(o.RefreshToken)
		if err != nil {
			return OAuthIdentity{}, fmt.Errorf("auth: encrypt refresh token: %w", err)
		}
	}
	var expires sql.NullInt64
	if !o.ExpiresAt.IsZero() {
		expires = sql.NullInt64{Int64: o.ExpiresAt.UTC().UnixNano(), Valid: true}
	}
	scopes := encodeOAuthScopes(o.Scopes)

	s.mu.Lock()
	defer s.mu.Unlock()

	const q = `
INSERT INTO oauth_identities (
    id, tenant_id, user_id, provider, provider_user_id, provider_login,
    purpose, access_token_enc, refresh_token_enc, scopes, expires_at,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, provider_user_id) DO UPDATE SET
    tenant_id         = excluded.tenant_id,
    user_id           = excluded.user_id,
    provider_login    = excluded.provider_login,
    purpose           = excluded.purpose,
    access_token_enc  = excluded.access_token_enc,
    refresh_token_enc = excluded.refresh_token_enc,
    scopes            = excluded.scopes,
    expires_at        = excluded.expires_at,
    updated_at        = excluded.updated_at
`
	if _, err := s.db.ExecContext(ctx, q,
		o.ID, o.TenantID, o.UserID, o.Provider, o.ProviderUserID,
		o.ProviderLogin, o.Purpose, accessEnc, refreshEnc, scopes,
		expires, o.CreatedAt.UnixNano(), o.UpdatedAt.UnixNano(),
	); err != nil {
		return OAuthIdentity{}, fmt.Errorf("auth: upsert oauth identity: %w", err)
	}

	return s.GetOAuthIdentityByProviderUserID(ctx, o.Provider, o.ProviderUserID)
}

func (s *sqliteStore) GetOAuthIdentityByProviderUserID(ctx context.Context, provider, providerUserID string) (OAuthIdentity, error) {
	if s.crypto == nil {
		return OAuthIdentity{}, ErrCryptoRequired
	}
	row := s.db.QueryRowContext(ctx,
		oauthSelect+`WHERE provider = ? AND provider_user_id = ?`,
		provider, providerUserID)
	o, err := s.scanOAuthIdentity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthIdentity{}, ErrOAuthIdentityNotFound
	}
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("auth: get oauth identity by provider user id: %w", err)
	}
	return o, nil
}

func (s *sqliteStore) GetOAuthIdentitiesByUser(ctx context.Context, tenantID, userID string) ([]OAuthIdentity, error) {
	if s.crypto == nil {
		return nil, ErrCryptoRequired
	}
	rows, err := s.db.QueryContext(ctx,
		oauthSelect+`WHERE tenant_id = ? AND user_id = ? ORDER BY created_at ASC`,
		tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("auth: list oauth identities by user: %w", err)
	}
	defer rows.Close()
	var out []OAuthIdentity
	for rows.Next() {
		o, err := s.scanOAuthIdentity(rows)
		if err != nil {
			return nil, fmt.Errorf("auth: scan oauth identity: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetBotIdentity(ctx context.Context, tenantID, provider string) (OAuthIdentity, error) {
	if s.crypto == nil {
		return OAuthIdentity{}, ErrCryptoRequired
	}
	row := s.db.QueryRowContext(ctx,
		oauthSelect+`WHERE tenant_id = ? AND provider = ? AND purpose = ? LIMIT 1`,
		tenantID, provider, OAuthPurposeBot)
	o, err := s.scanOAuthIdentity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthIdentity{}, ErrOAuthIdentityNotFound
	}
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("auth: get bot identity: %w", err)
	}
	return o, nil
}

func (s *sqliteStore) UpdateOAuthTokens(ctx context.Context, id string, accessToken, refreshToken string, expiresAt time.Time) error {
	if s.crypto == nil {
		return ErrCryptoRequired
	}
	if accessToken == "" {
		return fmt.Errorf("%w: empty access token", ErrInvalidOAuthIdentity)
	}
	accessEnc, err := s.crypto.EncryptString(accessToken)
	if err != nil {
		return fmt.Errorf("auth: encrypt access token: %w", err)
	}
	var refreshEnc []byte
	if refreshToken != "" {
		refreshEnc, err = s.crypto.EncryptString(refreshToken)
		if err != nil {
			return fmt.Errorf("auth: encrypt refresh token: %w", err)
		}
	}
	var expires sql.NullInt64
	if !expiresAt.IsZero() {
		expires = sql.NullInt64{Int64: expiresAt.UTC().UnixNano(), Valid: true}
	}
	now := time.Now().UTC().UnixNano()
	const q = `
UPDATE oauth_identities
   SET access_token_enc = ?, refresh_token_enc = ?, expires_at = ?, updated_at = ?
 WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, accessEnc, refreshEnc, expires, now, id)
	if err != nil {
		return fmt.Errorf("auth: update oauth tokens: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrOAuthIdentityNotFound
	}
	return nil
}

func (s *sqliteStore) DeleteOAuthIdentity(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM oauth_identities WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("auth: delete oauth identity: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrOAuthIdentityNotFound
	}
	return nil
}
