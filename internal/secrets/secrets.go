// Package secrets provides authenticated symmetric encryption for small
// values held at rest, such as OAuth refresh tokens and TOTP seeds.
//
// The construction is AES-256-GCM with a freshly random 12-byte nonce per
// message. Ciphertext blobs are framed with a single leading version byte
// (currently 0x01) so that key rotation and algorithm migration can be
// added later without breaking on-disk compatibility:
//
//	version(1) || nonce(12) || ciphertext+tag
//
// All exported types and functions are safe for concurrent use; the
// underlying cipher.AEAD is read-only after construction.
//
// This package depends only on the Go standard library and is CGO-free.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Wire format constants.
//
// blobVersion is the current and only supported version byte. New versions
// reserve room for changes to the AEAD construction or key derivation
// without breaking existing ciphertexts.
const (
	blobVersion byte = 0x01
	nonceSize        = 12 // AES-GCM standard nonce size in bytes.
	keySize          = 32 // AES-256 requires a 32-byte key.
)

// Errors returned by the secrets package.
var (
	// ErrNoKey means no encryption key was supplied where one is
	// required (e.g. an empty base64 string was passed to
	// NewBoxFromBase64).
	ErrNoKey = errors.New("secrets: no encryption key configured")

	// ErrInvalidKeySize means the supplied key was not exactly 32
	// bytes, which is required for AES-256.
	ErrInvalidKeySize = errors.New("secrets: encryption key must be 32 bytes")

	// ErrMalformedCiphertext means the blob passed to Decrypt is
	// shorter than the minimum required length or carries an unknown
	// version byte.
	ErrMalformedCiphertext = errors.New("secrets: malformed ciphertext")

	// ErrDecrypt is returned for any authentication or decryption
	// failure. The underlying cause is intentionally not wrapped to
	// avoid leaking information that could aid a chosen-ciphertext
	// attacker.
	ErrDecrypt = errors.New("secrets: decryption failed")
)

// Box encrypts and decrypts small secrets (OAuth tokens, TOTP seeds) for
// storage at rest using AES-256-GCM. A Box is safe for concurrent use.
type Box struct {
	aead cipher.AEAD
}

// NewBox builds a Box from a raw 32-byte key. It returns ErrInvalidKeySize
// when the key length is not exactly 32 bytes.
//
// The caller retains ownership of the key slice; Box keeps only the
// derived cipher.AEAD.
func NewBox(key []byte) (*Box, error) {
	if len(key) != keySize {
		return nil, ErrInvalidKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		// Unreachable for a 32-byte key, but propagate defensively.
		return nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// NewBoxFromBase64 builds a Box from a standard base64-encoded 32-byte
// key. Surrounding whitespace in the input is trimmed before decoding.
//
// It returns ErrNoKey if the input is empty after trimming and a wrapped
// error if the input is not valid base64. The decoded length is then
// validated by NewBox.
func NewBoxFromBase64(b64 string) (*Box, error) {
	trimmed := strings.TrimSpace(b64)
	if trimmed == "" {
		return nil, ErrNoKey
	}
	raw, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("secrets: decode base64 key: %w", err)
	}
	return NewBox(raw)
}

// Encrypt seals plaintext with a freshly random nonce and returns the
// framed blob `version || nonce || ciphertext+tag`.
//
// A new nonce is drawn from crypto/rand on every call; callers must not
// reuse the returned blob's nonce against any other ciphertext under the
// same key. The returned slice is owned by the caller.
func (b *Box) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: read nonce: %w", err)
	}
	overhead := b.aead.Overhead()
	out := make([]byte, 0, 1+nonceSize+len(plaintext)+overhead)
	out = append(out, blobVersion)
	out = append(out, nonce...)
	out = b.aead.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// Decrypt verifies and opens a blob produced by Encrypt and returns the
// original plaintext.
//
// It returns ErrMalformedCiphertext when the blob is too short or carries
// an unknown version byte, and ErrDecrypt for any authentication or
// decryption failure. The underlying AEAD error is intentionally not
// exposed.
func (b *Box) Decrypt(blob []byte) ([]byte, error) {
	overhead := b.aead.Overhead()
	if len(blob) < 1+nonceSize+overhead {
		return nil, ErrMalformedCiphertext
	}
	if blob[0] != blobVersion {
		return nil, ErrMalformedCiphertext
	}
	nonce := blob[1 : 1+nonceSize]
	ciphertext := blob[1+nonceSize:]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

// EncryptString is a thin convenience wrapper around Encrypt for string
// plaintext inputs.
func (b *Box) EncryptString(s string) ([]byte, error) {
	return b.Encrypt([]byte(s))
}

// DecryptString is a thin convenience wrapper around Decrypt that returns
// the recovered plaintext as a string.
func (b *Box) DecryptString(blob []byte) (string, error) {
	pt, err := b.Decrypt(blob)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// GenerateKey returns 32 cryptographically random bytes suitable for use
// as an AES-256 key. It is intended for tooling and one-shot key minting.
func GenerateKey() ([]byte, error) {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("secrets: generate key: %w", err)
	}
	return key, nil
}

// GenerateKeyBase64 returns a fresh 32-byte key encoded with standard
// base64. The result is round-trip compatible with NewBoxFromBase64.
func GenerateKeyBase64() (string, error) {
	key, err := GenerateKey()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
