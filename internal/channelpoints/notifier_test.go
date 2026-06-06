package channelpoints

import (
	"context"
	"testing"
)

type recordingNotifier struct {
	eventType string
	alert     RedemptionAlert
	calls     int
}

func (r *recordingNotifier) Notify(eventType string, alert RedemptionAlert) {
	r.calls++
	r.eventType = eventType
	r.alert = alert
}

func TestNotifierHandleBroadcastsAlert(t *testing.T) {
	out := &recordingNotifier{}
	n := NewNotifier(out)

	n.Handle(context.Background(), sampleRedemption())

	if out.calls != 1 {
		t.Fatalf("want 1 notify, got %d", out.calls)
	}
	if out.eventType != RedemptionEventType {
		t.Errorf("eventType = %q, want %q", out.eventType, RedemptionEventType)
	}
	if out.alert.Username != "Viewer1" {
		t.Errorf("username = %q, want Viewer1", out.alert.Username)
	}
	if out.alert.RewardTitle != "Hydrate" {
		t.Errorf("reward_title = %q, want Hydrate", out.alert.RewardTitle)
	}
	if out.alert.RewardCost != 500 {
		t.Errorf("reward_cost = %d, want 500", out.alert.RewardCost)
	}
	if out.alert.UserInput != "please play" {
		t.Errorf("user_input = %q, want 'please play'", out.alert.UserInput)
	}
}

func TestNotifierHandleFallsBackToUserLogin(t *testing.T) {
	out := &recordingNotifier{}
	n := NewNotifier(out)

	evt := sampleRedemption()
	evt.UserName = ""
	n.Handle(context.Background(), evt)

	if out.alert.Username != "viewer1" {
		t.Errorf("username = %q, want viewer1 fallback", out.alert.Username)
	}
}

func TestNotifierHandleNilOutIsNoop(t *testing.T) {
	n := NewNotifier(nil)

	n.Handle(context.Background(), sampleRedemption())
}
