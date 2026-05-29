package commands_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testTenant  = "tenant-1"
	testChannel = "chan-A"
	testViewer  = "viewer-1"
	testUser    = "alice"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newEngine() *commands.Engine {
	return commands.New(commands.Config{Logger: silentLogger()})
}

func msgText(text string) commands.Message {
	return commands.Message{
		Platform: "twitch",
		Channel:  testChannel,
		UserID:   testViewer,
		Username: testUser,
		Text:     text,
	}
}

func okHandler(reply string) commands.Handler {
	return func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
		return commands.Reply{Text: reply}
	}
}

func TestEngine_PrefixDefaultsToBang(t *testing.T) {
	e := commands.New(commands.Config{Logger: silentLogger()})
	assert.Equal(t, "!", e.Prefix())
}

func TestEngine_PrefixCustom(t *testing.T) {
	e := commands.New(commands.Config{Prefix: "?", Logger: silentLogger()})
	assert.Equal(t, "?", e.Prefix())
}

func TestHandle_NoPrefixIsNotACommand(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{Name: "foo", Handler: okHandler("hi")}))

	reply, ok := e.Handle(context.Background(), msgText("hello world"))
	assert.False(t, ok)
	assert.Empty(t, reply.Text)
}

func TestHandle_BarePrefixIsNotACommand(t *testing.T) {
	e := newEngine()
	reply, ok := e.Handle(context.Background(), msgText("!"))
	assert.False(t, ok)
	assert.Empty(t, reply.Text)

	reply, ok = e.Handle(context.Background(), msgText("!   "))
	assert.False(t, ok)
	assert.Empty(t, reply.Text)
}

func TestHandle_UnknownCommandSilentlyIgnored(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{Name: "foo", Handler: okHandler("hi")}))

	reply, ok := e.Handle(context.Background(), msgText("!nope arg1"))
	assert.False(t, ok)
	assert.Empty(t, reply.Text)
}

func TestHandle_KnownCommandRoutes(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{Name: "foo", Handler: okHandler("ok")}))

	reply, ok := e.Handle(context.Background(), msgText("!foo"))
	assert.True(t, ok)
	assert.Equal(t, "ok", reply.Text)
}

func TestHandle_CaseInsensitive(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{Name: "pity", Handler: okHandler("p")}))

	for _, in := range []string{"!pity", "!PITY", "!Pity", "!pItY"} {
		reply, ok := e.Handle(context.Background(), msgText(in))
		assert.True(t, ok, "input %q should route", in)
		assert.Equal(t, "p", reply.Text, "input %q", in)
	}
}

func TestHandle_LeadingWhitespaceStripped(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{Name: "ping", Handler: okHandler("pong")}))

	reply, ok := e.Handle(context.Background(), msgText("   \t!ping"))
	assert.True(t, ok)
	assert.Equal(t, "pong", reply.Text)
}

func TestHandle_Aliases(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{
		Name:    "commands",
		Aliases: []string{"help", "h"},
		Handler: okHandler("list"),
	}))

	for _, in := range []string{"!commands", "!help", "!h", "!HELP"} {
		reply, ok := e.Handle(context.Background(), msgText(in))
		assert.True(t, ok, "input %q", in)
		assert.Equal(t, "list", reply.Text)
	}
}

func TestHandle_ArgsPassedToHandler(t *testing.T) {
	e := newEngine()

	var gotArgs []string
	require.NoError(t, e.Register(commands.Command{
		Name: "foo",
		Handler: func(_ context.Context, _ commands.Message, args []string) commands.Reply {
			gotArgs = args
			return commands.Reply{}
		},
	}))

	_, ok := e.Handle(context.Background(), msgText("!foo a b c"))
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, gotArgs)
}

func TestHandle_ArgsCollapseWhitespace(t *testing.T) {
	e := newEngine()
	var gotArgs []string
	require.NoError(t, e.Register(commands.Command{
		Name: "foo",
		Handler: func(_ context.Context, _ commands.Message, args []string) commands.Reply {
			gotArgs = args
			return commands.Reply{}
		},
	}))

	_, ok := e.Handle(context.Background(), msgText("!foo   a\t\tb   c"))
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, gotArgs)
}

