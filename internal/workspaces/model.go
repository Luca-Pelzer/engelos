package workspaces

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	// ErrNotFound is returned when a workspace, membership or invitation
	// lookup matches no row.
	ErrNotFound = errors.New("workspaces: not found")

	// ErrAlreadyExists is returned when creating a workspace whose
	// (tenant, slug) already exists, or a duplicate membership.
	ErrAlreadyExists = errors.New("workspaces: already exists")

	// ErrInvalid is returned when an entity fails validation. The wrapped
	// detail says why.
	ErrInvalid = errors.New("workspaces: invalid")
)

// Role is a member's role within a single workspace. It deliberately reuses the
// string values of auth.Role so the two systems stay aligned, but the access a
// user has is governed by this per-workspace membership role, never by a global
// account role. v1 ships owner and mod only; guest is a planned follow-up.
type Role string

const (
	RoleOwner Role = "owner"
	RoleMod   Role = "mod"
)

func (r Role) valid() bool {
	switch r {
	case RoleOwner, RoleMod:
		return true
	default:
		return false
	}
}

// MembershipSource records how a membership came to be, so the UI can explain it
// and so a revoked Twitch mod can be re-evaluated without disturbing a manual
// invite.
type MembershipSource string

const (
	SourceOwner          MembershipSource = "owner"
	SourceInvite         MembershipSource = "invite"
	SourceTwitchVerified MembershipSource = "twitch_verified"
)

// Workspace is one managed channel. The slug is the URL key and equals the
// lowercase channel login; it is what the feature stores already use as their
// per-channel key, so a workspace formalises the previously free-text channel.
// AutoVerifyMods gates whether a logging-in Twitch moderator of this channel is
// auto-granted a mod membership.
type Workspace struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenant_id"`
	Slug            string    `json:"slug"`
	TwitchChannelID string    `json:"twitch_channel_id"`
	TwitchLogin     string    `json:"twitch_login"`
	DisplayName     string    `json:"display_name"`
	OwnerUserID     string    `json:"owner_user_id"`
	AutoVerifyMods  bool      `json:"auto_verify_mods"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Membership links a user to a workspace with a role. The pair
// (WorkspaceID, UserID) is unique.
type Membership struct {
	WorkspaceID string           `json:"workspace_id"`
	UserID      string           `json:"user_id"`
	Role        Role             `json:"role"`
	Source      MembershipSource `json:"source"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// Invitation is a pending grant addressed to a Twitch login. When that login
// next authenticates it is auto-accepted into a Membership. v1 invites mods only.
type Invitation struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	TwitchLogin string     `json:"twitch_login"`
	Role        Role       `json:"role"`
	Token       string     `json:"token"`
	InvitedBy   string     `json:"invited_by"`
	AcceptedAt  *time.Time `json:"accepted_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

const maxSlugLen = 40

func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// newToken returns a URL-safe random token for an invitation link.
func newToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("workspaces: token generation: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// normalizeLogin lower-cases and trims a Twitch login so lookups and slug
// derivation are stable regardless of how the login was typed.
func normalizeLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

func (w *Workspace) validate() error {
	if strings.TrimSpace(w.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	w.Slug = normalizeLogin(w.Slug)
	if w.Slug == "" {
		return fmt.Errorf("%w: slug is required", ErrInvalid)
	}
	if len(w.Slug) > maxSlugLen {
		return fmt.Errorf("%w: slug length %d exceeds %d", ErrInvalid, len(w.Slug), maxSlugLen)
	}
	w.TwitchLogin = normalizeLogin(w.TwitchLogin)
	if strings.TrimSpace(w.OwnerUserID) == "" {
		return fmt.Errorf("%w: owner_user_id is required", ErrInvalid)
	}
	return nil
}

func (m *Membership) validate() error {
	if strings.TrimSpace(m.WorkspaceID) == "" {
		return fmt.Errorf("%w: workspace_id is required", ErrInvalid)
	}
	if strings.TrimSpace(m.UserID) == "" {
		return fmt.Errorf("%w: user_id is required", ErrInvalid)
	}
	if !m.Role.valid() {
		return fmt.Errorf("%w: unknown role %q", ErrInvalid, m.Role)
	}
	return nil
}

func (i *Invitation) validate() error {
	if strings.TrimSpace(i.WorkspaceID) == "" {
		return fmt.Errorf("%w: workspace_id is required", ErrInvalid)
	}
	i.TwitchLogin = normalizeLogin(i.TwitchLogin)
	if i.TwitchLogin == "" {
		return fmt.Errorf("%w: twitch_login is required", ErrInvalid)
	}
	if !i.Role.valid() {
		return fmt.Errorf("%w: unknown role %q", ErrInvalid, i.Role)
	}
	return nil
}
