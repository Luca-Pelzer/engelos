package plugins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixturePlugin is a test-only plugin with a configurable id and default.
type fixturePlugin struct {
	id  string
	def bool
}

func (f fixturePlugin) Manifest() Manifest {
	return Manifest{
		ID:             f.id,
		Name:           "Fixture " + f.id,
		Description:    "a test plugin",
		Tier:           TierExperimental,
		DefaultEnabled: f.def,
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(fixturePlugin{id: "quotes", def: true}))
	got, ok := r.Get("quotes")
	require.True(t, ok)
	assert.Equal(t, "quotes", got.Manifest().ID)

	_, ok = r.Get("missing")
	assert.False(t, ok)
}

func TestRegistry_DuplicateIDRejected(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(fixturePlugin{id: "quotes"}))
	err := r.Register(fixturePlugin{id: "quotes"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistry_EmptyIDRejected(t *testing.T) {
	r := NewRegistry()
	err := r.Register(fixturePlugin{id: "  "})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty id")
}

func TestRegistry_ListStableOrder(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(fixturePlugin{id: "zeta"}))
	require.NoError(t, r.Register(fixturePlugin{id: "alpha"}))
	require.NoError(t, r.Register(fixturePlugin{id: "mike"}))
	list := r.List()
	require.Len(t, list, 3)
	assert.Equal(t, "alpha", list[0].ID)
	assert.Equal(t, "mike", list[1].ID)
	assert.Equal(t, "zeta", list[2].ID)
}

func TestActive_NilStoreUsesManifestDefaults(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(fixturePlugin{id: "quotes", def: true}))
	require.NoError(t, r.Register(fixturePlugin{id: "music", def: false}))

	active, err := r.Active(context.Background(), nil, "local")
	require.NoError(t, err)
	assert.True(t, active["quotes"], "quotes defaults on")
	assert.False(t, active["music"], "music defaults off (songrequests-style)")
}

func TestActive_PersistedStateOverridesDefault(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(fixturePlugin{id: "quotes", def: true}))
	require.NoError(t, r.Register(fixturePlugin{id: "music", def: false}))

	store := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "local", "quotes", false))
	require.NoError(t, store.Set(ctx, "local", "music", true))

	active, err := r.Active(ctx, store, "local")
	require.NoError(t, err)
	assert.False(t, active["quotes"], "persisted off overrides default on")
	assert.True(t, active["music"], "persisted on overrides default off")
}
