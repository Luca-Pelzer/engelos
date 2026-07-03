package actions

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TwitchClip is the neutral result of a clip creation, free of any adapter or
// SDK type.
type TwitchClip struct {
	ID      string
	EditURL string
	URL     string
}

// TwitchMarker is the neutral result of a stream marker creation.
type TwitchMarker struct {
	ID              string
	PositionSeconds int
}

// TwitchPoll is the neutral result of a poll creation.
type TwitchPoll struct {
	ID    string
	Title string
}

// TwitchChannel is the channel-management side-effect surface the twitch:*
// channel actions call out to. The host wires a thin adapter over the Twitch
// adapter's Helix methods. A nil value means these controls are unavailable and
// the actions fail with a clear error rather than a silent no-op.
type TwitchChannel interface {
	CreateClip(channel string, durationSeconds float64) (TwitchClip, error)
	CreateMarker(channel, description string) (TwitchMarker, error)
	CreatePoll(channel, title string, choices []string, durationSeconds int) (TwitchPoll, error)
	SetTitle(channel, title string) error
	SetCategory(channel, name string) (string, error)
	FulfillRedemption(channel, rewardID, redemptionID string) error
	CancelRedemption(channel, rewardID, redemptionID string) error
}

type twitchCreateClipConfig struct {
	DurationSeconds float64 `json:"duration_seconds"`
}

type twitchCreateClipAction struct {
	ch TwitchChannel
}

func (twitchCreateClipAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "twitch:create-clip",
		Name:        "Create Twitch clip",
		Description: "Creates a clip of the current stream. Outputs clip_id, edit_url and url.",
	}
}

func (a twitchCreateClipAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c twitchCreateClipConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: twitch:create-clip config: %w", err)
	}
	if a.ch == nil {
		return nil, fmt.Errorf("actions: twitch:create-clip: Twitch channel controls are not available")
	}
	clip, err := a.ch.CreateClip(triggerChannel(ec), c.DurationSeconds)
	if err != nil {
		return nil, fmt.Errorf("actions: twitch:create-clip: %w", err)
	}
	return &ActionResult{Outputs: map[string]any{
		"clip_id":  clip.ID,
		"edit_url": clip.EditURL,
		"url":      clip.URL,
	}}, nil
}

// markerDescriptionMax is Helix's cap on a stream marker description; longer
// descriptions are truncated rather than rejected so a rule never fails on a
// long template render.
const markerDescriptionMax = 140

type twitchCreateMarkerConfig struct {
	Description string `json:"description"`
}

type twitchCreateMarkerAction struct {
	ch TwitchChannel
}

func (twitchCreateMarkerAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "twitch:create-marker",
		Name:        "Create Twitch stream marker",
		Description: "Marks the current point in the live stream. description is optional, supports $(...) variables, and is truncated to 140 chars. Outputs marker_id and position_seconds. Errors when the stream is offline (the rest of the chain still runs).",
	}
}

func (a twitchCreateMarkerAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c twitchCreateMarkerConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: twitch:create-marker config: %w", err)
	}
	if a.ch == nil {
		return nil, fmt.Errorf("actions: twitch:create-marker: Twitch channel controls are not available")
	}
	// Truncate (no ellipsis) so Helix accepts the 140-char cap verbatim.
	description := strings.TrimSpace(c.Description)
	if r := []rune(description); len(r) > markerDescriptionMax {
		description = string(r[:markerDescriptionMax])
	}
	marker, err := a.ch.CreateMarker(triggerChannel(ec), description)
	if err != nil {
		return nil, fmt.Errorf("actions: twitch:create-marker: %w", err)
	}
	return &ActionResult{Outputs: map[string]any{
		"marker_id":        marker.ID,
		"position_seconds": marker.PositionSeconds,
	}}, nil
}

// Poll limits enforced by Twitch's Helix API.
const (
	pollTitleMax       = 60
	pollChoiceMax      = 25
	pollChoicesMin     = 2
	pollChoicesMax     = 5
	pollDurationMin    = 15
	pollDurationMax    = 1800
	pollDurationDefaul = 60
)

type twitchCreatePollConfig struct {
	Title           string   `json:"title"`
	Choices         []string `json:"choices"`
	DurationSeconds int      `json:"duration_seconds"`
}

type twitchCreatePollAction struct {
	ch TwitchChannel
}

func (twitchCreatePollAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "twitch:create-poll",
		Name:        "Create Twitch poll",
		Description: "Starts a poll: title (required, <=60 chars), 2-5 choices (each <=25 chars), duration_seconds 15-1800 (default 60). Needs the channel:manage:polls scope. Outputs poll_id and title.",
	}
}

