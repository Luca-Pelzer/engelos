package clipper

import (
	"fmt"
	"testing"
	"time"
)

// msg feeds a chat message from a generated user id.
func msg(d *Detector, channel string, userN int, text string, at time.Time) (bool, Reason) {
	return d.Message(channel, fmt.Sprintf("u%d", userN), text, at)
}

func TestMessage_NoFireBelowMinMessages(t *testing.T) {
	d := New(Options{Window: 10 * time.Second, MinMessages: 8, SpikeFactor: 3})
	start := time.Now()
	for i := 0; i < 7; i++ {
		if fired, _ := msg(d, "c", i, "hello", start); fired {
			t.Fatalf("fired at message %d, below MinMessages", i+1)
		}
	}
}

func TestMessage_ChatSpikeFiresOnce(t *testing.T) {
	d := New(Options{Window: 10 * time.Second, MinMessages: 8, SpikeFactor: 3, Cooldown: 90 * time.Second})
	start := time.Now()
	fires := 0
	for i := 0; i < 15; i++ {
		// Varied text so this isolates the rate signal (not copypasta/emote).
		fired, reason := msg(d, "c", i, fmt.Sprintf("message number %d here", i), start)
		if fired {
			fires++
			if reason != ReasonChatSpike {
				t.Fatalf("want chat-spike, got %q", reason)
			}
		}
	}
	if fires != 1 {
		t.Fatalf("want exactly one fire, got %d", fires)
	}
}

func TestMessage_SteadyLowRateNeverFires(t *testing.T) {
	d := New(Options{Window: 10 * time.Second, MinMessages: 8, SpikeFactor: 3})
	start := time.Now()
	for i := 0; i < 30; i++ {
		at := start.Add(time.Duration(i) * 3 * time.Second)
		if fired, _ := msg(d, "c", i, "just chatting", at); fired {
			t.Fatalf("steady low rate fired at i=%d", i)
		}
	}
}

func TestMessage_CooldownThenRefire(t *testing.T) {
	opts := Options{Window: 5 * time.Second, MinMessages: 3, SpikeFactor: 2, Cooldown: 10 * time.Second, BaselineHalfLife: 5 * time.Second}
	d := New(opts)
	start := time.Now()

	burst := func(at time.Time) (fires int) {
		for i := 0; i < 3; i++ {
			if fired, _ := msg(d, "c", i, "hello", at); fired {
				fires++
			}
		}
		return
	}

	if burst(start) != 1 {
		t.Fatalf("first burst should fire once")
	}
	if burst(start.Add(5*time.Second)) != 0 {
		t.Fatalf("burst within cooldown must not fire")
	}
	if burst(start.Add(11*time.Second)) != 1 {
		t.Fatalf("burst after cooldown should fire again")
	}
}

func TestKeyword_FiresOnDistinctUsers(t *testing.T) {
	d := New(Options{KeywordThreshold: 5, SignalWindow: 8 * time.Second, Cooldown: 90 * time.Second, MinMessages: 100})
	start := time.Now()
	for i := 0; i < 4; i++ {
		if fired, _ := msg(d, "c", i, "clip it", start); fired {
			t.Fatalf("fired at %d distinct users, below threshold", i+1)
		}
	}
	fired, reason := msg(d, "c", 5, "clip", start.Add(time.Second))
	if !fired || reason != ReasonKeyword {
		t.Fatalf("want keyword fire at 5 users, got fired=%v reason=%q", fired, reason)
	}
}

func TestKeyword_OneUserSpammingDoesNotFire(t *testing.T) {
	d := New(Options{KeywordThreshold: 5, SignalWindow: 8 * time.Second, MinMessages: 100})
	start := time.Now()
	for i := 0; i < 10; i++ {
		at := start.Add(time.Duration(i) * 100 * time.Millisecond)
		if fired, _ := d.Message("c", "spammer", "clip clip clip", at); fired {
			t.Fatalf("single spamming user fired at iteration %d", i)
		}
	}
}

func TestKeyword_ExcludedAnticipationIgnored(t *testing.T) {
	d := New(Options{KeywordThreshold: 3, SignalWindow: 8 * time.Second, MinMessages: 100})
	start := time.Now()
	for i := 0; i < 5; i++ {
		if fired, _ := msg(d, "c", i, "clip incoming", start); fired {
			t.Fatalf("excluded anticipation message fired at %d", i)
		}
	}
	for i := 5; i < 10; i++ {
		if fired, _ := msg(d, "c", i, "clip?", start); fired {
			t.Fatalf("question-marked message fired at %d", i)
		}
	}
}

func TestEmote_BurstFiresOnDistinctUsers(t *testing.T) {
	d := New(Options{EmoteThreshold: 6, SignalWindow: 8 * time.Second, Cooldown: 90 * time.Second, MinMessages: 100})
	start := time.Now()
	for i := 0; i < 5; i++ {
		if fired, _ := msg(d, "c", i, "KEKW", start); fired {
			t.Fatalf("fired at %d emote users, below threshold", i+1)
		}
	}
	fired, reason := msg(d, "c", 6, "KEKW that was funny", start.Add(time.Second))
	if !fired || reason != ReasonEmote {
		t.Fatalf("want emote fire at 6 users, got fired=%v reason=%q", fired, reason)
	}
}

