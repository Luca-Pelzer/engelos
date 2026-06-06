package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "auth.db") + "?_pragma=busy_timeout(5000)"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func makeUser(t *testing.T, tenant, email, username string, role Role) User {
	t.Helper()
	hash, err := HashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	return User{
		ID:           NewUserID(),
		TenantID:     tenant,
		Email:        email,
		Username:     username,
		PasswordHash: []byte(hash),
		Role:         role,
	}
}

// -------- Role / Permission ---------------------------------------------

func TestRolePermissions(t *testing.T) {
	t.Run("owner has every permission", func(t *testing.T) {
		for _, p := range AllPermissions() {
			assert.True(t, RoleOwner.Can(p), "owner missing %s", p)
		}
	})

	t.Run("admin lacks billing and users:write", func(t *testing.T) {
		assert.False(t, RoleAdmin.Can(PermBillingRead))
		assert.False(t, RoleAdmin.Can(PermBillingWrite))
		assert.False(t, RoleAdmin.Can(PermUsersWrite))
		assert.True(t, RoleAdmin.Can(PermUsersRead))
		assert.True(t, RoleAdmin.Can(PermSettingsWrite))
	})

	t.Run("mod can edit commands and automod", func(t *testing.T) {
		assert.True(t, RoleMod.Can(PermCommandsWrite))
		assert.True(t, RoleMod.Can(PermAutomodWrite))
		assert.True(t, RoleMod.Can(PermSettingsRead))
		assert.False(t, RoleMod.Can(PermSettingsWrite))
		assert.False(t, RoleMod.Can(PermIntegrationsRead))
		assert.False(t, RoleMod.Can(PermAPIKeysRead))
	})

	t.Run("viewer is read-only", func(t *testing.T) {
		for _, p := range AllPermissions() {
			if strings.HasSuffix(string(p), ":read") {
				assert.True(t, RoleViewer.Can(p), "viewer should grant %s", p)
			} else {
				assert.False(t, RoleViewer.Can(p), "viewer must not grant %s", p)
			}
		}
	})

	t.Run("invalid role grants nothing", func(t *testing.T) {
		var bogus Role = "intern"
		assert.False(t, bogus.Valid())
		assert.False(t, bogus.Can(PermCommandsRead))
		assert.Empty(t, bogus.Permissions())
	})
}

// -------- Password hashing ---------------------------------------------

func TestHashPasswordRoundtrip(t *testing.T) {
	pw := "Tr0ub4dor&3-secure!"
	h, err := HashPassword(pw)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=4$"))

	assert.NoError(t, VerifyPassword(pw, h))
	assert.Error(t, VerifyPassword("wrong", h))
}

func TestHashPasswordUniqueSalts(t *testing.T) {
	a, err := HashPassword("same")
	require.NoError(t, err)
	b, err := HashPassword("same")
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "salts must differ between calls")
}

