// Package integrations is the manifest + registry through which every
// integration (ElevenLabs, OBS, Ko-fi, a future AI avatar, ...) declares itself
// once and thereby gets a dashboard card, encrypted credential storage, and its
// workflow nodes auto-registered into the shared actions catalog. Registering an
// integration requires no changes to internal/actions or the catalog handler.
package integrations

import (
	"context"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

// AuthKind is how an integration authenticates.
type AuthKind string

const (
	AuthAPIKey AuthKind = "apikey"
	AuthOAuth  AuthKind = "oauth"
	AuthNone   AuthKind = "none"
)

// Manifest is the static, dashboard-facing description an integration exposes.
// ID is the stable slug used as the registry key and in credential storage.
type Manifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Icon        string   `json:"icon,omitempty"`
	AuthKind    AuthKind `json:"auth_kind"`
	SetupHref   string   `json:"setup_href,omitempty"`
}

// CredentialField describes one credential an apikey-kind integration collects,
// for the dashboard to render its setup form.
type CredentialField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
}

// CredentialSpec is the set of credential fields an integration collects.
type CredentialSpec struct {
	Fields []CredentialField `json:"fields"`
}

// Integration is a self-describing supplier of workflow nodes. Manifest returns
// its static metadata; RegisterNodes contributes its condition/action plugins to
// the shared actions catalog, so the catalog needs no per-integration code.
type Integration interface {
	Manifest() Manifest
	RegisterNodes(reg *actions.Registry) error
}

// CredentialProvider is optionally implemented by an integration that collects
// credentials, describing the fields its setup form should render.
type CredentialProvider interface {
	CredentialSpec() CredentialSpec
}

// ConnectionProber is optionally implemented by an integration that reports its
// own connection state from its authoritative config source (e.g. the TTS store
// or the AI config manager) rather than from the generic credential store. The
// integrations handler consults it before falling back to stored-credential
// presence.
type ConnectionProber interface {
	Connected(ctx context.Context, tenantID string) bool
}
