package twitch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	irc "github.com/gempir/go-twitch-irc/v4"
	"github.com/nicklaw5/helix/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

// fakeIRCClient is a hand-rolled implementation of [ircClient] used by the
// lifecycle tests so we never touch the network.
type fakeIRCClient struct {
	mu sync.Mutex

	joined   []string
	sent     []sentMessage
	lastTok  string
	tokCalls int

	onConnect      func()
	onPrivate      func(irc.PrivateMessage)
	onClear        func(irc.ClearMessage)
	onClearChat    func(irc.ClearChatMessage)
	onUserNotice   func(irc.UserNoticeMessage)
	onReconnect    func(irc.ReconnectMessage)
	connectErr     error
	connectStarted chan struct{}
	disconnectCh   chan struct{}
}

type sentMessage struct {
	channel string
	text    string
}

func newFakeIRCClient() *fakeIRCClient {
	return &fakeIRCClient{
		connectStarted: make(chan struct{}),
		disconnectCh:   make(chan struct{}),
	}
}

func (f *fakeIRCClient) OnConnect(cb func())                          { f.onConnect = cb }
func (f *fakeIRCClient) OnPrivateMessage(cb func(irc.PrivateMessage)) { f.onPrivate = cb }
func (f *fakeIRCClient) OnClearMessage(cb func(irc.ClearMessage))     { f.onClear = cb }
func (f *fakeIRCClient) OnClearChatMessage(cb func(irc.ClearChatMessage)) {
	f.onClearChat = cb
}
func (f *fakeIRCClient) OnUserNoticeMessage(cb func(irc.UserNoticeMessage)) { f.onUserNotice = cb }
func (f *fakeIRCClient) OnReconnectMessage(cb func(irc.ReconnectMessage))   { f.onReconnect = cb }

func (f *fakeIRCClient) Join(channels ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.joined = append(f.joined, channels...)
}

func (f *fakeIRCClient) Say(channel, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMessage{channel: channel, text: text})
}

func (f *fakeIRCClient) Connect() error {
	close(f.connectStarted)
	if f.connectErr != nil {
		return f.connectErr
	}
	<-f.disconnectCh
	return irc.ErrClientDisconnected
}

func (f *fakeIRCClient) Disconnect() error {
	select {
	case <-f.disconnectCh:
	default:
		close(f.disconnectCh)
	}
	return nil
}

func (f *fakeIRCClient) SetIRCToken(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastTok = token
	f.tokCalls++
}

func (f *fakeIRCClient) lastToken() (string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastTok, f.tokCalls
}

func (f *fakeIRCClient) sentMessages() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sentMessage, len(f.sent))
	copy(out, f.sent)
	return out
}

// fakeHelix records every call made to the Helix client surface so tests
// can assert dispatch.
type fakeHelix struct {
	mu              sync.Mutex
	banCalls        []*helix.BanUserParams
	unbanCalls      []*helix.UnbanUserParams
	deleteCalls     []*helix.DeleteChatMessageParams
	getUsersCalls   []*helix.UsersParams
	lastTok         string
	tokCalls        int
	banErr          error
	unbanErr        error
	deleteErr       error
	getUsersResp    *helix.UsersResponse
	getUsersErr     error
	defaultUsersFor map[string]string
}

func (h *fakeHelix) SetUserAccessToken(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastTok = token
	h.tokCalls++
}

func (h *fakeHelix) lastToken() (string, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastTok, h.tokCalls
}

func (h *fakeHelix) snapshotBan() []*helix.BanUserParams {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*helix.BanUserParams, len(h.banCalls))
	copy(out, h.banCalls)
	return out
}

func (h *fakeHelix) snapshotUnban() []*helix.UnbanUserParams {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*helix.UnbanUserParams, len(h.unbanCalls))
	copy(out, h.unbanCalls)
	return out
}

func (h *fakeHelix) snapshotDelete() []*helix.DeleteChatMessageParams {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*helix.DeleteChatMessageParams, len(h.deleteCalls))
	copy(out, h.deleteCalls)
	return out
}

func (h *fakeHelix) BanUser(p *helix.BanUserParams) (*helix.BanUserResponse, error) {
	h.mu.Lock()
	h.banCalls = append(h.banCalls, p)
	h.mu.Unlock()
	if h.banErr != nil {
		return nil, h.banErr
	}
	return &helix.BanUserResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}, nil
}

