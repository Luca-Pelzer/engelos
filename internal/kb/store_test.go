package kb

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "kb.db") + "?_pragma=busy_timeout(5000)"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustCreate(t *testing.T, s Store, channel, category, title, content string) Entry {
	t.Helper()
	e, err := s.Create(context.Background(), Entry{
		TenantID: "local", Channel: channel, Category: category,
		Title: title, Content: content, Enabled: true,
	})
	require.NoError(t, err)
	return e
}

func TestCreate_RoundTripAndDefaults(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got := mustCreate(t, s, "chan-a", "rules", "No spam", "Please do not spam the chat.")
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, "local", got.TenantID)
	assert.Equal(t, "rules", got.Category)
	assert.True(t, got.Enabled)
	assert.False(t, got.CreatedAt.IsZero())
	assert.Equal(t, got.CreatedAt, got.UpdatedAt)

	fetched, err := s.Get(ctx, "local", "chan-a", got.ID)
	require.NoError(t, err)
	assert.Equal(t, got.ID, fetched.ID)
	assert.Equal(t, "No spam", fetched.Title)

	_, err = s.Get(ctx, "local", "chan-a", "does-not-exist")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCreate_EmptyCategoryDefaultsToOther(t *testing.T) {
	s := newTestStore(t)
	got, err := s.Create(context.Background(), Entry{
		TenantID: "local", Channel: "c", Title: "t", Content: "body",
	})
	require.NoError(t, err)
	assert.Equal(t, "other", got.Category)
}

func TestValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cases := []struct {
		name string
		e    Entry
	}{
		{"no tenant", Entry{Channel: "c", Title: "t", Content: "x"}},
		{"no channel", Entry{TenantID: "local", Title: "t", Content: "x"}},
		{"no title", Entry{TenantID: "local", Channel: "c", Content: "x"}},
		{"no content", Entry{TenantID: "local", Channel: "c", Title: "t"}},
		{"bad category", Entry{TenantID: "local", Channel: "c", Category: "bogus", Title: "t", Content: "x"}},
		{"content too big", Entry{TenantID: "local", Channel: "c", Title: "t", Content: strings.Repeat("a", maxContentBytes+1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Create(ctx, tc.e)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}

	// Exactly at the cap is accepted.
	_, err := s.Create(ctx, Entry{TenantID: "local", Channel: "c", Title: "t", Content: strings.Repeat("a", maxContentBytes)})
	assert.NoError(t, err)
}

func TestUpdate_ReindexesSearch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	e := mustCreate(t, s, "chan-a", "faq", "banana", "the yellow fruit")

	hits, err := s.Search(ctx, "local", "chan-a", "banana", "", 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)

	e.Title = "apple"
	e.Content = "the red fruit"
	_, err = s.Update(ctx, e)
	require.NoError(t, err)

	// After the update the FTS index no longer matches the old term and does
	// match the new one — proving the AFTER UPDATE trigger re-indexed.
	stale, err := s.Search(ctx, "local", "chan-a", "banana", "", 5)
	require.NoError(t, err)
	assert.Empty(t, stale)

	fresh, err := s.Search(ctx, "local", "chan-a", "apple", "", 5)
	require.NoError(t, err)
	require.Len(t, fresh, 1)
	assert.Equal(t, "apple", fresh[0].Title)

	_, err = s.Update(ctx, Entry{ID: "missing", TenantID: "local", Channel: "chan-a", Title: "t", Content: "c"})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDelete_RemovesFromSearch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	e := mustCreate(t, s, "chan-a", "lore", "the gigglecat", "a running gag about a cat")
	hits, err := s.Search(ctx, "local", "chan-a", "gigglecat", "", 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)

	require.NoError(t, s.Delete(ctx, "local", "chan-a", e.ID))

	after, err := s.Search(ctx, "local", "chan-a", "gigglecat", "", 5)
	require.NoError(t, err)
	assert.Empty(t, after)

	assert.ErrorIs(t, s.Delete(ctx, "local", "chan-a", e.ID), ErrNotFound)
}

func TestSearch_TitleOutranksContent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mustCreate(t, s, "chan-a", "games", "boss fight tips", "the dragon shows up in phase two here")
	mustCreate(t, s, "chan-a", "games", "dragon", "how to beat the final encounter")

	hits, err := s.Search(ctx, "local", "chan-a", "dragon", "", 5)
	require.NoError(t, err)
	require.Len(t, hits, 2)
	// The entry with "dragon" in its TITLE must rank above the one that only
	// mentions it in the body, because bm25 weights the title column 10x.
	assert.Equal(t, "dragon", hits[0].Title)
}

