package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBox(t *testing.T) *Box {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	b, err := NewBox(key)
	require.NoError(t, err)
	return b
}

func TestNewBox_RejectsBadKeySizes(t *testing.T) {
	for _, n := range []int{0, 1, 15, 16, 24, 31, 33, 64} {
		t.Run("size", func(t *testing.T) {
			key := make([]byte, n)
			_, err := NewBox(key)
			require.ErrorIs(t, err, ErrInvalidKeySize, "n=%d", n)
		})
	}
}

func TestNewBox_AcceptsExact32Bytes(t *testing.T) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	b, err := NewBox(key)
	require.NoError(t, err)
	require.NotNil(t, b)
}

func TestNewBoxFromBase64(t *testing.T) {
	t.Run("empty string returns ErrNoKey", func(t *testing.T) {
		_, err := NewBoxFromBase64("")
		require.ErrorIs(t, err, ErrNoKey)
	})

	t.Run("whitespace-only returns ErrNoKey", func(t *testing.T) {
		_, err := NewBoxFromBase64("   \t\n  ")
		require.ErrorIs(t, err, ErrNoKey)
	})

	t.Run("invalid base64 returns error", func(t *testing.T) {
		_, err := NewBoxFromBase64("not!!!valid!!!base64@@@")
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrNoKey)
		assert.NotErrorIs(t, err, ErrInvalidKeySize)
	})

	t.Run("wrong decoded length returns ErrInvalidKeySize", func(t *testing.T) {
		short := base64.StdEncoding.EncodeToString([]byte("too short"))
		_, err := NewBoxFromBase64(short)
		require.ErrorIs(t, err, ErrInvalidKeySize)
	})

	t.Run("valid 32-byte key works", func(t *testing.T) {
		key := make([]byte, 32)
		_, err := rand.Read(key)
		require.NoError(t, err)
		enc := base64.StdEncoding.EncodeToString(key)
		b, err := NewBoxFromBase64(enc)
		require.NoError(t, err)
		require.NotNil(t, b)
	})

	t.Run("surrounding whitespace is trimmed", func(t *testing.T) {
		key := make([]byte, 32)
		_, err := rand.Read(key)
		require.NoError(t, err)
		enc := "  \n" + base64.StdEncoding.EncodeToString(key) + "\t\n"
		b, err := NewBoxFromBase64(enc)
		require.NoError(t, err)
		require.NotNil(t, b)
	})
}

func TestRoundTrip(t *testing.T) {
	b := newTestBox(t)

	large := make([]byte, 1024)
	_, err := rand.Read(large)
	require.NoError(t, err)

	cases := []struct {
		name string
		in   []byte
	}{
		{"empty", []byte{}},
		{"nil", nil},
		{"short", []byte("hi")},
		{"sentence", []byte("the quick brown fox jumps over the lazy dog")},
		{"1KB random", large},
		{"unicode", []byte("héllo 世界 🔐 — αβγ")},
		{"binary nul bytes", []byte{0, 0, 0, 1, 2, 3, 0, 0}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blob, err := b.Encrypt(tc.in)
			require.NoError(t, err)
			got, err := b.Decrypt(blob)
			require.NoError(t, err)
			if len(tc.in) == 0 {
				assert.Empty(t, got)
			} else {
				assert.True(t, bytes.Equal(tc.in, got))
			}
		})
	}
}

func TestEncryptStringDecryptString(t *testing.T) {
	b := newTestBox(t)

	cases := []string{
		"",
		"hello",
		"oauth-refresh-token-abc123",
		"héllo 世界 🔐",
		strings.Repeat("x", 4096),
	}
	for _, s := range cases {
		t.Run(caseName(s), func(t *testing.T) {
			blob, err := b.EncryptString(s)
			require.NoError(t, err)
			got, err := b.DecryptString(blob)
			require.NoError(t, err)
			assert.Equal(t, s, got)
		})
	}
}