func TestRegister_EmptyNameRejected(t *testing.T) {
	e := newEngine()
	err := e.Register(commands.Command{Name: "", Handler: okHandler("x")})
	assert.Error(t, err)

	err = e.Register(commands.Command{Name: "   ", Handler: okHandler("x")})
	assert.Error(t, err)
}

func TestRegister_NilHandlerRejected(t *testing.T) {
	e := newEngine()
	err := e.Register(commands.Command{Name: "foo", Handler: nil})
	assert.Error(t, err)
}

func TestRegister_DuplicateNameRejected(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{Name: "foo", Handler: okHandler("a")}))
	err := e.Register(commands.Command{Name: "FOO", Handler: okHandler("b")})
	assert.Error(t, err)
}

func TestRegister_DuplicateAliasRejected(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{
		Name: "foo", Aliases: []string{"f"}, Handler: okHandler("a"),
	}))
	err := e.Register(commands.Command{
		Name: "bar", Aliases: []string{"F"}, Handler: okHandler("b"),
	})
	assert.Error(t, err)
}

func TestRegister_AliasCollidesWithExistingName(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{Name: "foo", Handler: okHandler("a")}))
	err := e.Register(commands.Command{Name: "bar", Aliases: []string{"foo"}, Handler: okHandler("b")})
	assert.Error(t, err)
}

func TestRegister_EmptyAliasRejected(t *testing.T) {
	e := newEngine()
	err := e.Register(commands.Command{Name: "foo", Aliases: []string{""}, Handler: okHandler("a")})
	assert.Error(t, err)
}

func TestHandle_PanicRecovered(t *testing.T) {
	var buf bytes.Buffer
	var bufMu sync.Mutex
	logger := slog.New(slog.NewTextHandler(threadSafeWriter{w: &buf, mu: &bufMu}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := commands.New(commands.Config{Logger: logger})

	require.NoError(t, e.Register(commands.Command{
		Name: "boom",
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			panic("kaboom")
		},
	}))

	require.NotPanics(t, func() {
		reply, ok := e.Handle(context.Background(), msgText("!boom"))
		assert.True(t, ok)
		assert.Empty(t, reply.Text)
	})

	bufMu.Lock()
	logged := buf.String()
	bufMu.Unlock()
	assert.Contains(t, logged, "panic")
	assert.Contains(t, logged, "kaboom")
}

func TestCommands_SortedByName(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{Name: "zeta", Handler: okHandler("z")}))
	require.NoError(t, e.Register(commands.Command{Name: "alpha", Handler: okHandler("a")}))
	require.NoError(t, e.Register(commands.Command{Name: "mike", Handler: okHandler("m")}))

	cmds := e.Commands()
	require.Len(t, cmds, 3)
	assert.Equal(t, "alpha", cmds[0].Name)
	assert.Equal(t, "mike", cmds[1].Name)
	assert.Equal(t, "zeta", cmds[2].Name)
}

func TestCommands_ReturnsCopy(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{
		Name: "foo", Aliases: []string{"f"}, Handler: okHandler("x"),
	}))

	out := e.Commands()
	require.Len(t, out, 1)
	out[0].Aliases[0] = "mutated"

	again := e.Commands()
	assert.Equal(t, "f", again[0].Aliases[0])
}

func TestHandle_ConcurrentRace(t *testing.T) {
	e := newEngine()
	var counter atomic.Int64
	require.NoError(t, e.Register(commands.Command{
		Name: "ping",
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			counter.Add(1)
			return commands.Reply{Text: "pong"}
		},
	}))
	require.NoError(t, e.Register(commands.Command{
		Name: "noop",
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			counter.Add(1)
			return commands.Reply{}
		},
	}))

	const n = 50
	const iters = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			texts := []string{"!ping", "!noop", "!unknown", "no prefix", "!PING x"}
			for j := 0; j < iters; j++ {
				_, _ = e.Handle(context.Background(), msgText(texts[(i+j)%len(texts)]))
			}
		}(i)
	}
	wg.Wait()

	assert.Positive(t, counter.Load())
}

