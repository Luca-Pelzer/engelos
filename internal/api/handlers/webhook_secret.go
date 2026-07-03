package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

// webhookTriggerConfig is the persisted trigger config for a webhook rule: the
// HMAC secret the inbound webhook endpoint verifies request signatures against.
// It lives inside the rule's opaque TriggerFilter so no schema change is needed.
type webhookTriggerConfig struct {
	Secret string `json:"secret"`
}

// webhookSecretFrom extracts the HMAC secret from a webhook rule's trigger
// filter, returning "" when absent or unparseable.
func webhookSecretFrom(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var c webhookTriggerConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return ""
	}
	return c.Secret
}

// ensureWebhookSecret guarantees a webhook rule carries a secret: it keeps an
// explicitly supplied one, else falls back to prior (the existing rule's secret,
// which a masked GET never round-trips), else mints a fresh random secret.
// Non-webhook rules pass through untouched.
func ensureWebhookSecret(rule actions.Rule, prior string) (actions.Rule, error) {
	if rule.TriggerKind != actions.TriggerWebhook {
		return rule, nil
	}
	secret := webhookSecretFrom(rule.TriggerFilter)
	if secret == "" {
		secret = prior
	}
	if secret == "" {
		var err error
		secret, err = newWebhookSecret()
		if err != nil {
			return rule, err
		}
	}
	raw, err := json.Marshal(webhookTriggerConfig{Secret: secret})
	if err != nil {
		return rule, err
	}
	rule.TriggerFilter = raw
	return rule, nil
}

// newWebhookSecret mints a 256-bit random secret rendered as hex.
func newWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// maskTriggerFilter hides a webhook rule's secret in an API response, replacing
// it with a set flag and a short non-reversible hint (mirroring the AI config's
// api_key_hint). Non-webhook filters pass through unchanged.
func maskTriggerFilter(kind actions.TriggerKind, raw json.RawMessage) json.RawMessage {
	if kind != actions.TriggerWebhook {
		return rawOrNull(raw)
	}
	secret := webhookSecretFrom(raw)
	masked, err := json.Marshal(map[string]any{
		"secret_set":  secret != "",
		"secret_hint": hintSecret(secret),
	})
	if err != nil {
		return json.RawMessage("null")
	}
	return masked
}

// hintSecret renders a short, non-reversible hint of a secret: the first three
// and last three runes, or "…" for a value short enough that a hint would leak
// it. An empty secret hints nothing.
func hintSecret(secret string) string {
	r := []rune(secret)
	if len(r) <= 6 {
		if len(r) == 0 {
			return ""
		}
		return "…"
	}
	return string(r[:3]) + "…" + string(r[len(r)-3:])
}
