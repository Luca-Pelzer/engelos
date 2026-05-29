package commands_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandVariables_AllTokens(t *testing.T) {
	msg := commands.Message{Username: "alice", Channel: "kalmegh"}
	got := commands.ExpandVariables("hi $user, welcome to $channel — $args",
		msg, []string{"have", "fun"})
	assert.Equal(t, "hi alice, welcome to kalmegh — have fun", got)
}

func TestExpandVariables_NoArgsBecomesEmpty(t *testing.T) {
	msg := commands.Message{Username: "alice", Channel: "kalmegh"}
	got := commands.ExpandVariables("hi $args!", msg, nil)
	assert.Equal(t, "hi !", got)
}

func TestExpandVariables_UnknownTokensUntouched(t *testing.T) {
	msg := commands.Message{Username: "alice", Channel: "kalmegh"}
	got := commands.ExpandVariables("$user $foo $count $touser", msg, nil)
	assert.Equal(t, "alice $foo $count $touser", got)
}

func TestExpandVariables_MultipleOccurrences(t *testing.T) {
	msg := commands.Message{Username: "alice"}
	got := commands.ExpandVariables("$user $user $user", msg, nil)
	assert.Equal(t, "alice alice alice", got)
}

func TestExpandVariables_EmptyTemplate(t *testing.T) {
	assert.Equal(t, "", commands.ExpandVariables("", commands.Message{}, nil))
}

// fakeResolver records calls and returns a programmable result.
type fakeResolver struct {
	mu          sync.Mutex
	calls       []resolverCall
	rc          commands.ResolvedCommand
	hit         bool
	panicReason string
}

type resolverCall struct {
	channel string
	name    string
}

func (f *fakeResolver) Resolve(_ context.Context, channel, name string) (commands.ResolvedCommand, bool) {
	f.mu.Lock()
	f.calls = append(f.calls, resolverCall{channel: channel, name: name})
	f.mu.Unlock()
	if f.panicReason != "" {
		panic(f.panicReason)
	}
	return f.rc, f.hit
}

func (f *fakeResolver) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestResolver_HitExpandsResponse(t *testing.T) {
	r := &fakeResolver{rc: commands.ResolvedCommand{Response: "Welcome $user to $channel!"}, hit: true}
	e := commands.New(commands.Config{Logger: silentLogger(), Resolver: r})

	reply, ok := e.Handle(context.Background(), msgText("!hello"))
	require.True(t, ok)
	assert.Equal(t, "Welcome alice to chan-A!", reply.Text)
	assert.Equal(t, 1, r.callCount())
	assert.Equal(t, "hello", r.calls[0].name)
}

func TestResolver_MissYieldsNotACommand(t *testing.T) {
	r := &fakeResolver{hit: false}
	e := commands.New(commands.Config{Logger: silentLogger(), Resolver: r})

	reply, ok := e.Handle(context.Background(), msgText("!nothing"))
	assert.False(t, ok)
	assert.Empty(t, reply.Text)
}

func TestResolver_StaticTakesPrecedence(t *testing.T) {
	r := &fakeResolver{rc: commands.ResolvedCommand{Response: "dynamic"}, hit: true}
	e := commands.New(commands.Config{Logger: silentLogger(), Resolver: r})
	require.NoError(t, e.Register(commands.Command{
		Name:    "pity",
		Handler: okHandler("static"),
	}))

	reply, ok := e.Handle(context.Background(), msgText("!pity"))
	require.True(t, ok)
	assert.Equal(t, "static", reply.Text)
	assert.Equal(t, 0, r.callCount(), "resolver MUST NOT be consulted for static names")
}

func TestResolver_PermissionGate(t *testing.T) {
	r := &fakeResolver{
		rc:  commands.ResolvedCommand{Response: "mods only", MinRole: commands.RoleModerator},
		hit: true,
	}
	e := commands.New(commands.Config{Logger: silentLogger(), Resolver: r})

	reply, ok := e.Handle(context.Background(), msgText("!modcmd"))
	assert.True(t, ok, "denied dynamic invocation is still consumed")
	assert.Empty(t, reply.Text)

	mod := msgText("!modcmd")
	mod.IsModerator = true
	reply, ok = e.Handle(context.Background(), mod)
	assert.True(t, ok)
	assert.Equal(t, "mods only", reply.Text)
}

