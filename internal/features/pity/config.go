package pity

import (
	"errors"
	"fmt"
	"time"
)

// Config controls the behaviour of a [System].
//
// Defaults are provided by [DefaultConfig]. Always call [Config.Validate] before
// passing a Config to [New] - invalid values are rejected with descriptive
// errors rather than panicking.
type Config struct {
	// PointsPerMessage is the canonical grant size for a chat message.
	// Callers may override per call; this value is exposed so external
	// integrations have a default to fall back to.
	PointsPerMessage int

	// HardPityThreshold is the point total at which the next roll is a
	// guaranteed win. Must be > 0.
	HardPityThreshold int

	// SoftPityFraction is the fraction of HardPityThreshold at which the
	// effective win chance begins to interpolate toward 1.0. Must be in (0, 1).
	SoftPityFraction float64

	// BaseWinChance is the baseline probability of winning a roll before
	// soft-pity kicks in. Must be in [0, 1].
	BaseWinChance float64

	// MaxPointsPerWindow caps the total points a single viewer may accumulate
	// within WindowDuration. Set to 0 to disable rate limiting.
	MaxPointsPerWindow int

	// WindowDuration is the rolling window used for rate limiting. Must be > 0
	// whenever MaxPointsPerWindow > 0.
	WindowDuration time.Duration
}

// DefaultConfig returns the production-tuned defaults documented in
// docs/MASTER-VISION.md.
func DefaultConfig() Config {
	return Config{
		PointsPerMessage:   1,
		HardPityThreshold:  100,
		SoftPityFraction:   0.7,
		BaseWinChance:      0.05,
		MaxPointsPerWindow: 60,
		WindowDuration:     time.Hour,
	}
}

// Validate reports the first invariant violation in c. A nil return means c is
// safe to hand to [New].
func (c Config) Validate() error {
	if c.PointsPerMessage < 0 {
		return errors.New("pity: PointsPerMessage must be >= 0")
	}
	if c.HardPityThreshold <= 0 {
		return errors.New("pity: HardPityThreshold must be > 0")
	}
	if c.SoftPityFraction <= 0 || c.SoftPityFraction >= 1 {
		return fmt.Errorf("pity: SoftPityFraction must be in (0, 1), got %v", c.SoftPityFraction)
	}
	if c.BaseWinChance < 0 || c.BaseWinChance > 1 {
		return fmt.Errorf("pity: BaseWinChance must be in [0, 1], got %v", c.BaseWinChance)
	}
	if c.MaxPointsPerWindow < 0 {
		return errors.New("pity: MaxPointsPerWindow must be >= 0")
	}
	if c.MaxPointsPerWindow > 0 && c.WindowDuration <= 0 {
		return errors.New("pity: WindowDuration must be > 0 when MaxPointsPerWindow > 0")
	}
	return nil
}

// SoftPityThreshold is the integer-truncated point total at which soft-pity
// begins.
func (c Config) SoftPityThreshold() int {
	t := int(float64(c.HardPityThreshold) * c.SoftPityFraction)
	if t < 0 {
		return 0
	}
	if t >= c.HardPityThreshold {
		return c.HardPityThreshold - 1
	}
	return t
}