func (h *fakeHelix) UnbanUser(p *helix.UnbanUserParams) (*helix.UnbanUserResponse, error) {
	h.mu.Lock()
	h.unbanCalls = append(h.unbanCalls, p)
	h.mu.Unlock()
	if h.unbanErr != nil {
		return nil, h.unbanErr
	}
	return &helix.UnbanUserResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 204}}, nil
}

func (h *fakeHelix) DeleteChatMessage(p *helix.DeleteChatMessageParams) (*helix.DeleteChatMessageResponse, error) {
	h.mu.Lock()
	h.deleteCalls = append(h.deleteCalls, p)
	h.mu.Unlock()
	if h.deleteErr != nil {
		return nil, h.deleteErr
	}
	return &helix.DeleteChatMessageResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 204}}, nil
}

func (h *fakeHelix) GetUsers(p *helix.UsersParams) (*helix.UsersResponse, error) {
	h.mu.Lock()
	h.getUsersCalls = append(h.getUsersCalls, p)
	h.mu.Unlock()
	if h.getUsersErr != nil {
		return nil, h.getUsersErr
	}
	if h.getUsersResp != nil {
		return h.getUsersResp, nil
	}
	resp := &helix.UsersResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	for _, login := range p.Logins {
		id, ok := h.defaultUsersFor[login]
		if !ok {
			id = login + "-id"
		}
		resp.Data.Users = append(resp.Data.Users, helix.User{ID: id, Login: login})
	}
	if len(p.Logins) == 0 {
		resp.Data.Users = append(resp.Data.Users, helix.User{ID: "bot-id", Login: "bot"})
	}
	return resp, nil
}

func newTestAdapter(t *testing.T, anon bool) (*Adapter, *fakeIRCClient, *fakeHelix) {
	t.Helper()
	fake := newFakeIRCClient()
	hx := &fakeHelix{}
	cfg := Config{
		Channels: []string{"broadcaster"},
		ClientID: "cid",
		newIRCClient: func(_ Config) ircClient {
			return fake
		},
		newHelixClient: func(_ Config) (helixClient, error) {
			return hx, nil
		},
	}
	if !anon {
		cfg.Username = "bot"
		cfg.OAuthToken = "oauth:token"
	}
	a := New(cfg)
	return a, fake, hx
}

func TestName(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	assert.Equal(t, "twitch", a.Name())
}

func TestConnect_AnonymousJoinsChannels(t *testing.T) {
	a, fake, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	<-fake.connectStarted
	fake.mu.Lock()
	joined := append([]string(nil), fake.joined...)
	fake.mu.Unlock()
	assert.Equal(t, []string{"broadcaster"}, joined)
	assert.NoError(t, a.Health())
}

func TestConnect_AlreadyConnected(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.Connect(context.Background())
	assert.ErrorIs(t, err, ErrAlreadyConnected)
}

func TestConnect_ContextCancelTriggersDisconnect(t *testing.T) {
	a, fake, _ := newTestAdapter(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, a.Connect(ctx))
	<-fake.connectStarted

	cancel()

	require.Eventually(t, func() bool {
		return errors.Is(a.Health(), ErrNotConnected)
	}, 2*time.Second, 10*time.Millisecond)
}

func TestDisconnect_Idempotent(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	require.NoError(t, a.Disconnect(context.Background()))
	require.NoError(t, a.Disconnect(context.Background()))
	assert.ErrorIs(t, a.Health(), ErrNotConnected)
}

func TestEventsClosedAfterDisconnect(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	ch := a.Events()
	require.NoError(t, a.Disconnect(context.Background()))
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "events channel must be closed after disconnect")
	case <-time.After(time.Second):
		t.Fatal("events channel was not closed")
	}
}

func TestPrivateMessagePumpedToEventsChannel(t *testing.T) {
	a, fake, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	<-fake.connectStarted

	fake.onPrivate(irc.PrivateMessage{
		User:    irc.User{ID: "1", Name: "viewer"},
		Channel: "broadcaster",
		ID:      "msg-1",
		Message: "hi",
		Tags:    map[string]string{"id": "msg-1"},
	})

	select {
	case e := <-a.Events():
		require.Equal(t, adapters.EventMessageCreated, e.Type)
		assert.Equal(t, "msg-1", e.Message.ID)
	case <-time.After(time.Second):
		t.Fatal("expected event on Events channel")
	}
}

