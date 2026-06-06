package kick

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

// WebhookHandler returns the http.HandlerFunc that ingests Kick webhook
// deliveries. The router mounts it on a public path, outside the session-gated
// API group: authenticity is established by RSA-SHA256 signature verification,
// and replay is blocked by a timestamp window plus a message-id dedup cache.
// The handler always returns quickly and pushes normalized events without
// blocking, because a slow or failing endpoint causes Kick to unsubscribe it.
func (a *Adapter) WebhookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}

		eventType := r.Header.Get(headerEventType)
		messageID := r.Header.Get(headerMessageID)
		signature := r.Header.Get(headerSignature)
		timestamp := r.Header.Get(headerTimestamp)

		if err := a.verifySignature(messageID, timestamp, body, signature); err != nil {
			a.logger.Warn("kick webhook signature rejected", "err", err, "message_id", messageID)
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}

		if err := a.checkReplay(messageID, timestamp); err != nil {
			if err == errDuplicate {
				w.WriteHeader(http.StatusOK)
				return
			}
			a.logger.Warn("kick webhook replay rejected", "err", err, "message_id", messageID)
			http.Error(w, "replay rejected", http.StatusForbidden)
			return
		}

		if evt, ok := a.normalize(eventType, body); ok {
			a.emit(evt)
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

func (a *Adapter) verifySignature(messageID, timestamp string, body []byte, signature string) error {
	if messageID == "" || timestamp == "" || signature == "" {
		return ErrSignatureInvalid
	}
	decodedSig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return ErrSignatureInvalid
	}
	signed := messageID + "." + timestamp + "." + string(body)
	hashed := sha256.Sum256([]byte(signed))
	if err := rsa.VerifyPKCS1v15(a.pubKey, crypto.SHA256, hashed[:], decodedSig); err != nil {
		return ErrSignatureInvalid
	}
	return nil
}

var errDuplicate = adapterError("kick: duplicate message id")

type adapterError string

func (e adapterError) Error() string { return string(e) }

func (a *Adapter) checkReplay(messageID, timestamp string) error {
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return ErrReplayRejected
	}
	if a.nowFn().UTC().Sub(ts.UTC()) > replayWindow {
		return ErrReplayRejected
	}
	if _, seen := a.seen.Get(messageID); seen {
		return errDuplicate
	}
	a.seen.Add(messageID, struct{}{})
	return nil
}

func (a *Adapter) normalize(eventType string, body []byte) (adapters.Event, bool) {
	switch eventType {
	case "chat.message.sent":
		return a.normalizeChat(body)
	case "channel.subscription.new":
		return a.normalizeSubscription(body, adapters.EventUserSubscribed, false)
	case "channel.subscription.renewal":
		return a.normalizeSubscription(body, adapters.EventUserResubscribed, false)
	case "channel.subscription.gifts":
		return a.normalizeSubscription(body, adapters.EventUserSubscribed, true)
	case "moderation.banned":
		return a.normalizeBan(body)
	default:
		return adapters.Event{}, false
	}
}

func (a *Adapter) normalizeChat(body []byte) (adapters.Event, bool) {
	var msg chatMessageEvent
	if err := json.Unmarshal(body, &msg); err != nil {
		a.logger.Warn("kick chat decode failed", "err", err)
		return adapters.Event{}, false
	}
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageCreated,
		Platform:   platformName,
		Channel:    a.cfg.Channel,
		OccurredAt: parseTime(msg.CreatedAt),
		Message: &adapters.MessageEvent{
			ID:           msg.MessageID,
			UserID:       strconv.Itoa(msg.Sender.UserID),
			Username:     msg.Sender.Username,
			Content:      msg.Content,
			IsModerator:  msg.Sender.hasBadge("moderator"),
			IsSubscriber: msg.Sender.hasBadge("subscriber"),
			IsVIP:        msg.Sender.hasBadge("vip"),
			ReplyTo:      msg.RepliesTo.MessageID,
		},
	}, true
}

func (a *Adapter) normalizeSubscription(body []byte, t adapters.EventType, gift bool) (adapters.Event, bool) {
	var sub subscriptionEvent
	if err := json.Unmarshal(body, &sub); err != nil {
		a.logger.Warn("kick subscription decode failed", "err", err)
		return adapters.Event{}, false
	}
	payload := &adapters.SubscriptionEvent{
		UserID:      strconv.Itoa(sub.Subscriber.UserID),
		Username:    sub.Subscriber.Username,
		MonthsTotal: sub.Months,
		IsGift:      gift,
	}
	if gift && sub.Gifter.Username != "" {
		payload.GiftedBy = sub.Gifter.Username
	}
	return adapters.Event{
		ID:           adapters.NewEventID(),
		Type:         t,
		Platform:     platformName,
		Channel:      a.cfg.Channel,
		OccurredAt:   time.Now().UTC(),
		Subscription: payload,
	}, true
}

func (a *Adapter) normalizeBan(body []byte) (adapters.Event, bool) {
	var ban moderationBannedEvent
	if err := json.Unmarshal(body, &ban); err != nil {
		a.logger.Warn("kick ban decode failed", "err", err)
		return adapters.Event{}, false
	}
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventUserBanned,
		Platform:   platformName,
		Channel:    a.cfg.Channel,
		OccurredAt: parseTime(ban.Metadata.CreatedAt),
		UserAction: &adapters.UserActionEvent{
			Action:     "ban",
			TargetUser: ban.BannedUser.Username,
			Moderator:  ban.Moderator.Username,
			Reason:     ban.Metadata.Reason,
		},
	}, true
}

func parseTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Now().UTC()
}
