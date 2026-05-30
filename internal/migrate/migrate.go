package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Source identifies the bot an export came from.
type Source string

const (
	// SourceNightbot is a Nightbot custom-commands export.
	SourceNightbot Source = "nightbot"
	// SourceStreamElements is a StreamElements custom-commands export.
	SourceStreamElements Source = "streamelements"
)

// Role names the minimum role allowed to run an imported command, using
// engelOS's vocabulary. The parsers map each source's own access levels onto
// these four values.
const (
	RoleEveryone    = "everyone"
	RoleSubscriber  = "subscriber"
	RoleModerator   = "moderator"
	RoleBroadcaster = "broadcaster"
)

// defaultCooldown is applied when a source omits a command cooldown.
const defaultCooldown = 5

// ErrEmptyInput is returned when the supplied data has no content to parse.
var ErrEmptyInput = errors.New("migrate: empty input")

// ErrAmbiguousSource is returned by Parse when the source is unspecified and
// the data shape does not clearly identify Nightbot or StreamElements.
var ErrAmbiguousSource = errors.New("migrate: ambiguous source, specify one")

// Command is a neutral imported custom command, ready to persist via the
// engelOS customcommands store.
type Command struct {
	// Name is the command trigger without a leading '!', lowercased.
	Name string
	// Response is the message template the command replies with.
	Response string
	// Cooldown is the per-command cooldown in seconds (>= 0).
	Cooldown int
	// MinRole is one of the Role* constants.
	MinRole string
}

// Timer is a neutral imported timer.
type Timer struct {
	// Name is the timer's identifier.
	Name string
	// Response is the announced message.
	Response string
	// Interval is the seconds between fires.
	Interval int
	// MinLines is the chat lines required between fires; 0 when unknown.
	MinLines int
	// Enabled reports whether the timer is active.
	Enabled bool
}

// Result is the outcome of parsing an export.
type Result struct {
	// Commands are the successfully mapped commands.
	Commands []Command
	// Timers are the successfully mapped timers (Nightbot only, when present).
	Timers []Timer
	// Skipped holds human-readable notes for entries that could not be mapped.
	Skipped []string
}

// Parse converts an export into neutral records. When source is empty it sniffs
// the shape to auto-detect Nightbot vs StreamElements, returning
// ErrAmbiguousSource when it cannot tell. An explicit source skips detection.
func Parse(source Source, data []byte) (Result, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Result{}, ErrEmptyInput
	}
	switch source {
	case SourceNightbot:
		return ParseNightbot(data)
	case SourceStreamElements:
		return ParseStreamElements(data)
	case "":
		detected, err := detectSource(data)
		if err != nil {
			return Result{}, err
		}
		return Parse(detected, data)
	default:
		return Result{}, fmt.Errorf("migrate: unknown source %q", source)
	}
}

// detectSource sniffs distinctive field names to pick a parser. Nightbot uses
// "coolDown"/"userLevel"; StreamElements uses "accessLevel"/"reply". When both
// or neither appear it returns ErrAmbiguousSource.
func detectSource(data []byte) (Source, error) {
	lower := strings.ToLower(string(data))
	nightbot := strings.Contains(lower, "\"userlevel\"") || strings.Contains(lower, "\"cooldown\"") && strings.Contains(lower, "\"message\"")
	streamelements := strings.Contains(lower, "\"accesslevel\"") || strings.Contains(lower, "\"reply\"")
	switch {
	case nightbot && !streamelements:
		return SourceNightbot, nil
	case streamelements && !nightbot:
		return SourceStreamElements, nil
	default:
		return "", ErrAmbiguousSource
	}
}

// dedupe appends c to cmds unless a command with the same name already exists,
// in which case a skip note is returned. It returns the (possibly unchanged)
// slice and a note ("" when added).
func dedupe(cmds []Command, seen map[string]bool, c Command) ([]Command, string) {
	if seen[c.Name] {
		return cmds, fmt.Sprintf("duplicate command %q skipped", c.Name)
	}
	seen[c.Name] = true
	return append(cmds, c), ""
}

// normalizeName lowercases, trims, and strips a single leading '!' from a
// command trigger.
func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "!")
	return strings.ToLower(strings.TrimSpace(s))
}

// decodeArrayOrWrapped unmarshals data that is either a top-level JSON array of
// objects or an object wrapping the array under one of wrapKeys. It returns the
// raw element messages.
func decodeArrayOrWrapped(data []byte, wrapKeys ...string) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, err
	}
	for _, k := range wrapKeys {
		if raw, ok := obj[k]; ok {
			var arr []json.RawMessage
			if err := json.Unmarshal(raw, &arr); err != nil {
				return nil, err
			}
			return arr, nil
		}
	}
	return nil, fmt.Errorf("migrate: no command array found (looked for %s)", strings.Join(wrapKeys, ", "))
}