func TestResolver_CooldownGate(t *testing.T) {
	clk := newFakeClock()
	r := &fakeResolver{
		rc:  commands.ResolvedCommand{Response: "ok", Cooldown: 5 * time.Second},
		hit: true,
	}
	e := commands.New(commands.Config{Logger: silentLogger(), Now: clk.Now, Resolver: r})

	reply, ok := e.Handle(context.Background(), msgText("!cd"))
	require.True(t, ok)
	assert.Equal(t, "ok", reply.Text)

	reply, ok = e.Handle(context.Background(), msgText("!cd"))
	assert.True(t, ok, "throttled dynamic invocation is still consumed")
	assert.Empty(t, reply.Text)

	clk.Advance(5 * time.Second)
	reply, ok = e.Handle(context.Background(), msgText("!cd"))
	assert.True(t, ok)
	assert.Equal(t, "ok", reply.Text)
}

func TestResolver_PanicTreatedAsMiss(t *testing.T) {
	r := &fakeResolver{hit: true, panicReason: "kaboom"}
	e := commands.New(commands.Config{Logger: silentLogger(), Resolver: r})

	require.NotPanics(t, func() {
		reply, ok := e.Handle(context.Background(), msgText("!boom"))
		assert.False(t, ok)
		assert.Empty(t, reply.Text)
	})
}

func TestResolver_NotConsultedWhenNil(t *testing.T) {
	e := commands.New(commands.Config{Logger: silentLogger()})
	reply, ok := e.Handle(context.Background(), msgText("!nothing"))
	assert.False(t, ok)
	assert.Empty(t, reply.Text)
}

// ---- admin commands ----

type fakeCustomStore struct {
	mu      sync.Mutex
	addCall struct {
		called    bool
		channel   string
		name      string
		response  string
		minRole   string
		createdBy string
	}
	editCall struct {
		called   bool
		channel  string
		name     string
		response string
	}
	removeCall struct {
		called  bool
		channel string
		name    string
	}
	addErr, editErr, removeErr error
}

func (f *fakeCustomStore) Add(_ context.Context, channel, name, response, minRole, createdBy string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addCall.called = true
	f.addCall.channel = channel
	f.addCall.name = name
	f.addCall.response = response
	f.addCall.minRole = minRole
	f.addCall.createdBy = createdBy
	return f.addErr
}

func (f *fakeCustomStore) Edit(_ context.Context, channel, name, response string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.editCall.called = true
	f.editCall.channel = channel
	f.editCall.name = name
	f.editCall.response = response
	return f.editErr
}

func (f *fakeCustomStore) Remove(_ context.Context, channel, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeCall.called = true
	f.removeCall.channel = channel
	f.removeCall.name = name
	return f.removeErr
}

func modMsg(text string) commands.Message {
	m := msgText(text)
	m.IsModerator = true
	return m
}

func TestAddCommand_ParsesNameAndResponse(t *testing.T) {
	store := &fakeCustomStore{}
	cmd := commands.NewAddCommand(store)

	reply := cmd.Handler(context.Background(),
		modMsg("!addcom !hello Welcome $user to $channel"),
		[]string{"!hello", "Welcome", "$user", "to", "$channel"})

	assert.Contains(t, reply.Text, "added !hello")
	assert.True(t, store.addCall.called)
	assert.Equal(t, "hello", store.addCall.name)
	assert.Equal(t, "Welcome $user to $channel", store.addCall.response)
	assert.Equal(t, "everyone", store.addCall.minRole)
	assert.Equal(t, testViewer, store.addCall.createdBy)
}

func TestAddCommand_NoArgsUsageReply(t *testing.T) {
	store := &fakeCustomStore{}
	cmd := commands.NewAddCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!addcom"), nil)
	assert.Contains(t, reply.Text, "usage:")
	assert.False(t, store.addCall.called)
}

func TestAddCommand_NameOnlyUsageReply(t *testing.T) {
	store := &fakeCustomStore{}
	cmd := commands.NewAddCommand(store)
	reply := cmd.Handler(context.Background(),
		modMsg("!addcom !hello"), []string{"!hello"})
	assert.Contains(t, reply.Text, "usage:")
	assert.False(t, store.addCall.called)
}

func TestAddCommand_StoreErrorFriendlyReply(t *testing.T) {
	store := &fakeCustomStore{addErr: assertAnError{}}
	cmd := commands.NewAddCommand(store)
	reply := cmd.Handler(context.Background(),
		modMsg("!addcom !hello hi"), []string{"!hello", "hi"})
	assert.Contains(t, reply.Text, "couldn't add !hello")
	assert.NotContains(t, reply.Text, "\n")
}

func TestAddCommand_NilStoreFriendlyReply(t *testing.T) {
	cmd := commands.NewAddCommand(nil)
	reply := cmd.Handler(context.Background(),
		modMsg("!addcom !hello hi"), []string{"!hello", "hi"})
	assert.Contains(t, reply.Text, "unavailable")
}

