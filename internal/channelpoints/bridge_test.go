package channelpoints

import (
	"context"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch/eventsub"
)

type recordedFire struct {
	platform  string
	channel   string
	eventType string
	userID    string
	username  string
	data      map[string]any
}

type recordingEngine struct {
	fires []recordedFire
}

func (r *recordingEngine) FireEvent(platform, channel, eventType, userID, username string, data map[string]any) {
	r.fires = append(r.fires, recordedFire{
		platform:  platform,
		channel:   channel,
		eventType: eventType,
		userID:    userID,
		username:  username,
		data:      data,
	})
}

func sampleRedemption() eventsub.RedemptionEvent {
	return eventsub.RedemptionEvent{
		ID:                   "redemption-1",
		BroadcasterUserID:    "bc-1",
		BroadcasterUserLogin: "SomeChannel",
		UserID:               "user-1",
		UserLogin:            "viewer1",
		UserName:             "Viewer1",
		UserInput:            "please play",
		Status:               "unfulfilled",
		RewardID:             "reward-1",
		RewardTitle:          "Hydrate",
		RewardCost:           500,
	}
}

func TestBridgeHandleFiresEventTrigger(t *testing.T) {
	eng := &recordingEngine{}
	b := NewBridge(eng, nil)

	b.Handle(context.Background(), sampleRedemption())

	if len(eng.fires) != 1 {
		t.Fatalf("want 1 fire, got %d", len(eng.fires))
	}
	f := eng.fires[0]
	if f.platform != "twitch" {
		t.Errorf("platform = %q, want twitch", f.platform)
	}
	if f.channel != "somechannel" {
		t.Errorf("channel = %q, want somechannel (lowercased)", f.channel)
	}
	if f.eventType != RedemptionEventType {
		t.Errorf("eventType = %q, want %q", f.eventType, RedemptionEventType)
	}
	if f.userID != "user-1" {
		t.Errorf("userID = %q, want user-1", f.userID)
	}
	if f.username != "Viewer1" {
		t.Errorf("username = %q, want Viewer1", f.username)
	}
}

func TestBridgeHandlePopulatesRewardVars(t *testing.T) {
	eng := &recordingEngine{}
	b := NewBridge(eng, nil)

	b.Handle(context.Background(), sampleRedemption())

	d := eng.fires[0].data
	want := map[string]string{
		"reward_id":     "reward-1",
		"reward_title":  "Hydrate",
		"reward_cost":   "500",
		"user_input":    "please play",
		"user_login":    "viewer1",
		"redemption_id": "redemption-1",
		"status":        "unfulfilled",
	}
	for k, v := range want {
		got, ok := d[k]
		if !ok {
			t.Errorf("data[%q] missing", k)
			continue
		}
		if gs, _ := got.(string); gs != v {
			t.Errorf("data[%q] = %v, want %q", k, got, v)
		}
	}
}

func TestBridgeHandleFallsBackToUserLogin(t *testing.T) {
	eng := &recordingEngine{}
	b := NewBridge(eng, nil)

	evt := sampleRedemption()
	evt.UserName = ""
	b.Handle(context.Background(), evt)

	if got := eng.fires[0].username; got != "viewer1" {
		t.Errorf("username = %q, want viewer1 fallback", got)
	}
}

func TestBridgeHandleNilEngineIsNoop(t *testing.T) {
	b := NewBridge(nil, nil)

	b.Handle(context.Background(), sampleRedemption())
}

func TestBridgeHandleDropsEmptyBroadcasterLogin(t *testing.T) {
	eng := &recordingEngine{}
	b := NewBridge(eng, nil)

	evt := sampleRedemption()
	evt.BroadcasterUserLogin = "   "
	b.Handle(context.Background(), evt)

	if len(eng.fires) != 0 {
		t.Errorf("want 0 fires for empty login, got %d", len(eng.fires))
	}
}
