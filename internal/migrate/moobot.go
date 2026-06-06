package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Moobot (moo.bot) exports the whole dashboard via "Tools -> Import & Export ->
// Export this dashboard to a file". Moobot publishes no schema for that file, no
// public API, and no community parser exists, so the exact JSON keys are not
// authoritatively known. This parser is therefore deliberately tolerant: for
// every logical field it accepts the most likely key spellings taken from
// Moobot's own dashboard vocabulary, and it skips entries it cannot map instead
// of failing the whole import. When nothing at all maps it returns an error
// rather than a silent empty success, so a wrong file is never mistaken for a
// successful import. The role mapping is conservative (unknown access ->
// everyone) because Moobot's access model is a user-configured, now-deprecated
// numeric "Tier" system rather than a fixed set of named roles. Verify against a
// real export before trusting it for a production migration.

// Candidate JSON keys per logical field, most-likely first; matched
// case-insensitively, so only spelling variants are listed.
var (
	moobotNameKeys     = []string{"name", "command", "commandName", "trigger"}
	moobotResponseKeys = []string{"response", "message", "reply", "text"}
	moobotCooldownKeys = []string{"cooldown", "coolDown", "cooldownSeconds"}
	moobotAccessKeys   = []string{"userLevel", "accessLevel", "access", "tier", "minUserLevel", "restriction"}

	moobotTimerNameKeys     = []string{"description", "name", "title"}
	moobotTimerIntervalKeys = []string{"minutesBetweenPosts", "minutes", "interval"}
	moobotTimerLineKeys     = []string{"chatLinesBetweenPosts", "minChatLines", "chatLines", "lines"}
	moobotTimerEnabledKeys  = []string{"enabled", "active"}
	moobotTimerDisabledKeys = []string{"disabled"}
)

// ParseMoobot parses a Moobot dashboard export. Commands are read from a
// top-level array or from a "commands"/"customCommands" wrapper; timers from a
// "timers" wrapper when present. Entries missing a usable name or response are
// skipped with a note. If neither a command nor a timer can be mapped it returns
// an error, so an unrecognised file fails loudly instead of importing nothing.
func ParseMoobot(data []byte) (Result, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Result{}, ErrEmptyInput
	}
	var res Result
	seen := make(map[string]bool)

	// Commands are best-effort: a timers-only export has no command array, which
	// is not an error on its own, so a decode failure here is recorded and we
	// still try the timers section.
	if cmdRaws, err := decodeArrayOrWrapped(data, "commands", "customCommands"); err == nil {
		for i, raw := range cmdRaws {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				res.Skipped = append(res.Skipped, fmt.Sprintf("entry %d: invalid command object", i))
				continue
			}
			name := normalizeName(firstString(obj, moobotNameKeys))
			response := strings.TrimSpace(firstString(obj, moobotResponseKeys))
			if name == "" || response == "" {
				res.Skipped = append(res.Skipped, fmt.Sprintf("entry %d: missing name or response", i))
				continue
			}
			cooldown := firstInt(obj, moobotCooldownKeys)
			if cooldown <= 0 {
				cooldown = defaultCooldown
			}
			cmd := Command{
				Name:     name,
				Response: response,
				Cooldown: cooldown,
				MinRole:  moobotRole(firstRaw(obj, moobotAccessKeys)),
			}
			var note string
			res.Commands, note = dedupe(res.Commands, seen, cmd)
			if note != "" {
				res.Skipped = append(res.Skipped, note)
			}
		}
	}

	timers, timerNotes := parseMoobotTimers(data)
	res.Timers = timers
	res.Skipped = append(res.Skipped, timerNotes...)

	if len(res.Commands) == 0 && len(res.Timers) == 0 {
		return Result{}, fmt.Errorf("migrate: moobot: no commands or timers found")
	}
	return res, nil
}

