package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/customcommands"
	"github.com/Luca-Pelzer/engelos/internal/migrate"
	"github.com/Luca-Pelzer/engelos/internal/timers"
)

// Migrate exposes a one-shot import of Nightbot/StreamElements command and
// timer exports into the channel's engelOS stores. It is session-protected at
// the router layer. When the command store is nil the endpoint returns 501 so
// the router still boots with the feature off.
type Migrate struct {
	commands customcommands.Store
	timers   timers.Store
	tenantID string
	logger   *slog.Logger
}

// NewMigrate constructs the handler bundle.
func NewMigrate(commandStore customcommands.Store, timerStore timers.Store, tenantID string, logger *slog.Logger) *Migrate {
	if logger == nil {
		logger = slog.Default()
	}
	return &Migrate{commands: commandStore, timers: timerStore, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// Import handles POST /api/v1/migrate.
// Body: {channel, source?, data}. source is "nightbot", "streamelements", or
// empty for auto-detect; data is the raw JSON export as a string. It parses the
// export and creates the resulting commands (and timers, when present),
// returning counts plus any skipped-entry notes.
func (h *Migrate) Import(w http.ResponseWriter, r *http.Request) {
	if h.commands == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "migration_not_enabled"})
		return
	}
	var req struct {
		Channel string `json:"channel"`
		Source  string `json:"source"`
		Data    string `json:"data"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if strings.TrimSpace(req.Data) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "data is required"})
		return
	}

	result, err := migrate.Parse(migrate.Source(strings.ToLower(strings.TrimSpace(req.Source))), []byte(req.Data))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	imported, failed := h.persistCommands(r, channel, result.Commands)
	skipped := append([]string{}, result.Skipped...)
	skipped = append(skipped, failed...)

	timersImported, timersFailed := h.persistTimers(r, channel, result.Timers)
	skipped = append(skipped, timersFailed...)

	writeJSON(w, http.StatusOK, map[string]any{
		"channel":           channel,
		"commands_imported": imported,
		"timers_imported":   timersImported,
		"skipped":           skipped,
	})
}

// decode reads a JSON body up to 1MB (command exports can be large) into dst,
// rejecting unknown fields.
func (h *Migrate) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

// persistCommands creates each parsed command, collecting notes for any that
// fail to persist. It returns the count created and the failure notes.
func (h *Migrate) persistCommands(r *http.Request, channel string, cmds []migrate.Command) (int, []string) {
	var imported int
	var failed []string
	for _, c := range cmds {
		_, err := h.commands.Create(r.Context(), customcommands.CustomCommand{
			TenantID:  h.tenantID,
			Channel:   channel,
			Name:      c.Name,
			Response:  c.Response,
			MinRole:   c.MinRole,
			CreatedBy: "import",
		})
		if err != nil {
			h.logger.WarnContext(r.Context(), "migrate: command create failed",
				slog.String("name", c.Name), slog.Any("err", err))
			failed = append(failed, "command "+c.Name+": "+err.Error())
			continue
		}
		imported++
	}
	return imported, failed
}

// persistTimers creates each parsed timer when a timer store is configured.
func (h *Migrate) persistTimers(r *http.Request, channel string, ts []migrate.Timer) (int, []string) {
	if h.timers == nil {
		return 0, nil
	}
	var imported int
	var failed []string
	for _, t := range ts {
		_, err := h.timers.Create(r.Context(), timers.Timer{
			TenantID:     h.tenantID,
			Channel:      channel,
			Name:         t.Name,
			Message:      t.Response,
			Interval:     time.Duration(t.Interval) * time.Second,
			MinChatLines: t.MinLines,
			Enabled:      t.Enabled,
			CreatedBy:    "import",
		})
		if err != nil {
			h.logger.WarnContext(r.Context(), "migrate: timer create failed",
				slog.String("name", t.Name), slog.Any("err", err))
			failed = append(failed, "timer "+t.Name+": "+err.Error())
			continue
		}
		imported++
	}
	return imported, failed
}
