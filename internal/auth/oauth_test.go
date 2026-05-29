package auth

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBox(t *testing.T) *secrets.Box {
	t.Helper()
	key := bytes.Repeat([]byte{0x42}, 32)
	box, err := secrets.NewBox(key)
	require.NoError(t, err)
	return box
}

func newOAuthTestStore(t *testing.T) (Store, *secrets.Box, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.db")
	dsn := path + "?_pragma=busy_timeout(5000)"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	box := newTestBox(t)
	s, err := OpenSQLiteStore(context.Background(), dsn, logger, WithCrypto(box))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s, box, path
}

func mustSeedUser(t *testing.T, s Store, tenantID string) User {
	t.Helper()
	u := makeUser(t, tenantID, "oauth-user@example.com", "oauthuser", RoleOwner)
	created, err := s.CreateUser(context.Background(), u)
	require.NoError(t, err)
	return created
}

func TestOAuthIdentityValidate(t *testing.T) {
	good := OAuthIdentity{
		TenantID:       "local",
		UserID:         "u_1",
		Provider:       ProviderTwitch,
		ProviderUserID: "12345",
		Purpose:        OAuthPurposeUser,
		AccessToken:    "tok",
	}
	require.NoError(t, good.Validate())

	cases := []struct {
		name  string
		mut   func(o *OAuthIdentity)
		check error
	}{
		{"empty tenant", func(o *OAuthIdentity) { o.TenantID = "" }, ErrInvalidOAuthIdentity},
		{"empty user id", func(o *OAuthIdentity) { o.UserID = "" }, ErrInvalidOAuthIdentity},
		{"bad provider", func(o *OAuthIdentity) { o.Provider = "facebook" }, ErrInvalidOAuthIdentity},
		{"empty provider", func(o *OAuthIdentity) { o.Provider = "" }, ErrInvalidOAuthIdentity},
		{"empty provider user", func(o *OAuthIdentity) { o.ProviderUserID = "" }, ErrInvalidOAuthIdentity},
		{"bad purpose", func(o *OAuthIdentity) { o.Purpose = "admin" }, ErrInvalidOAuthIdentity},
		{"empty purpose", func(o *OAuthIdentity) { o.Purpose = "" }, ErrInvalidOAuthIdentity},
		{"empty access token", func(o *OAuthIdentity) { o.AccessToken = "" }, ErrInvalidOAuthIdentity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := good
			tc.mut(&o)
			assert.ErrorIs(t, o.Validate(), tc.check)
		})
	}

	var nilo *OAuthIdentity
	assert.ErrorIs(t, nilo.Validate(), ErrInvalidOAuthIdentity)
}

func TestOAuthScopeCodec(t *testing.T) {
	assert.Equal(t, "", encodeOAuthScopes(nil))
	assert.Equal(t, "", encodeOAuthScopes([]string{"", " "}))
	assert.Equal(t, "chat:read chat:edit", encodeOAuthScopes([]string{"chat:read", "chat:edit"}))

	assert.Nil(t, decodeOAuthScopes(""))
	assert.Equal(t, []string{"a", "b"}, decodeOAuthScopes("a b"))
	assert.Equal(t, []string{"a", "b"}, decodeOAuthScopes("  a   b  "))
}

func TestCreateOAuthIdentityRoundtripDecryptsTokens(t *testing.T) {
	s, _, _ := newOAuthTestStore(t)
	ctx := context.Background()
	u := mustSeedUser(t, s, "local")

	in := OAuthIdentity{
		TenantID:       "local",
		UserID:         u.ID,
		Provider:       ProviderTwitch,
		ProviderUserID: "tw-99",
		ProviderLogin:  "ninja",
		Purpose:        OAuthPurposeUser,
		AccessToken:    "atk_supersecret_PLAINTEXT_marker",
		RefreshToken:   "rtk_refresh_PLAINTEXT_marker",
		Scopes:         []string{"chat:read", "user:read:email"},
		ExpiresAt:      time.Now().Add(time.Hour).UTC().Truncate(time.Nanosecond),
	}
	out, err := s.CreateOAuthIdentity(ctx, in)
	require.NoError(t, err)

	assert.NotEmpty(t, out.ID, "id should be assigned")
	assert.Equal(t, in.AccessToken, out.AccessToken, "access token decrypted")
	assert.Equal(t, in.RefreshToken, out.RefreshToken, "refresh token decrypted")
	assert.Equal(t, in.Scopes, out.Scopes)
	assert.False(t, out.CreatedAt.IsZero())
	assert.False(t, out.UpdatedAt.IsZero())
	assert.WithinDuration(t, in.ExpiresAt, out.ExpiresAt, time.Microsecond)

	fetched, err := s.GetOAuthIdentityByProviderUserID(ctx, ProviderTwitch, "tw-99")
	require.NoError(t, err)
	assert.Equal(t, out.ID, fetched.ID)
	assert.Equal(t, in.AccessToken, fetched.AccessToken)
	assert.Equal(t, in.RefreshToken, fetched.RefreshToken)
}

