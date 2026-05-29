package twitch

import (
	"strconv"
	"strings"
	"time"

	irc "github.com/gempir/go-twitch-irc/v4"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

const platformName = "twitch"

// occurredAt prefers the IRC tmi-sent-ts tag when present so events keep
// their server-side timestamp; otherwise it falls back to the local clock.
func occurredAt(tags map[string]string) time.Time {
	if raw, ok := tags["tmi-sent-ts"]; ok && raw != "" {
		ms, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && ms > 0 {
			return time.UnixMilli(ms).UTC()
		}
	}
	return time.Now().UTC()
}

// badgeNames returns just the badge keys (e.g. "moderator", "vip", "subscriber/12")
// flattened to "subscriber" form for downstream consumers.
func badgeNames(badges map[string]int) []string {
	if len(badges) == 0 {
		return nil
	}
	out := make([]string, 0, len(badges))
	for name := range badges {
		out = append(out, name)
	}
	return out
}

// emoteIDs returns the platform-native emote ids in occurrence order.
// Duplicates are preserved because downstream consumers may want counts.
func emoteIDs(emotes []*irc.Emote) []string {
	if len(emotes) == 0 {
		return nil
	}
	out := make([]string, 0, len(emotes))
	for _, e := range emotes {
		if e == nil {
			continue
		}
		out = append(out, e.ID)
	}
	return out
}

// translatePrivateMessage converts a Twitch PRIVMSG into a normalized
// EventMessageCreated event. Pure function: no network, no shared state.
func translatePrivateMessage(m irc.PrivateMessage) adapters.Event {
	_, isSub := m.User.Badges["subscriber"]
	if !isSub {
		_, isSub = m.User.Badges["founder"]
	}
	var replyTo string
	if m.Reply != nil {
		replyTo = m.Reply.ParentMsgID
	}
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageCreated,
		Platform:   platformName,
		Channel:    m.Channel,
		OccurredAt: occurredAt(m.Tags),
		Message: &adapters.MessageEvent{
			ID:           m.ID,
			UserID:       m.User.ID,
			Username:     m.User.Name,
			Content:      m.Message,
			IsModerator:  m.User.IsMod || m.User.IsBroadcaster,
			IsSubscriber: isSub,
			IsVIP:        m.User.IsVip,
			Badges:       badgeNames(m.User.Badges),
			EmotesUsed:   emoteIDs(m.Emotes),
			ReplyTo:      replyTo,
		},
	}
}

// translateClearMessage converts a Twitch CLEARMSG (single-message deletion)
// into a normalized EventMessageDeleted event.
func translateClearMessage(m irc.ClearMessage) adapters.Event {
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageDeleted,
		Platform:   platformName,
		Channel:    m.Channel,
		OccurredAt: occurredAt(m.Tags),
		Message: &adapters.MessageEvent{
			ID:       m.TargetMsgID,
			Username: m.Login,
		},
	}
}

// translateClearChat converts a CLEARCHAT message into a ban or timeout
// event. CLEARCHAT with no TargetUserID is a channel-wide purge and is
// represented as an [adapters.Event] with zero Type so the caller can drop
// it.
func translateClearChat(m irc.ClearChatMessage) adapters.Event {
	if m.TargetUserID == "" && m.TargetUsername == "" {
		return adapters.Event{}
	}
	var (
		evtType  adapters.EventType
		duration time.Duration
		action   string
	)
	if m.BanDuration > 0 {
		evtType = adapters.EventUserTimedOut
		duration = time.Duration(m.BanDuration) * time.Second
		action = "timeout"
	} else {
		evtType = adapters.EventUserBanned
		action = "ban"
	}
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       evtType,
		Platform:   platformName,
		Channel:    m.Channel,
		OccurredAt: occurredAt(m.Tags),
		UserAction: &adapters.UserActionEvent{
			Action:     action,
			TargetUser: m.TargetUsername,
			Duration:   duration,
		},
	}
}

// translateUserNotice converts a USERNOTICE message into the matching
// subscription / raid event according to its msg-id tag. Returns a
// zero-value [adapters.Event] when the msg-id is not one we surface.
func translateUserNotice(m irc.UserNoticeMessage) adapters.Event {
	switch m.MsgID {
	case "sub":
		return adapters.Event{
			ID:         adapters.NewEventID(),
			Type:       adapters.EventUserSubscribed,
			Platform:   platformName,
			Channel:    m.Channel,
			OccurredAt: occurredAt(m.Tags),
			Subscription: &adapters.SubscriptionEvent{
				UserID:      m.User.ID,
				Username:    m.User.Name,
				Tier:        m.MsgParams["msg-param-sub-plan"],
				MonthsTotal: 1,
				Message:     m.Message,
			},
		}
	case "resub":
		months, _ := strconv.Atoi(m.MsgParams["msg-param-cumulative-months"])
		if months == 0 {
			months, _ = strconv.Atoi(m.MsgParams["msg-param-months"])
		}
		return adapters.Event{
			ID:         adapters.NewEventID(),
			Type:       adapters.EventUserResubscribed,
			Platform:   platformName,
			Channel:    m.Channel,
			OccurredAt: occurredAt(m.Tags),
			Subscription: &adapters.SubscriptionEvent{
				UserID:      m.User.ID,
				Username:    m.User.Name,
				Tier:        m.MsgParams["msg-param-sub-plan"],
				MonthsTotal: months,
				Message:     m.Message,
			},
		}
	case "subgift", "anonsubgift":
		months, _ := strconv.Atoi(m.MsgParams["msg-param-months"])
		if months == 0 {
			months = 1
		}
		gifter := m.User.Name
		if m.MsgID == "anonsubgift" || gifter == "" {
			if v := m.MsgParams["msg-param-sender-login"]; v != "" {
				gifter = v
			}
		}
		recipient := m.MsgParams["msg-param-recipient-user-name"]
		if recipient == "" {
			recipient = m.MsgParams["msg-param-recipient-display-name"]
		}
		return adapters.Event{
			ID:         adapters.NewEventID(),
			Type:       adapters.EventUserSubscribed,
			Platform:   platformName,
			Channel:    m.Channel,
			OccurredAt: occurredAt(m.Tags),
			Subscription: &adapters.SubscriptionEvent{
				UserID:      m.MsgParams["msg-param-recipient-id"],
				Username:    recipient,
				Tier:        m.MsgParams["msg-param-sub-plan"],
				MonthsTotal: months,
				IsGift:      true,
				GiftedBy:    gifter,
				Message:     m.Message,
			},
		}
	case "raid":
		viewers, _ := strconv.Atoi(m.MsgParams["msg-param-viewerCount"])
		from := m.MsgParams["msg-param-displayName"]
		if from == "" {
			from = m.MsgParams["msg-param-login"]
		}
		if from == "" {
			from = m.User.Name
		}
		return adapters.Event{
			ID:         adapters.NewEventID(),
			Type:       adapters.EventChannelRaided,
			Platform:   platformName,
			Channel:    m.Channel,
			OccurredAt: occurredAt(m.Tags),
			Raid: &adapters.RaidEvent{
				FromUsername: strings.ToLower(from),
				ViewerCount:  viewers,
			},
		}
	default:
		return adapters.Event{}
	}
}

// connectionEvent builds a connection-lifecycle event with the supplied
// reason and (optional) error message.
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
