package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// APIKeyPrefix is prepended to every plaintext API key so the key type
// can be recognised at a glance (and so accidental commits to GitHub
// can be detected by secret scanners).
const APIKeyPrefix = "eos_"

const apiKeyEntropyBytes = 32

// API-key errors. Compare with errors.Is.
var (
	ErrAPIKeyNotFound = errors.New("auth: api key not found")
	ErrAPIKeyRevoked  = errors.New("auth: api key revoked")
	ErrAPIKeyExpired  = errors.New("auth: api key expired")
	ErrInvalidAPIKey  = errors.New("auth: invalid api key")
)

// APIKey is a long-lived, scope-restricted bearer credential used by
// integrations and machine clients (e.g. a Stream Deck plugin). The
// plaintext value is returned only at creation time and never persisted;
// the Store keeps only the SHA-256 KeyHash.
//
// Scopes is the subset of Permission strings this key may exercise. The
// effective permission of a request is therefore
// (key.Scopes ∩ key.OwnerRole.Permissions()) - that is, a key cannot
// elevate above its creator.
type APIKey struct {
	ID          string
	TenantID    string
	Name        string
	Description string
	KeyHash     string
	Prefix      string
	Scopes      []Permission
	IPWhitelist []netip.Prefix
	RateLimit   int
	CreatedAt   time.Time
	CreatedBy   string
	LastUsedAt  time.Time
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
}

// NewAPIKey mints a fresh API key for the given tenant. It returns the
// plaintext token (which MUST be shown to the operator exactly once and
// then discarded) and the persistable record whose KeyHash matches the
// token.
//
// scopes is copied; the caller may mutate the input slice freely.
// expiresAt may be nil for non-expiring keys.
func NewAPIKey(tenantID, createdByUserID, name string, scopes []Permission, expiresAt *time.Time) (plaintext string, key APIKey, err error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", APIKey{}, fmt.Errorf("%w: empty tenant id", ErrInvalidAPIKey)
	}
	if strings.TrimSpace(createdByUserID) == "" {
		return "", APIKey{}, fmt.Errorf("%w: empty creator id", ErrInvalidAPIKey)
	}
	if strings.TrimSpace(name) == "" {
		return "", APIKey{}, fmt.Errorf("%w: empty name", ErrInvalidAPIKey)
	}
	if len(scopes) == 0 {
		return "", APIKey{}, fmt.Errorf("%w: empty scopes", ErrInvalidAPIKey)
	}

	raw := make([]byte, apiKeyEntropyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", APIKey{}, fmt.Errorf("auth: read random: %w", err)
	}
	plaintext = APIKeyPrefix + base64.RawURLEncoding.EncodeToString(raw)

	scopesCopy := make([]Permission, len(scopes))
	copy(scopesCopy, scopes)

	now := time.Now().UTC()
	key = APIKey{
		ID:        NewUserID(),
		TenantID:  tenantID,
		Name:      strings.TrimSpace(name),
		KeyHash:   HashTokenString(plaintext),
		Prefix:    APIKeyPrefix,
		Scopes:    scopesCopy,
		CreatedAt: now,
		CreatedBy: createdByUserID,
		ExpiresAt: expiresAt,
	}
	return plaintext, key, nil
}

// HasScope reports whether the key grants Permission p.
func (k APIKey) HasScope(p Permission) bool {
	for _, s := range k.Scopes {
		if s == p {
			return true
		}
	}
	return false
}

// Revoked reports whether the key has been explicitly revoked.
func (k APIKey) Revoked() bool { return k.RevokedAt != nil }

// Expired reports whether the key's ExpiresAt has been crossed at now.
// Keys with no ExpiresAt never expire.
func (k APIKey) Expired(now time.Time) bool {
	return k.ExpiresAt != nil && !now.Before(*k.ExpiresAt)
}

// AllowsIP reports whether srcIP is permitted by the key's IPWhitelist.
// An empty whitelist allows any source.
func (k APIKey) AllowsIP(srcIP netip.Addr) bool {
	if len(k.IPWhitelist) == 0 {
		return true
	}
	for _, p := range k.IPWhitelist {
		if p.Contains(srcIP) {
			return true
		}
	}
	return false
}

// Usable returns nil iff the key may currently authenticate a request.
// It checks (in order) revocation, expiry and srcIP whitelist.
func (k APIKey) Usable(now time.Time, srcIP netip.Addr) error {
	if k.Revoked() {
		return ErrAPIKeyRevoked
	}
	if k.Expired(now) {
		return ErrAPIKeyExpired
	}
	if !k.AllowsIP(srcIP) {
		return fmt.Errorf("%w: source ip not in whitelist", ErrInvalidAPIKey)
	}
	return nil
}

// Validate checks that k has the required fields for persistence.
func (k APIKey) Validate() error {
	if strings.TrimSpace(k.ID) == "" {
		return fmt.Errorf("%w: empty id", ErrInvalidAPIKey)
	}
	if strings.TrimSpace(k.TenantID) == "" {
		return fmt.Errorf("%w: empty tenant id", ErrInvalidAPIKey)
	}
	if strings.TrimSpace(k.Name) == "" {
		return fmt.Errorf("%w: empty name", ErrInvalidAPIKey)
	}
	if strings.TrimSpace(k.KeyHash) == "" {
		return fmt.Errorf("%w: empty key hash", ErrInvalidAPIKey)
	}
	if len(k.Scopes) == 0 {
		return fmt.Errorf("%w: empty scopes", ErrInvalidAPIKey)
	}
	if strings.TrimSpace(k.CreatedBy) == "" {
		return fmt.Errorf("%w: empty createdBy", ErrInvalidAPIKey)
	}
	return nil
}
