package contextmod

import (
	"strings"
	"sync"
	"time"
)

// Context-window safety bounds. These are hard caps independent of the
// configurable message count, so a spam flood can never balloon a prompt.
const (
	// defaultContextSize is the default number of recent messages retained per
	// channel and injected into the classify prompt.
	defaultContextSize = 30
	// maxContextBytes caps the total bytes of retained message text per
	// channel; oldest entries are evicted first to stay under it.
	maxContextBytes = 16 * 1024
	// maxMessageRunes truncates any single stored message so one giant paste
	// cannot dominate the window.
	maxMessageRunes = 500
)

// ctxEntry is one buffered chat line in a channel's rolling window.
type ctxEntry struct {
	Username string
	Text     string
	At       time.Time
}

// contextBuffer holds a fixed-capacity ring of recent chat messages per
// (tenant, channel) to give the AI moderation classifier recent conversation
// context.
//
// PRIVACY: the buffer is in-memory only. Nothing here is ever written to disk
// or the audit log, and the whole buffer is dropped when the daemon restarts.
// It exists solely to hand the classifier a few lines of surrounding chat for
// the message currently under review. It is safe for concurrent use.
type contextBuffer struct {
	capacity int
	mu       sync.Mutex
	byKey    map[string]*channelRing
}

// channelRing is one channel's chronological window plus a running byte total
// for the byte-cap accounting.
type channelRing struct {
	entries    []ctxEntry
	totalBytes int
}

// newContextBuffer builds a buffer retaining up to capacity messages per
// channel. capacity <= 0 yields a disabled buffer: observe is a no-op and
// recent always returns nil, so the classifier stays single-turn.
func newContextBuffer(capacity int) *contextBuffer {
	if capacity < 0 {
		capacity = 0
	}
	return &contextBuffer{capacity: capacity, byKey: make(map[string]*channelRing)}
}

// ctxKey composes the (tenant, channel) map key. Channel is normalised (trimmed,
// lower-cased) so casing differences share one window; tenant is used verbatim
// (trimmed) to keep tenants strictly isolated.
func ctxKey(tenant, channel string) string {
	return strings.TrimSpace(tenant) + "\x00" + strings.ToLower(strings.TrimSpace(channel))
}

// observe records a chat message for (tenant, channel). Blank username or text
// is ignored; the text is truncated to maxMessageRunes. After appending, the
// ring evicts oldest entries until it satisfies BOTH the message-count capacity
// and maxContextBytes.
func (b *contextBuffer) observe(tenant, channel, username, text string) {
	if b == nil || b.capacity == 0 {
		return
	}
	username = strings.TrimSpace(username)
	text = truncateRunes(strings.TrimSpace(text), maxMessageRunes)
	if username == "" || text == "" {
		return
	}
	key := ctxKey(tenant, channel)

	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.byKey[key]
	if r == nil {
		r = &channelRing{}
		b.byKey[key] = r
	}
	r.entries = append(r.entries, ctxEntry{Username: username, Text: text, At: time.Now().UTC()})
	r.totalBytes += len(text)
	// Evict oldest first until within the count cap and the byte cap. The
	// len > 1 guard keeps at least the newest message even if it alone were
	// large (it cannot be, given maxMessageRunes, but the guard is defensive).
	for len(r.entries) > b.capacity || (r.totalBytes > maxContextBytes && len(r.entries) > 1) {
		r.totalBytes -= len(r.entries[0].Text)
		r.entries = r.entries[1:]
	}
}

// recent returns up to n most recent messages for (tenant, channel), oldest
// first. n <= 0 or a disabled buffer returns nil. The returned slice is a copy,
// safe to read without holding the lock.
func (b *contextBuffer) recent(tenant, channel string, n int) []ctxEntry {
	if b == nil || b.capacity == 0 || n <= 0 {
		return nil
	}
	key := ctxKey(tenant, channel)

	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.byKey[key]
	if r == nil || len(r.entries) == 0 {
		return nil
	}
	start := 0
	if len(r.entries) > n {
		start = len(r.entries) - n
	}
	out := make([]ctxEntry, len(r.entries)-start)
	copy(out, r.entries[start:])
	return out
}

// truncateRunes shortens s to at most max runes (not bytes), so a single giant
// paste cannot dominate the window. A non-positive max returns s unchanged.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