// ---- builtins ----

type fakePity struct {
	status commands.PityStatus
}

func (f *fakePity) Status(_, _, _ string) commands.PityStatus { return f.status }

type fakeStreak struct {
	status commands.StreakStatus
}

func (f *fakeStreak) Status(_, _, _ string) commands.StreakStatus { return f.status }

func TestPityCommand_NormalChance(t *testing.T) {
	q := &fakePity{status: commands.PityStatus{Points: 47, EffectiveChance: 0.2345}}
	cmd := commands.NewPityCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!pity"), nil)

	assert.Equal(t, "@alice you have 47 pity points — 23% win chance", reply.Text)
}

func TestPityCommand_SoftPityHit(t *testing.T) {
	q := &fakePity{status: commands.PityStatus{Points: 60, SoftPityHit: true, EffectiveChance: 0.42}}
	cmd := commands.NewPityCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!pity"), nil)

	assert.Equal(t, "@alice you have 60 pity points — 42% win chance (soft pity hit!)", reply.Text)
}

func TestPityCommand_NearGuaranteed(t *testing.T) {
	q := &fakePity{status: commands.PityStatus{
		Points: 90, SoftPityHit: true, NearGuaranteed: true, EffectiveChance: 1.0,
	}}
	cmd := commands.NewPityCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!pity"), nil)

	assert.Equal(t, "@alice you have 90 pity points — guaranteed win incoming!", reply.Text)
}

func TestPityCommand_FallbackUsernameWhenMissing(t *testing.T) {
	q := &fakePity{status: commands.PityStatus{Points: 0, EffectiveChance: 0.05}}
	cmd := commands.NewPityCommand(testTenant, q)

	msg := msgText("!pity")
	msg.Username = ""
	reply := cmd.Handler(context.Background(), msg, nil)

	assert.Contains(t, reply.Text, "@viewer")
}

func TestPityCommand_PercentageRounding(t *testing.T) {
	q := &fakePity{status: commands.PityStatus{Points: 1, EffectiveChance: 0.236}}
	cmd := commands.NewPityCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!pity"), nil)
	assert.Contains(t, reply.Text, "24% win chance")
}

func TestStreakCommand_ActiveStreak(t *testing.T) {
	q := &fakeStreak{status: commands.StreakStatus{
		DaysCurrent: 12, DaysLongest: 30, FreezesAvailable: 3, NextMilestone: 30,
	}}
	cmd := commands.NewStreakCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!streak"), nil)

	assert.Equal(t, "@alice 🔥 12-day streak (longest 30) — 3 freezes — next milestone: 30", reply.Text)
}

func TestStreakCommand_SingularFreeze(t *testing.T) {
	q := &fakeStreak{status: commands.StreakStatus{
		DaysCurrent: 5, DaysLongest: 9, FreezesAvailable: 1, NextMilestone: 7,
	}}
	cmd := commands.NewStreakCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!streak"), nil)

	assert.Contains(t, reply.Text, "1 freeze ")
}

func TestStreakCommand_ZeroStreak(t *testing.T) {
	q := &fakeStreak{}
	cmd := commands.NewStreakCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!streak"), nil)

	assert.Equal(t, "@alice you have no active streak — chat today to start one!", reply.Text)
}

func TestStreakCommand_NoMilestoneConfigured(t *testing.T) {
	q := &fakeStreak{status: commands.StreakStatus{
		DaysCurrent: 365, DaysLongest: 365, FreezesAvailable: 0, NextMilestone: 0,
	}}
	cmd := commands.NewStreakCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!streak"), nil)

	assert.Contains(t, reply.Text, "365-day streak")
	assert.NotContains(t, reply.Text, "next milestone:")
}

func TestHelpCommand_ListsRegisteredCommandsSorted(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.NewPityCommand(testTenant, &fakePity{})))
	require.NoError(t, e.Register(commands.NewStreakCommand(testTenant, &fakeStreak{})))
	require.NoError(t, e.Register(commands.NewHelpCommand(e)))

	reply, ok := e.Handle(context.Background(), msgText("!commands"))
	require.True(t, ok)
	assert.Equal(t, "@alice Available commands: !commands !pity !streak", reply.Text)
}

