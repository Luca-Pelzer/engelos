package migrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNightbot_HappyPath(t *testing.T) {
	data := []byte(`[
		{"name":"!so","message":"Shoutout to $(1)","coolDown":30,"userLevel":"moderator"},
		{"name":"discord","message":"Join: discord.gg/x","coolDown":0,"userLevel":"everyone"}
	]`)
	res, err := ParseNightbot(data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 2)
	assert.Equal(t, "so", res.Commands[0].Name)
	assert.Equal(t, "Shoutout to $(1)", res.Commands[0].Response)
	assert.Equal(t, 30, res.Commands[0].Cooldown)
	assert.Equal(t, RoleModerator, res.Commands[0].MinRole)
	assert.Equal(t, "discord", res.Commands[1].Name)
	assert.Equal(t, defaultCooldown, res.Commands[1].Cooldown)
	assert.Equal(t, RoleEveryone, res.Commands[1].MinRole)
}

func TestParseNightbot_RoleMapping(t *testing.T) {
	cases := map[string]string{
		"owner": RoleBroadcaster, "broadcaster": RoleBroadcaster,
		"moderator": RoleModerator, "subscriber": RoleSubscriber,
		"regular": RoleSubscriber, "everyone": RoleEveryone, "weird": RoleEveryone,
	}
	for level, want := range cases {
		assert.Equal(t, want, nightbotRole(level), "level %q", level)
	}
}

func TestParseNightbot_WrappedObject(t *testing.T) {
	data := []byte(`{"commands":[{"name":"hi","message":"hello","userLevel":"everyone"}]}`)
	res, err := ParseNightbot(data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
	assert.Equal(t, "hi", res.Commands[0].Name)
}

func TestParseNightbot_SkipMissingFields(t *testing.T) {
	data := []byte(`[{"name":"","message":"x"},{"name":"ok","message":""},{"name":"good","message":"yes"}]`)
	res, err := ParseNightbot(data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
	assert.Equal(t, "good", res.Commands[0].Name)
	assert.Len(t, res.Skipped, 2)
}

func TestParseNightbot_DuplicateSkipped(t *testing.T) {
	data := []byte(`[{"name":"!dup","message":"a"},{"name":"dup","message":"b"}]`)
	res, err := ParseNightbot(data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
	assert.Equal(t, "a", res.Commands[0].Response)
	require.Len(t, res.Skipped, 1)
	assert.Contains(t, res.Skipped[0], "duplicate")
}

func TestParseStreamElements_HappyPath_NumberCooldown(t *testing.T) {
	data := []byte(`[
		{"command":"so","reply":"Shoutout","cooldown":15,"accessLevel":500},
		{"command":"!hype","reply":"HYPE","cooldown":0,"accessLevel":100}
	]`)
	res, err := ParseStreamElements(data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 2)
	assert.Equal(t, "so", res.Commands[0].Name)
	assert.Equal(t, 15, res.Commands[0].Cooldown)
	assert.Equal(t, RoleModerator, res.Commands[0].MinRole)
	assert.Equal(t, "hype", res.Commands[1].Name)
	assert.Equal(t, defaultCooldown, res.Commands[1].Cooldown)
	assert.Equal(t, RoleEveryone, res.Commands[1].MinRole)
}

func TestParseStreamElements_CooldownObject(t *testing.T) {
	data := []byte(`[{"command":"c","reply":"r","cooldown":{"user":5,"global":20},"accessLevel":1000}]`)
	res, err := ParseStreamElements(data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
	assert.Equal(t, 20, res.Commands[0].Cooldown)
	assert.Equal(t, RoleBroadcaster, res.Commands[0].MinRole)
}

func TestStreamElementsRole(t *testing.T) {
	assert.Equal(t, RoleBroadcaster, streamElementsRole(1000))
	assert.Equal(t, RoleModerator, streamElementsRole(500))
	assert.Equal(t, RoleSubscriber, streamElementsRole(250))
	assert.Equal(t, RoleEveryone, streamElementsRole(100))
	assert.Equal(t, RoleEveryone, streamElementsRole(0))
}

func TestParse_AutoDetectNightbot(t *testing.T) {
	data := []byte(`[{"name":"hi","message":"hello","userLevel":"everyone"}]`)
	res, err := Parse("", data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
}

func TestParse_AutoDetectStreamElements(t *testing.T) {
	data := []byte(`[{"command":"hi","reply":"hello","accessLevel":100}]`)
	res, err := Parse("", data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
}

func TestParse_AmbiguousSource(t *testing.T) {
	data := []byte(`[{"foo":"bar"}]`)
	_, err := Parse("", data)
	assert.ErrorIs(t, err, ErrAmbiguousSource)
}

func TestParse_EmptyInput(t *testing.T) {
	_, err := Parse("", []byte("   "))
	assert.ErrorIs(t, err, ErrEmptyInput)
}

func TestParse_InvalidJSON(t *testing.T) {
	_, err := Parse(SourceNightbot, []byte(`{not json`))
	assert.Error(t, err)
}

func TestParse_ExplicitSource(t *testing.T) {
	data := []byte(`[{"command":"hi","reply":"hello","accessLevel":250}]`)
	res, err := Parse(SourceStreamElements, data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
	assert.Equal(t, RoleSubscriber, res.Commands[0].MinRole)
}
