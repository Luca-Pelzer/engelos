package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// OAuth-related errors. Callers should compare with errors.Is.
var (
	// ErrOAuthIdentityNotFound is returned by Store implementations when
	// a lookup does not match any oauth_identities row.
	ErrOAuthIdentityNotFound = errors.New("auth: oauth identity not found")

	// ErrInvalidOAuthIdentity is returned by validation when an
	// OAuthIdentity struct is missing required fields or has an
	// unsupported provider/purpose.
	ErrInvalidOAuthIdentity = errors.New("auth: invalid oauth identity")

	// ErrCryptoRequired is returned by Store methods that need to
	// encrypt or decrypt token fields when the Store was opened without
	// a WithCrypto option.
	ErrCryptoRequired = errors.New("auth: oauth storage requires an encryption key (WithCrypto)")
)

// Supported OAuth provider identifiers.
const (
	ProviderTwitch  = "twitch"
	ProviderDiscord = "discord"
)

// Supported OAuth identity purposes.
//
// OAuthPurposeUser denotes an end-user SSO link (one per user per
// provider). OAuthPurposeBot denotes the single outbound bot account a
// tenant uses to drive feed adapters (one per tenant per provider).
const (
	OAuthPurposeUser = "user"
	OAuthPurposeBot  = "bot"
)

// OAuthIdentity is an external identity (Twitch, Discord, ...) linked
// to a local User. Token fields are PLAINTEXT in memory and ENCRYPTED
// at rest by the Store using internal/secrets.Box.
//
// Purpose distinguishes a user's personal SSO link ("user") from the
// shared outbound bot credential used by feed adapters ("bot").
type OAuthIdentity struct {
	ID             string
	TenantID       string
	UserID         string
	Provider       string
	ProviderUserID string
	ProviderLogin  string
	Purpose        string
	AccessToken    string
	RefreshToken   string
	Scopes         []string
	ExpiresAt      time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewOAuthIdentityID returns a fresh ULID string suitable for use as an
// OAuthIdentity.ID, mirroring NewUserID.
func NewOAuthIdentityID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// Validate checks that o contains the minimum data required for
// storage and that Provider/Purpose are recognised values.
func (o *OAuthIdentity) Validate() error {
	if o == nil {
		return fmt.Errorf("%w: nil identity", ErrInvalidOAuthIdentity)
	}
	if strings.TrimSpace(o.TenantID) == "" {
		return fmt.Errorf("%w: empty tenant id", ErrInvalidOAuthIdentity)
	}
	if strings.TrimSpace(o.UserID) == "" {
		return fmt.Errorf("%w: empty user id", ErrInvalidOAuthIdentity)
	}
	switch o.Provider {
	case ProviderTwitch, ProviderDiscord:
	default:
		return fmt.Errorf("%w: unsupported provider %q", ErrInvalidOAuthIdentity, o.Provider)
	}
	if strings.TrimSpace(o.ProviderUserID) == "" {
		return fmt.Errorf("%w: empty provider user id", ErrInvalidOAuthIdentity)
	}
	switch o.Purpose {
	case OAuthPurposeUser, OAuthPurposeBot:
	default:
		return fmt.Errorf("%w: unsupported purpose %q", ErrInvalidOAuthIdentity, o.Purpose)
	}
	if strings.TrimSpace(o.AccessToken) == "" {
		return fmt.Errorf("%w: empty access token", ErrInvalidOAuthIdentity)
	}
	return nil
}

// encodeOAuthScopes joins scopes with single spaces, matching the
// canonical OAuth scope serialisation used by Twitch and Discord.
func encodeOAuthScopes(scopes []string) string {
	if len(scopes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// decodeOAuthScopes splits a space-separated scope string, trimming and
// discarding empty entries. Returns nil for an empty input.
func decodeOAuthScopes(s string) []string {
	if s == "" {
		return nil
	}
	raw := strings.Fields(s)
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}