func TestHelpCommand_AliasHelp(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.NewHelpCommand(e)))

	reply, ok := e.Handle(context.Background(), msgText("!help"))
	require.True(t, ok)
	assert.Contains(t, reply.Text, "!commands")
}

func TestHelpCommand_NoCommandsRegistered(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.NewHelpCommand(e)))

	q := commands.NewHelpCommand(e)
	other := commands.New(commands.Config{Logger: silentLogger()})
	reply := q.Handler(context.Background(), msgText("!commands"), nil)
	assert.Contains(t, reply.Text, "!commands")
	_ = other
}

func TestBuiltins_ReplyLength(t *testing.T) {
	q := &fakeStreak{status: commands.StreakStatus{
		DaysCurrent: 999, DaysLongest: 9999, FreezesAvailable: 99, NextMilestone: 10000,
	}}
	cmd := commands.NewStreakCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!streak"), nil)
	assert.Less(t, len(reply.Text), 400)
	assert.NotContains(t, reply.Text, "\n")
}

// threadSafeWriter wraps an io.Writer with a mutex so log handlers can
// concurrently write under the race detector without tripping it.
type threadSafeWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (t threadSafeWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.w.Write(p)
}

// ---- permissions ----

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestSatisfies_RoleEveryoneAlwaysPasses(t *testing.T) {
	for _, m := range []commands.Message{
		{},
		{IsSubscriber: true},
		{IsVIP: true},
		{IsModerator: true},
		{IsBroadcaster: true},
	} {
		assert.True(t, exportedSatisfies(m, commands.RoleEveryone),
			"RoleEveryone should accept %#v", m)
	}
}

func TestSatisfies_ImplicationLadder(t *testing.T) {
	cases := []struct {
		name     string
		msg      commands.Message
		minRole  commands.Role
		expected bool
	}{
		{"bare → sub gate", commands.Message{}, commands.RoleSubscriber, false},
		{"bare → vip gate", commands.Message{}, commands.RoleVIP, false},
		{"bare → mod gate", commands.Message{}, commands.RoleModerator, false},
		{"bare → bcaster gate", commands.Message{}, commands.RoleBroadcaster, false},

		{"sub → sub", commands.Message{IsSubscriber: true}, commands.RoleSubscriber, true},
		{"sub → vip", commands.Message{IsSubscriber: true}, commands.RoleVIP, false},
		{"sub → mod", commands.Message{IsSubscriber: true}, commands.RoleModerator, false},
		{"sub → bcaster", commands.Message{IsSubscriber: true}, commands.RoleBroadcaster, false},

		{"vip → sub", commands.Message{IsVIP: true}, commands.RoleSubscriber, true},
		{"vip → vip", commands.Message{IsVIP: true}, commands.RoleVIP, true},
		{"vip → mod", commands.Message{IsVIP: true}, commands.RoleModerator, false},
		{"vip → bcaster", commands.Message{IsVIP: true}, commands.RoleBroadcaster, false},

		{"mod → sub", commands.Message{IsModerator: true}, commands.RoleSubscriber, true},
		{"mod → vip", commands.Message{IsModerator: true}, commands.RoleVIP, true},
		{"mod → mod", commands.Message{IsModerator: true}, commands.RoleModerator, true},
		{"mod → bcaster", commands.Message{IsModerator: true}, commands.RoleBroadcaster, false},

		{"bcaster → sub", commands.Message{IsBroadcaster: true}, commands.RoleSubscriber, true},
		{"bcaster → vip", commands.Message{IsBroadcaster: true}, commands.RoleVIP, true},
		{"bcaster → mod", commands.Message{IsBroadcaster: true}, commands.RoleModerator, true},
		{"bcaster → bcaster", commands.Message{IsBroadcaster: true}, commands.RoleBroadcaster, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, exportedSatisfies(tc.msg, tc.minRole))
		})
	}
}

