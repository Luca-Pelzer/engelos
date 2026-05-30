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
	getStreamsCalls []*helix.StreamsParams
	getStreamsResp  *helix.StreamsResponse
	getStreamsErr   error

	createRewardResp *helix.ChannelCustomRewardResponse
	createRewardErr  error
	lastCreateParams *helix.ChannelCustomRewardsParams

	listRewardsResp *helix.ChannelCustomRewardResponse
	listRewardsErr  error
	lastListParams  *helix.GetCustomRewardsParams

	deleteRewardResp *helix.DeleteCustomRewardsResponse
	deleteRewardErr  error
	lastDeleteReward *helix.DeleteCustomRewardsParams

	updateResp       *helix.ChannelCustomRewardsRedemptionResponse
	updateErr        error
	lastUpdateParams *helix.UpdateChannelCustomRewardsRedemptionStatusParams

	subResp       *helix.EventSubSubscriptionsResponse
	subErr        error
	lastSubParams *helix.EventSubSubscription
}

func (h *fakeHelix) CreateEventSubSubscription(p *helix.EventSubSubscription) (*helix.EventSubSubscriptionsResponse, error) {
	h.mu.Lock()
	h.lastSubParams = p
	h.mu.Unlock()
	if h.subErr != nil {
		return nil, h.subErr
	}
	if h.subResp != nil {
		return h.subResp, nil
	}
	return &helix.EventSubSubscriptionsResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 202}}, nil
}

func (h *fakeHelix) snapshotSub() *helix.EventSubSubscription {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastSubParams
}

func (h *fakeHelix) CreateCustomReward(p *helix.ChannelCustomRewardsParams) (*helix.ChannelCustomRewardResponse, error) {
	h.mu.Lock()
	h.lastCreateParams = p
	h.mu.Unlock()
	if h.createRewardErr != nil {
		return nil, h.createRewardErr
	}
	if h.createRewardResp != nil {
		return h.createRewardResp, nil
	}
	return &helix.ChannelCustomRewardResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}, nil
}

func (h *fakeHelix) GetCustomRewards(p *helix.GetCustomRewardsParams) (*helix.ChannelCustomRewardResponse, error) {
	h.mu.Lock()
	h.lastListParams = p
	h.mu.Unlock()
	if h.listRewardsErr != nil {
		return nil, h.listRewardsErr
	}
	if h.listRewardsResp != nil {
		return h.listRewardsResp, nil
	}
	return &helix.ChannelCustomRewardResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}, nil
}

func (h *fakeHelix) DeleteCustomReward(p *helix.DeleteCustomRewardsParams) (*helix.DeleteCustomRewardsResponse, error) {
	h.mu.Lock()
	h.lastDeleteReward = p
	h.mu.Unlock()
	if h.deleteRewardErr != nil {
		return nil, h.deleteRewardErr
	}
	if h.deleteRewardResp != nil {
		return h.deleteRewardResp, nil
	}
	return &helix.DeleteCustomRewardsResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 204}}, nil
}

func (h *fakeHelix) UpdateChannelCustomRewardsRedemptionStatus(p *helix.UpdateChannelCustomRewardsRedemptionStatusParams) (*helix.ChannelCustomRewardsRedemptionResponse, error) {
	h.mu.Lock()
	h.lastUpdateParams = p
	h.mu.Unlock()
	if h.updateErr != nil {
		return nil, h.updateErr
	}
	if h.updateResp != nil {
		return h.updateResp, nil
	}
	return &helix.ChannelCustomRewardsRedemptionResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}, nil
}

func (h *fakeHelix) snapshotCreate() *helix.ChannelCustomRewardsParams {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastCreateParams
}

func (h *fakeHelix) snapshotList() *helix.GetCustomRewardsParams {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastListParams
}

func (h *fakeHelix) snapshotDeleteReward() *helix.DeleteCustomRewardsParams {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastDeleteReward
}

