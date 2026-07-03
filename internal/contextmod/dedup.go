package contextmod

import (
	"strings"
	"sync"
	"time"
)

// dedupCache memoises recent classification results keyed by channel rules plus
// the normalised message text, so identical copy-paste spam (the same line
// posted many times in seconds) is only sent to the paid backend once. Entries
// expire after ttl. It is safe for concurrent use and bounded by maxEntries to
// avoid unbounded growth under a flood of distinct messages.
type dedupCache struct {
	ttl        time.Duration
	maxEntries int
	nowFunc    func() time.Time

	mu      sync.Mutex
	entries map[string]dedupEntry
}

type dedupEntry struct {
	decision Decision
	expires  time.Time
}

func newDedupCache(ttl time.Duration, maxEntries int) *dedupCache {
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	if maxEntries <= 0 {
		maxEntries = 2048
	}
	return &dedupCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		nowFunc:    time.Now,
		entries:    make(map[string]dedupEntry),
	}
}

// dedupKey collapses case and surrounding whitespace so trivially-varied repeats
// still collide.
func dedupKey(rules, text string) string {
	return strings.ToLower(strings.TrimSpace(rules)) + "\x00" + strings.ToLower(strings.TrimSpace(text))
}

// get returns a cached decision for key when present and unexpired.
func (c *dedupCache) get(key string) (Decision, bool) {
	now := c.nowFunc()
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return Decision{}, false
	}
	if now.After(e.expires) {
		delete(c.entries, key)
		return Decision{}, false
	}
	return e.decision, true
}

// put stores d under key. When the cache is full it is cleared wholesale, a
// cheap bound that is acceptable because entries are short-lived anyway.
func (c *dedupCache) put(key string, d Decision) {
	now := c.nowFunc()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.maxEntries {
		c.entries = make(map[string]dedupEntry, c.maxEntries)
	}
	c.entries[key] = dedupEntry{decision: d, expires: now.Add(c.ttl)}
}