// exportedSatisfies probes the satisfies semantics through the public API
// (Handle) since the method itself is unexported. It returns whether the
// gated command actually ran for the given Message + MinRole.
func exportedSatisfies(msg commands.Message, min commands.Role) bool {
	e := commands.New(commands.Config{Logger: silentLogger()})
	var ran atomic.Bool
	_ = e.Register(commands.Command{
		Name:    "gated",
		MinRole: min,
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			ran.Store(true)
			return commands.Reply{Text: "ok"}
		},
	})
	msg.Text = "!gated"
	if msg.Channel == "" {
		msg.Channel = testChannel
	}
	if msg.UserID == "" {
		msg.UserID = testViewer
	}
	_, _ = e.Handle(context.Background(), msg)
	return ran.Load()
}

func TestHandle_PermissionDenied_SilentlyConsumed(t *testing.T) {
	e := newEngine()
	var ran atomic.Bool
	require.NoError(t, e.Register(commands.Command{
		Name:    "modonly",
		MinRole: commands.RoleModerator,
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			ran.Store(true)
			return commands.Reply{Text: "ran"}
		},
	}))

	reply, ok := e.Handle(context.Background(), msgText("!modonly"))
	assert.True(t, ok, "denied invocation is handled (consumed)")
	assert.Empty(t, reply.Text, "denied invocation produces no text")
	assert.False(t, ran.Load(), "handler must not run when denied")
}

func TestHandle_ModeratorPassesSubAndVIPGates(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{
		Name: "subonly", MinRole: commands.RoleSubscriber, Handler: okHandler("s"),
	}))
	require.NoError(t, e.Register(commands.Command{
		Name: "viponly", MinRole: commands.RoleVIP, Handler: okHandler("v"),
	}))

	mod := msgText("!subonly")
	mod.IsModerator = true
	reply, ok := e.Handle(context.Background(), mod)
	assert.True(t, ok)
	assert.Equal(t, "s", reply.Text)

	mod.Text = "!viponly"
	reply, ok = e.Handle(context.Background(), mod)
	assert.True(t, ok)
	assert.Equal(t, "v", reply.Text)
}

func TestHandle_BroadcasterPassesModGate(t *testing.T) {
	e := newEngine()
	require.NoError(t, e.Register(commands.Command{
		Name: "modonly", MinRole: commands.RoleModerator, Handler: okHandler("m"),
	}))

	bc := msgText("!modonly")
	bc.IsBroadcaster = true
	reply, ok := e.Handle(context.Background(), bc)
	assert.True(t, ok)
	assert.Equal(t, "m", reply.Text)
}

// ---- cooldowns ----

func TestCooldown_GlobalSuppressionAndRecovery(t *testing.T) {
	clk := newFakeClock()
	e := commands.New(commands.Config{Logger: silentLogger(), Now: clk.Now})
	var calls atomic.Int32
	require.NoError(t, e.Register(commands.Command{
		Name:     "cd",
		Cooldown: 5 * time.Second,
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			calls.Add(1)
			return commands.Reply{Text: "ok"}
		},
	}))

	reply, ok := e.Handle(context.Background(), msgText("!cd"))
	assert.True(t, ok)
	assert.Equal(t, "ok", reply.Text)
	assert.EqualValues(t, 1, calls.Load())

	reply, ok = e.Handle(context.Background(), msgText("!cd"))
	assert.True(t, ok, "throttled invocation is still consumed")
	assert.Empty(t, reply.Text)
	assert.EqualValues(t, 1, calls.Load(), "throttled invocation must not run handler")

	clk.Advance(5 * time.Second)
	reply, ok = e.Handle(context.Background(), msgText("!cd"))
	assert.True(t, ok)
	assert.Equal(t, "ok", reply.Text)
	assert.EqualValues(t, 2, calls.Load())
}