func TestOnConnectEmitsConnectedEvent(t *testing.T) {
	a, fake, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	<-fake.connectStarted

	fake.onConnect()

	select {
	case e := <-a.Events():
		assert.Equal(t, adapters.EventConnected, e.Type)
	case <-time.After(time.Second):
		t.Fatal("expected connected event")
	}
}

func TestSendMessage_AnonymousRejected(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.Do(context.Background(), adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     "broadcaster",
		SendMessage: &adapters.SendMessageAction{Text: "hello"},
	})
	assert.ErrorIs(t, err, ErrAnonymous)
}

func TestSendMessage_AuthenticatedForwardsToSay(t *testing.T) {
	a, fake, _ := newTestAdapter(t, false)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.Do(context.Background(), adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     "broadcaster",
		SendMessage: &adapters.SendMessageAction{Text: "hello chat"},
	})
	require.NoError(t, err)
	sent := fake.sentMessages()
	require.Len(t, sent, 1)
	assert.Equal(t, "broadcaster", sent[0].channel)
	assert.Equal(t, "hello chat", sent[0].text)
}

func TestBanUser_ForwardsToHelix(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionBan,
		Channel: "broadcaster",
		Ban:     &adapters.BanAction{UserID: "42", Reason: "spam"},
	})
	require.NoError(t, err)
	calls := hx.snapshotBan()
	require.Len(t, calls, 1)
	assert.Equal(t, "987", calls[0].BroadcasterID)
	assert.Equal(t, "42", calls[0].Body.UserId)
	assert.Equal(t, "spam", calls[0].Body.Reason)
	assert.Equal(t, 0, calls[0].Body.Duration)
}

func TestTimeoutUser_PassesDuration(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionTimeout,
		Channel: "broadcaster",
		Timeout: &adapters.TimeoutAction{UserID: "42", Duration: 5 * time.Minute, Reason: "calm down"},
	})
	require.NoError(t, err)
	calls := hx.snapshotBan()
	require.Len(t, calls, 1)
	assert.Equal(t, 300, calls[0].Body.Duration)
}

func TestUntimeout_ForwardsToHelix(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.Do(context.Background(), adapters.Action{
		Type:      adapters.ActionUntimeout,
		Channel:   "broadcaster",
		Untimeout: &adapters.UntimeoutAction{UserID: "42"},
	})
	require.NoError(t, err)
	calls := hx.snapshotUnban()
	require.Len(t, calls, 1)
	assert.Equal(t, "42", calls[0].UserID)
}

func TestDeleteMessage_ForwardsToHelix(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.Do(context.Background(), adapters.Action{
		Type:          adapters.ActionDeleteMessage,
		Channel:       "broadcaster",
		DeleteMessage: &adapters.DeleteMessageAction{MessageID: "msg-9"},
	})
	require.NoError(t, err)
	calls := hx.snapshotDelete()
	require.Len(t, calls, 1)
	assert.Equal(t, "msg-9", calls[0].MessageID)
	assert.Equal(t, "987", calls[0].BroadcasterID)
}

func TestDoBeforeConnectFails(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	err := a.Do(context.Background(), adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     "broadcaster",
		SendMessage: &adapters.SendMessageAction{Text: "x"},
	})
	assert.ErrorIs(t, err, ErrNotConnected)
}

func TestDoUnknownAction(t *testing.T) {
	a, _, _ := newTestAdapter(t, false)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.Do(context.Background(), adapters.Action{Type: "nope", Channel: "broadcaster"})
	assert.ErrorIs(t, err, ErrUnknownAction)
}

func TestDoMissingPayloads(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	cases := []adapters.Action{
		{Type: adapters.ActionSendMessage, Channel: "broadcaster"},
		{Type: adapters.ActionDeleteMessage, Channel: "broadcaster"},
		{Type: adapters.ActionBan, Channel: "broadcaster"},
		{Type: adapters.ActionTimeout, Channel: "broadcaster"},
		{Type: adapters.ActionUntimeout, Channel: "broadcaster"},
	}
	for _, c := range cases {
		err := a.Do(context.Background(), c)
		assert.ErrorIs(t, err, ErrMissingPayload, "type=%s", c.Type)
	}
}

