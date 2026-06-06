package streak

import "time"

// Event type constants emitted by [System]. Each value is part of the public
// wire format and MUST NOT change without a versioning migration.
const (
	EventTypeStreakStarted   = "streak.started"
	EventTypeStreakContinued = "streak.continued"
	EventTypeStreakBroken    = "streak.broken"
	EventTypeStreakFrozen    = "streak.frozen"
	EventTypeStreakMilestone = "streak.milestone"
)

// StreakStartedPayload is the JSON body of [EventTypeStreakStarted].
type StreakStartedPayload struct {
	Channel  string `json:"channel"`
	ViewerID string `json:"viewer_id"`
	Username string `json:"username"`

	// TickDayUTC is the grace-adjusted effective UTC day from
	// streakDay(now, GraceWindow). The read model stores it verbatim into
	// State.LastTickDayUTC. Invariant: read and write must compare days on
	// the same grace-adjusted scale, so the read model must NOT recompute
	// the day from OccurredAt without the grace window.
	TickDayUTC time.Time `json:"tick_day_utc"`
}

// StreakContinuedPayload is the JSON body of [EventTypeStreakContinued].
type StreakContinuedPayload struct {
	Channel       string `json:"channel"`
	ViewerID      string `json:"viewer_id"`
	Username      string `json:"username"`
	DaysCurrent   int    `json:"days_current"`
	DaysLongest   int    `json:"days_longest"`
	SameDayReTick bool   `json:"same_day_retick"`

	// TickDayUTC carries the grace-adjusted day for non-same-day ticks. See
	// StreakStartedPayload.TickDayUTC for the day-scale invariant.
	TickDayUTC time.Time `json:"tick_day_utc"`
}

// StreakBrokenPayload is the JSON body of [EventTypeStreakBroken].
type StreakBrokenPayload struct {
	Channel     string `json:"channel"`
	ViewerID    string `json:"viewer_id"`
	Username    string `json:"username"`
	DaysAtBreak int    `json:"days_at_break"`
	MissedDays  int    `json:"missed_days"`
}

// StreakFrozenPayload is the JSON body of [EventTypeStreakFrozen].
type StreakFrozenPayload struct {
	Channel       string `json:"channel"`
	ViewerID      string `json:"viewer_id"`
	Username      string `json:"username"`
	DaysBridged   int    `json:"days_bridged"`
	FreezesSpent  int    `json:"freezes_spent"`
	FreezesRemain int    `json:"freezes_remain"`
	DaysCurrent   int    `json:"days_current"`

	// TickDayUTC carries the grace-adjusted day when the freeze bridges to a
	// new day (Tick path). Zero for a manual UseFreeze, which holds the day
	// in place. See StreakStartedPayload.TickDayUTC for the invariant.
	TickDayUTC time.Time `json:"tick_day_utc"`
}

// StreakMilestonePayload is the JSON body of [EventTypeStreakMilestone].
type StreakMilestonePayload struct {
	Channel        string `json:"channel"`
	ViewerID       string `json:"viewer_id"`
	Username       string `json:"username"`
	Milestone      int    `json:"milestone"`
	FreezesAwarded int    `json:"freezes_awarded"`
	FreezesTotal   int    `json:"freezes_total"`
}
