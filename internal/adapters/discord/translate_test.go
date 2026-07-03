package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

func TestTranslateMessageCreate_FullMessage(t *testing.T) {
	t.Parallel()

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "msg-1",
			ChannelID: "chan-1",
			GuildID:   "guild-1",
			Content:   "hello",
			Author: &discordgo.User{
				ID:       "user-1",
				Username: "alice",
			},
		},
	}

	ev := translateMessageCreate(m, "general", nil)

	assert.Equal(t, adapters.EventDiscordMessage, ev.Type)
	assert.Equal(t, platformName, ev.Platform)
	assert.Equal(t, "chan-1", ev.Channel)
	require.NotNil(t, ev.Discord)
	assert.Equal(t, "msg-1", ev.Discord.MessageID)
	assert.Equal(t, "user-1", ev.Discord.UserID)
	assert.Equal(t, "alice", ev.Discord.Username)
	assert.Equal(t, "hello", ev.Discord.Text)
	assert.Equal(t, "guild-1", ev.Discord.GuildID)
	assert.Equal(t, "chan-1", ev.Discord.ChannelID)
	assert.Equal(t, "general", ev.Discord.ChannelName)
	assert.False(t, ev.Discord.IsDM)
	assert.False(t, ev.Discord.IsModerator)
	assert.NotEmpty(t, ev.ID, "event id should be populated")
	assert.False(t, ev.OccurredAt.IsZero())
}

func TestTranslateMessageCreate_ModeratorRole(t *testing.T) {
	t.Parallel()

	modRole := &discordgo.Role{ID: "role-mod", Permissions: discordgo.PermissionManageMessages}
	plebRole := &discordgo.Role{ID: "role-pleb", Permissions: 0}
	roleByID := func(id string) *discordgo.Role {
		switch id {
		case "role-mod":
			return modRole
		case "role-pleb":
			return plebRole
		}
		return nil
	}

	t.Run("member has mod role", func(t *testing.T) {
		t.Parallel()
		m := &discordgo.MessageCreate{
			Message: &discordgo.Message{
				ID: "msg-4", ChannelID: "chan-1", GuildID: "guild-1",
				Author: &discordgo.User{ID: "u", Username: "n"},
				Member: &discordgo.Member{Roles: []string{"role-mod", "role-pleb"}},
			},
		}
		ev := translateMessageCreate(m, "", roleByID)
		require.NotNil(t, ev.Discord)
		assert.True(t, ev.Discord.IsModerator)
	})

	t.Run("member only has non-mod roles", func(t *testing.T) {
		t.Parallel()
		m := &discordgo.MessageCreate{
			Message: &discordgo.Message{
				ID: "msg-5", ChannelID: "chan-1", GuildID: "guild-1",
				Author: &discordgo.User{ID: "u", Username: "n"},
				Member: &discordgo.Member{Roles: []string{"role-pleb"}},
			},
		}
		ev := translateMessageCreate(m, "", roleByID)
		require.NotNil(t, ev.Discord)
		assert.False(t, ev.Discord.IsModerator)
	})
}

func TestTranslateMessageCreate_DirectMessage(t *testing.T) {
	t.Parallel()

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID: "dm-1", ChannelID: "dm-chan", GuildID: "", Content: "DM",
			Author: &discordgo.User{ID: "u", Username: "n"},
		},
	}
	ev := translateMessageCreate(m, "", nil)
	require.NotNil(t, ev.Discord)
	assert.Equal(t, "dm-chan", ev.Channel)
	assert.True(t, ev.Discord.IsDM, "empty GuildID marks a direct message")
	assert.False(t, ev.Discord.IsModerator)
}

func TestTranslateMessageCreate_NilMessage(t *testing.T) {
	t.Parallel()
	assert.Equal(t, adapters.Event{}, translateMessageCreate(nil, "", nil))
	assert.Equal(t, adapters.Event{}, translateMessageCreate(&discordgo.MessageCreate{}, "", nil))
}

func TestTranslateMessageCreate_NilAuthor(t *testing.T) {
	t.Parallel()
	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{ID: "x", ChannelID: "c", Content: "no-author"},
	}
	ev := translateMessageCreate(m, "", nil)
	require.NotNil(t, ev.Discord)
	assert.Empty(t, ev.Discord.UserID)
	assert.Empty(t, ev.Discord.Username)
}

func TestTranslateMessageDelete(t *testing.T) {
	t.Parallel()
	m := &discordgo.MessageDelete{
		Message: &discordgo.Message{ID: "msg-del", ChannelID: "chan-1"},
	}
	ev := translateMessageDelete(m)
	assert.Equal(t, adapters.EventMessageDeleted, ev.Type)
	assert.Equal(t, platformName, ev.Platform)
	assert.Equal(t, "chan-1", ev.Channel)
	require.NotNil(t, ev.Message)
	assert.Equal(t, "msg-del", ev.Message.ID)
}

func TestTranslateMessageDelete_Nil(t *testing.T) {
	t.Parallel()
	assert.Equal(t, adapters.Event{}, translateMessageDelete(nil))
	assert.Equal(t, adapters.Event{}, translateMessageDelete(&discordgo.MessageDelete{}))
}

func TestConnectionEvent(t *testing.T) {
	t.Parallel()
	ev := connectionEvent(adapters.EventConnected, "ready", "")
	assert.Equal(t, adapters.EventConnected, ev.Type)
	assert.Equal(t, platformName, ev.Platform)
	require.NotNil(t, ev.Connection)
	assert.Equal(t, "ready", ev.Connection.Reason)
	assert.Empty(t, ev.Connection.Error)
	assert.NotEmpty(t, ev.ID)

	d := connectionEvent(adapters.EventDisconnected, "gone", "boom")
	assert.Equal(t, adapters.EventDisconnected, d.Type)
	require.NotNil(t, d.Connection)
	assert.Equal(t, "boom", d.Connection.Error)
}

func TestRoleHasManageMessages(t *testing.T) {
	t.Parallel()
	assert.False(t, roleHasManageMessages(nil))
	assert.False(t, roleHasManageMessages(&discordgo.Role{Permissions: 0}))
	assert.True(t, roleHasManageMessages(&discordgo.Role{Permissions: discordgo.PermissionManageMessages}))
	assert.True(t, roleHasManageMessages(&discordgo.Role{
		Permissions: discordgo.PermissionManageMessages | discordgo.PermissionAdministrator,
	}))
}

func TestMemberIsModerator_NilCases(t *testing.T) {
	t.Parallel()
	assert.False(t, memberIsModerator(nil, func(string) *discordgo.Role { return nil }))
	assert.False(t, memberIsModerator(&discordgo.Member{Roles: []string{"x"}}, nil))
}
