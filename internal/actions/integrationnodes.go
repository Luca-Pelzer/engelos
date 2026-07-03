package actions

// This file exposes registration of the action nodes that belong to the
// ElevenLabs, OBS and AI-backend integrations. Their registration was moved out
// of RegisterBuiltins (Phase 3.3) so each node appears in the catalog only when
// its owning integration is registered and calls the matching helper from its
// RegisterNodes. The node types and their Definitions are unchanged, so the
// catalog output stays byte-identical to the pre-refactor builtin registration.

// RegisterTTSNodes registers the tts:speak action bound to speaker.
func RegisterTTSNodes(reg *Registry, speaker TTSSpeaker) error {
	return reg.RegisterAction(ttsSpeakAction{tts: speaker})
}

// RegisterOBSNodes registers the obs:switch-scene and obs:set-source-visibility
// actions bound to controller.
func RegisterOBSNodes(reg *Registry, controller OBSController) error {
	for _, a := range []ActionType{
		switchSceneAction{obs: controller},
		toggleSourceAction{obs: controller},
	} {
		if err := reg.RegisterAction(a); err != nil {
			return err
		}
	}
	return nil
}

// RegisterAINodes registers the ai:generate and ai:classify actions bound to ai.
func RegisterAINodes(reg *Registry, ai AICompleter) error {
	for _, a := range []ActionType{
		aiGenerateAction{ai: ai},
		aiClassifyAction{ai: ai},
	} {
		if err := reg.RegisterAction(a); err != nil {
			return err
		}
	}
	return nil
}
