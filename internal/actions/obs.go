package actions

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OBSController is the side-effect surface the OBS actions call out to. The host
// wires a thin goobs adapter so this package never imports the OBS client; a nil
// controller (no OBS configured) disables the OBS actions, which still register
// but no-op, so a rule referencing them never fails on a headless deploy.
type OBSController interface {
	SwitchScene(scene string) error
	SetSourceVisible(scene, source string, visible bool) error
}

type switchSceneConfig struct {
	Scene string `json:"scene"`
}

type switchSceneAction struct {
	obs OBSController
}

func (switchSceneAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "obs:switch-scene",
		Name:        "Switch OBS scene",
		Description: "Sets the active OBS program scene. Supports $(user) and other variables in the scene name.",
	}
}

func (a switchSceneAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c switchSceneConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: obs switch-scene config: %w", err)
	}
	scene := strings.TrimSpace(c.Scene)
	if scene == "" {
		return nil, nil
	}
	if a.obs == nil {
		return nil, nil
	}
	if err := a.obs.SwitchScene(scene); err != nil {
		return nil, fmt.Errorf("actions: obs switch-scene: %w", err)
	}
	return nil, nil
}

type toggleSourceConfig struct {
	Scene   string `json:"scene"`
	Source  string `json:"source"`
	Visible bool   `json:"visible"`
}

type toggleSourceAction struct {
	obs OBSController
}

func (toggleSourceAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "obs:set-source-visibility",
		Name:        "Show/hide OBS source",
		Description: "Shows or hides a source within an OBS scene.",
	}
}

func (a toggleSourceAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c toggleSourceConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: obs set-source-visibility config: %w", err)
	}
	scene := strings.TrimSpace(c.Scene)
	source := strings.TrimSpace(c.Source)
	if scene == "" || source == "" {
		return nil, nil
	}
	if a.obs == nil {
		return nil, nil
	}
	if err := a.obs.SetSourceVisible(scene, source, c.Visible); err != nil {
		return nil, fmt.Errorf("actions: obs set-source-visibility: %w", err)
	}
	return nil, nil
}
