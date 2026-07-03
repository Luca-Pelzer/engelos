package builtins

import (
	"context"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	"github.com/Luca-Pelzer/engelos/internal/integrations"
)

// Avatar is the stream-avatar integration. It contributes the avatar:speak and
// avatar:expression nodes, which stream TTS audio and pose directives to the
// OBS avatar overlay via the avatar hub. It is wired from the host environment
// (no dashboard credentials), so its AuthKind is none; connected reports
// whether at least one overlay is currently subscribed.
func Avatar(hub actions.AvatarHub, synth actions.AvatarSynth, connected Probe) integrations.Integration {
	return avatarIntegration{hub: hub, synth: synth, probe: connected}
}

type avatarIntegration struct {
	hub   actions.AvatarHub
	synth actions.AvatarSynth
	probe Probe
}

func (avatarIntegration) Manifest() integrations.Manifest {
	return integrations.Manifest{
		ID:          "avatar",
		Name:        "Avatar",
		Description: "Streams TTS audio and expression directives to the OBS avatar overlay via the avatar:speak and avatar:expression actions.",
		AuthKind:    integrations.AuthNone,
		SetupHref:   "/integrations",
	}
}

func (a avatarIntegration) RegisterNodes(reg *actions.Registry) error {
	return actions.RegisterAvatarNodes(reg, a.hub, a.synth)
}

func (a avatarIntegration) Connected(ctx context.Context, tenantID string) bool {
	return a.probe.connected(ctx, tenantID)
}