func (a twitchCreatePollAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c twitchCreatePollConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: twitch:create-poll config: %w", err)
	}
	title := strings.TrimSpace(c.Title)
	if title == "" {
		return nil, fmt.Errorf("actions: twitch:create-poll: title is required")
	}
	if len([]rune(title)) > pollTitleMax {
		return nil, fmt.Errorf("actions: twitch:create-poll: title exceeds %d characters", pollTitleMax)
	}
	choices := make([]string, 0, len(c.Choices))
	for _, ch := range c.Choices {
		t := strings.TrimSpace(ch)
		if t == "" {
			continue
		}
		if len([]rune(t)) > pollChoiceMax {
			return nil, fmt.Errorf("actions: twitch:create-poll: choice %q exceeds %d characters", t, pollChoiceMax)
		}
		choices = append(choices, t)
	}
	if len(choices) < pollChoicesMin || len(choices) > pollChoicesMax {
		return nil, fmt.Errorf("actions: twitch:create-poll: need %d-%d choices, got %d", pollChoicesMin, pollChoicesMax, len(choices))
	}
	duration := c.DurationSeconds
	if duration == 0 {
		duration = pollDurationDefaul
	}
	if duration < pollDurationMin || duration > pollDurationMax {
		return nil, fmt.Errorf("actions: twitch:create-poll: duration_seconds must be %d-%d, got %d", pollDurationMin, pollDurationMax, duration)
	}
	if a.ch == nil {
		return nil, fmt.Errorf("actions: twitch:create-poll: Twitch channel controls are not available")
	}
	poll, err := a.ch.CreatePoll(triggerChannel(ec), title, choices, duration)
	if err != nil {
		return nil, fmt.Errorf("actions: twitch:create-poll: %w", err)
	}
	return &ActionResult{Outputs: map[string]any{
		"poll_id": poll.ID,
		"title":   poll.Title,
	}}, nil
}

type twitchSetTitleConfig struct {
	Title string `json:"title"`
}

type twitchSetTitleAction struct {
	ch TwitchChannel
}

func (twitchSetTitleAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "twitch:set-title",
		Name:        "Set Twitch stream title",
		Description: "Sets the stream title. The title supports $(...) variables.",
	}
}

func (a twitchSetTitleAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c twitchSetTitleConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: twitch:set-title config: %w", err)
	}
	title := strings.TrimSpace(c.Title)
	if title == "" {
		return nil, fmt.Errorf("actions: twitch:set-title: title is required")
	}
	if a.ch == nil {
		return nil, fmt.Errorf("actions: twitch:set-title: Twitch channel controls are not available")
	}
	if err := a.ch.SetTitle(triggerChannel(ec), title); err != nil {
		return nil, fmt.Errorf("actions: twitch:set-title: %w", err)
	}
	return nil, nil
}

type twitchSetCategoryConfig struct {
	Category string `json:"category"`
}

type twitchSetCategoryAction struct {
	ch TwitchChannel
}

func (twitchSetCategoryAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "twitch:set-category",
		Name:        "Set Twitch category",
		Description: "Sets the stream category (game) by exact name. Outputs category (the resolved canonical name).",
	}
}

func (a twitchSetCategoryAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c twitchSetCategoryConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: twitch:set-category config: %w", err)
	}
	name := strings.TrimSpace(c.Category)
	if name == "" {
		return nil, fmt.Errorf("actions: twitch:set-category: category is required")
	}
	if a.ch == nil {
		return nil, fmt.Errorf("actions: twitch:set-category: Twitch channel controls are not available")
	}
	resolved, err := a.ch.SetCategory(triggerChannel(ec), name)
	if err != nil {
		return nil, fmt.Errorf("actions: twitch:set-category: %w", err)
	}
	return &ActionResult{Outputs: map[string]any{"category": resolved}}, nil
}

type twitchRedemptionConfig struct {
	Status       string `json:"status"`
	RewardID     string `json:"reward_id"`
	RedemptionID string `json:"redemption_id"`
}

type twitchRedemptionAction struct {
	ch TwitchChannel
}

func (twitchRedemptionAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "twitch:redemption",
		Name:        "Fulfill or cancel Channel-Points redemption",
		Description: "Marks a Channel-Points redemption fulfilled or canceled (status: fulfilled|canceled, default fulfilled). reward_id and redemption_id default to the triggering redemption event.",
	}
}

func (a twitchRedemptionAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c twitchRedemptionConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: twitch:redemption config: %w", err)
	}
	rewardID := firstNonEmpty(c.RewardID, triggerDataString(ec, "reward_id"))
	redemptionID := firstNonEmpty(c.RedemptionID, triggerDataString(ec, "redemption_id"))
	if rewardID == "" || redemptionID == "" {
		return nil, fmt.Errorf("actions: twitch:redemption: reward_id and redemption_id are required")
	}
	if a.ch == nil {
		return nil, fmt.Errorf("actions: twitch:redemption: Twitch channel controls are not available")
	}
	channel := triggerChannel(ec)
	switch strings.ToLower(strings.TrimSpace(c.Status)) {
	case "", "fulfilled", "fulfill":
		if err := a.ch.FulfillRedemption(channel, rewardID, redemptionID); err != nil {
			return nil, fmt.Errorf("actions: twitch:redemption: %w", err)
		}
	case "canceled", "cancelled", "cancel":
		if err := a.ch.CancelRedemption(channel, rewardID, redemptionID); err != nil {
			return nil, fmt.Errorf("actions: twitch:redemption: %w", err)
		}
	default:
		return nil, fmt.Errorf("actions: twitch:redemption: status must be fulfilled or canceled, got %q", c.Status)
	}
	return nil, nil
}

// firstNonEmpty returns the first of its arguments that is non-empty after
// trimming, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// triggerDataString renders a trigger Data value as a string, for defaulting a
// config field from the event that fired the rule (e.g. a redemption's ids).
func triggerDataString(ec *ExecutionContext, key string) string {
	if ec.Trigger.Data == nil {
		return ""
	}
	if v, ok := ec.Trigger.Data[key]; ok {
		return stringifyValue(v)
	}
	return ""
}
