package eventsourcing_test

import (
	"encoding/json"
	"sort"
	"sync"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/stretchr/testify/require"
)

// TestNewEvent_MonotonicWithinMillisecond guards the M7-era fix: NewEvent must
// mint strictly increasing ULIDs even when many ids land in the same
// millisecond, otherwise the store's id-ordered replay and AfterID cursor can
// reorder or skip events. Generating a tight burst forces same-ms collisions.
func TestNewEvent_MonotonicWithinMillisecond(t *testing.T) {
	const n = 5000
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ev, err := eventsourcing.NewEvent("tenant", "x.y", json.RawMessage(`{}`))
		require.NoError(t, err)
		ids = append(ids, ev.ID.String())
	}

	sorted := make([]string, len(ids))
	copy(sorted, ids)
	sort.Strings(sorted)

	require.Equal(t, sorted, ids, "ids must already be in lexical (creation) order")
	for i := 1; i < len(ids); i++ {
		require.Greater(t, ids[i], ids[i-1], "ids must be strictly increasing at %d", i)
	}
}

// TestNewEvent_ConcurrentUnique proves the mutex around the monotonic entropy
// makes NewEvent safe under the concurrent, multi-goroutine access the
// per-channel dispatcher now creates: no duplicate ids, no data race.
func TestNewEvent_ConcurrentUnique(t *testing.T) {
	const goroutines = 16
	const perG = 500

	var wg sync.WaitGroup
	results := make([][]string, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			local := make([]string, 0, perG)
			for i := 0; i < perG; i++ {
				ev, err := eventsourcing.NewEvent("tenant", "x.y", json.RawMessage(`{}`))
				if err != nil {
					t.Errorf("NewEvent: %v", err)
					return
				}
				local = append(local, ev.ID.String())
			}
			results[g] = local
		}(g)
	}
	wg.Wait()

	seen := make(map[string]struct{}, goroutines*perG)
	for _, batch := range results {
		for _, id := range batch {
			if _, dup := seen[id]; dup {
				t.Fatalf("duplicate id minted concurrently: %s", id)
			}
			seen[id] = struct{}{}
		}
	}
	require.Len(t, seen, goroutines*perG)
}