func caseName(s string) string {
	const max = 16
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func TestNonceIsRandom_SamePlaintextDifferentBlobs(t *testing.T) {
	b := newTestBox(t)
	pt := []byte("totp-seed-deadbeef")

	blob1, err := b.Encrypt(pt)
	require.NoError(t, err)
	blob2, err := b.Encrypt(pt)
	require.NoError(t, err)

	require.Equal(t, len(blob1), len(blob2))
	assert.False(t, bytes.Equal(blob1, blob2),
		"two encryptions of the same plaintext produced identical blobs — nonce is not random")

	assert.False(t, bytes.Equal(blob1[1:1+nonceSize], blob2[1:1+nonceSize]),
		"nonce bytes are identical across two encryptions")
}

func TestDecrypt_TamperDetection(t *testing.T) {
	b := newTestBox(t)
	blob, err := b.Encrypt([]byte("authenticated-data"))
	require.NoError(t, err)

	require.Greater(t, len(blob), 1+nonceSize+1)
	tampered := make([]byte, len(blob))
	copy(tampered, blob)
	tampered[len(tampered)-1] ^= 0x01

	_, err = b.Decrypt(tampered)
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestDecrypt_TamperInNonce(t *testing.T) {
	b := newTestBox(t)
	blob, err := b.Encrypt([]byte("authenticated-data"))
	require.NoError(t, err)

	tampered := make([]byte, len(blob))
	copy(tampered, blob)
	tampered[1] ^= 0xFF

	_, err = b.Decrypt(tampered)
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestDecryptString_ReturnsEmptyOnError(t *testing.T) {
	b := newTestBox(t)
	got, err := b.DecryptString(nil)
	require.ErrorIs(t, err, ErrMalformedCiphertext)
	assert.Equal(t, "", got)

	blob, err := b.EncryptString("payload")
	require.NoError(t, err)
	blob[len(blob)-1] ^= 0xFF
	got, err = b.DecryptString(blob)
	require.ErrorIs(t, err, ErrDecrypt)
	assert.Equal(t, "", got)
}

func TestDecrypt_WrongKey(t *testing.T) {
	a := newTestBox(t)
	c := newTestBox(t)

	blob, err := a.Encrypt([]byte("cross-key payload"))
	require.NoError(t, err)

	_, err = c.Decrypt(blob)
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestDecrypt_Malformed(t *testing.T) {
	b := newTestBox(t)

	t.Run("nil", func(t *testing.T) {
		_, err := b.Decrypt(nil)
		require.ErrorIs(t, err, ErrMalformedCiphertext)
	})

	t.Run("empty", func(t *testing.T) {
		_, err := b.Decrypt([]byte{})
		require.ErrorIs(t, err, ErrMalformedCiphertext)
	})

	t.Run("too short for version+nonce+tag", func(t *testing.T) {
		_, err := b.Decrypt(make([]byte, 1+nonceSize+1))
		require.ErrorIs(t, err, ErrMalformedCiphertext)
	})

	t.Run("wrong version byte 0x02", func(t *testing.T) {
		good, err := b.Encrypt([]byte("data"))
		require.NoError(t, err)
		bad := make([]byte, len(good))
		copy(bad, good)
		bad[0] = 0x02
		_, err = b.Decrypt(bad)
		require.ErrorIs(t, err, ErrMalformedCiphertext)
	})

	t.Run("wrong version byte 0x00", func(t *testing.T) {
		good, err := b.Encrypt([]byte("data"))
		require.NoError(t, err)
		bad := make([]byte, len(good))
		copy(bad, good)
		bad[0] = 0x00
		_, err = b.Decrypt(bad)
		require.ErrorIs(t, err, ErrMalformedCiphertext)
	})
}

func TestGenerateKey(t *testing.T) {
	k1, err := GenerateKey()
	require.NoError(t, err)
	require.Len(t, k1, 32)

	k2, err := GenerateKey()
	require.NoError(t, err)
	require.Len(t, k2, 32)

	assert.False(t, bytes.Equal(k1, k2), "two fresh keys collided")

	_, err = NewBox(k1)
	require.NoError(t, err)
}

func TestGenerateKeyBase64_RoundTripsThroughNewBoxFromBase64(t *testing.T) {
	enc, err := GenerateKeyBase64()
	require.NoError(t, err)

	raw, err := base64.StdEncoding.DecodeString(enc)
	require.NoError(t, err)
	require.Len(t, raw, 32)

	b, err := NewBoxFromBase64(enc)
	require.NoError(t, err)

	blob, err := b.EncryptString("payload")
	require.NoError(t, err)
	got, err := b.DecryptString(blob)
	require.NoError(t, err)
	assert.Equal(t, "payload", got)
}

func TestErrors_AreDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrDecrypt, ErrMalformedCiphertext))
	assert.False(t, errors.Is(ErrMalformedCiphertext, ErrDecrypt))
	assert.False(t, errors.Is(ErrNoKey, ErrInvalidKeySize))
}
