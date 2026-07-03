package avatar

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func openTestStore(t *testing.T) TokenStore {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "avatar.db")
	s, err := OpenSQLiteStore(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestTokenGetOrCreateIsStable(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	tok1, err := s.GetOrCreate(ctx, "tenant", "chan")
	require.NoError(t, err)
	require.Len(t, tok1, 64)

	tok2, err := s.GetOrCreate(ctx, "tenant", "chan")
	require.NoError(t, err)
	require.Equal(t, tok1, tok2, "token must be stable across reads")
}

func TestTokenGetOrCreateNormalizesChannel(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	tok1, err := s.GetOrCreate(ctx, "tenant", "MyChan")
	require.NoError(t, err)
	tok2, err := s.GetOrCreate(ctx, "tenant", "#mychan")
	require.NoError(t, err)
	require.Equal(t, tok1, tok2)
}

func TestTokenGetOrCreateRejectsEmpty(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.GetOrCreate(ctx, "", "chan")
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.GetOrCreate(ctx, "tenant", "")
	require.ErrorIs(t, err, ErrInvalid)
}

func TestTokenVerify(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	tok, err := s.GetOrCreate(ctx, "tenant", "chan")
	require.NoError(t, err)

	ok, err := s.Verify(ctx, "tenant", "chan", tok)
	require.NoError(t, err)
	require.True(t, ok, "correct token must verify")

	for name, tc := range map[string]struct {
		tenant, channel, token string
	}{
		"wrong token":     {"tenant", "chan", "deadbeef"},
		"empty token":     {"tenant", "chan", ""},
		"unknown channel": {"tenant", "other", tok},
		"wrong tenant":    {"other", "chan", tok},
	} {
		t.Run(name, func(t *testing.T) {
			ok, err := s.Verify(ctx, tc.tenant, tc.channel, tc.token)
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

func TestTokenIsolatedPerChannel(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	a, err := s.GetOrCreate(ctx, "tenant", "a")
	require.NoError(t, err)
	b, err := s.GetOrCreate(ctx, "tenant", "b")
	require.NoError(t, err)
	require.NotEqual(t, a, b, "different channels must get different tokens")

	ok, err := s.Verify(ctx, "tenant", "b", a)
	require.NoError(t, err)
	require.False(t, ok, "channel a's token must not verify for channel b")
}
