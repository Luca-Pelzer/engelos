package actions

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// AvatarHub broadcasts avatar directives to the overlay relay. The
// avatar.Hub satisfies it. A nil value means the avatar overlay is not wired
// and the avatar nodes fail with a clear error.
type AvatarHub interface {
	Speak(channel, id, text, voice, audioB64 string, alignment json.RawMessage)
	Expression(channel, id, expression string, holdSeconds int)
}

// AvatarSynth turns text into MP3 audio for a channel, resolving the channel's
// stored TTS credentials. The tts.Service satisfies it. A nil value means TTS
// is not configured and avatar:speak fails with a clear error.
type AvatarSynth interface {
	SynthesizeText(ctx context.Context, channel, voice, text string) ([]byte, error)
}

// avatarSpeakConcurrency caps concurrent (synthesize + broadcast) speak
// directives per channel; requests beyond it are dropped with a node error.
const avatarSpeakConcurrency = 3

// avatarExpressions is the closed set of poses the avatar overlay understands.
var avatarExpressions = map[string]struct{}{
	"idle":     {},
	"talking":  {},
	"happy":    {},
	"thinking": {},
	"confused": {},
	"hype":     {},
	"error":    {},
}

func validAvatarExpression(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	_, ok := avatarExpressions[s]
	return s, ok
}

type avatarSpeakConfig struct {
	Text             string `json:"text"`
	Voice            string `json:"voice"`
	ExpressionDuring string `json:"expression_during"`
	Channel          string `json:"channel"`
}

type avatarSpeakAction struct {
	hub     AvatarHub
	synth   AvatarSynth
	limiter *avatarSpeakLimiter
}

func (avatarSpeakAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "avatar:speak",
		Name:        "Avatar Speak",
		Description: "Synthesizes text to speech and streams it to the avatar overlay as a speak directive (audio plus an optional talking expression). text supports $(...) variables; voice and expression_during are optional. Requires an ElevenLabs key configured under TTS.",
	}
}

func (a avatarSpeakAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c avatarSpeakConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: avatar:speak config: %w", err)
	}
	text := strings.TrimSpace(c.Text)
	if text == "" {
		return nil, nil
	}
	if a.hub == nil {
		return nil, fmt.Errorf("actions: avatar:speak: avatar overlay is not configured")
	}
	if a.synth == nil {
		return nil, fmt.Errorf("actions: avatar:speak: tts not configured")
	}

	var expr string
	if raw := strings.TrimSpace(c.ExpressionDuring); raw != "" {
		v, ok := validAvatarExpression(raw)
		if !ok {
			return nil, fmt.Errorf("actions: avatar:speak: invalid expression_during %q", raw)
		}
		expr = v
	}

	channel := strings.TrimSpace(c.Channel)
	if channel == "" {
		channel = ec.Channel
	}
	if channel == "" {
		channel = ec.Trigger.Channel
	}

	if a.limiter != nil {
		if !a.limiter.acquire(channel) {
			return nil, fmt.Errorf("actions: avatar:speak: rate limited (max %d concurrent per channel)", a.limiter.max)
		}
		defer a.limiter.release(channel)
	}

	if expr != "" {
		a.hub.Expression(channel, newDirectiveID(), expr, 0)
	}

	voice := strings.TrimSpace(c.Voice)
	audio, err := a.synth.SynthesizeText(ec.Ctx, channel, voice, text)
	if err != nil {
		return nil, fmt.Errorf("actions: avatar:speak: %w", err)
	}
	if len(audio) == 0 {
		return nil, nil
	}

	id := newDirectiveID()
	a.hub.Speak(channel, id, text, voice, base64.StdEncoding.EncodeToString(audio), nil)
	return &ActionResult{Outputs: map[string]any{"avatar_speak_id": id}}, nil
}

type avatarExpressionConfig struct {
	Expression  string `json:"expression"`
	HoldSeconds int    `json:"hold_seconds"`
	Channel     string `json:"channel"`
}

type avatarExpressionAction struct {
	hub AvatarHub
}

func (avatarExpressionAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "avatar:expression",
		Name:        "Avatar Expression",
		Description: "Sets the avatar overlay expression: one of idle, talking, happy, thinking, confused, hype, error. hold_seconds > 0 asks the overlay to revert to idle after the hold; 0 holds until changed.",
	}
}

func (a avatarExpressionAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c avatarExpressionConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: avatar:expression config: %w", err)
	}
	if strings.TrimSpace(c.Expression) == "" {
		return nil, fmt.Errorf("actions: avatar:expression: expression is required")
	}
	expr, ok := validAvatarExpression(c.Expression)
	if !ok {
		return nil, fmt.Errorf("actions: avatar:expression: invalid expression %q", c.Expression)
	}
	if a.hub == nil {
		return nil, fmt.Errorf("actions: avatar:expression: avatar overlay is not configured")
	}
	hold := c.HoldSeconds
	if hold < 0 {
		hold = 0
	}
	channel := strings.TrimSpace(c.Channel)
	if channel == "" {
		channel = ec.Channel
	}
	if channel == "" {
		channel = ec.Trigger.Channel
	}
	id := newDirectiveID()
	a.hub.Expression(channel, id, expr, hold)
	return &ActionResult{Outputs: map[string]any{"avatar_expression_id": id}}, nil
}

// RegisterAvatarNodes registers avatar:speak and avatar:expression bound to hub
// and synth, with a per-channel concurrent-speak cap.
func RegisterAvatarNodes(reg *Registry, hub AvatarHub, synth AvatarSynth) error {
	for _, a := range []ActionType{
		avatarSpeakAction{hub: hub, synth: synth, limiter: newAvatarSpeakLimiter(avatarSpeakConcurrency)},
		avatarExpressionAction{hub: hub},
	} {
		if err := reg.RegisterAction(a); err != nil {
			return err
		}
	}
	return nil
}

// avatarSpeakLimiter bounds concurrent speak directives per channel. It counts
// in-flight directives (synthesis is the slow part) and refuses new ones once a
// channel is at its cap, so a runaway rule cannot spawn unbounded synthesis.
type avatarSpeakLimiter struct {
	mu       sync.Mutex
	inflight map[string]int
	max      int
}

func newAvatarSpeakLimiter(max int) *avatarSpeakLimiter {
	if max < 1 {
		max = 1
	}
	return &avatarSpeakLimiter{inflight: make(map[string]int), max: max}
}

func (l *avatarSpeakLimiter) acquire(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inflight[key] >= l.max {
		return false
	}
	l.inflight[key]++
	return true
}

func (l *avatarSpeakLimiter) release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := l.inflight[key]; n > 1 {
		l.inflight[key] = n - 1
	} else {
		delete(l.inflight, key)
	}
}

// newDirectiveID returns a short random id correlating a directive's frames.
func newDirectiveID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "dir"
	}
	return hex.EncodeToString(b)
}