func TestEmote_DifferentEmotesDoNotCombine(t *testing.T) {
	d := New(Options{EmoteThreshold: 4, SignalWindow: 8 * time.Second, MinMessages: 100})
	start := time.Now()
	msg(d, "c", 1, "pog", start)
	msg(d, "c", 2, "pog", start)
	msg(d, "c", 3, "lul", start)
	if fired, _ := msg(d, "c", 4, "lul", start); fired {
		t.Fatalf("distinct emotes must not combine into one burst")
	}
}

func TestCopypasta_FiresOnDistinctUsers(t *testing.T) {
	d := New(Options{CopypastaThreshold: 5, SignalWindow: 8 * time.Second, Cooldown: 90 * time.Second, MinMessages: 100})
	start := time.Now()
	const phrase = "this is a copypasta moment"
	for i := 0; i < 4; i++ {
		if fired, _ := msg(d, "c", i, phrase, start); fired {
			t.Fatalf("fired at %d, below copypasta threshold", i+1)
		}
	}
	fired, reason := msg(d, "c", 5, phrase, start.Add(time.Second))
	if !fired || reason != ReasonCopypasta {
		t.Fatalf("want copypasta fire, got fired=%v reason=%q", fired, reason)
	}
}

func TestComposite_WeakSignalsCombine(t *testing.T) {
	opts := Options{
		Window: 10 * time.Second, MinMessages: 6, SpikeFactor: 3,
		BaselineHalfLife: 2 * time.Minute, Cooldown: 90 * time.Second,
		SignalWindow:     8 * time.Second,
		KeywordThreshold: 6, EmoteThreshold: 6, CopypastaThreshold: 6,
		CompositeThreshold: 0.55,
		WeightRate:         0.3, WeightKeyword: 0.4, WeightEmote: 0.3,
	}
	d := New(opts)
	start := time.Now()
	for i := 0; i < 3; i++ {
		d.Message("c", fmt.Sprintf("base%d", i), "hi", start.Add(time.Duration(i)*4*time.Second))
	}
	at := start.Add(20 * time.Second)
	fired := false
	var lastReason Reason
	for i := 0; i < 10; i++ {
		text := "hello"
		if i < 3 {
			text = "clip"
		} else if i < 6 {
			text = "pog"
		}
		f, r := d.Message("c", fmt.Sprintf("burst%d", i), text, at.Add(time.Duration(i)*100*time.Millisecond))
		if f {
			fired = true
			lastReason = r
			break
		}
	}
	if !fired {
		t.Fatalf("expected a fire from combined weak signals")
	}
	if lastReason != ReasonComposite && lastReason != ReasonChatSpike {
		t.Fatalf("unexpected reason %q", lastReason)
	}
}

func TestRaid_FiresImmediately(t *testing.T) {
	d := New(Options{Cooldown: 90 * time.Second})
	start := time.Now()
	fired, reason := d.Raid("c", 50, start)
	if !fired || reason != ReasonRaid {
		t.Fatalf("raid should fire: fired=%v reason=%q", fired, reason)
	}
	if fired, _ := d.Raid("c", 50, start.Add(time.Second)); fired {
		t.Fatalf("raid within cooldown must not fire")
	}
}

func TestRaid_ZeroViewersNoFire(t *testing.T) {
	d := New(DefaultOptions())
	if fired, _ := d.Raid("c", 0, time.Now()); fired {
		t.Fatalf("raid with zero viewers must not fire")
	}
}

func TestSub_BurstFires(t *testing.T) {
	d := New(Options{Window: 30 * time.Second, Cooldown: 90 * time.Second, SubBoost: 5})
	start := time.Now()
	if fired, _ := d.Sub("c", start); fired {
		t.Fatalf("single sub should not fire")
	}
	fired, reason := d.Sub("c", start.Add(2*time.Second))
	if !fired || reason != ReasonSubBurst {
		t.Fatalf("two subs in window should fire sub-burst: fired=%v reason=%q", fired, reason)
	}
}

func TestMessage_SustainedHighRateStopsFiring(t *testing.T) {
	opts := Options{Window: 10 * time.Second, MinMessages: 5, SpikeFactor: 3, Cooldown: 1 * time.Second, BaselineHalfLife: 5 * time.Second,
		KeywordThreshold: 100, EmoteThreshold: 100, CopypastaThreshold: 100, CompositeThreshold: 2}
	d := New(opts)
	start := time.Now()
	fires := 0
	for sec := 0; sec < 60; sec++ {
		for m := 0; m < 4; m++ {
			at := start.Add(time.Duration(sec)*time.Second + time.Duration(m)*250*time.Millisecond)
			if fired, _ := d.Message("c", fmt.Sprintf("u%d", sec*4+m), "spam text here", at); fired {
				fires++
			}
		}
	}
	if fires == 0 {
		t.Fatalf("expected at least one initial fire")
	}
	if fires > 5 {
		t.Fatalf("sustained rate kept firing %d times; baseline should adapt", fires)
	}
}

func TestChannelsIndependent(t *testing.T) {
	d := New(Options{KeywordThreshold: 3, SignalWindow: 8 * time.Second, Cooldown: 90 * time.Second, MinMessages: 100})
	start := time.Now()
	for i := 0; i < 3; i++ {
		msg(d, "a", i, "clip", start)
	}
	if fired, _ := msg(d, "b", 1, "clip", start); fired {
		t.Fatalf("channel b should be independent and not fire")
	}
}
