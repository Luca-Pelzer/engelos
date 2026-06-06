package main

import (
	"context"
	"log/slog"

	"github.com/Luca-Pelzer/engelos/internal/actions"
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

func (a actionsEngineAdapter) OnMessage(platform, channel, userID, username, text string,
	isBroadcaster, isModerator, isVIP, isSubscriber bool) {
	a.engine.Fire(context.Background(), actions.Trigger{
		Kind:          actions.TriggerEvent,
		Platform:      platform,
		Channel:       channel,
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
func newActionsEngine(store actions.Store, sender platformSender, obs actions.OBSController, tenantID string, logger *slog.Logger) (actionsEngineAdapter, *actions.Engine, *actions.Registry, func(), error) {
	reg := actions.NewRegistry()
	if err := actions.RegisterBuiltins(reg, actions.Services{
		Chat: actionsChatSender{sender: sender},
		OBS:  obs,
	}); err != nil {
		return actionsEngineAdapter{}, nil, nil, nil, err
	}
	engine, err := actions.New(actions.Config{
		TenantID: tenantID,
		Source:   store,
		Registry: reg,
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
