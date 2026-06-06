package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type addQuoteCall struct {
	channel   string
	text      string
	createdBy string
}

type fakeQuoteStore struct {
	addCalls    []addQuoteCall
	addNumber   int
	addErr      error
	getView     commands.QuoteView
	getOK       bool
	randomView  commands.QuoteView
	randomOK    bool
	deleteCalls []int
	deleteErr   error
}

func (f *fakeQuoteStore) Add(_ context.Context, channel, text, createdBy string) (int, error) {
	f.addCalls = append(f.addCalls, addQuoteCall{channel, text, createdBy})
	return f.addNumber, f.addErr
}

func (f *fakeQuoteStore) Get(_ context.Context, _ string, _ int) (commands.QuoteView, bool) {
	return f.getView, f.getOK
}

func (f *fakeQuoteStore) Random(_ context.Context, _ string) (commands.QuoteView, bool) {
	return f.randomView, f.randomOK
}

func (f *fakeQuoteStore) Delete(_ context.Context, _ string, number int) error {
	f.deleteCalls = append(f.deleteCalls, number)
	return f.deleteErr
}

func TestAddQuote_ParsesAndCallsStore(t *testing.T) {
	store := &fakeQuoteStore{addNumber: 4}
	cmd := commands.NewAddQuoteCommand(store)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)

	reply := cmd.Handler(context.Background(),
		modMsg("!addquote that was hilarious"),
		[]string{"that", "was", "hilarious"})

	require.Len(t, store.addCalls, 1)
	c := store.addCalls[0]
	assert.Equal(t, testChannel, c.channel)
	assert.Equal(t, "that was hilarious", c.text)
	assert.Equal(t, testViewer, c.createdBy)
	assert.Contains(t, reply.Text, "#4")
	assert.NotContains(t, reply.Text, "\n")
}

func TestAddQuote_EmptyTextUsage(t *testing.T) {
	store := &fakeQuoteStore{}
	cmd := commands.NewAddQuoteCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!addquote"), nil)
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.addCalls)
}

func TestAddQuote_StoreErrorFriendlyReply(t *testing.T) {
	store := &fakeQuoteStore{addErr: errors.New("boom")}
	cmd := commands.NewAddQuoteCommand(store)
	reply := cmd.Handler(context.Background(),
		modMsg("!addquote hi"), []string{"hi"})
	assert.Contains(t, reply.Text, "couldn't add quote")
	assert.NotContains(t, reply.Text, "\n")
}

func TestAddQuote_NilStore(t *testing.T) {
	cmd := commands.NewAddQuoteCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!addquote hi"), []string{"hi"})
	assert.Contains(t, reply.Text, "quotes are unavailable")
}

func TestQuote_ByNumber(t *testing.T) {
	store := &fakeQuoteStore{getView: commands.QuoteView{Number: 3, Text: "legendary"}, getOK: true}
	cmd := commands.NewQuoteCommand(store)
	assert.Equal(t, commands.RoleEveryone, cmd.MinRole)

	reply := cmd.Handler(context.Background(), msgText("!quote 3"), []string{"3"})
	assert.Contains(t, reply.Text, "#3: legendary")
}

func TestQuote_RandomNoArg(t *testing.T) {
	store := &fakeQuoteStore{randomView: commands.QuoteView{Number: 7, Text: "random one"}, randomOK: true}
	cmd := commands.NewQuoteCommand(store)
	reply := cmd.Handler(context.Background(), msgText("!quote"), nil)
	assert.Contains(t, reply.Text, "#7: random one")
}

func TestQuote_MissingNumber(t *testing.T) {
	store := &fakeQuoteStore{getOK: false}
	cmd := commands.NewQuoteCommand(store)
	reply := cmd.Handler(context.Background(), msgText("!quote 99"), []string{"99"})
	assert.Contains(t, reply.Text, "no quote #99")
}

func TestQuote_RandomEmpty(t *testing.T) {
	store := &fakeQuoteStore{randomOK: false}
	cmd := commands.NewQuoteCommand(store)
	reply := cmd.Handler(context.Background(), msgText("!quote"), nil)
	assert.Contains(t, reply.Text, "no quotes yet")
}

func TestQuote_NonNumericArgUsage(t *testing.T) {
	store := &fakeQuoteStore{}
	cmd := commands.NewQuoteCommand(store)
	reply := cmd.Handler(context.Background(), msgText("!quote abc"), []string{"abc"})
	assert.Contains(t, reply.Text, "usage: !quote")
}

func TestQuote_NonPositiveArgUsage(t *testing.T) {
	store := &fakeQuoteStore{}
	cmd := commands.NewQuoteCommand(store)
	reply := cmd.Handler(context.Background(), msgText("!quote 0"), []string{"0"})
	assert.Contains(t, reply.Text, "usage: !quote")
}

func TestQuote_NilStore(t *testing.T) {
	cmd := commands.NewQuoteCommand(nil)
	reply := cmd.Handler(context.Background(), msgText("!quote"), nil)
	assert.Contains(t, reply.Text, "quotes are unavailable")
}

func TestDeleteQuote_CallsStore(t *testing.T) {
	store := &fakeQuoteStore{}
	cmd := commands.NewDeleteQuoteCommand(store)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)

	reply := cmd.Handler(context.Background(), modMsg("!delquote 3"), []string{"3"})
	require.Len(t, store.deleteCalls, 1)
	assert.Equal(t, 3, store.deleteCalls[0])
	assert.Contains(t, reply.Text, "deleted quote #3")
}

func TestDeleteQuote_MissingNumberUsage(t *testing.T) {
	store := &fakeQuoteStore{}
	cmd := commands.NewDeleteQuoteCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!delquote"), nil)
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.deleteCalls)
}

func TestDeleteQuote_BadNumberUsage(t *testing.T) {
	store := &fakeQuoteStore{}
	cmd := commands.NewDeleteQuoteCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!delquote abc"), []string{"abc"})
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.deleteCalls)
}

func TestDeleteQuote_StoreErrorFriendlyReply(t *testing.T) {
	store := &fakeQuoteStore{deleteErr: errors.New("boom")}
	cmd := commands.NewDeleteQuoteCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!delquote 9"), []string{"9"})
	assert.Contains(t, reply.Text, "couldn't delete quote #9")
	assert.NotContains(t, reply.Text, "\n")
}

func TestDeleteQuote_NilStore(t *testing.T) {
	cmd := commands.NewDeleteQuoteCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!delquote 3"), []string{"3"})
	assert.Contains(t, reply.Text, "quotes are unavailable")
}