func TestCreateOAuthIdentityEncryptsTokensAtRest(t *testing.T) {
	s, _, dbPath := newOAuthTestStore(t)
	ctx := context.Background()
	u := mustSeedUser(t, s, "local")

	plaintext := "atk_DO_NOT_LEAK_THIS_at_rest_PLAINTEXT"
	refreshPT := "rtk_DO_NOT_LEAK_either_PLAINTEXT"
	_, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID:       "local",
		UserID:         u.ID,
		Provider:       ProviderDiscord,
		ProviderUserID: "dc-7",
		Purpose:        OAuthPurposeUser,
		AccessToken:    plaintext,
		RefreshToken:   refreshPT,
	})
	require.NoError(t, err)

	raw, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })

	var accessEnc []byte
	var refreshEnc []byte
	err = raw.QueryRowContext(ctx,
		`SELECT access_token_enc, refresh_token_enc FROM oauth_identities WHERE provider = ? AND provider_user_id = ?`,
		ProviderDiscord, "dc-7").Scan(&accessEnc, &refreshEnc)
	require.NoError(t, err)

	require.NotEmpty(t, accessEnc)
	require.NotEmpty(t, refreshEnc)
	assert.NotContains(t, string(accessEnc), plaintext, "access token must be encrypted at rest")
	assert.NotContains(t, string(accessEnc), "PLAINTEXT", "access token blob must not leak plaintext markers")
	assert.NotContains(t, string(refreshEnc), refreshPT, "refresh token must be encrypted at rest")
	assert.NotContains(t, string(refreshEnc), "PLAINTEXT", "refresh token blob must not leak plaintext markers")
	assert.Equal(t, byte(0x01), accessEnc[0], "secrets.Box version byte")
	assert.Equal(t, byte(0x01), refreshEnc[0], "secrets.Box version byte")
}