func TestAddCommand_IsModOnly(t *testing.T) {
	cmd := commands.NewAddCommand(&fakeCustomStore{})
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)
	assert.Equal(t, 2*time.Second, cmd.UserCooldown)
	assert.Contains(t, cmd.Aliases, "addcmd")
}

func TestEditCommand_CallsEdit(t *testing.T) {
	store := &fakeCustomStore{}
	cmd := commands.NewEditCommand(store)

	reply := cmd.Handler(context.Background(),
		modMsg("!editcom !hello new text"),
		[]string{"!hello", "new", "text"})
	assert.Contains(t, reply.Text, "edited !hello")
	assert.True(t, store.editCall.called)
	assert.Equal(t, "hello", store.editCall.name)
	assert.Equal(t, "new text", store.editCall.response)
}

func TestEditCommand_StoreErrorFriendlyReply(t *testing.T) {
	store := &fakeCustomStore{editErr: assertAnError{}}
	cmd := commands.NewEditCommand(store)
	reply := cmd.Handler(context.Background(),
		modMsg("!editcom !hello hi"), []string{"!hello", "hi"})
	assert.Contains(t, reply.Text, "couldn't edit !hello")
}

func TestEditCommand_IsModOnly(t *testing.T) {
	cmd := commands.NewEditCommand(&fakeCustomStore{})
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)
	assert.Contains(t, cmd.Aliases, "editcmd")
}

func TestDeleteCommand_CallsRemove(t *testing.T) {
	store := &fakeCustomStore{}
	cmd := commands.NewDeleteCommand(store)

	reply := cmd.Handler(context.Background(),
		modMsg("!delcom !hello"), []string{"!hello"})
	assert.Contains(t, reply.Text, "deleted !hello")
	assert.True(t, store.removeCall.called)
	assert.Equal(t, "hello", store.removeCall.name)
}

func TestDeleteCommand_NoArgsUsageReply(t *testing.T) {
	store := &fakeCustomStore{}
	cmd := commands.NewDeleteCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!delcom"), nil)
	assert.Contains(t, reply.Text, "usage:")
	assert.False(t, store.removeCall.called)
}

func TestDeleteCommand_StoreErrorFriendlyReply(t *testing.T) {
	store := &fakeCustomStore{removeErr: assertAnError{}}
	cmd := commands.NewDeleteCommand(store)
	reply := cmd.Handler(context.Background(),
		modMsg("!delcom !hello"), []string{"!hello"})
	assert.Contains(t, reply.Text, "couldn't delete !hello")
}

func TestDeleteCommand_IsModOnly(t *testing.T) {
	cmd := commands.NewDeleteCommand(&fakeCustomStore{})
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)
	assert.Contains(t, cmd.Aliases, "delcmd")
}

func TestAdminCommands_PermissionGateAcrossEngine(t *testing.T) {
	store := &fakeCustomStore{}
	e := commands.New(commands.Config{Logger: silentLogger()})
	require.NoError(t, e.Register(commands.NewAddCommand(store)))

	// non-mod call → silent denial, store NOT called
	reply, ok := e.Handle(context.Background(), msgText("!addcom !hi yo"))
	assert.True(t, ok, "denied invocation consumed by engine")
	assert.Empty(t, reply.Text)
	assert.False(t, store.addCall.called)

	// mod call → reaches handler and store
	reply, ok = e.Handle(context.Background(), modMsg("!addcom !hi yo"))
	require.True(t, ok)
	assert.Contains(t, reply.Text, "added !hi")
	assert.True(t, store.addCall.called)
}

func TestResolver_DoesNotRaceWithStaticRegistration(t *testing.T) {
	r := &fakeResolver{rc: commands.ResolvedCommand{Response: "dyn"}, hit: true}
	e := commands.New(commands.Config{Logger: silentLogger(), Resolver: r})
	require.NoError(t, e.Register(commands.Command{Name: "ping", Handler: okHandler("pong")}))

	var statics, dynamics atomic.Int64
	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := "!ping"
			if i%2 == 0 {
				text = "!whatever"
			}
			reply, ok := e.Handle(context.Background(), msgText(text))
			if !ok {
				return
			}
			switch reply.Text {
			case "pong":
				statics.Add(1)
			case "dyn":
				dynamics.Add(1)
			}
		}(i)
	}
	wg.Wait()
	assert.Positive(t, statics.Load())
	assert.Positive(t, dynamics.Load())
}

// assertAnError is a trivial error sentinel for store-failure tests.
type assertAnError struct{}

func (assertAnError) Error() string { return "boom" }