func TestCooldown_PerUser(t *testing.T) {
	clk := newFakeClock()
	e := commands.New(commands.Config{Logger: silentLogger(), Now: clk.Now})
	var calls atomic.Int32
	require.NoError(t, e.Register(commands.Command{
		Name:         "cd",
		UserCooldown: 5 * time.Second,
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			calls.Add(1)
			return commands.Reply{Text: "ok"}
		},
	}))

	msgA := msgText("!cd")
	msgA.UserID = "user-A"
	msgB := msgText("!cd")
	msgB.UserID = "user-B"

	_, ok := e.Handle(context.Background(), msgA)
	require.True(t, ok)
	assert.EqualValues(t, 1, calls.Load())

	reply, ok := e.Handle(context.Background(), msgA)
	assert.True(t, ok)
	assert.Empty(t, reply.Text)
	assert.EqualValues(t, 1, calls.Load(), "A throttled")

	reply, ok = e.Handle(context.Background(), msgB)
	assert.True(t, ok)
	assert.Equal(t, "ok", reply.Text)
	assert.EqualValues(t, 2, calls.Load(), "B is independent of A's cooldown")

	clk.Advance(5 * time.Second)
	reply, ok = e.Handle(context.Background(), msgA)
	assert.True(t, ok)
	assert.Equal(t, "ok", reply.Text)
	assert.EqualValues(t, 3, calls.Load(), "A recovers after window")
}

func TestCooldown_PermissionDeniedDoesNotArm(t *testing.T) {
	clk := newFakeClock()
	e := commands.New(commands.Config{Logger: silentLogger(), Now: clk.Now})
	var calls atomic.Int32
	require.NoError(t, e.Register(commands.Command{
		Name:     "modcd",
		MinRole:  commands.RoleModerator,
		Cooldown: 5 * time.Second,
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			calls.Add(1)
			return commands.Reply{Text: "ok"}
		},
	}))

	denied := msgText("!modcd")
	_, ok := e.Handle(context.Background(), denied)
	assert.True(t, ok)
	assert.EqualValues(t, 0, calls.Load())

	mod := msgText("!modcd")
	mod.IsModerator = true
	reply, ok := e.Handle(context.Background(), mod)
	assert.True(t, ok, "denied invocation must not arm the global cooldown")
	assert.Equal(t, "ok", reply.Text)
	assert.EqualValues(t, 1, calls.Load())
}

func TestCooldown_ThrottledAttemptDoesNotExtendWindow(t *testing.T) {
	clk := newFakeClock()
	e := commands.New(commands.Config{Logger: silentLogger(), Now: clk.Now})
	var calls atomic.Int32
	require.NoError(t, e.Register(commands.Command{
		Name:     "cd",
		Cooldown: 5 * time.Second,
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			calls.Add(1)
			return commands.Reply{Text: "ok"}
		},
	}))

	_, ok := e.Handle(context.Background(), msgText("!cd"))
	require.True(t, ok)
	assert.EqualValues(t, 1, calls.Load())

	clk.Advance(2 * time.Second)
	reply, ok := e.Handle(context.Background(), msgText("!cd"))
	assert.True(t, ok)
	assert.Empty(t, reply.Text, "still throttled at t=2s")
	assert.EqualValues(t, 1, calls.Load())

	clk.Advance(3 * time.Second)
	reply, ok = e.Handle(context.Background(), msgText("!cd"))
	assert.True(t, ok)
	assert.Equal(t, "ok", reply.Text,
		"window is measured from first success (t=0+5=5s), not from suppressed attempt")
	assert.EqualValues(t, 2, calls.Load())
}

func TestCooldown_ZeroMeansNoThrottle(t *testing.T) {
	e := newEngine()
	var calls atomic.Int32
	require.NoError(t, e.Register(commands.Command{
		Name: "free",
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			calls.Add(1)
			return commands.Reply{Text: "ok"}
		},
	}))

	for i := 0; i < 25; i++ {
		_, ok := e.Handle(context.Background(), msgText("!free"))
		require.True(t, ok)
	}
	assert.EqualValues(t, 25, calls.Load())
}

func TestCooldown_ConcurrentRace(t *testing.T) {
	clk := newFakeClock()
	e := commands.New(commands.Config{Logger: silentLogger(), Now: clk.Now})
	var calls atomic.Int32
	require.NoError(t, e.Register(commands.Command{
		Name:     "cd",
		Cooldown: time.Hour,
		Handler: func(_ context.Context, _ commands.Message, _ []string) commands.Reply {
			calls.Add(1)
			return commands.Reply{Text: "ok"}
		},
	}))

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = e.Handle(context.Background(), msgText("!cd"))
		}()
	}
	wg.Wait()

	assert.GreaterOrEqual(t, calls.Load(), int32(1),
		"at least one of the racing calls must run")
}

