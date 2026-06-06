package automod

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBannedWordsEmptyMatchRegexRejected(t *testing.T) {
	for _, pattern := range []string{"a*", ".*", "x?"} {
		cfg := DefaultConfig()
		cfg.BannedWords.Enabled = true
		cfg.BannedWords.Entries = []BannedEntry{
			{Phrase: pattern, MatchMode: MatchRegex, Verdict: VerdictBan},
		}
		e, err := NewEngine(cfg)
		require.Error(t, err, "pattern %q should be rejected", pattern)
		assert.Nil(t, e)
		assert.Contains(t, err.Error(), "empty string")
	}
}

func TestBannedWordsValidRegexDoesNotFireOnNonMatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BannedWords.Enabled = true
	cfg.BannedWords.Entries = []BannedEntry{
		{Phrase: `\d{4,}`, MatchMode: MatchRegex, Verdict: VerdictBan},
	}
	e := mustEngine(t, cfg)

	got := e.Evaluate(Message{Text: "a perfectly clean message"}, everyone())
	assert.Equal(t, VerdictPass, got.Verdict)

	hit := e.Evaluate(Message{Text: "code 12345"}, everyone())
	assert.Equal(t, VerdictBan, hit.Verdict)
}
