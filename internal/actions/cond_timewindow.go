package actions

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type timeWindowConfig struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Days     []string `json:"days"`
	Timezone string   `json:"timezone"`
}

// timeWindowCondition passes inside a daily [from,to) clock window. now is
// injectable so a test can pin the clock; a nil now uses time.Now.
type timeWindowCondition struct {
	now func() time.Time
}

func (timeWindowCondition) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "cond:time-window",
		Name:        "Within time window",
		Description: "Passes when the current time is within [from,to) (HH:MM, 24h) on the configured days (mon..sun; empty = every day) in the given IANA timezone (default the server's local zone). Windows crossing midnight (e.g. 22:00-02:00) are supported.",
	}
}

func (a timeWindowCondition) Evaluate(ec *ExecutionContext, config json.RawMessage) (bool, error) {
	var c timeWindowConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return false, fmt.Errorf("actions: cond:time-window config: %w", err)
	}
	fromMin, err := parseHHMM(c.From)
	if err != nil {
		return false, fmt.Errorf("actions: cond:time-window: from: %w", err)
	}
	toMin, err := parseHHMM(c.To)
	if err != nil {
		return false, fmt.Errorf("actions: cond:time-window: to: %w", err)
	}
	loc := time.Local
	if tz := strings.TrimSpace(c.Timezone); tz != "" {
		l, lerr := time.LoadLocation(tz)
		if lerr != nil {
			return false, fmt.Errorf("actions: cond:time-window: timezone %q: %w", tz, lerr)
		}
		loc = l
	}
	nowFn := a.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn().In(loc)

	if len(c.Days) > 0 && !dayAllowed(c.Days, now.Weekday()) {
		return false, nil
	}
	if fromMin == toMin {
		return false, nil
	}
	nowMin := now.Hour()*60 + now.Minute()
	if fromMin < toMin {
		return nowMin >= fromMin && nowMin < toMin, nil
	}
	// Crosses midnight: active after from OR before to on the same calendar day.
	return nowMin >= fromMin || nowMin < toMin, nil
}

// parseHHMM parses a 24-hour "HH:MM" string into minutes-since-midnight.
func parseHHMM(s string) (int, error) {
	s = strings.TrimSpace(s)
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	hh, err := strconv.Atoi(h)
	if err != nil || hh < 0 || hh > 23 {
		return 0, fmt.Errorf("invalid hour in %q", s)
	}
	mm, err := strconv.Atoi(m)
	if err != nil || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("invalid minute in %q", s)
	}
	return hh*60 + mm, nil
}

var weekdayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// dayAllowed reports whether wd is named in days (three-letter, case-insensitive).
func dayAllowed(days []string, wd time.Weekday) bool {
	for _, d := range days {
		if w, ok := weekdayNames[strings.ToLower(strings.TrimSpace(d))]; ok && w == wd {
			return true
		}
	}
	return false
}
