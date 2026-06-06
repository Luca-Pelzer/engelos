package actions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVars_SubstituteString_WellKnownFields(t *testing.T) {
	trigger := Trigger{
		Username:  "alice",
		UserID:    "12345",
		Channel:   "general",
		Platform:  "discord",
		Text:      "hello world",
		EventType: "message",
	}
	ec := newExecutionContext(context.Background(), sampleRule("general", "test"), trigger)

	tests := []struct {
		token    string
		expected string
	}{
		{"$(user)", "alice"},
		{"$(username)", "alice"},
		{"$(userid)", "12345"},
		{"$(user.id)", "12345"},
		{"$(channel)", "general"},
		{"$(platform)", "discord"},
		{"$(message)", "hello world"},
		{"$(text)", "hello world"},
		{"$(event)", "message"},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			result := substituteString(ec, tt.token)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestVars_SubstituteString_CaseInsensitive(t *testing.T) {
	trigger := Trigger{
		Username: "bob",
		UserID:   "999",
		Channel:  "lobby",
	}
	ec := newExecutionContext(context.Background(), sampleRule("lobby", "test"), trigger)

	tests := []struct {
		input    string
		expected string
	}{
		{"$(USER)", "bob"},
		{"$(Username)", "bob"},
		{"$(USERID)", "999"},
		{"$(User.ID)", "999"},
		{"$(CHANNEL)", "lobby"},
		{"$(Channel)", "lobby"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := substituteString(ec, tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestVars_SubstituteString_MultipleTokens(t *testing.T) {
	trigger := Trigger{
		Username: "charlie",
		Channel:  "chat",
		Platform: "twitch",
	}
	ec := newExecutionContext(context.Background(), sampleRule("chat", "test"), trigger)

	input := "User $(user) joined $(channel) on $(platform)"
	expected := "User charlie joined chat on twitch"
	result := substituteString(ec, input)
	assert.Equal(t, expected, result)
}

func TestVars_SubstituteString_UnknownToken(t *testing.T) {
	trigger := Trigger{Username: "dave"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)

	result := substituteString(ec, "Hello $(nope) there")
	assert.Equal(t, "Hello  there", result)
}

func TestVars_SubstituteString_NoToken(t *testing.T) {
	trigger := Trigger{Username: "eve"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)

	input := "plain text with no tokens"
	result := substituteString(ec, input)
	assert.Equal(t, input, result)
}

func TestVars_SubstituteString_OutputValue(t *testing.T) {
	trigger := Trigger{Username: "frank"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)
	ec.SetOutput("myout", "output_value")

	result := substituteString(ec, "Got $(myout)")
	assert.Equal(t, "Got output_value", result)
}

func TestVars_SubstituteString_DataMapValue(t *testing.T) {
	trigger := Trigger{
		Username: "grace",
		Data:     map[string]any{"custom_field": "custom_value"},
	}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)

	result := substituteString(ec, "Data: $(custom_field)")
	assert.Equal(t, "Data: custom_value", result)
}

func TestVars_SubstituteString_Precedence(t *testing.T) {
	trigger := Trigger{
		Username: "well_known_user",
		Data:     map[string]any{"user": "data_user"},
	}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)
	ec.SetOutput("user", "output_user")

	result := substituteString(ec, "$(user)")
	assert.Equal(t, "well_known_user", result, "well-known field should take precedence")
}

func TestVars_SubstituteString_OutputBeatsData(t *testing.T) {
	trigger := Trigger{
		Username: "henry",
		Data:     map[string]any{"foo": "data_foo"},
	}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)
	ec.SetOutput("foo", "output_foo")

	result := substituteString(ec, "$(foo)")
	assert.Equal(t, "output_foo", result, "output should take precedence over Data")
}

func TestVars_StringifyValue_Bool(t *testing.T) {
	trigger := Trigger{Username: "ivy"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)
	ec.SetOutput("flag_true", true)
	ec.SetOutput("flag_false", false)

	assert.Equal(t, "true", substituteString(ec, "$(flag_true)"))
	assert.Equal(t, "false", substituteString(ec, "$(flag_false)"))
}

func TestVars_StringifyValue_Float64(t *testing.T) {
	trigger := Trigger{Username: "jack"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)
	ec.SetOutput("whole", float64(3))
	ec.SetOutput("decimal", float64(3.14))

	assert.Equal(t, "3", substituteString(ec, "$(whole)"))
	assert.Equal(t, "3.14", substituteString(ec, "$(decimal)"))
}

func TestVars_StringifyValue_Int(t *testing.T) {
	trigger := Trigger{Username: "kate"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)
	ec.SetOutput("count", 42)

	assert.Equal(t, "42", substituteString(ec, "$(count)"))
}

func TestVars_StringifyValue_Nil(t *testing.T) {
	trigger := Trigger{Username: "leo"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)
	ec.SetOutput("empty", nil)

	assert.Equal(t, "", substituteString(ec, "$(empty)"))
}

func TestVars_StringifyValue_DataMapTypes(t *testing.T) {
	trigger := Trigger{
		Username: "mia",
		Data: map[string]any{
			"dbool":  true,
			"dfloat": float64(7),
			"dnil":   nil,
		},
	}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)

	assert.Equal(t, "true", substituteString(ec, "$(dbool)"))
	assert.Equal(t, "7", substituteString(ec, "$(dfloat)"))
	assert.Equal(t, "", substituteString(ec, "$(dnil)"))
}

func TestVars_SubstituteRaw_StringLeaves(t *testing.T) {
	trigger := Trigger{
		Username: "nina",
		Channel:  "room",
	}
	ec := newExecutionContext(context.Background(), sampleRule("room", "test"), trigger)

	input := mustJSON(t, map[string]any{
		"text":   "hi $(user)",
		"keep":   5,
		"nested": map[string]any{"a": "$(channel)"},
		"arr":    []any{"$(user)", "x"},
	})

	result := substituteRaw(ec, input)

	var out map[string]any
	require.NoError(t, json.Unmarshal(result, &out))

	assert.Equal(t, "hi nina", out["text"])
	assert.Equal(t, float64(5), out["keep"])

	nested, ok := out["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "room", nested["a"])

	arr, ok := out["arr"].([]any)
	require.True(t, ok)
	require.Len(t, arr, 2)
	assert.Equal(t, "nina", arr[0])
	assert.Equal(t, "x", arr[1])
}

func TestVars_SubstituteRaw_KeysNotSubstituted(t *testing.T) {
	trigger := Trigger{Username: "oscar"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)

	input := []byte(`{"$(user)":"value","normal":"$(user)"}`)
	result := substituteRaw(ec, input)

	var out map[string]any
	require.NoError(t, json.Unmarshal(result, &out))

	_, hasTokenKey := out["$(user)"]
	assert.True(t, hasTokenKey, "key with token syntax should remain unchanged")
	assert.Equal(t, "value", out["$(user)"])
	assert.Equal(t, "oscar", out["normal"])
}

func TestVars_SubstituteRaw_NoTokenUnchanged(t *testing.T) {
	trigger := Trigger{Username: "pat"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)

	input := []byte(`{"key":"value","num":123}`)
	result := substituteRaw(ec, input)

	assert.Equal(t, input, []byte(result))
}

func TestVars_SubstituteRaw_InvalidJSONUnchanged(t *testing.T) {
	trigger := Trigger{Username: "quinn"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)

	input := []byte(`{invalid json $(user)}`)
	result := substituteRaw(ec, input)

	assert.Equal(t, input, []byte(result))
}

func TestVars_SubstituteRaw_EmptyBlobUnchanged(t *testing.T) {
	trigger := Trigger{Username: "ray"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)

	var input json.RawMessage
	result := substituteRaw(ec, input)

	assert.Equal(t, input, result)
}

func TestVars_SubstituteRaw_SecurityInjection(t *testing.T) {
	trigger := Trigger{Username: "sam"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)
	ec.SetOutput("dangerous", `a"b`)

	input := mustJSON(t, map[string]any{
		"msg": "val: $(dangerous)",
	})

	result := substituteRaw(ec, input)

	var out map[string]any
	require.NoError(t, json.Unmarshal(result, &out), "result should be valid JSON")
	assert.Equal(t, `val: a"b`, out["msg"])

	roundtrip, err := json.Marshal(out)
	require.NoError(t, err)
	var out2 map[string]any
	require.NoError(t, json.Unmarshal(roundtrip, &out2))
	assert.Equal(t, out["msg"], out2["msg"])
}

func TestVars_SubstituteRaw_SecurityBraces(t *testing.T) {
	trigger := Trigger{Username: "tina"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)
	ec.SetOutput("braces", `{"injected":true}`)

	input := mustJSON(t, map[string]any{
		"data": "prefix $(braces) suffix",
	})

	result := substituteRaw(ec, input)

	var out map[string]any
	require.NoError(t, json.Unmarshal(result, &out), "result should be valid JSON")
	assert.Equal(t, `prefix {"injected":true} suffix`, out["data"])
}

func TestVars_SubstituteRaw_NonStringScalarsUntouched(t *testing.T) {
	trigger := Trigger{Username: "uma"}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "test"), trigger)

	input := mustJSON(t, map[string]any{
		"bool":   true,
		"number": 42,
		"null":   nil,
		"float":  3.14,
	})

	result := substituteRaw(ec, input)

	var out map[string]any
	require.NoError(t, json.Unmarshal(result, &out))

	assert.Equal(t, true, out["bool"])
	assert.Equal(t, float64(42), out["number"])
	assert.Nil(t, out["null"])
	assert.Equal(t, 3.14, out["float"])
}