// parseMoobotTimers extracts timers from a "timers" wrapper. engelOS timers
// carry a literal message, whereas a Moobot timer can instead reference commands
// by name; only timers that carry an inline message and a positive interval are
// importable, the rest are skipped with a note. Moobot configures the cadence in
// minutes, which is converted to the seconds engelOS stores.
func parseMoobotTimers(data []byte) ([]Timer, []string) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &obj); err != nil {
		return nil, nil
	}
	raw, ok := obj["timers"]
	if !ok {
		return nil, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(raw, &raws); err != nil {
		return nil, nil
	}
	var (
		out     []Timer
		skipped []string
	)
	for i, tr := range raws {
		var t map[string]json.RawMessage
		if err := json.Unmarshal(tr, &t); err != nil {
			skipped = append(skipped, fmt.Sprintf("timer %d: invalid object", i))
			continue
		}
		name := strings.TrimSpace(firstString(t, moobotTimerNameKeys))
		message := strings.TrimSpace(firstString(t, moobotResponseKeys))
		if name == "" || message == "" {
			skipped = append(skipped, fmt.Sprintf("timer %d: no inline message (command-list timers are not importable)", i))
			continue
		}
		minutes := firstInt(t, moobotTimerIntervalKeys)
		if minutes <= 0 {
			skipped = append(skipped, fmt.Sprintf("timer %q: missing or non-positive interval", name))
			continue
		}
		out = append(out, Timer{
			Name:     name,
			Response: message,
			Interval: minutes * 60,
			MinLines: firstInt(t, moobotTimerLineKeys),
			Enabled:  moobotTimerEnabled(t),
		})
	}
	return out, skipped
}

// moobotTimerEnabled reads an explicit enabled/active flag, falls back to an
// inverted disabled flag, and defaults to enabled when neither is present.
func moobotTimerEnabled(t map[string]json.RawMessage) bool {
	if v, ok := firstBool(t, moobotTimerEnabledKeys); ok {
		return v
	}
	if v, ok := firstBool(t, moobotTimerDisabledKeys); ok {
		return !v
	}
	return true
}

// moobotRole maps a Moobot access value onto an engelOS role. The value may be a
// string keyword or a numeric tier. Numeric tiers are user-configured and carry
// no fixed meaning, so they map to everyone; unknown strings do too.
func moobotRole(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return RoleEveryone
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch level := strings.ToLower(strings.TrimSpace(s)); {
		case level == "":
			return RoleEveryone
		case strings.Contains(level, "owner"),
			strings.Contains(level, "broadcaster"),
			strings.Contains(level, "streamer"):
			return RoleBroadcaster
		case strings.Contains(level, "mod"):
			return RoleModerator
		case strings.Contains(level, "sub"),
			strings.Contains(level, "vip"),
			strings.Contains(level, "regular"):
			return RoleSubscriber
		default:
			return RoleEveryone
		}
	}
	return RoleEveryone
}

// firstRaw returns the first present key's value, preferring an exact match
// over a case-insensitive one.
func firstRaw(obj map[string]json.RawMessage, keys []string) json.RawMessage {
	for _, k := range keys {
		if v, ok := obj[k]; ok {
			return v
		}
	}
	lower := make(map[string]json.RawMessage, len(obj))
	for k, v := range obj {
		lower[strings.ToLower(k)] = v
	}
	for _, k := range keys {
		if v, ok := lower[strings.ToLower(k)]; ok {
			return v
		}
	}
	return nil
}

func firstString(obj map[string]json.RawMessage, keys []string) string {
	raw := firstRaw(obj, keys)
	if raw == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// firstInt decodes the first matching key as an integer, tolerating a quoted
// number like "30".
func firstInt(obj map[string]json.RawMessage, keys []string) int {
	raw := firstRaw(obj, keys)
	if raw == nil {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return v
		}
	}
	return 0
}

func firstBool(obj map[string]json.RawMessage, keys []string) (value bool, found bool) {
	raw := firstRaw(obj, keys)
	if raw == nil {
		return false, false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, true
	}
	return false, false
}
