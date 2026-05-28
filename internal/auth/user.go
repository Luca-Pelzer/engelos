package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// User-related errors. Callers should compare with errors.Is.
var (
	// ErrUserNotFound is returned by Store implementations when a lookup
	// (by ID, email or username) does not match any row.
	ErrUserNotFound = errors.New("auth: user not found")

	// ErrUserAlreadyExists is returned by Store implementations when a
	// CreateUser call would violate the unique-per-tenant constraint on
	// email or username.
	ErrUserAlreadyExists = errors.New("auth: user already exists")

	// ErrInvalidUser is returned by validation when a User struct is
	// missing required fields or contains malformed data.
	ErrInvalidUser = errors.New("auth: invalid user")
)

// User represents a human (or service) account that can authenticate
// against the engelOS instance. Users are scoped to a TenantID; in
// self-hosted deployments there is typically a single tenant called
// "local".
//
// PasswordHash holds an Argon2id PHC string (see hash.go). TOTPSecret
// holds the *encrypted* TOTP shared secret; encryption keys are managed
// outside this package (see the config package's secret-box helper).
//
// Time fields are kept in UTC by the constructors and the Store
// implementation.
type User struct {
	ID           string    // ULID, lowercase
	TenantID     string    // multi-tenant scope; "local" for self-hosters
	Email        string    // unique per tenant, stored lowercased
	Username     string    // unique per tenant, stored lowercased
	PasswordHash []byte    // Argon2id PHC string ($argon2id$...)
	Role         Role      // RBAC tier
	TOTPSecret   []byte    // encrypted-at-rest TOTP secret; nil if 2FA off
	CreatedAt    time.Time // set by CreateUser
	UpdatedAt    time.Time // bumped on every UpdateUser
	LastLoginAt  time.Time // bumped on successful authentication
	Disabled     bool      // soft-lock; disabled users cannot log in
}

// NewUserID returns a fresh ULID string suitable for use as a User.ID.
// It uses crypto/rand for entropy so two concurrent calls in the same
// millisecond will still produce distinct IDs.
func NewUserID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// NormalizeEmail returns email lower-cased and trimmed. It does not
// perform any RFC-5321 parsing; that's the caller's responsibility.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormalizeUsername returns username lower-cased and trimmed.
func NormalizeUsername(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Validate checks that u contains the minimum data required for storage.
// It does NOT verify password strength or email format; those are the
// caller's responsibility.
func (u *User) Validate() error {
	if u == nil {
		return fmt.Errorf("%w: nil user", ErrInvalidUser)
	}
	if strings.TrimSpace(u.ID) == "" {
		return fmt.Errorf("%w: empty id", ErrInvalidUser)
	}
	if strings.TrimSpace(u.TenantID) == "" {
		return fmt.Errorf("%w: empty tenant id", ErrInvalidUser)
	}
	if NormalizeEmail(u.Email) == "" {
		return fmt.Errorf("%w: empty email", ErrInvalidUser)
	}
	if NormalizeUsername(u.Username) == "" {
		return fmt.Errorf("%w: empty username", ErrInvalidUser)
	}
	if len(u.PasswordHash) == 0 {
		return fmt.Errorf("%w: empty password hash", ErrInvalidUser)
	}
	if !u.Role.Valid() {
		return fmt.Errorf("%w: invalid role %q", ErrInvalidUser, u.Role)
	}
	return nil
}