func TestVerifyPasswordRejectsGarbage(t *testing.T) {
	cases := []string{
		"",
		"$argon2id$v=19$m=65536$abc$def",
		"$bcrypt$v=1$m=1,t=1,p=1$aa$bb",
		"$argon2id$v=99$m=1,t=1,p=1$aa$bb",
	}
	for _, c := range cases {
		err := VerifyPassword("x", c)
		assert.Error(t, err, "garbage %q", c)
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	a := HashTokenString("hello")
	b := HashTokenString("hello")
	c := HashTokenString("world")
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
	assert.Len(t, a, 64, "sha256 hex = 64 chars")
}

// -------- Users CRUD + tenant isolation --------------------------------

func TestUserCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u := makeUser(t, "tenant-a", "Alice@example.com", "Alice", RoleOwner)
	created, err := s.CreateUser(ctx, u)
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", created.Email, "email should be lowercased")
	assert.Equal(t, "alice", created.Username)
	assert.False(t, created.CreatedAt.IsZero())

	fetched, err := s.GetUserByID(ctx, "tenant-a", created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, created.PasswordHash, fetched.PasswordHash)

	byEmail, err := s.GetUserByEmail(ctx, "tenant-a", "ALICE@example.com")
	require.NoError(t, err)
	assert.Equal(t, created.ID, byEmail.ID)

	fetched.Role = RoleAdmin
	require.NoError(t, s.UpdateUser(ctx, fetched))
	reread, err := s.GetUserByID(ctx, "tenant-a", fetched.ID)
	require.NoError(t, err)
	assert.Equal(t, RoleAdmin, reread.Role)
	assert.True(t, reread.UpdatedAt.After(reread.CreatedAt) || reread.UpdatedAt.Equal(reread.CreatedAt))

	users, err := s.ListUsers(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Len(t, users, 1)

	require.NoError(t, s.DeleteUser(ctx, "tenant-a", fetched.ID))
	_, err = s.GetUserByID(ctx, "tenant-a", fetched.ID)
	assert.ErrorIs(t, err, ErrUserNotFound)
	assert.ErrorIs(t, s.DeleteUser(ctx, "tenant-a", fetched.ID), ErrUserNotFound)
}

func TestUserTenantIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a := makeUser(t, "tenant-a", "bob@example.com", "bob", RoleOwner)
	b := makeUser(t, "tenant-b", "bob@example.com", "bob", RoleOwner)

	_, err := s.CreateUser(ctx, a)
	require.NoError(t, err)
	_, err = s.CreateUser(ctx, b)
	require.NoError(t, err, "same email allowed across tenants")

	_, err = s.GetUserByEmail(ctx, "tenant-a", "bob@example.com")
	require.NoError(t, err)
	_, err = s.GetUserByEmail(ctx, "tenant-c", "bob@example.com")
	assert.ErrorIs(t, err, ErrUserNotFound)
}

func TestUserDuplicateEmailReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := makeUser(t, "t", "dup@x.com", "first", RoleOwner)
	_, err := s.CreateUser(ctx, first)
	require.NoError(t, err)

	second := makeUser(t, "t", "DUP@X.com", "second", RoleAdmin)
	_, err = s.CreateUser(ctx, second)
	assert.ErrorIs(t, err, ErrUserAlreadyExists)
}

func TestUserDuplicateUsernameReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := makeUser(t, "t", "a@x.com", "shared", RoleOwner)
	_, err := s.CreateUser(ctx, first)
	require.NoError(t, err)

	second := makeUser(t, "t", "b@x.com", "shared", RoleAdmin)
	_, err = s.CreateUser(ctx, second)
	assert.ErrorIs(t, err, ErrUserAlreadyExists)
}

// Race test: concurrent CreateUser with the same email must yield
// exactly one success and N-1 ErrUserAlreadyExists.
func TestUserConcurrentCreateRace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const N = 16
	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		successes   int
		duplicates  int
		otherErrors []error
	)
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			u := makeUser(t, "t", "race@x.com", fmt.Sprintf("user-%d", i), RoleOwner)
			<-start
			_, err := s.CreateUser(ctx, u)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrUserAlreadyExists):
				duplicates++
			default:
				otherErrors = append(otherErrors, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.Equal(t, 1, successes, "exactly one writer wins")
	assert.Equal(t, N-1, duplicates, "the rest must see ErrUserAlreadyExists")
	assert.Empty(t, otherErrors, "no unexpected errors")
}

func TestUserValidate(t *testing.T) {
	u := User{}
	assert.ErrorIs(t, u.Validate(), ErrInvalidUser)

	good := makeUser(t, "t", "x@y.com", "x", RoleOwner)
	assert.NoError(t, good.Validate())

	bad := good
	bad.Role = "garbage"
	assert.ErrorIs(t, bad.Validate(), ErrInvalidUser)
}

// -------- Sessions ------------------------------------------------------

func TestSessionLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, makeUser(t, "t", "u@x.com", "u", RoleOwner))
	require.NoError(t, err)

	token, sess, err := NewSession(u.TenantID, u.ID, "Mozilla/5.0", "127.0.0.1", time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Equal(t, HashTokenString(token), sess.TokenHash)
	assert.False(t, sess.Expired(time.Now()))

	require.NoError(t, s.CreateSession(ctx, sess))

	got, err := s.GetSessionByTokenHash(ctx, sess.TokenHash)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, got.ID)
	assert.Equal(t, sess.UserID, got.UserID)

	_, err = s.GetSessionByTokenHash(ctx, "deadbeef")
	assert.ErrorIs(t, err, ErrSessionNotFound)

	require.NoError(t, s.DeleteSession(ctx, sess.TokenHash))
	_, err = s.GetSessionByTokenHash(ctx, sess.TokenHash)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionExpiry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, makeUser(t, "t", "u@x.com", "u", RoleOwner))
	require.NoError(t, err)

	_, fresh, err := NewSession(u.TenantID, u.ID, "", "", time.Hour)
	require.NoError(t, err)
	require.NoError(t, s.CreateSession(ctx, fresh))

	_, stale, err := NewSession(u.TenantID, u.ID, "", "", time.Hour)
	require.NoError(t, err)
	stale.CreatedAt = time.Now().UTC().Add(-time.Hour)
	stale.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	require.NoError(t, s.CreateSession(ctx, stale))

	n, err := s.DeleteExpiredSessions(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	_, err = s.GetSessionByTokenHash(ctx, fresh.TokenHash)
	assert.NoError(t, err)
	_, err = s.GetSessionByTokenHash(ctx, stale.TokenHash)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionUniqueTokens(t *testing.T) {
	a, _, err := NewSession("t", "u", "", "", time.Hour)
	require.NoError(t, err)
	b, _, err := NewSession("t", "u", "", "", time.Hour)
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestSessionValidate(t *testing.T) {
	bad := Session{}
	assert.ErrorIs(t, bad.Validate(), ErrInvalidSession)
}

// -------- API keys ------------------------------------------------------

func TestAPIKeyLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, makeUser(t, "t", "owner@x.com", "owner", RoleOwner))
	require.NoError(t, err)

	scopes := []Permission{PermCommandsRead, PermCommandsWrite}
	plaintext, key, err := NewAPIKey(u.TenantID, u.ID, "streamdeck-mod", scopes, nil)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(plaintext, APIKeyPrefix))
	assert.Equal(t, HashTokenString(plaintext), key.KeyHash)

	require.NoError(t, s.CreateAPIKey(ctx, key))

	got, err := s.GetAPIKeyByHash(ctx, key.KeyHash)
	require.NoError(t, err)
	assert.Equal(t, key.ID, got.ID)
	assert.True(t, got.HasScope(PermCommandsWrite))
	assert.False(t, got.HasScope(PermBillingWrite))

	all, err := s.ListAPIKeys(ctx, u.TenantID)
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestAPIKeyRevoke(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, makeUser(t, "t", "owner@x.com", "owner", RoleOwner))
	require.NoError(t, err)

	_, key, err := NewAPIKey(u.TenantID, u.ID, "bot", []Permission{PermChatTokenSafe()}, nil)
	require.NoError(t, err)
	require.NoError(t, s.CreateAPIKey(ctx, key))

	require.NoError(t, s.RevokeAPIKey(ctx, key.ID))
	got, err := s.GetAPIKeyByHash(ctx, key.KeyHash)
	require.NoError(t, err)
	assert.True(t, got.Revoked())
	assert.Error(t, got.Usable(time.Now(), netip.MustParseAddr("127.0.0.1")))
	assert.ErrorIs(t, got.Usable(time.Now(), netip.MustParseAddr("127.0.0.1")), ErrAPIKeyRevoked)

	assert.ErrorIs(t, s.RevokeAPIKey(ctx, key.ID), ErrAPIKeyNotFound,
		"second revoke is a no-op against the WHERE revoked_at IS NULL guard")
}