func TestDoContextCancelled(t *testing.T) {
	a, _, _ := newTestAdapter(t, false)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := a.Do(ctx, adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     "broadcaster",
		SendMessage: &adapters.SendMessageAction{Text: "x"},
	})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestHealth_ReportsNotConnectedBeforeConnect(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	assert.ErrorIs(t, a.Health(), ErrNotConnected)
}

func TestClearChatPumpedAsTimeout(t *testing.T) {
	a, fake, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	<-fake.connectStarted

	fake.onClearChat(irc.ClearChatMessage{
		Channel:        "broadcaster",
		BanDuration:    30,
		TargetUserID:   "9",
		TargetUsername: "spammer",
	})
	select {
	case e := <-a.Events():
		assert.Equal(t, adapters.EventUserTimedOut, e.Type)
	case <-time.After(time.Second):
		t.Fatal("expected timeout event")
	}
}

func TestUserNoticeIgnoredForUnknownMsgID(t *testing.T) {
	a, fake, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	<-fake.connectStarted

	fake.onUserNotice(irc.UserNoticeMessage{MsgID: "primepaidupgrade"})

	select {
	case e := <-a.Events():
		t.Fatalf("unexpected event %v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestReconnectEmitsReconnectingEvent(t *testing.T) {
	a, fake, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	<-fake.connectStarted

	fake.onReconnect(irc.ReconnectMessage{})

	select {
	case e := <-a.Events():
		assert.Equal(t, adapters.EventReconnecting, e.Type)
	case <-time.After(time.Second):
		t.Fatal("expected reconnecting event")
	}
}

func TestEmitDropsWhenChannelFull(t *testing.T) {
	a, fake, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	<-fake.connectStarted

	for i := 0; i < eventBuffer+10; i++ {
		fake.onPrivate(irc.PrivateMessage{
			Channel: "broadcaster",
			ID:      "m",
			User:    irc.User{Name: "x"},
		})
	}
	drained := 0
	for {
		select {
		case <-a.Events():
			drained++
		case <-time.After(100 * time.Millisecond):
			assert.LessOrEqual(t, drained, eventBuffer)
			return
		}
	}
}

func TestAnonymousUsernameShape(t *testing.T) {
	for i := 0; i < 20; i++ {
		got := anonymousUsername()
		require.Len(t, got, len("justinfan")+6)
		require.True(t, got[:len("justinfan")] == "justinfan")
		for _, r := range got[len("justinfan"):] {
			require.True(t, r >= '0' && r <= '9')
		}
	}
}

func TestDefaultHelixFactory_BuildsClientWithBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)

	c, err := defaultHelixFactory(Config{ClientID: "cid", OAuthToken: "tok", HelixBaseURL: srv.URL})
	require.NoError(t, err)
	require.NotNil(t, c)
	resp, err := c.GetUsers(&helix.UsersParams{Logins: []string{"x"}})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestHelixStatusError(t *testing.T) {
	assert.NoError(t, helixStatusError("op", 0, ""))
	assert.NoError(t, helixStatusError("op", 204, ""))
	err := helixStatusError("op", 401, "bad token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "bad token")
	err2 := helixStatusError("op", 500, "")
	require.Error(t, err2)
	assert.Contains(t, err2.Error(), "500")
}

func TestBroadcasterIDCachedAfterPrivateMessage(t *testing.T) {
	a, fake, hx := newTestAdapter(t, false)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	<-fake.connectStarted

	fake.onPrivate(irc.PrivateMessage{
		Channel: "broadcaster",
		RoomID:  "987",
		User:    irc.User{Name: "x"},
		ID:      "m",
	})
	<-a.Events()

	hx.mu.Lock()
	prevGetUsers := len(hx.getUsersCalls)
	hx.mu.Unlock()

	err := a.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionBan,
		Channel: "broadcaster",
		Ban:     &adapters.BanAction{UserID: "42"},
	})
	require.NoError(t, err)

	banCalls := hx.snapshotBan()
	require.Len(t, banCalls, 1)
	assert.Equal(t, "987", banCalls[0].BroadcasterID)

	hx.mu.Lock()
	usersTail := append([]*helix.UsersParams(nil), hx.getUsersCalls[prevGetUsers:]...)
	hx.mu.Unlock()
	resolverCalls := 0
	for _, p := range usersTail {
		if len(p.Logins) > 0 {
			resolverCalls++
		}
	}
	assert.Equal(t, 0, resolverCalls, "broadcaster id should come from cache, not Helix")
}

