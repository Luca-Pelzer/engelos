package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	"github.com/Luca-Pelzer/engelos/internal/adapters"
	"github.com/Luca-Pelzer/engelos/internal/adapters/discord"
	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch"
	"github.com/Luca-Pelzer/engelos/internal/kb"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
)

// actionsChatSender adapts the platform fan-out sender to actions.ChatSender so
// the built-in send-chat action can post to chat without internal/actions
// importing the adapter layer. The platform sender wants a context; a rule
// firing is best-effort and short-lived, so a Background context is fine.
type actionsChatSender struct {
	sender platformSender
}

func (a actionsChatSender) Send(channel, text string) error {
	return a.sender.Send(context.Background(), channel, text)
}

// actionsEngineAdapter adapts the actions.Engine to runtime.ActionEngine. It
// translates the dispatcher's flat per-event arguments into an actions.Trigger
// and fires the engine, which matches rules and runs their actions on its own
// worker pool. Every call is non-blocking.
type actionsEngineAdapter struct {
	engine *actions.Engine
}

func (a actionsEngineAdapter) OnMessage(platform, channel, messageID, userID, username, text string,
	isBroadcaster, isModerator, isVIP, isSubscriber bool) {
	a.engine.Fire(context.Background(), actions.Trigger{
		Kind:          actions.TriggerEvent,
		Platform:      platform,
		Channel:       channel,
		MessageID:     messageID,
		UserID:        userID,
		Username:      username,
		Text:          text,
		IsBroadcaster: isBroadcaster,
		IsModerator:   isModerator,
		IsVIP:         isVIP,
		IsSubscriber:  isSubscriber,
		EventType:     "message.created",
	})
	a.engine.Fire(context.Background(), actions.Trigger{
		Kind:          actions.TriggerCommand,
		Platform:      platform,
		Channel:       channel,
		MessageID:     messageID,
		UserID:        userID,
		Username:      username,
		Text:          text,
		IsBroadcaster: isBroadcaster,
		IsModerator:   isModerator,
		IsVIP:         isVIP,
		IsSubscriber:  isSubscriber,
	})
}

func (a actionsEngineAdapter) OnEvent(platform, channel, eventType string, data map[string]any) {
	a.engine.Fire(context.Background(), actions.Trigger{
		Kind:      actions.TriggerEvent,
		Platform:  platform,
		Channel:   channel,
		EventType: eventType,
		Data:      data,
	})
}

// FireEvent fires an event trigger carrying a triggering user, satisfying
// channelpoints.RuleEngine so a Channel-Points redemption can run dashboard
// rules. Unlike OnEvent it threads UserID/Username through so the redeeming
// viewer resolves in $(user)/$(userid); the reward fields ride along in data.
func (a actionsEngineAdapter) FireEvent(platform, channel, eventType, userID, username string, data map[string]any) {
	a.engine.Fire(context.Background(), actions.Trigger{
		Kind:      actions.TriggerEvent,
		Platform:  platform,
		Channel:   channel,
		UserID:    userID,
		Username:  username,
		EventType: eventType,
		Data:      data,
	})
}

// newActionsEngine builds the Action-Engine: a registry seeded with the
// built-in plugins (the chat action wired to the live platform sender), backed
// by the rule store, and started. The returned adapter satisfies
// runtime.ActionEngine and the returned registry is shared with the HTTP
// handler so the dashboard can render the live plugin catalog; Stop drains the
// worker pool at shutdown.
func newActionsEngine(store actions.Store, sender platformSender, discordPoster actions.DiscordPoster,
	twitchMod actions.TwitchModerator, twitchChannel actions.TwitchChannel,
	streamState actions.StreamStateProvider, kbSearcher actions.KBSearcher, recorder actions.RunRecorder,
	tenantID string, logger *slog.Logger) (actionsEngineAdapter, *actions.Engine, *actions.Registry, func(), error) {
	reg := actions.NewRegistry()
	if err := actions.RegisterBuiltins(reg, actions.Services{
		Chat:          actionsChatSender{sender: sender},
		Discord:       discordPoster,
		TwitchMod:     twitchMod,
		TwitchChannel: twitchChannel,
		StreamState:   streamState,
		KB:            kbSearcher,
	}); err != nil {
		return actionsEngineAdapter{}, nil, nil, nil, err
	}
	engine, err := actions.New(actions.Config{
		TenantID: tenantID,
		Source:   store,
		Registry: reg,
		Recorder: recorder,
		Logger:   logger,
	})
	if err != nil {
		return actionsEngineAdapter{}, nil, nil, nil, err
	}
	engine.Start()
	return actionsEngineAdapter{engine: engine}, engine, reg, engine.Stop, nil
}

