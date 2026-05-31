package clipper

import (
	"fmt"
	"testing"
	"time"
)

func TestDefaultSettings_ReproducesDefaultOptions(t *testing.T) {
	d := DefaultOptions()
	s := DefaultSettings()
	if !s.Enabled {
		t.Fatalf("default settings should be enabled")
	}
	if s.KeywordThreshold != d.KeywordThreshold {
		t.Fatalf("keyword: got %d want %d", s.KeywordThreshold, d.KeywordThreshold)
	}
	if s.EmoteThreshold != d.EmoteThreshold {
		t.Fatalf("emote: got %d want %d", s.EmoteThreshold, d.EmoteThreshold)
	}
	if s.CopypastaThreshold != d.CopypastaThreshold {
		t.Fatalf("copypasta: got %d want %d", s.CopypastaThreshold, d.CopypastaThreshold)
	}
	if s.MinMessages != d.MinMessages {
		t.Fatalf("min messages: got %d want %d", s.MinMessages, d.MinMessages)
	}
	if s.SpikeFactor != d.SpikeFactor {
		t.Fatalf("spike: got %v want %v", s.SpikeFactor, d.SpikeFactor)
	}
	if s.CompositeThreshold != d.CompositeThreshold {
		t.Fatalf("composite: got %v want %v", s.CompositeThreshold, d.CompositeThreshold)
	}
	if want := int(d.Cooldown / time.Second); s.CooldownSeconds != want {
		t.Fatalf("cooldown: got %d want %d", s.CooldownSeconds, want)
	}
}

func TestApplyTo_ZeroFieldsInherit(t *testing.T) {
	base := DefaultOptions()
	// An all-zero Settings (other than Enabled) must leave base untouched.
	out := Settings{Enabled: true}.ApplyTo(base)
	if out.KeywordThreshold != base.KeywordThreshold ||
		out.EmoteThreshold != base.EmoteThreshold ||
		out.CopypastaThreshold != base.CopypastaThreshold ||
		out.MinMessages != base.MinMessages ||
		out.SpikeFactor != base.SpikeFactor ||
		out.CompositeThreshold != base.CompositeThreshold ||
		out.Cooldown != base.Cooldown {
		t.Fatalf("zero settings changed base options: %+v vs %+v", out, base)
	}
}

func TestApplyTo_PositiveFieldsOverride(t *testing.T) {
	base := DefaultOptions()
	s := Settings{
		Enabled:            true,
		KeywordThreshold:   3,
		EmoteThreshold:     4,
		CopypastaThreshold: 2,
		MinMessages:        5,
		SpikeFactor:        2.5,
		CompositeThreshold: 0.5,
		CooldownSeconds:    45,
	}
	out := s.ApplyTo(base)
	if out.KeywordThreshold != 3 {
		t.Fatalf("keyword override: got %d", out.KeywordThreshold)
	}
	if out.EmoteThreshold != 4 {
		t.Fatalf("emote override: got %d", out.EmoteThreshold)
	}
	if out.CopypastaThreshold != 2 {
		t.Fatalf("copypasta override: got %d", out.CopypastaThreshold)
	}
	if out.MinMessages != 5 {
		t.Fatalf("min messages override: got %d", out.MinMessages)
	}
	if out.SpikeFactor != 2.5 {
		t.Fatalf("spike override: got %v", out.SpikeFactor)
	}
	if out.CompositeThreshold != 0.5 {
		t.Fatalf("composite override: got %v", out.CompositeThreshold)
	}
	if out.Cooldown != 45*time.Second {
		t.Fatalf("cooldown override: got %v", out.Cooldown)
	}
	// Fields NOT in Settings must be preserved from base.
	if out.Window != base.Window || out.BaselineHalfLife != base.BaselineHalfLife {
		t.Fatalf("non-settings fields were not preserved")
	}
}

// TestApplyTo_LowerThresholdMakesSmallChannelFire is the core motivation: a
// small channel that never reaches the default 5 distinct clip-typers should
// fire once its threshold is lowered to 3.
func TestApplyTo_LowerThresholdMakesSmallChannelFire(t *testing.T) {
	base := DefaultOptions()
	lowered := Settings{Enabled: true, KeywordThreshold: 3}.ApplyTo(base)
	d := New(lowered)
	start := time.Now()
	// Only 3 distinct users type clip; default threshold (5) would not fire.
	var fired bool
	var reason Reason
	for i := 0; i < 3; i++ {
		f, rs := d.Message("smallch", fmt.Sprintf("user-%d", i), "clip it", start.Add(time.Duration(i)*time.Second))
		if f {
			fired, reason = f, rs
		}
	}
	if !fired || reason != ReasonKeyword {
		t.Fatalf("lowered-threshold small channel should fire keyword at 3 users, got fired=%v reason=%q", fired, reason)
	}
}