func TestHelixGetUsersFails_BanErrors(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.getUsersErr = errors.New("boom")
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.Do(context.Background(), adapters.Action{
		Type:    adapters.ActionBan,
		Channel: "broadcaster",
		Ban:     &adapters.BanAction{UserID: "42"},
	})
	require.Error(t, err)
}

func TestSetToken_RawTokenPrefixesIRCAndRawHelix(t *testing.T) {
	a, fake, hx := newTestAdapter(t, false)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	require.NoError(t, a.SetToken("newtok"))

	ircTok, ircN := fake.lastToken()
	hxTok, hxN := hx.lastToken()
	assert.Equal(t, "oauth:newtok", ircTok)
	assert.Equal(t, 1, ircN)
	assert.Equal(t, "newtok", hxTok)
	assert.Equal(t, 1, hxN)

	a.mu.Lock()
	cfgTok := a.cfg.OAuthToken
	connected := a.connected
	a.mu.Unlock()
	assert.Equal(t, "newtok", cfgTok)
	assert.True(t, connected, "adapter must remain connected after SetToken")
	assert.NoError(t, a.Health())
}

func TestSetToken_AlreadyPrefixedNoDoublePrefix(t *testing.T) {
	a, fake, hx := newTestAdapter(t, false)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	require.NoError(t, a.SetToken("oauth:newtok"))

	ircTok, _ := fake.lastToken()
	hxTok, _ := hx.lastToken()
	assert.Equal(t, "oauth:newtok", ircTok)
	assert.Equal(t, "newtok", hxTok)
}

func TestSetToken_EmptyTokenRejected(t *testing.T) {
	a, fake, hx := newTestAdapter(t, false)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.SetToken("")
	assert.ErrorIs(t, err, ErrEmptyToken)

	_, ircN := fake.lastToken()
	_, hxN := hx.lastToken()
	assert.Equal(t, 0, ircN)
	assert.Equal(t, 0, hxN)

	a.mu.Lock()
	cfgTok := a.cfg.OAuthToken
	a.mu.Unlock()
	assert.Equal(t, "oauth:token", cfgTok, "cfg unchanged after empty SetToken")
}

func TestSetToken_NotConnected(t *testing.T) {
	a, _, _ := newTestAdapter(t, false)
	err := a.SetToken("newtok")
	assert.ErrorIs(t, err, ErrNotConnected)
}

func TestSetToken_AnonymousRotationRejected(t *testing.T) {
	a, fake, hx := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.SetToken("newtok")
	assert.ErrorIs(t, err, ErrAnonymousRotation)

	_, ircN := fake.lastToken()
	_, hxN := hx.lastToken()
	assert.Equal(t, 0, ircN)
	assert.Equal(t, 0, hxN)
}

func TestSetToken_DoesNotDisruptEventStream(t *testing.T) {
	a, fake, _ := newTestAdapter(t, false)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	<-fake.connectStarted

	require.NoError(t, a.SetToken("rotated"))
	assert.NoError(t, a.Health())

	fake.onPrivate(irc.PrivateMessage{
		User:    irc.User{ID: "1", Name: "viewer"},
		Channel: "broadcaster",
		ID:      "msg-after-rotate",
		Message: "still here",
		Tags:    map[string]string{"id": "msg-after-rotate"},
	})
	select {
	case e := <-a.Events():
		assert.Equal(t, adapters.EventMessageCreated, e.Type)
		assert.Equal(t, "msg-after-rotate", e.Message.ID)
	case <-time.After(time.Second):
		t.Fatal("events channel disrupted by SetToken")
	}
}

func TestSetToken_RaceWithHealthAndDo(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	var wg sync.WaitGroup
	const iters = 50

	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = a.SetToken("tok-rotating")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = a.Health()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = a.Do(context.Background(), adapters.Action{
				Type:        adapters.ActionSendMessage,
				Channel:     "broadcaster",
				SendMessage: &adapters.SendMessageAction{Text: "x"},
			})
		}
	}()
	wg.Wait()
}
