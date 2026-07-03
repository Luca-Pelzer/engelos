package actions

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncateRunes(t *testing.T) {
	assert.Equal(t, "abc", truncateRunes("abc", 5))
	assert.Equal(t, "abc", truncateRunes("abc", 3))
	assert.Equal(t, "ab…", truncateRunes("abcdef", 2))
	assert.Equal(t, "日本…", truncateRunes("日本語テスト", 2))
}

func TestSummarizeOutputs_MasksSensitiveKeys(t *testing.T) {
	cases := []struct {
		key    string
		masked bool
	}{
		{"secret", true},
		{"api_key", true},
		{"X-Auth-Token", true},
		{"password", true},
		{"authorization", true},
		{"session_cookie", true},
		{"greeting", false},
		{"label", false},
	}
	for _, tc := range cases {
		out := summarizeOutputs("transform:template", map[string]any{tc.key: "supersecretvalue"})
		if tc.masked {
			assert.Contains(t, out, tc.key+"=<redacted>", tc.key)
			assert.NotContains(t, out, "supersecretvalue", tc.key)
		} else {
			assert.Contains(t, out, "supersecretvalue", tc.key)
		}
	}
}

func TestSummarizeOutputs_TruncatesValue(t *testing.T) {
	long := strings.Repeat("a", summaryValueMaxRunes+50)
	out := summarizeOutputs("transform:template", map[string]any{"greeting": long})
	assert.Contains(t, out, "greeting=")
	assert.Contains(t, out, "…")
	assert.NotContains(t, out, long)
}

func TestSummarizeOutputs_Empty(t *testing.T) {
	assert.Equal(t, "", summarizeOutputs("builtin:log", nil))
	assert.Equal(t, "", summarizeOutputs("builtin:log", map[string]any{}))
}

func TestSummarizeHTTPOutputs_HidesBodyValuesHeaders(t *testing.T) {
	out := summarizeOutputs("http:request", map[string]any{
		"status":     "200",
		"body":       `{"token":"leaked","name":"ada"}`,
		"ok":         "true",
		"json.token": "leaked",
		"json.name":  "ada",
	})
	assert.Contains(t, out, "status=200")
	assert.Contains(t, out, "body_len=")
	assert.Contains(t, out, "json_keys=[json.name,json.token]")
	// Never the body content, the json values, or the ok flag.
	assert.NotContains(t, out, "leaked")
	assert.NotContains(t, out, "ada")
	assert.NotContains(t, out, "ok=")
}

func TestSummarizeTrigger(t *testing.T) {
	assert.Equal(t, "command:hello", summarizeTrigger(Trigger{Kind: TriggerCommand, Text: "!hello world"}))
	assert.Equal(t, "event:stream.online", summarizeTrigger(Trigger{Kind: TriggerEvent, EventType: "stream.online"}))
	assert.Equal(t, "timer", summarizeTrigger(Trigger{Kind: TriggerTimer}))
	assert.Equal(t, "manual", summarizeTrigger(Trigger{Kind: TriggerManual}))
}

func TestSummarizeTrigger_WebhookKeyNamesOnly(t *testing.T) {
	got := summarizeTrigger(Trigger{Kind: TriggerWebhook, Data: map[string]any{
		"secret":  "xyz-value",
		"release": "v1.2.3",
	}})
	assert.Equal(t, "webhook:{release,secret}", got)
	assert.NotContains(t, got, "xyz-value")
	assert.NotContains(t, got, "v1.2.3")
}
