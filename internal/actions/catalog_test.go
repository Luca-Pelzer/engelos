package actions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActionCatalogListsEveryNode asserts registry.Actions() enumerates every
// first-party action node id, so the dashboard palette and the API catalog stay
// in sync with what RegisterBuiltins installs. A new node is not "done" until it
// appears here.
func TestActionCatalogListsEveryNode(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))

	got := make(map[string]bool)
	for _, def := range reg.Actions() {
		got[def.ID] = true
	}

	want := []string{
		"builtin:send-chat", "builtin:delay", "builtin:log",
		"http:request",
		"discord:post",
		"twitch:delete-message", "twitch:timeout", "twitch:ban",
		"twitch:create-clip", "twitch:create-marker", "twitch:create-poll",
		"twitch:set-title", "twitch:set-category",
		"twitch:redemption",
		"flow:stop-if",
		"transform:template",
		"kb:lookup",
	}
	for _, id := range want {
		assert.True(t, got[id], "action catalog missing %q", id)
	}

	// The OBS, AI, TTS and discord:reply nodes are contributed only by their
	// integrations, so RegisterBuiltins alone must not expose them.
	for _, id := range []string{
		"obs:switch-scene", "obs:set-source-visibility",
		"ai:generate", "ai:classify", "tts:speak",
		"discord:reply",
	} {
		assert.False(t, got[id], "integration node %q must not be a builtin", id)
	}
}

// TestConditionCatalogListsEveryCondition asserts registry.Conditions()
// enumerates every first-party condition id.
func TestConditionCatalogListsEveryCondition(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))

	got := make(map[string]bool)
	for _, def := range reg.Conditions() {
		got[def.ID] = true
	}

	want := []string{
		"builtin:message-contains", "builtin:user-role",
		"cond:regex", "cond:time-window", "cond:stream-state", "cond:output",
		"cond:cooldown",
	}
	for _, id := range want {
		assert.True(t, got[id], "condition catalog missing %q", id)
	}
}
