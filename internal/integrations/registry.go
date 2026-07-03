package integrations

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

// Registry holds the registered integrations keyed by manifest id. It is safe
// for concurrent use; registration happens at startup and lookups on request.
type Registry struct {
	mu    sync.RWMutex
	items map[string]Integration
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{items: make(map[string]Integration)}
}

// Register adds an integration. It errors on an empty or duplicate id so a typo
// never silently shadows another integration.
func (r *Registry) Register(i Integration) error {
	id := strings.TrimSpace(i.Manifest().ID)
	if id == "" {
		return fmt.Errorf("integrations: integration has empty id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.items[id]; dup {
		return fmt.Errorf("integrations: integration %q already registered", id)
	}
	r.items[id] = i
	return nil
}

// Get returns the integration for id and whether it was found.
func (r *Registry) Get(id string) (Integration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, ok := r.items[id]
	return i, ok
}

// List returns every registered manifest, sorted by id for a stable order.
func (r *Registry) List() []Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Manifest, 0, len(r.items))
	for _, i := range r.items {
		out = append(out, i.Manifest())
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// RegisterAllNodes contributes every registered integration's workflow nodes to
// the actions catalog. One call wires all integrations' condition/action
// definitions into actionsReg, so a new integration appears in the catalog with
// no changes to internal/actions. Integrations are processed in id order for a
// deterministic outcome; the first RegisterNodes error aborts.
func (r *Registry) RegisterAllNodes(actionsReg *actions.Registry) error {
	r.mu.RLock()
	ids := make([]string, 0, len(r.items))
	for id := range r.items {
		ids = append(ids, id)
	}
	r.mu.RUnlock()
	sort.Strings(ids)
	for _, id := range ids {
		i, ok := r.Get(id)
		if !ok {
			continue
		}
		if err := i.RegisterNodes(actionsReg); err != nil {
			return fmt.Errorf("integrations: register nodes for %q: %w", id, err)
		}
	}
	return nil
}
