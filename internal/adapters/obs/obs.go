// Package obs adapts the OBS WebSocket client (goobs) to the action engine's
// OBSController surface. It owns a single lazily-established connection and
// reconnects on demand, so OBS can start, stop or restart independently of the
// daemon: a rule that fires while OBS is down fails cleanly and the next fire
// reconnects, with no restart of engelOS required.
package obs

import (
	"fmt"
	"strings"
	"sync"

	"github.com/andreykaipov/goobs"
	"github.com/andreykaipov/goobs/api/requests/sceneitems"
	"github.com/andreykaipov/goobs/api/requests/scenes"
)

// Config is the OBS WebSocket connection configuration. Host is the
// host:port of the OBS WebSocket server (for example "127.0.0.1:4455");
// Password is the server password (empty when auth is disabled).
type Config struct {
	Host     string
	Password string
}

// Controller is a reconnect-tolerant OBS WebSocket controller. The zero value
// is not usable; build one with New. All methods are safe for concurrent use.
type Controller struct {
	cfg Config

	mu     sync.Mutex
	client *goobs.Client
}

// New builds a Controller for the given config. It does NOT dial yet: the first
// action that needs OBS establishes the connection, so a daemon can boot with
// OBS offline and start working the moment OBS comes up.
func New(cfg Config) *Controller {
	return &Controller{cfg: cfg}
}

// clientLocked returns a live goobs client, dialing once and caching it. The
// caller must hold c.mu.
func (c *Controller) clientLocked() (*goobs.Client, error) {
	if c.client != nil {
		return c.client, nil
	}
	opts := []goobs.Option{}
	if c.cfg.Password != "" {
		opts = append(opts, goobs.WithPassword(c.cfg.Password))
	}
	client, err := goobs.New(c.cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("obs: connect %s: %w", c.cfg.Host, err)
	}
	c.client = client
	return client, nil
}

// dropLocked tears down a client that returned an error so the next call
// redials. The caller must hold c.mu.
func (c *Controller) dropLocked() {
	if c.client != nil {
		_ = c.client.Disconnect()
		c.client = nil
	}
}

// SwitchScene sets the active OBS program scene.
func (c *Controller) SwitchScene(scene string) error {
	scene = strings.TrimSpace(scene)
	if scene == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	client, err := c.clientLocked()
	if err != nil {
		return err
	}
	_, err = client.Scenes.SetCurrentProgramScene(
		scenes.NewSetCurrentProgramSceneParams().WithSceneName(scene))
	if err != nil {
		c.dropLocked()
		return fmt.Errorf("obs: switch scene %q: %w", scene, err)
	}
	return nil
}

// SetSourceVisible shows or hides a source within a scene. It first resolves the
// scene item id for the source name (OBS addresses items by numeric id, not
// name), then toggles its enabled flag.
func (c *Controller) SetSourceVisible(scene, source string, visible bool) error {
	scene = strings.TrimSpace(scene)
	source = strings.TrimSpace(source)
	if scene == "" || source == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	client, err := c.clientLocked()
	if err != nil {
		return err
	}
	idResp, err := client.SceneItems.GetSceneItemId(
		sceneitems.NewGetSceneItemIdParams().WithSceneName(scene).WithSourceName(source))
	if err != nil {
		c.dropLocked()
		return fmt.Errorf("obs: resolve source %q in %q: %w", source, scene, err)
	}
	_, err = client.SceneItems.SetSceneItemEnabled(
		sceneitems.NewSetSceneItemEnabledParams().
			WithSceneName(scene).
			WithSceneItemId(idResp.SceneItemId).
			WithSceneItemEnabled(visible))
	if err != nil {
		c.dropLocked()
		return fmt.Errorf("obs: set %q visibility in %q: %w", source, scene, err)
	}
	return nil
}

// Close disconnects the underlying client if connected. Safe to call when never
// connected.
func (c *Controller) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil
	}
	err := c.client.Disconnect()
	c.client = nil
	return err
}