// PermChatTokenSafe returns a Permission that is guaranteed not to be
// validated against the AllPermissions list (the Store does not
// validate scopes - that's the api layer's job), so this lets us
// exercise a free-form scope in tests.
func PermChatTokenSafe() Permission { return Permission("chat:write") }

func TestAPIKeyExpiry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, makeUser(t, "t", "owner@x.com", "owner", RoleOwner))
	require.NoError(t, err)

	past := time.Now().UTC().Add(-time.Minute)
	_, key, err := NewAPIKey(u.TenantID, u.ID, "expired", []Permission{PermCommandsRead}, &past)
	require.NoError(t, err)
	require.NoError(t, s.CreateAPIKey(ctx, key))

	got, err := s.GetAPIKeyByHash(ctx, key.KeyHash)
	require.NoError(t, err)
	assert.True(t, got.Expired(time.Now()))
	assert.ErrorIs(t, got.Usable(time.Now(), netip.MustParseAddr("127.0.0.1")), ErrAPIKeyExpired)
}

func TestAPIKeyIPWhitelist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, makeUser(t, "t", "owner@x.com", "owner", RoleOwner))
	require.NoError(t, err)

	_, key, err := NewAPIKey(u.TenantID, u.ID, "wl", []Permission{PermCommandsRead}, nil)
	require.NoError(t, err)
	key.IPWhitelist = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	require.NoError(t, s.CreateAPIKey(ctx, key))

	got, err := s.GetAPIKeyByHash(ctx, key.KeyHash)
	require.NoError(t, err)
	require.Len(t, got.IPWhitelist, 1)
	assert.True(t, got.AllowsIP(netip.MustParseAddr("10.1.2.3")))
	assert.False(t, got.AllowsIP(netip.MustParseAddr("8.8.8.8")))
	assert.NoError(t, got.Usable(time.Now(), netip.MustParseAddr("10.0.0.1")))
	assert.Error(t, got.Usable(time.Now(), netip.MustParseAddr("8.8.8.8")))
}

func TestAPIKeyUpdateLastUsed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, makeUser(t, "t", "owner@x.com", "owner", RoleOwner))
	require.NoError(t, err)

	_, key, err := NewAPIKey(u.TenantID, u.ID, "k", []Permission{PermCommandsRead}, nil)
	require.NoError(t, err)
	require.NoError(t, s.CreateAPIKey(ctx, key))

	when := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, s.UpdateAPIKeyLastUsed(ctx, key.ID, when))

	got, err := s.GetAPIKeyByHash(ctx, key.KeyHash)
	require.NoError(t, err)
	assert.WithinDuration(t, when, got.LastUsedAt, time.Second)
}

func TestAPIKeyNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.GetAPIKeyByHash(ctx, "missing")
	assert.ErrorIs(t, err, ErrAPIKeyNotFound)
	assert.ErrorIs(t, s.RevokeAPIKey(ctx, "missing"), ErrAPIKeyNotFound)
	assert.ErrorIs(t, s.UpdateAPIKeyLastUsed(ctx, "missing", time.Now()), ErrAPIKeyNotFound)
}

func TestAPIKeyValidate(t *testing.T) {
	bad := APIKey{}
	assert.ErrorIs(t, bad.Validate(), ErrInvalidAPIKey)
}

func TestNewAPIKeyRejectsEmptyScopes(t *testing.T) {
	_, _, err := NewAPIKey("t", "u", "k", nil, nil)
	assert.ErrorIs(t, err, ErrInvalidAPIKey)
}

func TestNewSessionRejectsEmpty(t *testing.T) {
	_, _, err := NewSession("", "u", "", "", 0)
	assert.ErrorIs(t, err, ErrInvalidSession)
	_, _, err = NewSession("t", "", "", "", 0)
	assert.ErrorIs(t, err, ErrInvalidSession)
}
