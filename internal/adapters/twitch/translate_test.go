package twitch

import (
	"testing"
	"time"

	irc "github.com/gempir/go-twitch-irc/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

func TestOccurredAtUsesTmiSentTs(t *testing.T) {
	tags := map[string]string{"tmi-sent-ts": "1700000000123"}
	got := occurredAt(tags)
	assert.Equal(t, time.UnixMilli(1700000000123).UTC(), got)
}

func TestOccurredAtFallsBackToNow(t *testing.T) {
	got := occurredAt(map[string]string{})
	assert.WithinDuration(t, time.Now().UTC(), got, time.Second)
}

func TestBadgeNamesEmpty(t *testing.T) {
	assert.Nil(t, badgeNames(nil))
	assert.Nil(t, badgeNames(map[string]int{}))
}

func TestBadgeNamesRoundTrip(t *testing.T) {
	got := badgeNames(map[string]int{"moderator": 1, "subscriber": 12})
	assert.ElementsMatch(t, []string{"moderator", "subscriber"}, got)
}

func TestEmoteIDs(t *testing.T) {
	assert.Nil(t, emoteIDs(nil))
	got := emoteIDs([]*irc.Emote{{ID: "25"}, nil, {ID: "1902"}})
	assert.Equal(t, []string{"25", "1902"}, got)
}

func TestTranslatePrivateMessage_Basic(t *testing.T) {
	m := irc.PrivateMessage{
		User: irc.User{
			ID:     "12345",
			Name:   "viewer",
			Badges: map[string]int{"subscriber": 6},
		},
		Channel: "broadcaster",
		ID:      "msg-id-1",
		Tags: map[string]string{
			"id":          "msg-id-1",
			"tmi-sent-ts": "1700000001000",
		},
		Message: "hello",
		Emotes:  []*irc.Emote{{ID: "25"}, {ID: "25"}, {ID: "1902"}},
	}
	evt := translatePrivateMessage(m)

	require.Equal(t, adapters.EventMessageCreated, evt.Type)
	require.Equal(t, "twitch", evt.Platform)
	require.Equal(t, "broadcaster", evt.Channel)
	require.NotEmpty(t, evt.ID)
	require.NotNil(t, evt.Message)
	assert.Equal(t, "msg-id-1", evt.Message.ID)
	assert.Equal(t, "12345", evt.Message.UserID)
	assert.Equal(t, "viewer", evt.Message.Username)
	assert.Equal(t, "hello", evt.Message.Content)
	assert.False(t, evt.Message.IsModerator)
	assert.True(t, evt.Message.IsSubscriber)
	assert.False(t, evt.Message.IsVIP)
	assert.ElementsMatch(t, []string{"subscriber"}, evt.Message.Badges)
	assert.Equal(t, []string{"25", "25", "1902"}, evt.Message.EmotesUsed)
	assert.Empty(t, evt.Message.ReplyTo)
	assert.Equal(t, time.UnixMilli(1700000001000).UTC(), evt.OccurredAt)
}

func TestTranslatePrivateMessage_ModBroadcasterVIPReply(t *testing.T) {
	m := irc.PrivateMessage{
		User: irc.User{
			ID:            "777",
			Name:          "owner",
			IsBroadcaster: true,
			IsVip:         true,
			Badges:        map[string]int{"broadcaster": 1, "vip": 1},
		},
		Channel: "owner",
		ID:      "x",
		Tags: map[string]string{
			"reply-parent-msg-id": "parent-1",
		},
		Reply: &irc.Reply{ParentMsgID: "parent-1"},
	}
	evt := translatePrivateMessage(m)
	require.NotNil(t, evt.Message)
	assert.True(t, evt.Message.IsModerator, "broadcaster counts as moderator")
	assert.True(t, evt.Message.IsVIP)
	assert.Equal(t, "parent-1", evt.Message.ReplyTo)
}

func TestTranslateClearMessage(t *testing.T) {
	m := irc.ClearMessage{
		Channel:     "broadcaster",
		Login:       "viewer",
		TargetMsgID: "abc",
		Tags:        map[string]string{"tmi-sent-ts": "1700000002000"},
	}
	evt := translateClearMessage(m)
	require.Equal(t, adapters.EventMessageDeleted, evt.Type)
	require.NotNil(t, evt.Message)
	assert.Equal(t, "abc", evt.Message.ID)
	assert.Equal(t, "viewer", evt.Message.Username)
	assert.Equal(t, "broadcaster", evt.Channel)
}

func TestTranslateClearChat_Timeout(t *testing.T) {
	m := irc.ClearChatMessage{
		Channel:        "broadcaster",
		BanDuration:    60,
		TargetUserID:   "999",
		TargetUsername: "spammer",
	}
	evt := translateClearChat(m)
	require.Equal(t, adapters.EventUserTimedOut, evt.Type)
	require.NotNil(t, evt.UserAction)
	assert.Equal(t, "timeout", evt.UserAction.Action)
	assert.Equal(t, "spammer", evt.UserAction.TargetUser)
	assert.Equal(t, 60*time.Second, evt.UserAction.Duration)
}

func TestTranslateClearChat_Ban(t *testing.T) {
	m := irc.ClearChatMessage{
		Channel:        "broadcaster",
		BanDuration:    0,
		TargetUserID:   "999",
		TargetUsername: "spammer",
	}
	evt := translateClearChat(m)
	require.Equal(t, adapters.EventUserBanned, evt.Type)
	require.NotNil(t, evt.UserAction)
	assert.Equal(t, "ban", evt.UserAction.Action)
	assert.Equal(t, time.Duration(0), evt.UserAction.Duration)
}

func TestTranslateClearChat_ChannelWidePurgeIgnored(t *testing.T) {
	evt := translateClearChat(irc.ClearChatMessage{Channel: "broadcaster"})
	assert.Equal(t, adapters.EventType(""), evt.Type)
}

func TestTranslateUserNotice_Sub(t *testing.T) {
	m := irc.UserNoticeMessage{
		User:    irc.User{ID: "1", Name: "newsub"},
		Channel: "broadcaster",
		MsgID:   "sub",
		MsgParams: map[string]string{
			"msg-param-sub-plan": "1000",
		},
		Message: "yay",
	}
	evt := translateUserNotice(m)
	require.Equal(t, adapters.EventUserSubscribed, evt.Type)
	require.NotNil(t, evt.Subscription)
	assert.Equal(t, "newsub", evt.Subscription.Username)
	assert.Equal(t, "1000", evt.Subscription.Tier)
	assert.Equal(t, 1, evt.Subscription.MonthsTotal)
	assert.False(t, evt.Subscription.IsGift)
	assert.Equal(t, "yay", evt.Subscription.Message)
}

func TestTranslateUserNotice_Resub(t *testing.T) {
	m := irc.UserNoticeMessage{
		User:    irc.User{ID: "2", Name: "loyal"},
		Channel: "broadcaster",
		MsgID:   "resub",
		MsgParams: map[string]string{
			"msg-param-sub-plan":          "2000",
			"msg-param-cumulative-months": "12",
		},
	}
	evt := translateUserNotice(m)
	require.Equal(t, adapters.EventUserResubscribed, evt.Type)
	require.NotNil(t, evt.Subscription)
	assert.Equal(t, "2000", evt.Subscription.Tier)
	assert.Equal(t, 12, evt.Subscription.MonthsTotal)
}

func TestTranslateUserNotice_SubGift(t *testing.T) {
	m := irc.UserNoticeMessage{
		User:    irc.User{ID: "3", Name: "santa"},
		Channel: "broadcaster",
		MsgID:   "subgift",
		MsgParams: map[string]string{
			"msg-param-sub-plan":            "1000",
			"msg-param-recipient-id":        "99",
			"msg-param-recipient-user-name": "lucky",
			"msg-param-months":              "3",
		},
	}
	evt := translateUserNotice(m)
	require.Equal(t, adapters.EventUserSubscribed, evt.Type)
	require.NotNil(t, evt.Subscription)
	assert.True(t, evt.Subscription.IsGift)
	assert.Equal(t, "santa", evt.Subscription.GiftedBy)
	assert.Equal(t, "lucky", evt.Subscription.Username)
	assert.Equal(t, "99", evt.Subscription.UserID)
	assert.Equal(t, 3, evt.Subscription.MonthsTotal)
}

func TestTranslateUserNotice_AnonSubGift(t *testing.T) {
	m := irc.UserNoticeMessage{
		Channel: "broadcaster",
		MsgID:   "anonsubgift",
		MsgParams: map[string]string{
			"msg-param-sub-plan":            "1000",
			"msg-param-recipient-user-name": "lucky",
			"msg-param-sender-login":        "ananonymousgifter",
		},
	}
	evt := translateUserNotice(m)
	require.Equal(t, adapters.EventUserSubscribed, evt.Type)
	require.NotNil(t, evt.Subscription)
	assert.True(t, evt.Subscription.IsGift)
	assert.Equal(t, "ananonymousgifter", evt.Subscription.GiftedBy)
}

func TestTranslateUserNotice_Raid(t *testing.T) {
	m := irc.UserNoticeMessage{
		User:    irc.User{Name: "raider"},
		Channel: "broadcaster",
		MsgID:   "raid",
		MsgParams: map[string]string{
			"msg-param-viewerCount": "42",
			"msg-param-displayName": "Raider",
			"msg-param-login":       "raider",
		},
	}
	evt := translateUserNotice(m)
	require.Equal(t, adapters.EventChannelRaided, evt.Type)
	require.NotNil(t, evt.Raid)
	assert.Equal(t, "raider", evt.Raid.FromUsername)
	assert.Equal(t, 42, evt.Raid.ViewerCount)
}

func TestTranslateUserNotice_UnknownMsgID(t *testing.T) {
	evt := translateUserNotice(irc.UserNoticeMessage{MsgID: "primepaidupgrade"})
	assert.Equal(t, adapters.EventType(""), evt.Type)
}

func TestConnectionEvent(t *testing.T) {
	evt := connectionEvent(adapters.EventConnected, "ch", "reason", "")
	assert.Equal(t, adapters.EventConnected, evt.Type)
	assert.Equal(t, "twitch", evt.Platform)
	assert.Equal(t, "ch", evt.Channel)
	require.NotNil(t, evt.Connection)
	assert.Equal(t, "reason", evt.Connection.Reason)
}
