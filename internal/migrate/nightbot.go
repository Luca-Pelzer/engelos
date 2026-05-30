package migrate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// nightbotCommand mirrors the fields engelOS needs from a Nightbot custom
// command export entry. Unknown fields are ignored by encoding/json.
type nightbotCommand struct {
	Name      string `json:"name"`
	Message   string `json:"message"`
	CoolDown  int    `json:"coolDown"`
	UserLevel string `json:"userLevel"`
}

// ParseNightbot parses a Nightbot custom-commands export (a top-level array, or
// an object wrapping the array under "commands"/"customCommands"). Entries
// missing a name or message are skipped with a note rather than failing the
// whole parse.
func ParseNightbot(data []byte) (Result, error) {
	raws, err := decodeArrayOrWrapped(data, "commands", "customCommands")
	if err != nil {
		return Result{}, fmt.Errorf("migrate: nightbot: %w", err)
	}
	var res Result
	seen := make(map[string]bool)
	for i, raw := range raws {
		var nc nightbotCommand
		if err := json.Unmarshal(raw, &nc); err != nil {
			res.Skipped = append(res.Skipped, fmt.Sprintf("entry %d: invalid command object", i))
			continue
		}
		name := normalizeName(nc.Name)
		response := strings.TrimSpace(nc.Message)
		if name == "" || response == "" {
			res.Skipped = append(res.Skipped, fmt.Sprintf("entry %d (%q): missing name or message", i, nc.Name))
			continue
		}
		cooldown := nc.CoolDown
		if cooldown <= 0 {
			cooldown = defaultCooldown
		}
		cmd := Command{
			Name:     name,
			Response: response,
			Cooldown: cooldown,
			MinRole:  nightbotRole(nc.UserLevel),
		}
		var note string
		res.Commands, note = dedupe(res.Commands, seen, cmd)
		if note != "" {
			res.Skipped = append(res.Skipped, note)
		}
	}
	return res, nil
}

// nightbotRole maps a Nightbot userLevel onto an engelOS role. Unknown values
// default to everyone.
func nightbotRole(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "owner", "broadcaster":
		return RoleBroadcaster
	case "moderator":
		return RoleModerator
	case "subscriber", "regular":
		return RoleSubscriber
	default:
		return RoleEveryone
	}
}
