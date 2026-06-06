package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

// ChatController executes outbound chat actions (send a message, moderate a
// user) on the connected platform adapters. It is the HTTP-facing counterpart
// to the dispatcher's inbound event consumption: the /chat dashboard page
// reads live messages over the WebSocket and writes back through here.
type ChatController struct {
	// Platforms are the connected adapters. A target of "all" fans the
	// action out to every adapter whose Name matches a live-chat platform;
	// a specific target addresses just that one.
	Platforms []adapters.Platform
	// Channel is the default channel actions are routed to (the operator's
	// own channel). Adapters that key on channel use this.
	Channel string
}

type chatSendRequest struct {
	Platform string `json:"platform"`
	Text     string `json:"text"`
}

type chatModerateRequest struct {
	Platform  string `json:"platform"`
	MessageID string `json:"message_id"`
	Username  string `json:"username"`
	Action    string `json:"action"`
}

// Send handles POST /api/v1/chat/send. It dispatches an ActionSendMessage to
// the targeted platform(s). Target "all" sends to every connected adapter.
func (c *ChatController) Send(w http.ResponseWriter, r *http.Request) {
	var req chatSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty_text"})
		return
	}

	act := adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     c.Channel,
		SendMessage: &adapters.SendMessageAction{Text: req.Text},
	}
	if c.dispatch(r.Context(), req.Platform, act) {
		writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "no_platform_accepted"})
}

// Moderate handles POST /api/v1/chat/moderate. It maps a UI action
// (delete/timeout/ban) onto the adapter Action and dispatches it.
func (c *ChatController) Moderate(w http.ResponseWriter, r *http.Request) {
	var req chatModerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}

	act := adapters.Action{Channel: c.Channel}
	switch req.Action {
	case "delete":
		act.Type = adapters.ActionDeleteMessage
		act.DeleteMessage = &adapters.DeleteMessageAction{MessageID: req.MessageID}
	case "timeout":
		act.Type = adapters.ActionTimeout
		act.Timeout = &adapters.TimeoutAction{UserID: req.Username, Duration: 10 * time.Minute}
	case "ban":
		act.Type = adapters.ActionBan
		act.Ban = &adapters.BanAction{UserID: req.Username}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown_action"})
		return
	}

	if c.dispatch(r.Context(), req.Platform, act) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "no_platform_accepted"})
}

// dispatch sends act to the matching platform(s). target "all" (or empty)
// fans out to every adapter; otherwise only the adapter whose Name equals
// target is used. Returns true if at least one adapter accepted the action.
func (c *ChatController) dispatch(ctx context.Context, target string, act adapters.Action) bool {
	all := target == "" || target == "all"
	accepted := false
	for _, p := range c.Platforms {
		if !all && p.Name() != target {
			continue
		}
		if err := p.Do(ctx, act); err == nil {
			accepted = true
		}
	}
	return accepted
}