func (h *fakeHelix) snapshotUpdate() *helix.UpdateChannelCustomRewardsRedemptionStatusParams {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastUpdateParams
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

func (h *fakeHelix) GetStreams(p *helix.StreamsParams) (*helix.StreamsResponse, error) {
	h.mu.Lock()
	h.getStreamsCalls = append(h.getStreamsCalls, p)
	h.mu.Unlock()
	if h.getStreamsErr != nil {
		return nil, h.getStreamsErr
	}
	if h.getStreamsResp != nil {
		return h.getStreamsResp, nil
	}
	return &helix.StreamsResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}, nil
}

func (h *fakeHelix) getStreamsCallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.getStreamsCalls)
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

func liveStreamsResp(start time.Time) *helix.StreamsResponse {
	resp := &helix.StreamsResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	resp.Data.Streams = []helix.Stream{{
		UserLogin:   "broadcaster",
		Type:        "live",
		StartedAt:   start,
		GameName:    "Elden Ring",
		Title:       "blind run",
		ViewerCount: 42,
	}}
	return resp
}

func TestStreamInfo_AnonymousReturnsHelixUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.StreamInfo(context.Background(), "broadcaster")
	assert.ErrorIs(t, err, ErrHelixUnavailable)
}

func TestStreamInfo_Live(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	start := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	hx.getStreamsResp = liveStreamsResp(start)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	info, err := a.StreamInfo(context.Background(), "#Broadcaster")
	require.NoError(t, err)
	assert.True(t, info.Live)
	assert.Equal(t, start, info.StartedAt)
	assert.Equal(t, "Elden Ring", info.GameName)
	assert.Equal(t, "blind run", info.Title)
	assert.Equal(t, 42, info.ViewerCount)
}

func TestStreamInfo_Offline(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.getStreamsResp = &helix.StreamsResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	info, err := a.StreamInfo(context.Background(), "broadcaster")
	require.NoError(t, err)
	assert.False(t, info.Live)
	assert.True(t, info.StartedAt.IsZero())
	assert.Empty(t, info.GameName)
	assert.Empty(t, info.Title)
}

func TestStreamInfo_CachesWithinTTLAndExpires(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.getStreamsResp = liveStreamsResp(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	now := time.Date(2026, 5, 30, 13, 0, 0, 0, time.UTC)
	a.nowFn = func() time.Time { return now }

	_, err := a.StreamInfo(context.Background(), "broadcaster")
	require.NoError(t, err)
	_, err = a.StreamInfo(context.Background(), "broadcaster")
	require.NoError(t, err)
	assert.Equal(t, 1, hx.getStreamsCallCount(), "second call within TTL must hit cache")

	now = now.Add(streamCacheTTL + time.Second)
	_, err = a.StreamInfo(context.Background(), "broadcaster")
	require.NoError(t, err)
	assert.Equal(t, 2, hx.getStreamsCallCount(), "call past TTL must re-fetch")
}

func TestStreamInfo_HelixErrorNotCached(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.getStreamsErr = errors.New("boom")
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.StreamInfo(context.Background(), "broadcaster")
	require.Error(t, err)
	_, err = a.StreamInfo(context.Background(), "broadcaster")
	require.Error(t, err)
	assert.Equal(t, 2, hx.getStreamsCallCount(), "errors must not be cached; both calls hit Helix")
}

func usersResp(u ...helix.User) *helix.UsersResponse {
	resp := &helix.UsersResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	resp.Data.Users = append(resp.Data.Users, u...)
	return resp
}

func customRewardsResp(r ...helix.ChannelCustomReward) *helix.ChannelCustomRewardResponse {
	resp := &helix.ChannelCustomRewardResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	resp.Data.ChannelCustomRewards = append(resp.Data.ChannelCustomRewards, r...)
	return resp
}

func TestBroadcasterType(t *testing.T) {
	cases := []struct {
		name string
		typ  string
	}{
		{"affiliate", "affiliate"},
		{"partner", "partner"},
		{"none", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, hx := newTestAdapter(t, false)
			hx.getUsersResp = usersResp(helix.User{ID: "987", Login: "broadcaster", BroadcasterType: tc.typ})
			require.NoError(t, a.Connect(context.Background()))
			t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

			got, err := a.BroadcasterType(context.Background(), "#Broadcaster")
			require.NoError(t, err)
			assert.Equal(t, tc.typ, got)
		})
	}
}

