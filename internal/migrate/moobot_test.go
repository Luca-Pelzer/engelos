package migrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMoobot_HappyPath(t *testing.T) {
	data := []byte(`{"commands":[
		{"name":"!so","response":"Shoutout to $(1)","cooldown":30,"userLevel":"moderator"},
		{"name":"discord","response":"Join: discord.gg/x","userLevel":"everyone"}
	]}`)
	res, err := ParseMoobot(data)
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

func TestParseMoobot_TopLevelArray(t *testing.T) {
	data := []byte(`[{"command":"hi","message":"hello"}]`)
	res, err := ParseMoobot(data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
	assert.Equal(t, "hi", res.Commands[0].Name)
	assert.Equal(t, "hello", res.Commands[0].Response)
}

func TestParseMoobot_AlternateFieldNames(t *testing.T) {
	data := []byte(`[
		{"commandName":"a","text":"ra","cooldownSeconds":12,"access":"vip"},
		{"trigger":"b","reply":"rb","coolDown":"7","tier":"subscriber"}
	]`)
	res, err := ParseMoobot(data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 2)
	assert.Equal(t, "a", res.Commands[0].Name)
	assert.Equal(t, "ra", res.Commands[0].Response)
	assert.Equal(t, 12, res.Commands[0].Cooldown)
	assert.Equal(t, RoleSubscriber, res.Commands[0].MinRole)
	assert.Equal(t, "b", res.Commands[1].Name)
	assert.Equal(t, 7, res.Commands[1].Cooldown)
	assert.Equal(t, RoleSubscriber, res.Commands[1].MinRole)
}

func TestMoobotRole(t *testing.T) {
	cases := map[string]string{
		"owner":      RoleBroadcaster,
		"streamer":   RoleBroadcaster,
		"moderator":  RoleModerator,
		"mod":        RoleModerator,
		"subscriber": RoleSubscriber,
		"vip":        RoleSubscriber,
		"regular":    RoleSubscriber,
		"everyone":   RoleEveryone,
		"":           RoleEveryone,
		"weird":      RoleEveryone,
	}
	for level, want := range cases {
		assert.Equal(t, want, moobotRole([]byte(`"`+level+`"`)), "level %q", level)
	}
}

func TestMoobotRole_NumericTierIsEveryone(t *testing.T) {
	assert.Equal(t, RoleEveryone, moobotRole([]byte(`2`)))
	assert.Equal(t, RoleEveryone, moobotRole(nil))
}

func TestParseMoobot_Timers(t *testing.T) {
	data := []byte(`{"timers":[
		{"description":"social","message":"Follow me","minutesBetweenPosts":15,"chatLinesBetweenPosts":8},
		{"name":"off","message":"x","minutes":5,"enabled":false}
	]}`)
	res, err := ParseMoobot(data)
	require.NoError(t, err)
	require.Len(t, res.Timers, 2)
	assert.Equal(t, "social", res.Timers[0].Name)
	assert.Equal(t, "Follow me", res.Timers[0].Response)
	assert.Equal(t, 15*60, res.Timers[0].Interval)
	assert.Equal(t, 8, res.Timers[0].MinLines)
	assert.True(t, res.Timers[0].Enabled)
	assert.False(t, res.Timers[1].Enabled)
}

func TestParseMoobot_TimerDisabledFlag(t *testing.T) {
	data := []byte(`{"timers":[{"name":"t","message":"m","interval":10,"disabled":true}]}`)
	res, err := ParseMoobot(data)
	require.NoError(t, err)
	require.Len(t, res.Timers, 1)
	assert.False(t, res.Timers[0].Enabled)
}

func TestParseMoobot_CommandListTimerSkipped(t *testing.T) {
	data := []byte(`{"timers":[{"description":"rot","commands":["a","b"],"minutesBetweenPosts":20}]}`)
	res, err := ParseMoobot(data)
	require.Error(t, err)
	assert.Empty(t, res.Commands)
	assert.Empty(t, res.Timers)
}

func TestParseMoobot_SkipMissingFields(t *testing.T) {
	data := []byte(`[{"name":"","response":"x"},{"name":"ok","response":""},{"name":"good","response":"yes"}]`)
	res, err := ParseMoobot(data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
	assert.Equal(t, "good", res.Commands[0].Name)
	assert.Len(t, res.Skipped, 2)
}

func TestParseMoobot_DuplicateSkipped(t *testing.T) {
	data := []byte(`[{"name":"!dup","response":"a"},{"name":"dup","response":"b"}]`)
	res, err := ParseMoobot(data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
	assert.Equal(t, "a", res.Commands[0].Response)
	require.Len(t, res.Skipped, 1)
	assert.Contains(t, res.Skipped[0], "duplicate")
}

func TestParseMoobot_EmptyInput(t *testing.T) {
	_, err := ParseMoobot([]byte("   "))
	assert.ErrorIs(t, err, ErrEmptyInput)
}

func TestParseMoobot_NothingMappableFailsLoudly(t *testing.T) {
	data := []byte(`[{"foo":"bar"}]`)
	_, err := ParseMoobot(data)
	assert.Error(t, err)
}

func TestParse_ExplicitMoobot(t *testing.T) {
	data := []byte(`[{"name":"hi","response":"hello","userLevel":"moderator"}]`)
	res, err := Parse(SourceMoobot, data)
	require.NoError(t, err)
	require.Len(t, res.Commands, 1)
	assert.Equal(t, RoleModerator, res.Commands[0].MinRole)
}

func TestParse_MoobotNotAutoDetected(t *testing.T) {
	data := []byte(`[{"name":"hi","response":"hello"}]`)
	_, err := Parse("", data)
	assert.ErrorIs(t, err, ErrAmbiguousSource)
}