func TestSearch_DisabledAndCategoryFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	on := mustCreate(t, s, "chan-a", "rules", "keyword one", "body")
	off := mustCreate(t, s, "chan-a", "faq", "keyword two", "body")
	off.Enabled = false
	_, err := s.Update(ctx, off)
	require.NoError(t, err)

	all, err := s.Search(ctx, "local", "chan-a", "keyword", "", 5)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, on.ID, all[0].ID)

	byCat, err := s.Search(ctx, "local", "chan-a", "keyword", "rules", 5)
	require.NoError(t, err)
	assert.Len(t, byCat, 1)

	none, err := s.Search(ctx, "local", "chan-a", "keyword", "lore", 5)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestTenantChannelIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mustCreate(t, s, "chan-a", "rules", "secret alpha", "channel a body")
	mustCreate(t, s, "chan-b", "rules", "secret alpha", "channel b body")

	a, err := s.Search(ctx, "local", "chan-a", "secret", "", 5)
	require.NoError(t, err)
	require.Len(t, a, 1)
	assert.Equal(t, "chan-a", a[0].Channel)

	other, err := s.Search(ctx, "other-tenant", "chan-a", "secret", "", 5)
	require.NoError(t, err)
	assert.Empty(t, other)

	list, err := s.List(ctx, "local", "chan-a", "", 50)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestList_NewestFirstAndCategoryFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mustCreate(t, s, "chan-a", "rules", "first", "b")
	mustCreate(t, s, "chan-a", "faq", "second", "b")
	third := mustCreate(t, s, "chan-a", "rules", "third", "b")

	all, err := s.List(ctx, "local", "chan-a", "", 50)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, third.ID, all[0].ID)

	onlyRules, err := s.List(ctx, "local", "chan-a", "rules", 50)
	require.NoError(t, err)
	assert.Len(t, onlyRules, 2)
	for _, e := range onlyRules {
		assert.Equal(t, "rules", e.Category)
	}
}

func TestList_IncludesDisabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	e := mustCreate(t, s, "chan-a", "rules", "toggle me", "b")
	e.Enabled = false
	_, err := s.Update(ctx, e)
	require.NoError(t, err)

	list, err := s.List(ctx, "local", "chan-a", "", 50)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.False(t, list[0].Enabled)
}

// TestSearch_HostileInputNeverErrors feeds the raw, adversarial strings a
// viewer could type as a command argument straight into Search. Because
// SanitizeMatchQuery quotes every token, FTS5 operators and unbalanced
// punctuation are treated as literal text and can neither error nor inject.
func TestSearch_HostileInputNeverErrors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustCreate(t, s, "chan-a", "rules", "be nice", "respect everyone in chat")

	hostile := []string{
		`"`, `""`, `un"balanced`, `*`, `^caret`, `(paren`, `a)b`,
		`NEAR(a b)`, `foo OR bar`, `foo AND bar`, `-minus`, `+plus`,
		`col:umn`, `"quoted phrase`, `emoji 🎮🔥`, `안녕하세요`,
		`a* OR b*`, `{brace}`, `[bracket]`, `back\slash`,
		strings.Repeat("x", maxContentBytes), "   ", "",
	}
	for _, q := range hostile {
		_, err := s.Search(ctx, "local", "chan-a", q, "", 5)
		assert.NoErrorf(t, err, "hostile query %q must not error", q)
	}
}

func TestSanitizeMatchQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"schedule", `"schedule"`},
		{"stream schedule", `"stream" OR "schedule"`},
		{`say "hi"`, `"say" OR """hi"""`},
		{`NEAR OR *`, `"NEAR" OR "OR" OR "*"`},
	}
	for _, tc := range cases {
		assert.Equalf(t, tc.want, SanitizeMatchQuery(tc.in), "input %q", tc.in)
	}
}

func TestSearch_EmptyQueryReturnsNothing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustCreate(t, s, "chan-a", "rules", "anything", "some body text")

	hits, err := s.Search(ctx, "local", "chan-a", "   ", "", 5)
	require.NoError(t, err)
	assert.Empty(t, hits)
}