func TestBroadcasterType_AnonymousUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.BroadcasterType(context.Background(), "broadcaster")
	assert.ErrorIs(t, err, ErrHelixUnavailable)
}

func TestBroadcasterType_GetUsersErrorPropagated(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.getUsersErr = errors.New("boom")
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.BroadcasterType(context.Background(), "broadcaster")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestBroadcasterType_EmptyUserList(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.getUsersResp = usersResp()
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.BroadcasterType(context.Background(), "broadcaster")
	require.Error(t, err)
}

func TestCreateReward_MapsSpecAndResponse(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.createRewardResp = customRewardsResp(helix.ChannelCustomReward{
		ID:        "rew-1",
		Title:     "Hydrate",
		Cost:      500,
		Prompt:    "make me drink water",
		IsEnabled: true,
	})
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	reward, err := a.CreateReward(context.Background(), "#Broadcaster", RewardSpec{
		Title:           "Hydrate",
		Cost:            500,
		Prompt:          "make me drink water",
		Enabled:         true,
		UserInputNeeded: true,
		BackgroundColor: "#00E5CB",
	})
	require.NoError(t, err)
	assert.Equal(t, Reward{ID: "rew-1", Title: "Hydrate", Cost: 500, Prompt: "make me drink water", Enabled: true}, reward)

	p := hx.snapshotCreate()
	require.NotNil(t, p)
	assert.Equal(t, "987", p.BroadcasterID)
	assert.Equal(t, "Hydrate", p.Title)
	assert.Equal(t, 500, p.Cost)
	assert.Equal(t, "make me drink water", p.Prompt)
	assert.True(t, p.IsEnabled)
	assert.True(t, p.IsUserInputRequired)
	assert.Equal(t, "#00E5CB", p.BackgroundColor)
}

func TestCreateReward_AnonymousUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.CreateReward(context.Background(), "broadcaster", RewardSpec{Title: "x", Cost: 1})
	assert.ErrorIs(t, err, ErrHelixUnavailable)
}

func TestCreateReward_HelixErrorPropagated(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.createRewardErr = errors.New("kaboom")
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.CreateReward(context.Background(), "broadcaster", RewardSpec{Title: "x", Cost: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kaboom")
}

func TestListManageableRewards_SetsFlagAndMaps(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.listRewardsResp = customRewardsResp(
		helix.ChannelCustomReward{ID: "a", Title: "First", Cost: 100, Prompt: "p1", IsEnabled: true},
		helix.ChannelCustomReward{ID: "b", Title: "Second", Cost: 200, Prompt: "p2", IsEnabled: false},
	)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	rewards, err := a.ListManageableRewards(context.Background(), "broadcaster")
	require.NoError(t, err)
	require.Len(t, rewards, 2)
	assert.Equal(t, Reward{ID: "a", Title: "First", Cost: 100, Prompt: "p1", Enabled: true}, rewards[0])
	assert.Equal(t, Reward{ID: "b", Title: "Second", Cost: 200, Prompt: "p2", Enabled: false}, rewards[1])

	p := hx.snapshotList()
	require.NotNil(t, p)
	assert.Equal(t, "987", p.BroadcasterID)
	assert.True(t, p.OnlyManageableRewards)
}

func TestListManageableRewards_AnonymousUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.ListManageableRewards(context.Background(), "broadcaster")
	assert.ErrorIs(t, err, ErrHelixUnavailable)
}

func TestDeleteReward_PassesIDsAndPropagatesError(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	require.NoError(t, a.DeleteReward(context.Background(), "broadcaster", "rew-9"))
	p := hx.snapshotDeleteReward()
	require.NotNil(t, p)
	assert.Equal(t, "987", p.BroadcasterID)
	assert.Equal(t, "rew-9", p.ID)

	hx.deleteRewardErr = errors.New("nope")
	err := a.DeleteReward(context.Background(), "broadcaster", "rew-9")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}

func TestFulfillAndCancelRedemption_StatusMapping(t *testing.T) {
	cases := []struct {
		name   string
		call   func(a *Adapter) error
		status string
	}{
		{"fulfill", func(a *Adapter) error {
			return a.FulfillRedemption(context.Background(), "broadcaster", "rew-1", "red-7")
		}, "FULFILLED"},
		{"cancel", func(a *Adapter) error {
			return a.CancelRedemption(context.Background(), "broadcaster", "rew-1", "red-7")
		}, "CANCELED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, hx := newTestAdapter(t, false)
			hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
			require.NoError(t, a.Connect(context.Background()))
			t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

			require.NoError(t, tc.call(a))
			p := hx.snapshotUpdate()
			require.NotNil(t, p)
			assert.Equal(t, tc.status, p.Status)
			assert.Equal(t, "987", p.BroadcasterID)
			assert.Equal(t, "rew-1", p.RewardID)
			assert.Equal(t, "red-7", p.ID)
		})
	}
}

