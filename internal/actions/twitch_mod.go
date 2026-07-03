package actions

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TwitchModerator is the moderation side-effect surface the twitch:* mod
// actions call out to. The host wires a thin adapter that routes through the
// same platform.Do seam the dispatcher's auto-moderation uses. A nil value
// means Twitch moderation is unavailable and the actions fail with a clear
// error rather than a silent no-op.
type TwitchModerator interface {
	DeleteMessage(channel, messageID string) error
	Timeout(channel, userID, reason string, seconds int) error
	Ban(channel, userID, reason string) error
}

// defaultTimeoutSeconds is applied when a twitch:timeout rule omits (or zeroes)
// the duration, matching a common "10 minute" moderation default.
const defaultTimeoutSeconds = 600

type twitchDeleteMessageConfig struct {
	MessageID string `json:"message_id"`
}

type twitchDeleteMessageAction struct {
	mod TwitchModerator
}

func (twitchDeleteMessageAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "twitch:delete-message",
		Name:        "Delete Twitch message",
		Description: "Deletes a single chat message. Defaults to the triggering message; set message_id (supports $(message.id)) to target another.",
	}
}

func (a twitchDeleteMessageAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c twitchDeleteMessageConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: twitch:delete-message config: %w", err)
	}
	messageID := strings.TrimSpace(c.MessageID)
	if messageID == "" {
		messageID = strings.TrimSpace(ec.Trigger.MessageID)
	}
	if messageID == "" {
		return nil, fmt.Errorf("actions: twitch:delete-message: no message id (not a message trigger and message_id unset)")
	}
	if a.mod == nil {
		return nil, fmt.Errorf("actions: twitch:delete-message: Twitch moderation is not available")
	}
	if err := a.mod.DeleteMessage(triggerChannel(ec), messageID); err != nil {
		return nil, fmt.Errorf("actions: twitch:delete-message: %w", err)
	}
	return nil, nil
}

type twitchTimeoutConfig struct {
	UserID  string `json:"user_id"`
	Seconds int    `json:"duration_seconds"`
	Reason  string `json:"reason"`
}

type twitchTimeoutAction struct {
	mod TwitchModerator
}

func (twitchTimeoutAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "twitch:timeout",
		Name:        "Timeout Twitch user",
		Description: "Times out a user for duration_seconds (default 600). Defaults to the triggering user; set user_id (supports $(userid)) to target another.",
	}
}

func (a twitchTimeoutAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c twitchTimeoutConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: twitch:timeout config: %w", err)
	}
	userID := strings.TrimSpace(c.UserID)
	if userID == "" {
		userID = strings.TrimSpace(ec.Trigger.UserID)
	}
	if userID == "" {
		return nil, fmt.Errorf("actions: twitch:timeout: no user id")
	}
	seconds := c.Seconds
	if seconds <= 0 {
		seconds = defaultTimeoutSeconds
	}
	if a.mod == nil {
		return nil, fmt.Errorf("actions: twitch:timeout: Twitch moderation is not available")
	}
	if err := a.mod.Timeout(triggerChannel(ec), userID, strings.TrimSpace(c.Reason), seconds); err != nil {
		return nil, fmt.Errorf("actions: twitch:timeout: %w", err)
	}
	return nil, nil
}

type twitchBanConfig struct {
	UserID string `json:"user_id"`
	Reason string `json:"reason"`
}

type twitchBanAction struct {
	mod TwitchModerator
}

func (twitchBanAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "twitch:ban",
		Name:        "Ban Twitch user",
		Description: "Permanently bans a user. Defaults to the triggering user; set user_id (supports $(userid)) to target another.",
	}
}

func (a twitchBanAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c twitchBanConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: twitch:ban config: %w", err)
	}
	userID := strings.TrimSpace(c.UserID)
	if userID == "" {
		userID = strings.TrimSpace(ec.Trigger.UserID)
	}
	if userID == "" {
		return nil, fmt.Errorf("actions: twitch:ban: no user id")
	}
	if a.mod == nil {
		return nil, fmt.Errorf("actions: twitch:ban: Twitch moderation is not available")
	}
	if err := a.mod.Ban(triggerChannel(ec), userID, strings.TrimSpace(c.Reason)); err != nil {
		return nil, fmt.Errorf("actions: twitch:ban: %w", err)
	}
	return nil, nil
}

// triggerChannel returns the rule's channel, falling back to the trigger's
// channel, the Twitch login the moderation call targets.
func triggerChannel(ec *ExecutionContext) string {
	if ec.Channel != "" {
		return ec.Channel
	}
	return ec.Trigger.Channel
}
