package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// sessionTokenBytes is the entropy of an opaque session token (32B
	// = 256 bits, well above the practical brute-force boundary).
	sessionTokenBytes = 32

	// DefaultSessionLifetime bounds how long a freshly-issued session is
	// valid before the user must re-authenticate.
	DefaultSessionLifetime = 7 * 24 * time.Hour
)

// Session errors. Compare with errors.Is.
var (
	ErrSessionNotFound = errors.New("auth: session not found")
	ErrSessionExpired  = errors.New("auth: session expired")
	ErrInvalidSession  = errors.New("auth: invalid session")
)

// Session is an opaque, server-side credential. The plaintext token is
// returned only by NewSession (and never persisted); the Store keeps
// only the SHA-256 hash of the token in TokenHash, so a database leak
// does not let an attacker forge sessions.
//
// UserAgent and RemoteIP are retained for audit only; they are not
// part of any security check (UA spoofing is trivial).
type Session struct {
	ID         string
	TenantID   string
	UserID     string
	TokenHash  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt time.Time
	UserAgent  string
	RemoteIP   string
}

// NewSession mints a fresh session for the given user. It returns the
// plaintext token (which must be transported to the caller exactly
// once - typically via a Set-Cookie header) and the persisted Session
// record whose TokenHash matches that token.
//
// lifetime <= 0 falls back to DefaultSessionLifetime.
func NewSession(tenantID, userID, userAgent, remoteIP string, lifetime time.Duration) (token string, session Session, err error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", Session{}, fmt.Errorf("%w: empty tenant id", ErrInvalidSession)
	}
	if strings.TrimSpace(userID) == "" {
		return "", Session{}, fmt.Errorf("%w: empty user id", ErrInvalidSession)
	}
	if lifetime <= 0 {
		lifetime = DefaultSessionLifetime
	}

	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", Session{}, fmt.Errorf("auth: read random: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)

	now := time.Now().UTC()
	session = Session{
		ID:         NewUserID(),
		TenantID:   tenantID,
		UserID:     userID,
		TokenHash:  HashTokenString(token),
		CreatedAt:  now,
		ExpiresAt:  now.Add(lifetime),
		LastUsedAt: now,
		UserAgent:  userAgent,
		RemoteIP:   remoteIP,
	}
	return token, session, nil
}

// Expired reports whether the session has passed its ExpiresAt boundary
// relative to now.
func (s Session) Expired(now time.Time) bool {
	return !s.ExpiresAt.IsZero() && !now.Before(s.ExpiresAt)
}

// Validate checks that s contains the fields required for persistence.
func (s Session) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: empty id", ErrInvalidSession)
	}
	if strings.TrimSpace(s.TenantID) == "" {
		return fmt.Errorf("%w: empty tenant id", ErrInvalidSession)
	}
	if strings.TrimSpace(s.UserID) == "" {
		return fmt.Errorf("%w: empty user id", ErrInvalidSession)
	}
	if strings.TrimSpace(s.TokenHash) == "" {
		return fmt.Errorf("%w: empty token hash", ErrInvalidSession)
	}
	if s.ExpiresAt.IsZero() || !s.ExpiresAt.After(s.CreatedAt) {
		return fmt.Errorf("%w: invalid expiry", ErrInvalidSession)
	}
	return nil
}
