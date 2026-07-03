package actions

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// DiscordPoster posts a message to a Discord channel by id. The host wires a
// thin adapter over the discordgo session. A nil value means Discord is not
// connected and the discord:post action fails with a clear error.
type DiscordPoster interface {
	PostMessage(channelID, text string) error
}

type discordPostConfig struct {
	ChannelID string `json:"channel_id"`
	Text      string `json:"text"`
}

type discordPostAction struct {
	discord DiscordPoster
}

func (discordPostAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "discord:post",
		Name:        "Post to Discord",
		Description: "Sends a message to a Discord channel by id. The channel id and text support $(...) variables.",
	}
}

func (a discordPostAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c discordPostConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: discord:post config: %w", err)
	}
	channelID := strings.TrimSpace(c.ChannelID)
	if channelID == "" {
		return nil, fmt.Errorf("actions: discord:post: channel_id is required")
	}
	text := strings.TrimSpace(c.Text)
	if text == "" {
		return nil, nil
	}
	if a.discord == nil {
		return nil, fmt.Errorf("actions: discord:post: Discord is not connected")
	}
	if err := a.discord.PostMessage(channelID, text); err != nil {
		return nil, fmt.Errorf("actions: discord:post: %w", err)
	}
	return nil, nil
}

type discordReplyConfig struct {
	Text      string `json:"text"`
	ChannelID string `json:"channel_id"`
}

type discordReplyAction struct {
	discord DiscordPoster
	limiter *replyRateLimiter
}

func (discordReplyAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "discord:reply",
		Name:        "Reply on Discord",
		Description: "Sends a message to a Discord channel, defaulting to the channel the triggering discord.message came from ($(discord.channel_id)). text supports $(...) variables. Outbound replies are rate limited per channel; over the limit the reply is dropped and the rule continues.",
	}
}

func (a discordReplyAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c discordReplyConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: discord:reply config: %w", err)
	}
	channelID := strings.TrimSpace(c.ChannelID)
	if channelID == "" {
		channelID = strings.TrimSpace(triggerDataString(ec, "discord.channel_id"))
	}
	if channelID == "" {
		return nil, fmt.Errorf("actions: discord:reply: no channel_id and no originating Discord channel in the trigger")
	}
	text := strings.TrimSpace(c.Text)
	if text == "" {
		return nil, nil
	}
	if a.discord == nil {
		return nil, fmt.Errorf("actions: discord:reply: Discord is not connected")
	}
	if a.limiter != nil && !a.limiter.allow(channelID) {
		return &ActionResult{Outputs: map[string]any{"rate_limited": true}}, nil
	}
	if err := a.discord.PostMessage(channelID, text); err != nil {
		return nil, fmt.Errorf("actions: discord:reply: %w", err)
	}
	return nil, nil
}

// RegisterDiscordReplyNode registers the discord:reply action bound to poster,
// with a per-channel outbound rate limit of ~5 replies per 5 seconds.
func RegisterDiscordReplyNode(reg *Registry, poster DiscordPoster) error {
	return reg.RegisterAction(discordReplyAction{
		discord: poster,
		limiter: newReplyRateLimiter(1, 5, nil),
	})
}

// replyRateLimiter is a per-channel token-bucket limiter for outbound Discord
// replies. Each channel key gets an independent bucket so one busy channel never
// starves another. Safe for concurrent use.
type replyRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*replyBucket
	rate    float64
	burst   float64
	now     func() time.Time
}

type replyBucket struct {
	tokens float64
	last   time.Time
}

func newReplyRateLimiter(rate, burst float64, now func() time.Time) *replyRateLimiter {
	if now == nil {
		now = time.Now
	}
	return &replyRateLimiter{buckets: make(map[string]*replyBucket), rate: rate, burst: burst, now: now}
}

func (l *replyRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.buckets[key]
	if b == nil {
		b = &replyBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = math.Min(l.burst, b.tokens+elapsed*l.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}
