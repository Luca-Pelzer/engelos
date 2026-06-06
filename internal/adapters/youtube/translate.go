package youtube

import (
	"time"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

const platformName = "youtube"

// translateAction tells runPoll what to do with a translated message: emit
// the event, skip it silently, or treat it as the end of the chat.
type translateAction int

const (
	translateSkip translateAction = iota
	translateEmit
	translateChatEnded
)

// liveChatListResponse mirrors the subset of liveChatMessages.list the adapter
// consumes.
type liveChatListResponse struct {
	NextPageToken         string            `json:"nextPageToken"`
	PollingIntervalMillis int64             `json:"pollingIntervalMillis"`
	Items                 []liveChatMessage `json:"items"`
}

type liveChatMessage struct {
	ID      string                 `json:"id"`
	Snippet liveChatMessageSnippet `json:"snippet"`
	Author  liveChatAuthorDetails  `json:"authorDetails"`
}

type liveChatMessageSnippet struct {
	Type           string `json:"type"`
	DisplayMessage string `json:"displayMessage"`
	PublishedAt    string `json:"publishedAt"`

	TextMessageDetails struct {
		MessageText string `json:"messageText"`
	} `json:"textMessageDetails"`

	MessageDeletedDetails struct {
		DeletedMessageID string `json:"deletedMessageId"`
	} `json:"messageDeletedDetails"`

	UserBannedDetails struct {
		BannedUserDetails struct {
			ChannelID   string `json:"channelId"`
			DisplayName string `json:"displayName"`
		} `json:"bannedUserDetails"`
	} `json:"userBannedDetails"`
}

type liveChatAuthorDetails struct {
	ChannelID       string `json:"channelId"`
	DisplayName     string `json:"displayName"`
	IsChatOwner     bool   `json:"isChatOwner"`
	IsChatModerator bool   `json:"isChatModerator"`
	IsChatSponsor   bool   `json:"isChatSponsor"`
}

// videoListResponse mirrors the subset of videos.list used to resolve the
// active live chat id from a video id.
type videoListResponse struct {
	Items []struct {
		LiveStreamingDetails struct {
			ActiveLiveChatID string `json:"activeLiveChatId"`
		} `json:"liveStreamingDetails"`
	} `json:"items"`
}

// insertMessageRequest is the body for liveChatMessages.insert.
type insertMessageRequest struct {
	Snippet struct {
		LiveChatID         string `json:"liveChatId"`
		Type               string `json:"type"`
		TextMessageDetails struct {
			MessageText string `json:"messageText"`
		} `json:"textMessageDetails"`
	} `json:"snippet"`
}

// insertBanRequest is the body for liveChatBans.insert (permanent or
// temporary). BanDurationSeconds is omitted for permanent bans.
type insertBanRequest struct {
	Snippet struct {
		LiveChatID         string `json:"liveChatId"`
		Type               string `json:"type"`
		BanDurationSeconds uint64 `json:"banDurationSeconds,omitempty"`
		BannedUserDetails  struct {
			ChannelID string `json:"channelId"`
		} `json:"bannedUserDetails"`
	} `json:"snippet"`
}

// translateMessage converts a liveChatMessage into a normalized event. It is a
// pure function: no network calls and no adapter-state access. The returned
// translateAction tells the caller whether to emit, skip, or stop polling.
func translateMessage(m liveChatMessage, channel string) (adapters.Event, translateAction) {
	switch m.Snippet.Type {
	case "textMessageEvent":
		content := m.Snippet.DisplayMessage
		if content == "" {
			content = m.Snippet.TextMessageDetails.MessageText
		}
		evt := newEvent(adapters.EventMessageCreated, channel, m.Snippet.PublishedAt)
		evt.Message = &adapters.MessageEvent{
			ID:           m.ID,
			UserID:       m.Author.ChannelID,
			Username:     m.Author.DisplayName,
			Content:      content,
			IsModerator:  m.Author.IsChatModerator || m.Author.IsChatOwner,
			IsSubscriber: m.Author.IsChatSponsor,
			IsVIP:        false,
		}
		return evt, translateEmit

	case "newSponsorEvent":
		evt := newEvent(adapters.EventUserSubscribed, channel, m.Snippet.PublishedAt)
		evt.Subscription = &adapters.SubscriptionEvent{
			UserID:   m.Author.ChannelID,
			Username: m.Author.DisplayName,
			Tier:     "member",
		}
		return evt, translateEmit

	case "messageDeletedEvent":
		evt := newEvent(adapters.EventMessageDeleted, channel, m.Snippet.PublishedAt)
		evt.Message = &adapters.MessageEvent{
			ID: m.Snippet.MessageDeletedDetails.DeletedMessageID,
		}
		return evt, translateEmit

	case "userBannedEvent":
		target := m.Snippet.UserBannedDetails.BannedUserDetails.DisplayName
		if target == "" {
			target = m.Snippet.UserBannedDetails.BannedUserDetails.ChannelID
		}
		evt := newEvent(adapters.EventUserBanned, channel, m.Snippet.PublishedAt)
		evt.UserAction = &adapters.UserActionEvent{
			Action:     "ban",
			TargetUser: target,
		}
		return evt, translateEmit

	case "chatEndedEvent":
		return adapters.Event{}, translateChatEnded

	default:
		return adapters.Event{}, translateSkip
	}
}

// newEvent builds the common envelope shared by every translated event,
// parsing publishedAt as RFC3339 and falling back to the current time.
func newEvent(t adapters.EventType, channel, publishedAt string) adapters.Event {
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       t,
		Platform:   platformName,
		Channel:    channel,
		OccurredAt: parseTimestamp(publishedAt),
	}
}

func parseTimestamp(raw string) time.Time {
	if raw != "" {
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return ts.UTC()
		}
	}
	return time.Now().UTC()
}

// connectionEvent constructs a platform.connected/disconnected/reconnecting
// event with the given reason and error message.
func connectionEvent(t adapters.EventType, channel, reason, errMsg string) adapters.Event {
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       t,
		Platform:   platformName,
		Channel:    channel,
		OccurredAt: time.Now().UTC(),
		Connection: &adapters.ConnectionEvent{Reason: reason, Error: errMsg},
	}
}
