package actions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransformTemplate_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	_, ok := reg.Action("transform:template")
	assert.True(t, ok)
}

func TestTransformTemplate_RendersPlaceholders(t *testing.T) {
	act := transformTemplateAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{Username: "ada"})

	// The engine substitutes config before Execute; simulate that with
	// substituteRaw so the test exercises the real render pipeline.
	cfg := substituteRaw(ec, mustJSON(t, map[string]any{"template": "hi $(user)!", "output_key": "greeting"}))
	res, err := act.Execute(ec, cfg)
	require.NoError(t, err)
	assert.Equal(t, "hi ada!", res.Outputs["greeting"])
}

func TestTransformTemplate_DefaultOutputKey(t *testing.T) {
	act := transformTemplateAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"template": "static"}))
	require.NoError(t, err)
	assert.Equal(t, "static", res.Outputs["text"])
}

func TestTransformTemplate_JSONExtractHappy(t *testing.T) {
	act := transformTemplateAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})
	ec.SetOutput("body", `{"user":{"name":"ada"},"items":[{"id":"x1"}]}`)

	res, err := act.Execute(ec, mustJSON(t, map[string]any{
		"json_source": "body", "json_path": "user.name", "output_key": "name",
	}))
	require.NoError(t, err)
	assert.Equal(t, "ada", res.Outputs["name"])

	res, err = act.Execute(ec, mustJSON(t, map[string]any{
		"json_source": "body", "json_path": "items.0.id", "output_key": "first",
	}))
	require.NoError(t, err)
	assert.Equal(t, "x1", res.Outputs["first"])
}

func TestTransformTemplate_JSONExtractMissingPath(t *testing.T) {
	act := transformTemplateAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})
	ec.SetOutput("body", `{"user":{"name":"ada"}}`)

	_, err := act.Execute(ec, mustJSON(t, map[string]any{
		"json_source": "body", "json_path": "user.age",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTransformTemplate_JSONExtractInvalidJSON(t *testing.T) {
	act := transformTemplateAction{}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})
	ec.SetOutput("body", `not json`)

	_, err := act.Execute(ec, mustJSON(t, map[string]any{
		"json_source": "body", "json_path": "x",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid JSON")
}