func TestCreateOAuthIdentityUpsertsOnConflict(t *testing.T) {
	s, _, dbPath := newOAuthTestStore(t)
	ctx := context.Background()
	u := mustSeedUser(t, s, "local")

	first, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID:       "local",
		UserID:         u.ID,
		Provider:       ProviderTwitch,
		ProviderUserID: "tw-upsert",
		ProviderLogin:  "old-login",
		Purpose:        OAuthPurposeUser,
		AccessToken:    "atk_v1",
		RefreshToken:   "rtk_v1",
		Scopes:         []string{"chat:read"},
	})
	require.NoError(t, err)

	second, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID:       "local",
		UserID:         u.ID,
		Provider:       ProviderTwitch,
		ProviderUserID: "tw-upsert",
		ProviderLogin:  "new-login",
		Purpose:        OAuthPurposeBot,
		AccessToken:    "atk_v2",
		RefreshToken:   "rtk_v2",
		Scopes:         []string{"chat:read", "chat:edit"},
	})
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID, "upsert keeps the same row id")
	assert.Equal(t, "new-login", second.ProviderLogin)
	assert.Equal(t, OAuthPurposeBot, second.Purpose)
	assert.Equal(t, "atk_v2", second.AccessToken)
	assert.Equal(t, "rtk_v2", second.RefreshToken)
	assert.Equal(t, []string{"chat:read", "chat:edit"}, second.Scopes)

	raw, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	var n int
	require.NoError(t, raw.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM oauth_identities WHERE provider = ? AND provider_user_id = ?`,
		ProviderTwitch, "tw-upsert").Scan(&n))
	assert.Equal(t, 1, n, "row count must stay 1 after upsert")
}

func TestGetOAuthIdentitiesByUser(t *testing.T) {
	s, _, _ := newOAuthTestStore(t)
	ctx := context.Background()
	u := mustSeedUser(t, s, "local")

	_, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderTwitch,
		ProviderUserID: "tw-1", Purpose: OAuthPurposeUser, AccessToken: "a1",
	})
	require.NoError(t, err)
	_, err = s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderDiscord,
		ProviderUserID: "dc-1", Purpose: OAuthPurposeUser, AccessToken: "a2",
	})
	require.NoError(t, err)

	out, err := s.GetOAuthIdentitiesByUser(ctx, "local", u.ID)
	require.NoError(t, err)
	require.Len(t, out, 2)

	providers := map[string]bool{}
	for _, o := range out {
		providers[o.Provider] = true
		assert.NotEmpty(t, o.AccessToken, "tokens decrypted on list")
	}
	assert.True(t, providers[ProviderTwitch])
	assert.True(t, providers[ProviderDiscord])

	empty, err := s.GetOAuthIdentitiesByUser(ctx, "local", "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestGetBotIdentity(t *testing.T) {
	s, _, _ := newOAuthTestStore(t)
	ctx := context.Background()
	u := mustSeedUser(t, s, "local")

	_, err := s.GetBotIdentity(ctx, "local", ProviderTwitch)
	assert.ErrorIs(t, err, ErrOAuthIdentityNotFound)

	_, err = s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderTwitch,
		ProviderUserID: "tw-user-self", Purpose: OAuthPurposeUser, AccessToken: "user-tok",
	})
	require.NoError(t, err)

	_, err = s.GetBotIdentity(ctx, "local", ProviderTwitch)
	assert.ErrorIs(t, err, ErrOAuthIdentityNotFound, "user-purpose row must not satisfy GetBotIdentity")

	bot, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderTwitch,
		ProviderUserID: "tw-bot", Purpose: OAuthPurposeBot, AccessToken: "bot-tok",
	})
	require.NoError(t, err)

	found, err := s.GetBotIdentity(ctx, "local", ProviderTwitch)
	require.NoError(t, err)
	assert.Equal(t, bot.ID, found.ID)
	assert.Equal(t, "bot-tok", found.AccessToken)
}

func TestUpdateOAuthTokens(t *testing.T) {
	s, _, _ := newOAuthTestStore(t)
	ctx := context.Background()
	u := mustSeedUser(t, s, "local")

	created, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderDiscord,
		ProviderUserID: "dc-upd", Purpose: OAuthPurposeUser, AccessToken: "atk_old",
		RefreshToken: "rtk_old",
	})
	require.NoError(t, err)

	newExpiry := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Nanosecond)
	require.NoError(t, s.UpdateOAuthTokens(ctx, created.ID, "atk_new", "rtk_new", newExpiry))

	got, err := s.GetOAuthIdentityByProviderUserID(ctx, ProviderDiscord, "dc-upd")
	require.NoError(t, err)
	assert.Equal(t, "atk_new", got.AccessToken)
	assert.Equal(t, "rtk_new", got.RefreshToken)
	assert.WithinDuration(t, newExpiry, got.ExpiresAt, time.Microsecond)
	assert.True(t, got.UpdatedAt.After(created.UpdatedAt) || got.UpdatedAt.Equal(created.UpdatedAt))

	err = s.UpdateOAuthTokens(ctx, "nonexistent-id", "x", "", time.Time{})
	assert.ErrorIs(t, err, ErrOAuthIdentityNotFound)

	err = s.UpdateOAuthTokens(ctx, created.ID, "", "", time.Time{})
	assert.ErrorIs(t, err, ErrInvalidOAuthIdentity)
}

func TestDeleteOAuthIdentity(t *testing.T) {
	s, _, _ := newOAuthTestStore(t)
	ctx := context.Background()
	u := mustSeedUser(t, s, "local")

	created, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderTwitch,
		ProviderUserID: "tw-del", Purpose: OAuthPurposeUser, AccessToken: "atk",
	})
	require.NoError(t, err)

	require.NoError(t, s.DeleteOAuthIdentity(ctx, created.ID))

	_, err = s.GetOAuthIdentityByProviderUserID(ctx, ProviderTwitch, "tw-del")
	assert.ErrorIs(t, err, ErrOAuthIdentityNotFound)

	err = s.DeleteOAuthIdentity(ctx, created.ID)
	assert.ErrorIs(t, err, ErrOAuthIdentityNotFound)
}

func TestOAuthFKCascadeOnUserDelete(t *testing.T) {
	s, _, _ := newOAuthTestStore(t)
	ctx := context.Background()
	u := mustSeedUser(t, s, "local")

	_, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderTwitch,
		ProviderUserID: "tw-cascade", Purpose: OAuthPurposeUser, AccessToken: "atk",
	})
	require.NoError(t, err)

	require.NoError(t, s.DeleteUser(ctx, "local", u.ID))

	_, err = s.GetOAuthIdentityByProviderUserID(ctx, ProviderTwitch, "tw-cascade")
	assert.ErrorIs(t, err, ErrOAuthIdentityNotFound, "FK cascade should remove oauth identity")
}

func TestListOAuthIdentitiesExpiringBefore(t *testing.T) {
	s, _, _ := newOAuthTestStore(t)
	ctx := context.Background()
	u := mustSeedUser(t, s, "local")

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	past, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderTwitch,
		ProviderUserID: "tw-past", Purpose: OAuthPurposeUser, AccessToken: "atk-past",
		ExpiresAt: base.Add(-time.Hour),
	})
	require.NoError(t, err)

	atCutoff, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderTwitch,
		ProviderUserID: "tw-cutoff", Purpose: OAuthPurposeUser, AccessToken: "atk-cutoff",
		ExpiresAt: base,
	})
	require.NoError(t, err)

	future, err := s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderTwitch,
		ProviderUserID: "tw-future", Purpose: OAuthPurposeUser, AccessToken: "atk-future",
		ExpiresAt: base.Add(time.Hour),
	})
	require.NoError(t, err)

	_, err = s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: u.ID, Provider: ProviderDiscord,
		ProviderUserID: "dc-null", Purpose: OAuthPurposeUser, AccessToken: "atk-null",
	})
	require.NoError(t, err)

	got, err := s.ListOAuthIdentitiesExpiringBefore(ctx, base)
	require.NoError(t, err)
	require.Len(t, got, 2, "expected past + at-cutoff (inclusive), excluding future and NULL")
	assert.Equal(t, past.ID, got[0].ID, "ordered by expires_at ASC")
	assert.Equal(t, atCutoff.ID, got[1].ID, "cutoff is inclusive (<=)")
	assert.Equal(t, "atk-past", got[0].AccessToken, "tokens are decrypted on list")

	got, err = s.ListOAuthIdentitiesExpiringBefore(ctx, base.Add(2*time.Hour))
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, past.ID, got[0].ID)
	assert.Equal(t, atCutoff.ID, got[1].ID)
	assert.Equal(t, future.ID, got[2].ID)

	got, err = s.ListOAuthIdentitiesExpiringBefore(ctx, base.Add(-2*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestOAuthMethodsRequireCrypto(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "auth.db") + "?_pragma=busy_timeout(5000)"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	_, err = s.CreateOAuthIdentity(ctx, OAuthIdentity{
		TenantID: "local", UserID: "u", Provider: ProviderTwitch,
		ProviderUserID: "x", Purpose: OAuthPurposeUser, AccessToken: "atk",
	})
	assert.ErrorIs(t, err, ErrCryptoRequired)

	_, err = s.GetOAuthIdentityByProviderUserID(ctx, ProviderTwitch, "x")
	assert.ErrorIs(t, err, ErrCryptoRequired)

	_, err = s.GetOAuthIdentitiesByUser(ctx, "local", "u")
	assert.ErrorIs(t, err, ErrCryptoRequired)

	_, err = s.GetBotIdentity(ctx, "local", ProviderTwitch)
	assert.ErrorIs(t, err, ErrCryptoRequired)

	err = s.UpdateOAuthTokens(ctx, "id", "atk", "", time.Time{})
	assert.ErrorIs(t, err, ErrCryptoRequired)

	_, err = s.ListOAuthIdentitiesExpiringBefore(ctx, time.Now())
	assert.ErrorIs(t, err, ErrCryptoRequired)
}

func TestNewOAuthIdentityIDIsULID(t *testing.T) {
	a := NewOAuthIdentityID()
	b := NewOAuthIdentityID()
	assert.NotEqual(t, a, b)
	assert.Len(t, a, 26, "ULID base32 = 26 chars")
}
