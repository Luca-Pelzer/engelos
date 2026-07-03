// SPDX-License-Identifier: Apache-2.0

package sdk

// AuthKind is how an integration authenticates.
type AuthKind string

const (
	// AuthAPIKey collects one or more secret credential fields.
	AuthAPIKey AuthKind = "apikey"
	// AuthOAuth authenticates through an OAuth flow the daemon owns.
	AuthOAuth AuthKind = "oauth"
	// AuthNone needs no credentials.
	AuthNone AuthKind = "none"
)

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

// IntegrationManifest is the static, dashboard-facing description an integration
// exposes. ID is the stable slug used as the registry key and in credential
// storage.
type IntegrationManifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Icon        string   `json:"icon,omitempty"`
	AuthKind    AuthKind `json:"auth_kind"`
	SetupHref   string   `json:"setup_href,omitempty"`
}

// Nodes are the workflow nodes an integration contributes to the shared catalog.
type Nodes struct {
	Actions    []Action
	Conditions []Condition
}

// Integration is a self-describing supplier of workflow nodes plus dashboard
// metadata. Manifest returns its static description; Nodes returns the
// action/condition nodes it contributes to the catalog.
type Integration interface {
	Manifest() IntegrationManifest
	Nodes() Nodes
}

// CredentialProvider is optionally implemented by an integration that collects
// credentials, describing the fields its setup form should render.
type CredentialProvider interface {
	CredentialSpec() CredentialSpec
}
