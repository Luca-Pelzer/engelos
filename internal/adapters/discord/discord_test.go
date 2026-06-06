package discord

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

func newTestAdapter(cfg Config) *Adapter {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return New(cfg)
}

func TestAdapter_Name(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "x"})
	assert.Equal(t, "discord", a.Name())
}

func TestAdapter_Connect_EmptyToken(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: ""})
	err := a.Connect(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTokenRequired), "got %v", err)
}

func TestAdapter_Connect_WhitespaceToken(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "   "})
	err := a.Connect(context.Background())
	assert.True(t, errors.Is(err, ErrTokenRequired), "got %v", err)
}

func TestAdapter_Connect_SessionFactoryError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("factory boom")
	a := newTestAdapter(Config{
		Token: "tkn",
		NewSession: func(token string) (*discordgo.Session, error) {
			assert.Equal(t, "Bot tkn", token, "Bot prefix should be added")
			return nil, sentinel
		},
	})
	err := a.Connect(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.ErrorIs(t, a.Health(), ErrNotConnected)
}

func TestAdapter_Connect_TokenWithBotPrefix(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("stop here")
	var got string
	a := newTestAdapter(Config{
		Token: "Bot already",
		NewSession: func(token string) (*discordgo.Session, error) {
			got = token
			return nil, sentinel
		},
	})
	_ = a.Connect(context.Background())
	assert.Equal(t, "Bot already", got, "existing prefix must not be doubled")
}

func TestAdapter_Disconnect_Idempotent(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	require.NoError(t, a.Disconnect(context.Background()))
	require.NoError(t, a.Disconnect(context.Background()))
}

func TestAdapter_Health_NotConnected(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	assert.ErrorIs(t, a.Health(), ErrNotConnected)
}

func TestAdapter_Do_NotConnected(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	err := a.Do(context.Background(), adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     "c",
		SendMessage: &adapters.SendMessageAction{Text: "hi"},
	})
	assert.ErrorIs(t, err, ErrNotConnected)
}

func TestAdapter_Do_ContextCancelled(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := a.Do(ctx, adapters.Action{Type: adapters.ActionSendMessage, Channel: "c"})
	assert.ErrorIs(t, err, context.Canceled)
}

// fakeConnected installs minimum internal state so Do/emit code paths can be
// exercised without a real gateway connection. Open() makes a real network
// call we cannot stub, so this white-box helper is the only way to reach the
// switch in Do and the buffered emit path.
func fakeConnected(a *Adapter, sess *discordgo.Session) {
	a.mu.Lock()
	a.session = sess
	a.events = make(chan adapters.Event, eventBuffer)
	a.connected = true
	a.healthErr = nil
	a.mu.Unlock()
}

func TestAdapter_Do_UnknownActionType(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	fakeConnected(a, &discordgo.Session{})

	err := a.Do(context.Background(), adapters.Action{
		Type:    "no-such-action",
		Channel: "c",
	})
	assert.ErrorIs(t, err, ErrUnknownAction)
}

func TestAdapter_Do_MissingPayloads(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	fakeConnected(a, &discordgo.Session{})

	cases := []struct {
		name string
		act  adapters.Action
	}{
		{"send", adapters.Action{Type: adapters.ActionSendMessage, Channel: "c"}},
		{"delete", adapters.Action{Type: adapters.ActionDeleteMessage, Channel: "c"}},
		{"ban", adapters.Action{Type: adapters.ActionBan, Channel: "c"}},
		{"timeout", adapters.Action{Type: adapters.ActionTimeout, Channel: "c"}},
		{"untimeout", adapters.Action{Type: adapters.ActionUntimeout, Channel: "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := a.Do(context.Background(), tc.act)
			assert.ErrorIs(t, err, ErrMissingPayload)
		})
	}
}

func TestAdapter_Do_BanWithoutChannelMapping(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	fakeConnected(a, &discordgo.Session{})

	err := a.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionBan,
		Channel: "unmapped-channel",
		Ban:     &adapters.BanAction{UserID: "u"},
	})
	assert.ErrorIs(t, err, ErrUnknownGuild)
}

func TestAdapter_Events_BufferSize(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	fakeConnected(a, &discordgo.Session{})

	ch := a.Events()
	require.NotNil(t, ch)

	for i := 0; i < eventBuffer; i++ {
		a.emit(adapters.Event{ID: "x", Type: adapters.EventMessageCreated})
	}

	done := make(chan struct{})
	go func() {
		a.emit(adapters.Event{ID: "drop-me", Type: adapters.EventMessageCreated})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emit blocked when channel was full; should drop instead")
	}

	count := 0
drain:
	for {
		select {
		case <-ch:
			count++
		default:
			break drain
		}
	}
	assert.Equal(t, eventBuffer, count, "should have drained exactly eventBuffer events")
}

