// Package streamstate tracks per-channel live/offline state in memory, fed from
// platform stream.online/stream.offline events, so a workflow condition can ask
// whether a channel is currently live without a per-evaluation API call.
package streamstate

import (
	"strings"
	"sync"
)

// Tracker is a concurrency-safe per-channel live-state flag. Construct with New.
// Channel keys are normalised (trimmed, lower-cased, leading '#' stripped) so a
// rule's channel and an event's broadcaster login resolve to the same entry.
type Tracker struct {
	mu   sync.RWMutex
	live map[string]bool
}

// New returns an empty Tracker; every channel reads as offline until a
// stream.online event marks it live.
func New() *Tracker {
	return &Tracker{live: make(map[string]bool)}
}

// Set records whether channel is currently live. An empty channel is ignored.
func (t *Tracker) Set(channel string, live bool) {
	key := normalize(channel)
	if key == "" {
		return
	}
	t.mu.Lock()
	t.live[key] = live
	t.mu.Unlock()
}

// IsLive reports whether channel is currently marked live.
func (t *Tracker) IsLive(channel string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.live[normalize(channel)]
}

func normalize(channel string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(channel)), "#")
}
