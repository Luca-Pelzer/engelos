package discord

import (
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

const platformName = "discord"

// roleHasManageMessages reports whether the given role grants the
// PermissionManageMessages bit. The caller is responsible for filtering down
// to roles the member actually holds.
func roleHasManageMessages(role *discordgo.Role) bool {
	if role == nil {
		return false
	}
	return role.Permissions&discordgo.PermissionManageMessages != 0
}

// memberIsModerator returns true when any role the member holds carries
// PermissionManageMessages. roleByID resolves a guild's role id to its
// definition; it may return nil for unknown ids.
func memberIsModerator(member *discordgo.Member, roleByID func(string) *discordgo.Role) bool {
	if member == nil || roleByID == nil {
		return false
	}
	for _, id := range member.Roles {
		if roleHasManageMessages(roleByID(id)) {
			return true
		}
	}
	return false
}

// translateMessageCreate converts a discordgo MessageCreate into the
// platform-neutral adapters.Event carrying a [adapters.DiscordMessageEvent]. It
// is a pure function: it makes no network calls. channelName is the resolved
// guild-channel name (empty when unknown or a DM); roleByID resolves guild roles
// so IsModerator reflects a member with Manage-Messages permission.
func translateMessageCreate(m *discordgo.MessageCreate, channelName string, roleByID func(string) *discordgo.Role) adapters.Event {
	if m == nil || m.Message == nil {
		return adapters.Event{}
	}
	var userID, username string
	if m.Author != nil {
		userID = m.Author.ID
		username = m.Author.Username
	}
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventDiscordMessage,
		Platform:   platformName,
		Channel:    m.ChannelID,
		OccurredAt: time.Now().UTC(),
		Discord: &adapters.DiscordMessageEvent{
			GuildID:     m.GuildID,
			ChannelID:   m.ChannelID,
			ChannelName: channelName,
			MessageID:   m.ID,
			Username:    username,
			UserID:      userID,
			Text:        m.Content,
			IsDM:        m.GuildID == "",
			IsModerator: memberIsModerator(m.Member, roleByID),
		},
	}
}

// translateMessageDelete converts a discordgo MessageDelete into the
// platform-neutral adapters.Event.
func translateMessageDelete(m *discordgo.MessageDelete) adapters.Event {
	if m == nil || m.Message == nil {
		return adapters.Event{}
	}
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageDeleted,
		Platform:   platformName,
		Channel:    m.ChannelID,
		OccurredAt: time.Now().UTC(),
		Message: &adapters.MessageEvent{
			ID: m.ID,
		},
	}
}

// connectionEvent constructs a platform.connected/disconnected/reconnecting
// event with the given reason and error message.
func connectionEvent(t adapters.EventType, reason, errMsg string) adapters.Event {
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       t,
		Platform:   platformName,
		OccurredAt: time.Now().UTC(),
		Connection: &adapters.ConnectionEvent{Reason: reason, Error: errMsg},
	}
}
