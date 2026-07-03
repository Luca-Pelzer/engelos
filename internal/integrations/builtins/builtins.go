// Package builtins holds the first-party integrations that ship with engelos:
// ElevenLabs (TTS), OBS and the AI backend. Each is a thin adapter that declares
// a manifest, contributes its existing workflow nodes via the actions package's
// Register*Nodes helpers, and reports "connected" from its authoritative config
// source (the TTS store, OBS wiring, or the AI config manager) through a probe
// supplied by the host - so no credential storage is duplicated.
package builtins

import (
	"context"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	"github.com/Luca-Pelzer/engelos/internal/integrations"
)

// Probe reports whether an integration is connected for a tenant, reading its
// own config source. A nil probe reports not connected.
type Probe func(ctx context.Context, tenantID string) bool

func (p Probe) connected(ctx context.Context, tenantID string) bool {
	return p != nil && p(ctx, tenantID)
}

// ElevenLabs is the ElevenLabs (text-to-speech) integration. It contributes the
// tts:speak node and reports connected from the TTS key store via connected.
func ElevenLabs(speaker actions.TTSSpeaker, connected Probe) integrations.Integration {
	return elevenLabs{speaker: speaker, probe: connected}
}

type elevenLabs struct {
	speaker actions.TTSSpeaker
	probe   Probe
}

func (elevenLabs) Manifest() integrations.Manifest {
	return integrations.Manifest{
		ID:          "elevenlabs",
		Name:        "ElevenLabs",
		Description: "ElevenLabs text-to-speech: the tts:speak action reads text aloud on the channel's TTS overlay.",
		AuthKind:    integrations.AuthAPIKey,
		SetupHref:   "/tts",
	}
}

func (e elevenLabs) RegisterNodes(reg *actions.Registry) error {
	return actions.RegisterTTSNodes(reg, e.speaker)
}

func (elevenLabs) CredentialSpec() integrations.CredentialSpec {
	return integrations.CredentialSpec{Fields: []integrations.CredentialField{
		{Key: "api_key", Label: "API key", Hint: "Your ElevenLabs API key"},
	}}
}

func (e elevenLabs) Connected(ctx context.Context, tenantID string) bool {
	return e.probe.connected(ctx, tenantID)
}

// OBS is the OBS Studio integration. It contributes the obs:switch-scene and
// obs:set-source-visibility nodes; it is configured via the host environment
// (no dashboard credentials), so its AuthKind is none and connected reports
// whether OBS control is wired.
func OBS(controller actions.OBSController, connected Probe) integrations.Integration {
	return obsIntegration{controller: controller, probe: connected}
}

type obsIntegration struct {
	controller actions.OBSController
	probe      Probe
}

func (obsIntegration) Manifest() integrations.Manifest {
	return integrations.Manifest{
		ID:          "obs",
		Name:        "OBS",
		Description: "OBS Studio scene and source control via obs:switch-scene and obs:set-source-visibility.",
		AuthKind:    integrations.AuthNone,
	}
}

func (o obsIntegration) RegisterNodes(reg *actions.Registry) error {
	return actions.RegisterOBSNodes(reg, o.controller)
}

func (o obsIntegration) Connected(ctx context.Context, tenantID string) bool {
	return o.probe.connected(ctx, tenantID)
}

// Discord is the Discord bot integration. It contributes the discord:reply node
// and reports connected from the live gateway session via connected. The bot
// token is host-configured (ENGELOS_DISCORD_TOKEN), so its AuthKind is none.
func Discord(poster actions.DiscordPoster, connected Probe) integrations.Integration {
	return discordIntegration{poster: poster, probe: connected}
}

type discordIntegration struct {
	poster actions.DiscordPoster
	probe  Probe
}

func (discordIntegration) Manifest() integrations.Manifest {
	return integrations.Manifest{
		ID:          "discord",
		Name:        "Discord",
		Description: "Discord bot: reads guild/DM messages as the discord.message event plus command triggers, and the discord:reply action answers in-channel.",
		AuthKind:    integrations.AuthNone,
		SetupHref:   "/integrations",
	}
}

func (d discordIntegration) RegisterNodes(reg *actions.Registry) error {
	return actions.RegisterDiscordReplyNode(reg, d.poster)
}

func (d discordIntegration) Connected(ctx context.Context, tenantID string) bool {
	return d.probe.connected(ctx, tenantID)
}

// AIBackend is the AI backend integration. It contributes the ai:generate and
// ai:classify nodes and reports connected from the AI config manager state.
func AIBackend(completer actions.AICompleter, connected Probe) integrations.Integration {
	return aiBackend{completer: completer, probe: connected}
}

type aiBackend struct {
	completer actions.AICompleter
	probe     Probe
}

func (aiBackend) Manifest() integrations.Manifest {
	return integrations.Manifest{
		ID:          "ai-backend",
		Name:        "AI backend",
		Description: "The configured AI provider that powers the ai:generate and ai:classify actions.",
		AuthKind:    integrations.AuthAPIKey,
		SetupHref:   "/ai-mod/settings",
	}
}

func (a aiBackend) RegisterNodes(reg *actions.Registry) error {
	return actions.RegisterAINodes(reg, a.completer)
}

func (aiBackend) CredentialSpec() integrations.CredentialSpec {
	return integrations.CredentialSpec{Fields: []integrations.CredentialField{
		{Key: "api_key", Label: "API key", Hint: "Provider API key (managed under AI settings)"},
	}}
}

func (a aiBackend) Connected(ctx context.Context, tenantID string) bool {
	return a.probe.connected(ctx, tenantID)
}

// Kofi is the Ko-fi donations integration. It contributes no workflow action
// nodes - donations arrive as "donation" trigger events via the Ko-fi webhook -
// and declares the verification_token credential. It implements no
// ConnectionProber, so the handler reports connected from the generic
// credential store (a stored verification_token), the framework default path.
func Kofi() integrations.Integration {
	return kofi{}
}

type kofi struct{}

func (kofi) Manifest() integrations.Manifest {
	return integrations.Manifest{
		ID:          "kofi",
		Name:        "Ko-fi",
		Description: "Ko-fi donations fire the 'donation' event so rules can react when a supporter tips.",
		AuthKind:    integrations.AuthAPIKey,
		SetupHref:   "/integrations",
	}
}

func (kofi) RegisterNodes(*actions.Registry) error { return nil }

func (kofi) CredentialSpec() integrations.CredentialSpec {
	return integrations.CredentialSpec{Fields: []integrations.CredentialField{
		{Key: "verification_token", Label: "Verification token", Hint: "From your Ko-fi webhook settings"},
	}}
}
