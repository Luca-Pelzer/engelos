package actions

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingOBS struct {
	mu        sync.Mutex
	scenes    []string
	vis       []obsVisCall
	switchErr error
	visErr    error
}

type obsVisCall struct {
	scene   string
	source  string
	visible bool
}

func (o *recordingOBS) SwitchScene(scene string) error {
	if o.switchErr != nil {
		return o.switchErr
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.scenes = append(o.scenes, scene)
	return nil
}

func (o *recordingOBS) SetSourceVisible(scene, source string, visible bool) error {
	if o.visErr != nil {
		return o.visErr
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.vis = append(o.vis, obsVisCall{scene, source, visible})
	return nil
}

func TestOBSActions_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{OBS: &recordingOBS{}}))
	_, ok := reg.Action("obs:switch-scene")
	assert.True(t, ok)
	_, ok = reg.Action("obs:set-source-visibility")
	assert.True(t, ok)
}

func TestOBSSwitchScene(t *testing.T) {
	t.Run("switches the named scene", func(t *testing.T) {
		obs := &recordingOBS{}
		reg := NewRegistry()
		require.NoError(t, RegisterBuiltins(reg, Services{OBS: obs}))
		act, _ := reg.Action("obs:switch-scene")
		ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

		_, err := act.Execute(ec, mustJSON(t, map[string]any{"scene": "Starting Soon"}))
		require.NoError(t, err)
		assert.Equal(t, []string{"Starting Soon"}, obs.scenes)
	})

	t.Run("empty scene no-op", func(t *testing.T) {
		obs := &recordingOBS{}
		reg := NewRegistry()
		require.NoError(t, RegisterBuiltins(reg, Services{OBS: obs}))
		act, _ := reg.Action("obs:switch-scene")
		ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

		_, err := act.Execute(ec, mustJSON(t, map[string]any{"scene": "  "}))
		require.NoError(t, err)
		assert.Empty(t, obs.scenes)
	})

	t.Run("nil controller no-op", func(t *testing.T) {
		reg := NewRegistry()
		require.NoError(t, RegisterBuiltins(reg, Services{}))
		act, _ := reg.Action("obs:switch-scene")
		ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

		_, err := act.Execute(ec, mustJSON(t, map[string]any{"scene": "Live"}))
		require.NoError(t, err)
	})

	t.Run("controller error surfaces", func(t *testing.T) {
		obs := &recordingOBS{switchErr: errors.New("obs down")}
		reg := NewRegistry()
		require.NoError(t, RegisterBuiltins(reg, Services{OBS: obs}))
		act, _ := reg.Action("obs:switch-scene")
		ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

		_, err := act.Execute(ec, mustJSON(t, map[string]any{"scene": "Live"}))
		require.Error(t, err)
	})
}

func TestOBSSetSourceVisibility(t *testing.T) {
	t.Run("shows a source", func(t *testing.T) {
		obs := &recordingOBS{}
		reg := NewRegistry()
		require.NoError(t, RegisterBuiltins(reg, Services{OBS: obs}))
		act, _ := reg.Action("obs:set-source-visibility")
		ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

		_, err := act.Execute(ec, mustJSON(t, map[string]any{"scene": "Main", "source": "Webcam", "visible": true}))
		require.NoError(t, err)
		require.Len(t, obs.vis, 1)
		assert.Equal(t, obsVisCall{"Main", "Webcam", true}, obs.vis[0])
	})

	t.Run("missing scene or source no-op", func(t *testing.T) {
		obs := &recordingOBS{}
		reg := NewRegistry()
		require.NoError(t, RegisterBuiltins(reg, Services{OBS: obs}))
		act, _ := reg.Action("obs:set-source-visibility")
		ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

		_, err := act.Execute(ec, mustJSON(t, map[string]any{"scene": "Main", "source": ""}))
		require.NoError(t, err)
		assert.Empty(t, obs.vis)
	})
}
