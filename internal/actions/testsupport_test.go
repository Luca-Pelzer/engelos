package actions

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestStore opens a fresh file-backed SQLite store in a temp dir, mirroring
// the helper used by internal/counters and internal/timers tests.
func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "actions.db") + "?_pragma=busy_timeout(5000)"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// discardLogger returns a logger that drops all output, for quiet tests.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mustJSON marshals v to json.RawMessage, failing the test on error. Use it to
// build plugin config blobs inline.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// sampleRule builds a minimal valid event-kind rule with a single enabled
// log action and no conditions, scoped to ("local", channel).
func sampleRule(channel, name string) Rule {
	return Rule{
		TenantID:    "local",
		Channel:     channel,
		Name:        name,
		Enabled:     true,
		TriggerKind: TriggerEvent,
		Conditions:  ConditionList{Mode: ConditionModeAll},
		Actions: ActionList{
			Actions: []ActionInstance{
				{TypeID: "builtin:log", Enabled: true, Config: json.RawMessage(`{"message":"hi"}`)},
			},
		},
	}
}

// staticSource is an in-memory RuleSource for engine tests: it returns the
// enabled rules it was seeded with for any matching (tenant, channel).
type staticSource struct {
	mu    sync.Mutex
	rules []Rule
}

func (s *staticSource) ListEnabled(_ context.Context, tenantID, channel string) ([]Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Rule
	for _, r := range s.rules {
		if r.Enabled && r.TenantID == tenantID && r.Channel == channel {
			out = append(out, r)
		}
	}
	return out, nil
}

// recordingChat is a ChatSender that records every Send for assertions.
type recordingChat struct {
	mu   sync.Mutex
	sent []sentMessage
}

type sentMessage struct {
	Channel string
	Text    string
}

func (c *recordingChat) Send(channel, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, sentMessage{Channel: channel, Text: text})
	return nil
}

func (c *recordingChat) messages() []sentMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]sentMessage, len(c.sent))
	copy(out, c.sent)
	return out
}
