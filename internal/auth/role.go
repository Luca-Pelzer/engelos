package auth

import "slices"

// Role identifies a coarse permission tier that a User is assigned to.
// Roles are intentionally limited to a small enum so that operators do
// not have to reason about arbitrary permission combinations; if a
// finer grain is required, an API key with explicit scopes should be
// used instead.
type Role string

const (
	// RoleOwner has every permission. There is exactly one Owner per
	// tenant. The Owner cannot be demoted or deleted by anyone except
	// themselves.
	RoleOwner Role = "owner"

	// RoleAdmin can manage almost everything except billing and the
	// Owner account itself.
	RoleAdmin Role = "admin"

	// RoleMod can manage commands and automod and read settings, but
	// cannot touch credentials, integrations or users.
	RoleMod Role = "mod"

	// RoleViewer has read-only access to dashboards and logs.
	RoleViewer Role = "viewer"
)

// Valid reports whether r is one of the defined roles.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMod, RoleViewer:
		return true
	}
	return false
}

// Permission is a fine-grained capability string of the form
// "<resource>:<action>", for example "commands:write".
type Permission string

// Resource permissions used throughout engelOS.
const (
	PermCommandsRead     Permission = "commands:read"
	PermCommandsWrite    Permission = "commands:write"
	PermAutomodRead      Permission = "automod:read"
	PermAutomodWrite     Permission = "automod:write"
	PermSettingsRead     Permission = "settings:read"
	PermSettingsWrite    Permission = "settings:write"
	PermUsersRead        Permission = "users:read"
	PermUsersWrite       Permission = "users:write"
	PermIntegrationsRead Permission = "integrations:read"
	PermIntegrationsWrt  Permission = "integrations:write"
	PermAPIKeysRead      Permission = "api_keys:read"
	PermAPIKeysWrite     Permission = "api_keys:write"
	PermBillingRead      Permission = "billing:read"
	PermBillingWrite     Permission = "billing:write"
)

// AllPermissions returns the canonical list of every Permission that the
// auth package knows about. Useful for UIs that need to render a full
// scope-picker for API keys.
func AllPermissions() []Permission {
	return []Permission{
		PermCommandsRead, PermCommandsWrite,
		PermAutomodRead, PermAutomodWrite,
		PermSettingsRead, PermSettingsWrite,
		PermUsersRead, PermUsersWrite,
		PermIntegrationsRead, PermIntegrationsWrt,
		PermAPIKeysRead, PermAPIKeysWrite,
		PermBillingRead, PermBillingWrite,
	}
}

// readOnlyPermissions returns every "<x>:read" permission. Used to
// derive the Viewer role.
func readOnlyPermissions() []Permission {
	all := AllPermissions()
	out := make([]Permission, 0, len(all))
	for _, p := range all {
		if isReadPermission(p) {
			out = append(out, p)
		}
	}
	return out
}

func isReadPermission(p Permission) bool {
	s := string(p)
	if len(s) < 5 {
		return false
	}
	return s[len(s)-5:] == ":read"
}

// Permissions returns the set of permissions granted to r. The returned
// slice is a freshly-allocated copy that the caller may mutate.
//
// Role mapping:
//
//   - Owner:  every permission
//   - Admin:  every permission EXCEPT billing:* and users:write
//   - Mod:    commands:*, automod:*, settings:read
//   - Viewer: every "<x>:read" permission
func (r Role) Permissions() []Permission {
	switch r {
	case RoleOwner:
		return slices.Clone(AllPermissions())

	case RoleAdmin:
		all := AllPermissions()
		out := make([]Permission, 0, len(all))
		for _, p := range all {
			switch p {
			case PermBillingRead, PermBillingWrite, PermUsersWrite:
				continue
			}
			out = append(out, p)
		}
		return out

	case RoleMod:
		return []Permission{
			PermCommandsRead, PermCommandsWrite,
			PermAutomodRead, PermAutomodWrite,
			PermSettingsRead,
		}

	case RoleViewer:
		return readOnlyPermissions()
	}
	return nil
}

// Can reports whether the role grants permission p.
func (r Role) Can(p Permission) bool {
	for _, have := range r.Permissions() {
		if have == p {
			return true
		}
	}
	return false
}
