package kick

type badge struct {
	Text  string `json:"text"`
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type identity struct {
	UsernameColor string  `json:"username_color"`
	Badges        []badge `json:"badges"`
}

type userEvent struct {
	IsAnonymous bool      `json:"is_anonymous"`
	UserID      int       `json:"user_id"`
	Username    string    `json:"username"`
	IsVerified  bool      `json:"is_verified"`
	ChannelSlug string    `json:"channel_slug"`
	Identity    *identity `json:"identity"`
}

func (u userEvent) hasBadge(kind string) bool {
	if u.Identity == nil {
		return false
	}
	for _, b := range u.Identity.Badges {
		if b.Type == kind {
			return true
		}
	}
	return false
}

type chatMessageEvent struct {
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	RepliesTo struct {
		MessageID string    `json:"message_id"`
		Content   string    `json:"content"`
		Sender    userEvent `json:"sender"`
	} `json:"replies_to"`
	Broadcaster userEvent `json:"broadcaster"`
	Sender      userEvent `json:"sender"`
}

type subscriptionEvent struct {
	Broadcaster userEvent `json:"broadcaster"`
	Subscriber  userEvent `json:"subscriber"`
	Gifter      userEvent `json:"gifter"`
	Months      int       `json:"duration"`
}

type moderationBannedEvent struct {
	Broadcaster userEvent `json:"broadcaster"`
	Moderator   userEvent `json:"moderator"`
	BannedUser  userEvent `json:"banned_user"`
	Metadata    struct {
		Reason    string `json:"reason"`
		CreatedAt string `json:"created_at"`
		ExpiresAt string `json:"expires_at"`
	} `json:"metadata"`
}

type sendMessageRequest struct {
	BroadcasterUserID int    `json:"broadcaster_user_id"`
	Content           string `json:"content"`
	Type              string `json:"type"`
	ReplyToMessageID  string `json:"reply_to_message_id,omitempty"`
}

type banRequest struct {
	BroadcasterUserID int    `json:"broadcaster_user_id"`
	UserID            int    `json:"user_id"`
	Duration          *int   `json:"duration,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

type unbanRequest struct {
	BroadcasterUserID int `json:"broadcaster_user_id"`
	UserID            int `json:"user_id"`
}

type subscriptionEventRef struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type subscriptionCreateRequest struct {
	Method            string                 `json:"method"`
	BroadcasterUserID int                    `json:"broadcaster_user_id"`
	Events            []subscriptionEventRef `json:"events"`
}

type subscriptionDeleteRequest struct {
	ID []string `json:"id"`
}

type subscriptionListResponse struct {
	Data []struct {
		ID   string `json:"subscription_id"`
		Name string `json:"name"`
	} `json:"data"`
}
