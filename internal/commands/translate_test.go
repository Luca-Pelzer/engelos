package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type setLangCall struct {
	channel string
	lang    string
}

type fakeTranslateStore struct {
	enabled    bool
	lang       string
	setEnaErr  error
	setLangErr error
	enaCalls   []setEconomyCall
	langCalls  []setLangCall
}

func (f *fakeTranslateStore) TranslateStatus(_ context.Context, _ string) (bool, string) {
	lang := f.lang
	if lang == "" {
		lang = "en"
	}
	return f.enabled, lang
}

func (f *fakeTranslateStore) SetTranslateEnabled(_ context.Context, channel string, enabled bool) error {
	f.enaCalls = append(f.enaCalls, setEconomyCall{channel, enabled})
	return f.setEnaErr
}

func (f *fakeTranslateStore) SetTranslateLang(_ context.Context, channel, lang string) error {
	f.langCalls = append(f.langCalls, setLangCall{channel, lang})
	return f.setLangErr
}

func TestTranslateToggle_NameAndRole(t *testing.T) {
	cmd := commands.NewTranslateToggleCommand(&fakeTranslateStore{})
	assert.Equal(t, "translate", cmd.Name)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)
}

func TestTranslateToggle_StatusReportsLang(t *testing.T) {
	store := &fakeTranslateStore{enabled: true, lang: "es"}
	cmd := commands.NewTranslateToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!translate"), nil)
	assert.Contains(t, reply.Text, "ON")
	assert.Contains(t, reply.Text, "es")
	assert.Empty(t, store.enaCalls)
}

func TestTranslateToggle_TurnOn(t *testing.T) {
	store := &fakeTranslateStore{enabled: false}
	cmd := commands.NewTranslateToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!translate on"), []string{"on"})
	require.Len(t, store.enaCalls, 1)
	assert.Equal(t, setEconomyCall{"chan-A", true}, store.enaCalls[0])
	assert.Contains(t, reply.Text, "ON")
}

func TestTranslateToggle_TurnOff(t *testing.T) {
	store := &fakeTranslateStore{enabled: true}
	cmd := commands.NewTranslateToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!translate off"), []string{"off"})
	require.Len(t, store.enaCalls, 1)
	assert.Equal(t, setEconomyCall{"chan-A", false}, store.enaCalls[0])
	assert.Contains(t, reply.Text, "OFF")
}

func TestTranslateToggle_SetLang(t *testing.T) {
	store := &fakeTranslateStore{enabled: true}
	cmd := commands.NewTranslateToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!translate lang es"), []string{"lang", "es"})
	require.Len(t, store.langCalls, 1)
	assert.Equal(t, setLangCall{"chan-A", "es"}, store.langCalls[0])
	assert.Contains(t, reply.Text, "es")
}

func TestTranslateToggle_SetLangRegionSuffix(t *testing.T) {
	store := &fakeTranslateStore{enabled: true}
	cmd := commands.NewTranslateToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!translate lang pt-br"), []string{"lang", "PT-BR"})
	require.Len(t, store.langCalls, 1)
	assert.Equal(t, setLangCall{"chan-A", "pt-br"}, store.langCalls[0])
	assert.Contains(t, reply.Text, "pt-br")
}

func TestTranslateToggle_SetLangInvalid(t *testing.T) {
	store := &fakeTranslateStore{enabled: true}
	cmd := commands.NewTranslateToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!translate lang englishhh"), []string{"lang", "englishhh"})
	assert.Contains(t, reply.Text, "not a valid language code")
	assert.Empty(t, store.langCalls)
}

func TestTranslateToggle_LangMissingArg(t *testing.T) {
	store := &fakeTranslateStore{enabled: true}
	cmd := commands.NewTranslateToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!translate lang"), []string{"lang"})
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.langCalls)
}

func TestTranslateToggle_SetError(t *testing.T) {
	store := &fakeTranslateStore{enabled: true, setEnaErr: errors.New("boom")}
	cmd := commands.NewTranslateToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!translate off"), []string{"off"})
	assert.Contains(t, reply.Text, "couldn't update")
}

func TestTranslateToggle_BadArg(t *testing.T) {
	store := &fakeTranslateStore{enabled: true}
	cmd := commands.NewTranslateToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!translate wat"), []string{"wat"})
	assert.Contains(t, reply.Text, "usage:")
}

func TestTranslateToggle_NilStore(t *testing.T) {
	cmd := commands.NewTranslateToggleCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!translate"), nil)
	assert.Contains(t, reply.Text, "unavailable")
}
