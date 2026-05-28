package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters per OWASP 2024 recommendations.
//
// These values intentionally exceed the OWASP minimum (m=46MiB, t=1) and
// are aligned with the "second" profile (m=64MiB, t=3) which is widely
// considered safe through 2030 for interactive logins on commodity
// hardware.
const (
	argon2Memory      uint32 = 64 * 1024 // 64 MiB
	argon2Iterations  uint32 = 3
	argon2Parallelism uint8  = 4
	argon2SaltLen            = 16
	argon2KeyLen      uint32 = 32
)

// Errors returned by the hash helpers.
var (
	// ErrInvalidHash means the encoded hash string is not in the
	// expected PHC format.
	ErrInvalidHash = errors.New("auth: invalid argon2id hash format")

	// ErrIncompatibleVersion means the hash was produced by a newer
	// version of argon2 than this binary knows.
	ErrIncompatibleVersion = errors.New("auth: incompatible argon2 version")
)

// HashPassword derives an Argon2id hash for the given plaintext password
// using a freshly-generated 16-byte random salt. The output is a PHC
// formatted string:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<base64-salt>$<base64-hash>
//
// It is safe to call from multiple goroutines.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("auth: empty password")
	}
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	key := argon2.IDKey(
		[]byte(password),
		salt,
		argon2Iterations,
		argon2Memory,
		argon2Parallelism,
		argon2KeyLen,
	)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argon2Memory,
		argon2Iterations,
		argon2Parallelism,
		b64.EncodeToString(salt),
		b64.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the given PHC encoded
// hash. It returns nil on success, ErrInvalidHash on a malformed hash,
// ErrIncompatibleVersion on a future argon2 version, or a generic error
// on mismatch.
//
// The comparison of the derived key uses crypto/subtle.ConstantTimeCompare
// to avoid timing-based side channels.
func VerifyPassword(password, encoded string) error {
	if password == "" {
		return fmt.Errorf("auth: empty password")
	}
	parts := strings.Split(encoded, "$")
	// Expected layout: ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 {
		return ErrInvalidHash
	}
	if parts[1] != "argon2id" {
		return ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return ErrInvalidHash
	}
	if version != argon2.Version {
		return ErrIncompatibleVersion
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(
		parts[3], "m=%d,t=%d,p=%d",
		&memory, &iterations, &parallelism,
	); err != nil {
		return ErrInvalidHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return ErrInvalidHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return ErrInvalidHash
	}
	got := argon2.IDKey(
		[]byte(password),
		salt,
		iterations,
		memory,
		parallelism,
		uint32(len(want)),
	)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return errors.New("auth: password does not match")
	}
	return nil
}

// HashToken returns the lowercase hex SHA-256 digest of the given token
// bytes. It is used for session tokens and API keys: those are random
// values of high entropy, so a fast (non-stretching) hash is both
// sufficient and necessary to keep lookups in indexed columns cheap.
//
// The output is deterministic, so it is safe to use as a unique index
// key.
func HashToken(token []byte) string {
	sum := sha256.Sum256(token)
	return hex.EncodeToString(sum[:])
}

// HashTokenString is a convenience wrapper around HashToken for string
// inputs.
func HashTokenString(token string) string {
	return HashToken([]byte(token))
}