// ---- leaderboard ----

type fakeLeaderboard struct {
	pity   []commands.LeaderboardEntry
	streak []commands.LeaderboardEntry
}

func (f *fakeLeaderboard) PityTop(_, _ string, _ int) []commands.LeaderboardEntry {
	return f.pity
}

func (f *fakeLeaderboard) StreakTop(_, _ string, _ int) []commands.LeaderboardEntry {
	return f.streak
}

func TestLeaderboardCommand_PityDefault(t *testing.T) {
	q := &fakeLeaderboard{
		pity: []commands.LeaderboardEntry{
			{Username: "alice", Score: 47},
			{Username: "bob", Score: 33},
			{Username: "carol", Score: 12},
		},
	}
	cmd := commands.NewLeaderboardCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!leaderboard"), nil)
	assert.Equal(t, "🏆 Pity leaders: 1. alice (47) 2. bob (33) 3. carol (12)", reply.Text)
}

func TestLeaderboardCommand_PityExplicitArg(t *testing.T) {
	q := &fakeLeaderboard{
		pity: []commands.LeaderboardEntry{{Username: "alice", Score: 1}},
	}
	cmd := commands.NewLeaderboardCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!leaderboard pity"), []string{"pity"})
	assert.Equal(t, "🏆 Pity leaders: 1. alice (1)", reply.Text)
}

func TestLeaderboardCommand_StreakArg(t *testing.T) {
	q := &fakeLeaderboard{
		streak: []commands.LeaderboardEntry{
			{Username: "alice", Score: 30},
			{Username: "bob", Score: 12},
		},
	}
	cmd := commands.NewLeaderboardCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!leaderboard streak"), []string{"streak"})
	assert.Equal(t, "🔥 Streak leaders: 1. alice (30d) 2. bob (12d)", reply.Text)
}

func TestLeaderboardCommand_EmptyBoard(t *testing.T) {
	q := &fakeLeaderboard{}
	cmd := commands.NewLeaderboardCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!leaderboard"), nil)
	assert.Equal(t, "No leaderboard data yet — start chatting!", reply.Text)
}

func TestLeaderboardCommand_NilQuerier(t *testing.T) {
	cmd := commands.NewLeaderboardCommand(testTenant, nil)
	reply := cmd.Handler(context.Background(), msgText("!leaderboard"), nil)
	assert.Empty(t, reply.Text)
}

func TestLeaderboardCommand_Alias(t *testing.T) {
	cmd := commands.NewLeaderboardCommand(testTenant, &fakeLeaderboard{})
	assert.Equal(t, "leaderboard", cmd.Name)
	assert.Contains(t, cmd.Aliases, "top")
}

func TestLeaderboardCommand_ReplyLengthBounded(t *testing.T) {
	q := &fakeLeaderboard{
		pity: []commands.LeaderboardEntry{
			{Username: "alice", Score: 99999},
			{Username: "bob", Score: 88888},
			{Username: "carol", Score: 77777},
		},
	}
	cmd := commands.NewLeaderboardCommand(testTenant, q)
	reply := cmd.Handler(context.Background(), msgText("!leaderboard"), nil)
	assert.Less(t, len(reply.Text), 400)
	assert.NotContains(t, reply.Text, "\n")
}

// ---- defensive sanity: pity/streak builtin defaults ----

func TestBuiltins_HaveDefaultUserCooldown(t *testing.T) {
	pity := commands.NewPityCommand(testTenant, &fakePity{})
	streak := commands.NewStreakCommand(testTenant, &fakeStreak{})
	assert.Equal(t, 5*time.Second, pity.UserCooldown)
	assert.Equal(t, 5*time.Second, streak.UserCooldown)
}

func TestLeaderboard_HasDefaultGlobalCooldown(t *testing.T) {
	cmd := commands.NewLeaderboardCommand(testTenant, &fakeLeaderboard{})
	assert.Equal(t, 10*time.Second, cmd.Cooldown)
}
