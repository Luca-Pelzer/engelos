package streak

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
}

// StreakContinuedPayload is the JSON body of [EventTypeStreakContinued].
type StreakContinuedPayload struct {
	Channel       string `json:"channel"`
	ViewerID      string `json:"viewer_id"`
	Username      string `json:"username"`
	DaysCurrent   int    `json:"days_current"`
	DaysLongest   int    `json:"days_longest"`
	SameDayReTick bool   `json:"same_day_retick"`
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
