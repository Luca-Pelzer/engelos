package streak

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Config controls the behaviour of a [System].
//
// Defaults are provided by [DefaultConfig]. Always call [Config.Validate]
// before passing a Config to [New] - invalid values are rejected with
// descriptive errors rather than panicking.
type Config struct {
	// FreezeMilestones maps streak length to the number of freeze credits
	// awarded when the viewer crosses it for the first time. Common values:
	//   { 7: 1, 30: 3, 100: 7, 365: 30 }
	FreezeMilestones map[int]int

	// MaxFreezesHeld caps how many freeze credits a viewer can stockpile.
	// Must be > 0.
	MaxFreezesHeld int

	// GraceWindow is the duration after midnight UTC during which a Tick
	// still counts for the previous day (handles streamers who go past
	// midnight). Must be < 24h, may be 0 to disable.
	GraceWindow time.Duration

	// Logger receives operational events. Defaults to slog.Default() when
	// nil is passed to [New].
	Logger *slog.Logger
}

// DefaultConfig returns the production-tuned defaults documented in
// docs/MASTER-VISION.md.
func DefaultConfig() Config {
	return Config{
		FreezeMilestones: map[int]int{
			7:   1,
			30:  3,
			100: 7,
			365: 30,
		},
		MaxFreezesHeld: 30,
		GraceWindow:    6 * time.Hour,
	}
}

// Validate reports the first invariant violation in c. A nil return means c
// is safe to hand to [New].
func (c Config) Validate() error {
	if c.MaxFreezesHeld <= 0 {
		return errors.New("streak: MaxFreezesHeld must be > 0")
	}
	if c.GraceWindow < 0 {
		return fmt.Errorf("streak: GraceWindow must be >= 0, got %v", c.GraceWindow)
	}
	if c.GraceWindow >= 24*time.Hour {
		return fmt.Errorf("streak: GraceWindow must be < 24h, got %v", c.GraceWindow)
	}
	for days, award := range c.FreezeMilestones {
		if days <= 0 {
			return fmt.Errorf("streak: FreezeMilestones key must be > 0, got %d", days)
		}
		if award < 0 {
			return fmt.Errorf("streak: FreezeMilestones[%d] award must be >= 0, got %d", days, award)
		}
	}
	return nil
}