// compile-time interface check.
var _ runtime.ActionEngine = actionsEngineAdapter{}

// twitchModAdapter implements actions.TwitchModerator by routing through the
// Twitch adapter's platform.Do seam, the same path the dispatcher's auto-mod
// uses, so a rule-driven ban/timeout/delete goes through one code path.
type twitchModAdapter struct {
	adapter *twitch.Adapter
}

func (m twitchModAdapter) DeleteMessage(channel, messageID string) error {
	return m.adapter.Do(context.Background(), adapters.Action{
		Type:          adapters.ActionDeleteMessage,
		Channel:       channel,
		DeleteMessage: &adapters.DeleteMessageAction{MessageID: messageID},
	})
}

func (m twitchModAdapter) Timeout(channel, userID, reason string, seconds int) error {
	return m.adapter.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionTimeout,
		Channel: channel,
		Timeout: &adapters.TimeoutAction{
			UserID:   userID,
			Reason:   reason,
			Duration: time.Duration(seconds) * time.Second,
		},
	})
}

func (m twitchModAdapter) Ban(channel, userID, reason string) error {
	return m.adapter.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionBan,
		Channel: channel,
		Ban:     &adapters.BanAction{UserID: userID, Reason: reason},
	})
}

// twitchChannelAdapter implements actions.TwitchChannel over the Twitch
// adapter's Helix methods, translating the SDK-free views the adapter returns
// into the actions package's neutral types.
type twitchChannelAdapter struct {
	adapter *twitch.Adapter
}

func (c twitchChannelAdapter) CreateClip(channel string, durationSeconds float64) (actions.TwitchClip, error) {
	v, err := c.adapter.CreateClip(context.Background(), channel, durationSeconds)
	if err != nil {
		return actions.TwitchClip{}, err
	}
	return actions.TwitchClip{ID: v.ID, EditURL: v.EditURL, URL: v.URL}, nil
}

func (c twitchChannelAdapter) CreateMarker(channel, description string) (actions.TwitchMarker, error) {
	v, err := c.adapter.CreateStreamMarker(context.Background(), channel, description)
	if err != nil {
		return actions.TwitchMarker{}, err
	}
	return actions.TwitchMarker{ID: v.ID, PositionSeconds: v.PositionSeconds}, nil
}

func (c twitchChannelAdapter) CreatePoll(channel, title string, choices []string, durationSeconds int) (actions.TwitchPoll, error) {
	v, err := c.adapter.CreatePoll(context.Background(), channel, title, choices, durationSeconds)
	if err != nil {
		return actions.TwitchPoll{}, err
	}
	return actions.TwitchPoll{ID: v.ID, Title: v.Title}, nil
}

func (c twitchChannelAdapter) SetTitle(channel, title string) error {
	return c.adapter.SetStreamTitle(context.Background(), channel, title)
}

func (c twitchChannelAdapter) SetCategory(channel, name string) (string, error) {
	v, err := c.adapter.SetCategory(context.Background(), channel, name)
	if err != nil {
		return "", err
	}
	return v.Name, nil
}

func (c twitchChannelAdapter) FulfillRedemption(channel, rewardID, redemptionID string) error {
	return c.adapter.FulfillRedemption(context.Background(), channel, rewardID, redemptionID)
}

func (c twitchChannelAdapter) CancelRedemption(channel, rewardID, redemptionID string) error {
	return c.adapter.CancelRedemption(context.Background(), channel, rewardID, redemptionID)
}

// discordPostAdapter implements actions.DiscordPoster by routing through the
// Discord adapter's platform.Do seam.
type discordPostAdapter struct {
	adapter *discord.Adapter
}

func (d discordPostAdapter) PostMessage(channelID, text string) error {
	return d.adapter.Do(context.Background(), adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     channelID,
		SendMessage: &adapters.SendMessageAction{Text: text},
	})
}

// kbSearchAdapter narrows the kb store to the actions.KBSearcher read surface,
// reducing full entries to the three fields the kb:lookup node renders.
type kbSearchAdapter struct {
	store kb.Store
}

func (a kbSearchAdapter) Search(ctx context.Context, tenantID, channel, query, category string, limit int) ([]actions.KBHit, error) {
	entries, err := a.store.Search(ctx, tenantID, channel, query, category, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]actions.KBHit, 0, len(entries))
	for _, e := range entries {
		hits = append(hits, actions.KBHit{Category: e.Category, Title: e.Title, Content: e.Content})
	}
	return hits, nil
}