func TestRedemption_AnonymousUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	assert.ErrorIs(t, a.FulfillRedemption(context.Background(), "b", "r", "x"), ErrHelixUnavailable)
	assert.ErrorIs(t, a.CancelRedemption(context.Background(), "b", "r", "x"), ErrHelixUnavailable)
}

func TestRewardBroadcasterID_CachedAcrossCalls(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	require.NoError(t, a.DeleteReward(context.Background(), "broadcaster", "rew-1"))
	require.NoError(t, a.FulfillRedemption(context.Background(), "broadcaster", "rew-1", "red-1"))

	hx.mu.Lock()
	resolverCalls := 0
	for _, p := range hx.getUsersCalls {
		for _, login := range p.Logins {
			if login == "broadcaster" {
				resolverCalls++
			}
		}
	}
	hx.mu.Unlock()
	assert.Equal(t, 1, resolverCalls, "broadcaster id must be resolved via GetUsers only once then cached")
}

func TestSubscribeRedemptions_HappyPath(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	require.NoError(t, a.SubscribeRedemptions(context.Background(), "#Broadcaster", "sess-42"))

	p := hx.snapshotSub()
	require.NotNil(t, p)
	assert.Equal(t, "channel.channel_points_custom_reward_redemption.add", p.Type)
	assert.Equal(t, "1", p.Version)
	assert.Equal(t, "987", p.Condition.BroadcasterUserID)
	assert.Equal(t, "websocket", p.Transport.Method)
	assert.Equal(t, "sess-42", p.Transport.SessionID)
}

func TestSubscribeRedemptions_AnonymousUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.SubscribeRedemptions(context.Background(), "broadcaster", "sess-1")
	assert.ErrorIs(t, err, ErrHelixUnavailable)
}

func TestSubscribeRedemptions_HelixErrorPropagated(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.subErr = errors.New("kaboom")
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.SubscribeRedemptions(context.Background(), "broadcaster", "sess-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kaboom")
}

func TestSubscribeRedemptions_Non2xxSurfacesError(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.subResp = &helix.EventSubSubscriptionsResponse{
		ResponseCommon: helix.ResponseCommon{StatusCode: 403, ErrorMessage: "forbidden"},
	}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.SubscribeRedemptions(context.Background(), "broadcaster", "sess-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "forbidden")
}

func TestCreateReward_Non2xxSurfacesError(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.createRewardResp = &helix.ChannelCustomRewardResponse{
		ResponseCommon: helix.ResponseCommon{StatusCode: 403, ErrorMessage: "channel points are not available for the broadcaster"},
	}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.CreateReward(context.Background(), "broadcaster", RewardSpec{Title: "x", Cost: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "channel points are not available")
}
