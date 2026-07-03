package plugins

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Registry holds the registered plugins keyed by manifest id. It is safe for
// concurrent use; registration happens at startup and lookups on request.
type Registry struct {
	items map[string]Plugin
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{items: make(map[string]Plugin)}
}

// Register adds a plugin. It errors on an empty or duplicate id so a typo never
// silently shadows another plugin.
func (r *Registry) Register(p Plugin) error {
	id := strings.TrimSpace(p.Manifest().ID)
	if id == "" {
		return fmt.Errorf("plugins: plugin has empty id")
	}
	if _, dup := r.items[id]; dup {
		return fmt.Errorf("plugins: plugin %q already registered", id)
	}
	r.items[id] = p
	return nil
}

// Get returns the plugin for id and whether it was found.
func (r *Registry) Get(id string) (Plugin, bool) {
	p, ok := r.items[strings.TrimSpace(id)]
	return p, ok
}

// List returns every registered manifest, sorted by id for a stable order.
func (r *Registry) List() []Manifest {
	out := make([]Manifest, 0, len(r.items))
	for _, p := range r.items {
		out = append(out, p.Manifest())
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// Active resolves each registered plugin's effective enabled state for a tenant
// by layering the persisted desired state over the manifest default. The result
// is the authoritative "what should be mounted" set; cmd/engelos captures it
// once at startup to gate store-opening and route/command wiring, and passes the
// same snapshot to the API as the current (versus desired) state. A nil store
// means "nothing persisted", so every plugin falls back to its manifest default.
func (r *Registry) Active(ctx context.Context, state StateStore, tenantID string) (map[string]bool, error) {
	out := make(map[string]bool, len(r.items))
	for _, p := range r.items {
		m := p.Manifest()
		if state == nil {
			out[m.ID] = m.DefaultEnabled
			continue
		}
		enabled, err := state.GetOrDefault(ctx, tenantID, m.ID, m.DefaultEnabled)
		if err != nil {
			return nil, fmt.Errorf("plugins: resolve %q: %w", m.ID, err)
		}
		out[m.ID] = enabled
	}
	return out, nil
}
