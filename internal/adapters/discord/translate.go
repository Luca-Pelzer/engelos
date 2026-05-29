package discord

import (
	"regexp"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

const platformName = "discord"

var customEmoteRE = regexp.MustCompile(`<a?:([A-Za-z0-9_]+):(\d+)>`)

// parseEmotes extracts the numeric ids of custom Discord emotes from a
// message body. The shape `<:name:id>` and `<a:name:id>` are both
// recognized; the id (which is what downstream systems care about) is
// returned. Unicode emoji and plain text are ignored.
func parseEmotes(content string) []string {
	matches := customEmoteRE.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[2])
	}
	return out
}

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
// platform-neutral adapters.Event. It is a pure function: it makes no
// network calls and does not look at the Adapter's state directly.
func translateMessageCreate(m *discordgo.MessageCreate, roleByID func(string) *discordgo.Role) adapters.Event {
	if m == nil || m.Message == nil {
		return adapters.Event{}
	}
	var (
		userID, username, replyTo string
	)
	if m.Author != nil {
		userID = m.Author.ID
		username = m.Author.Username
	}
	if m.MessageReference != nil {
		replyTo = m.MessageReference.MessageID
	}
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageCreated,
		Platform:   platformName,
		Channel:    m.ChannelID,
		OccurredAt: time.Now().UTC(),
		Message: &adapters.MessageEvent{
			ID:           m.ID,
			UserID:       userID,
			Username:     username,
			Content:      m.Content,
			IsModerator:  memberIsModerator(m.Member, roleByID),
			IsSubscriber: false,
			EmotesUsed:   parseEmotes(m.Content),
			ReplyTo:      replyTo,
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
