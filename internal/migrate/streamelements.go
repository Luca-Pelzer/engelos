package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// seCooldown decodes StreamElements' cooldown field, which appears either as a
// plain number of seconds or as an object {"user":N,"global":M}. The larger of
// user/global is used as the effective cooldown.
type seCooldown int

// UnmarshalJSON accepts both the number and the {user,global} object forms.
func (c *seCooldown) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*c = 0
		return nil
	}
	if b[0] == '{' {
		var obj struct {
			User   int `json:"user"`
			Global int `json:"global"`
		}
		if err := json.Unmarshal(b, &obj); err != nil {
			return err
		}
		if obj.Global > obj.User {
			*c = seCooldown(obj.Global)
		} else {
			*c = seCooldown(obj.User)
		}
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*c = seCooldown(n)
	return nil
}

// streamElementsCommand mirrors the fields engelOS needs from a StreamElements
// custom command export entry.
type streamElementsCommand struct {
	Command     string     `json:"command"`
	Reply       string     `json:"reply"`
	Cooldown    seCooldown `json:"cooldown"`
	AccessLevel int        `json:"accessLevel"`
}

// ParseStreamElements parses a StreamElements custom-commands export (a
// top-level array, or an object wrapping the array under "commands"). Entries
// missing a command or reply are skipped with a note.
func ParseStreamElements(data []byte) (Result, error) {
	raws, err := decodeArrayOrWrapped(data, "commands")
	if err != nil {
		return Result{}, fmt.Errorf("migrate: streamelements: %w", err)
	}
	var res Result
	seen := make(map[string]bool)
	for i, raw := range raws {
		var sc streamElementsCommand
		if err := json.Unmarshal(raw, &sc); err != nil {
			res.Skipped = append(res.Skipped, fmt.Sprintf("entry %d: invalid command object", i))
			continue
		}
		name := normalizeName(sc.Command)
		response := strings.TrimSpace(sc.Reply)
		if name == "" || response == "" {
			res.Skipped = append(res.Skipped, fmt.Sprintf("entry %d (%q): missing command or reply", i, sc.Command))
			continue
		}
		cooldown := int(sc.Cooldown)
		if cooldown <= 0 {
			cooldown = defaultCooldown
		}
		cmd := Command{
			Name:     name,
			Response: response,
			Cooldown: cooldown,
			MinRole:  streamElementsRole(sc.AccessLevel),
		}
		var note string
		res.Commands, note = dedupe(res.Commands, seen, cmd)
		if note != "" {
			res.Skipped = append(res.Skipped, note)
		}
	}
	return res, nil
}

// streamElementsRole maps a StreamElements numeric accessLevel onto an engelOS
// role. The platform uses 100 (everyone), 250 (subscriber), 500 (moderator),
// 1000 (broadcaster); unknown values default to everyone.
func streamElementsRole(level int) string {
	switch {
	case level >= 1000:
		return RoleBroadcaster
	case level >= 500:
		return RoleModerator
	case level >= 250:
		return RoleSubscriber
	default:
		return RoleEveryone
	}
}
