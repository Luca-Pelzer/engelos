package pity

// Event type constants emitted by [System]. Each value is part of the public
// wire format and MUST NOT change without a versioning migration.
const (
	EventTypePointsGranted = "pity.points.granted"
	EventTypeRollMade      = "pity.roll.made"
	EventTypeWinGuaranteed = "pity.win.guaranteed"
	EventTypeWinNatural    = "pity.win.natural"
	EventTypeReset         = "pity.reset"
)

// PointsGrantedPayload is the JSON body of [EventTypePointsGranted].
type PointsGrantedPayload struct {
	Channel  string `json:"channel"`
	ViewerID string `json:"viewer_id"`
	Username string `json:"username"`
	Amount   int    `json:"amount"`
	NewTotal int    `json:"new_total"`
	Reason   string `json:"reason"`
}

// RollMadePayload is the JSON body of [EventTypeRollMade].
type RollMadePayload struct {
	Channel         string  `json:"channel"`
	ViewerID        string  `json:"viewer_id"`
	Username        string  `json:"username"`
	PointsAtRoll    int     `json:"points_at_roll"`
	EffectiveChance float64 `json:"effective_chance"`
	Won             bool    `json:"won"`
	WasGuaranteed   bool    `json:"was_guaranteed"`
	// RngSeed is recorded only when the System runs against a seeded RNG.
	// Production (crypto/rand) leaves this zero, hence omitempty.
	RngSeed int64 `json:"rng_seed,omitempty"`
}

// WinPayload is the JSON body of [EventTypeWinNatural] and
// [EventTypeWinGuaranteed].
type WinPayload struct {
	Channel       string `json:"channel"`
	ViewerID      string `json:"viewer_id"`
	Username      string `json:"username"`
	PointsAtWin   int    `json:"points_at_win"`
	WasGuaranteed bool   `json:"was_guaranteed"`
}

// ResetPayload is the JSON body of [EventTypeReset].
// Reason is one of "win", "admin", or "config-change".
type ResetPayload struct {
	Channel  string `json:"channel"`
	ViewerID string `json:"viewer_id"`
	Reason   string `json:"reason"`
}