func TestAdapter_Emit_WhenNotConnected_Drops(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	a.emit(adapters.Event{ID: "lost", Type: adapters.EventConnected})
	assert.Nil(t, a.Events())
}

func TestAdapter_ChannelAllowedID(t *testing.T) {
	t.Parallel()

	t.Run("empty allow-list permits everything", func(t *testing.T) {
		t.Parallel()
		a := newTestAdapter(Config{Token: "tkn"})
		assert.True(t, a.channelAllowedID("any-channel"))
	})

	t.Run("non-empty allow-list filters", func(t *testing.T) {
		t.Parallel()
		a := newTestAdapter(Config{Token: "tkn", Channels: []string{"allowed-1", "allowed-2"}})
		assert.True(t, a.channelAllowedID("allowed-1"))
		assert.True(t, a.channelAllowedID("allowed-2"))
		assert.False(t, a.channelAllowedID("blocked"))
	})

	t.Run("empty channel ids in config are ignored", func(t *testing.T) {
		t.Parallel()
		a := newTestAdapter(Config{Token: "tkn", Channels: []string{"", "x"}})
		assert.True(t, a.channelAllowedID("x"))
		assert.False(t, a.channelAllowedID(""))
	})
}

func TestAdapter_OnMessageCreate_LearnsGuildMapping(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	fakeConnected(a, &discordgo.Session{})

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "m1",
			ChannelID: "chan-1",
			GuildID:   "guild-1",
			Content:   "hi",
			Author:    &discordgo.User{ID: "u", Username: "n"},
		},
	}
	a.onMessageCreate(m)

	a.mu.Lock()
	g := a.channelToGuild["chan-1"]
	a.mu.Unlock()
	assert.Equal(t, "guild-1", g)

	select {
	case ev := <-a.Events():
		assert.Equal(t, adapters.EventMessageCreated, ev.Type)
	case <-time.After(time.Second):
		t.Fatal("expected message.created event on the channel")
	}
}

func TestAdapter_OnMessageCreate_ChannelFiltered(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn", Channels: []string{"only-this"}})
	fakeConnected(a, &discordgo.Session{})

	m := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "m1",
			ChannelID: "not-this",
			Content:   "hi",
			Author:    &discordgo.User{ID: "u", Username: "n"},
		},
	}
	a.onMessageCreate(m)

	select {
	case ev := <-a.Events():
		t.Fatalf("expected drop, got %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAdapter_OnReady_PopulatesChannelMapping(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	fakeConnected(a, &discordgo.Session{})

	r := &discordgo.Ready{
		Guilds: []*discordgo.Guild{
			{
				ID: "g1",
				Channels: []*discordgo.Channel{
					{ID: "c-a"},
					{ID: "c-b"},
				},
			},
			{ID: "g-empty"},
		},
	}
	a.onReady(r)

	a.mu.Lock()
	gotA := a.channelToGuild["c-a"]
	gotB := a.channelToGuild["c-b"]
	a.mu.Unlock()
	assert.Equal(t, "g1", gotA)
	assert.Equal(t, "g1", gotB)

	select {
	case ev := <-a.Events():
		assert.Equal(t, adapters.EventConnected, ev.Type)
	case <-time.After(time.Second):
		t.Fatal("expected platform.connected event")
	}
}

func TestAdapter_OnDisconnect_EmitsBothEvents(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	fakeConnected(a, &discordgo.Session{})

	a.onDisconnect()

	var types []adapters.EventType
	deadline := time.After(time.Second)
	for len(types) < 2 {
		select {
		case ev := <-a.Events():
			types = append(types, ev.Type)
		case <-deadline:
			t.Fatalf("only got %d events: %v", len(types), types)
		}
	}
	assert.Equal(t, []adapters.EventType{adapters.EventDisconnected, adapters.EventReconnecting}, types)
	assert.Error(t, a.Health())
}

func TestAdapter_OnConnect_ClearsHealthErr(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	fakeConnected(a, &discordgo.Session{})
	a.mu.Lock()
	a.healthErr = errors.New("transient")
	a.mu.Unlock()

	a.onConnect()
	assert.NoError(t, a.Health())
}

func TestAdapter_RoleLookup_NilSessionState(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})
	fakeConnected(a, &discordgo.Session{})

	lookup := a.roleLookup("guild-1")
	assert.Nil(t, lookup("any-role"), "no State cache -> always nil")
	assert.Nil(t, lookup(""), "empty role -> nil")

	lookup2 := a.roleLookup("")
	assert.Nil(t, lookup2("any-role"), "empty guild -> nil")
}

// Ensure concurrent Health/Do reads under -race don't trip the mutex.
func TestAdapter_ConcurrentHealthAndDo(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(Config{Token: "tkn"})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = a.Health()
		}()
		go func() {
			defer wg.Done()
			_ = a.Do(context.Background(), adapters.Action{
				Type:    adapters.ActionSendMessage,
				Channel: "c",
			})
		}()
	}
	wg.Wait()
}
