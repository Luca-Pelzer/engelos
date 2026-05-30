package clipper

import (
	"testing"
	"time"
)

func TestMessage_NoFireBelowMinMessages(t *testing.T) {
	d := New(Options{Window: 10 * time.Second, MinMessages: 8, SpikeFactor: 3})
	start := time.Now()
	for i := 0; i < 7; i++ {
		fired, _ := d.Message("c", start)
		if fired {
			t.Fatalf("fired at message %d, below MinMessages", i+1)
		}
	}
}

func TestMessage_ChatSpikeFiresOnce(t *testing.T) {
	d := New(Options{Window: 10 * time.Second, MinMessages: 8, SpikeFactor: 3, Cooldown: 90 * time.Second})
	start := time.Now()
	fires := 0
	for i := 0; i < 15; i++ {
		fired, reason := d.Message("c", start)
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
		if fired, _ := d.Message("c", at); fired {
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
			if fired, _ := d.Message("c", at); fired {
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

func TestRaid_FiresImmediately(t *testing.T) {
	d := New(Options{Cooldown: 90 * time.Second})
	start := time.Now()
	fired, reason := d.Raid("c", 50, start)
	if !fired || reason != ReasonRaid {
		t.Fatalf("raid should fire: fired=%v reason=%q", fired, reason)
	}
	// Second raid within cooldown does not fire.
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
	opts := Options{Window: 10 * time.Second, MinMessages: 5, SpikeFactor: 3, Cooldown: 1 * time.Second, BaselineHalfLife: 5 * time.Second}
	d := New(opts)
	start := time.Now()
	fires := 0
	// Sustain a high, steady rate for a long time: 4 messages per second for
	// 60 seconds. After the initial spike the baseline must rise so firing
	// stops (no infinite fires).
	for sec := 0; sec < 60; sec++ {
		for m := 0; m < 4; m++ {
			at := start.Add(time.Duration(sec)*time.Second + time.Duration(m)*250*time.Millisecond)
			if fired, _ := d.Message("c", at); fired {
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
	d := New(Options{Window: 10 * time.Second, MinMessages: 3, SpikeFactor: 2, Cooldown: 90 * time.Second})
	start := time.Now()
	for i := 0; i < 3; i++ {
		d.Message("a", start)
	}
	// Channel b has seen nothing, so a single message must not fire.
	if fired, _ := d.Message("b", start); fired {
		t.Fatalf("channel b should be independent and not fire")
	}
}
