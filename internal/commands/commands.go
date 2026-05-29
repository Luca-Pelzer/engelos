package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// Message is the platform-neutral command-invocation context the engine
// needs. The runtime dispatcher fills it from an adapters.Event before
// calling [Engine.Handle]. Username is informational only — routing keys
// off Channel/UserID/Text.
//
// The IsBroadcaster/IsModerator/IsVIP/IsSubscriber flags carry the
// author's role badges as reported by the source platform. They are
// consumed by [Engine.Handle] for permission gating against a command's
// [Command.MinRole]. See [Message.satisfies] for the implication ordering.
type Message struct {
	Platform      string
	Channel       string
	UserID        string
	Username      string
	IsBroadcaster bool
	IsModerator   bool
	IsVIP         bool
	IsSubscriber  bool
	Text          string
}

// Reply is what a [Handler] returns. An empty Text means "no reply" — the
// dispatcher sends nothing. Reply is a struct (not a bare string) so future
// fields (reply-to, whisper, ...) can be added without a breaking change.
type Reply struct {
	Text string
}

// Handler executes one command. ctx carries cancellation/deadline from the
// dispatcher. args are the whitespace-split tokens AFTER the command name.
type Handler func(ctx context.Context, msg Message, args []string) Reply

// Role is the minimum privilege a command requires. Roles are ordered:
// RoleEveryone < RoleSubscriber < RoleVIP < RoleModerator < RoleBroadcaster.
type Role int

// Role ordering. Higher-numbered roles imply every lower-numbered role for
// command-gating purposes (see [Message.satisfies]).
const (
	RoleEveryone Role = iota
	RoleSubscriber
	RoleVIP
	RoleModerator
	RoleBroadcaster
)

// satisfies reports whether the message author holds at least min. The
// implication ordering is the non-obvious bit: a Broadcaster passes every
// gate (broadcaster ⇒ moderator ⇒ VIP ⇒ subscriber ⇒ everyone); a
// Moderator passes mod/VIP/subscriber/everyone gates (mods can run
// sub-gated and VIP-gated commands); a VIP passes VIP/subscriber/everyone;
// a Subscriber passes subscriber/everyone; RoleEveryone always passes.
//
// The implications are an authorisation convenience, not a claim about
// platform semantics — a Twitch mod is not literally a subscriber, but for
// the purpose of "can this user run a sub-only command?" the answer is
// yes.
func (m Message) satisfies(min Role) bool {
	highest := RoleEveryone
	switch {
	case m.IsBroadcaster:
		highest = RoleBroadcaster
	case m.IsModerator:
		highest = RoleModerator
	case m.IsVIP:
		highest = RoleVIP
	case m.IsSubscriber:
		highest = RoleSubscriber
	}
	return highest >= min
}

// Command is a registered command: its primary Name (without prefix),
// optional Aliases, a one-line Help string, and the Handler. Names and
// aliases are compared case-insensitively.
//
// MinRole gates who may invoke the command (default RoleEveryone = open).
// A request that fails the gate is silently consumed (see [Engine.Handle]).
//
// Cooldown and UserCooldown throttle successful invocations per-channel
// (global) and per-(command,user) respectively. Either zero disables that
// dimension. See the package doc for the precise arming semantics.
type Command struct {
	Name    string
	Aliases []string
	Help    string
	Handler Handler

	// MinRole is the minimum [Role] required to invoke this command. The
	// zero value [RoleEveryone] keeps the command open to all viewers and
	// is backward-compatible with pre-gating registrations.
	MinRole Role

	// Cooldown is the minimum interval between successful invocations of
	// this command in a given channel (global cooldown). Zero disables.
	Cooldown time.Duration

	// UserCooldown is the minimum interval between successful invocations
	// by the SAME user (per-user cooldown). Zero disables.
	UserCooldown time.Duration
}

// Config configures an [Engine].
type Config struct {
	// Prefix is the command prefix, e.g. "!". When empty, [New] defaults
	// to "!".
	Prefix string
	// Logger receives handler-panic and registration logs. When nil,
	// [slog.Default] is used.
	Logger *slog.Logger
	// Now overrides the clock for cooldown bookkeeping. Defaults to
	// [time.Now]. Tests inject a controllable fake clock here.
	Now func() time.Time
}

// Engine parses prefixed chat messages and dispatches them to registered
// [Command]s. Handle is safe for concurrent use; Register and Handle are
// serialised via an RWMutex so late registration is race-free.
//
// Cooldown bookkeeping lives behind a dedicated [sync.Mutex] (cdMu) — kept
// separate from the registration RWMutex so the hot path (a successful
// invocation) takes one short write-lock on cdMu instead of upgrading the
// registration RWMutex.
//
// See the package doc for parsing rules, the silent-unknown-command and
// silent-denied/throttled rationale, and the cooldown arming semantics.
type Engine struct {
	prefix string
	logger *slog.Logger
	now    func() time.Time

	mu       sync.RWMutex
	primary  []Command
	byName   map[string]Command
	primName map[string]bool

	// cdMu guards the cooldown maps. globalCD records the last successful
	// fire time per primary command name. userCD records the last
	// successful fire per (name, userID) keyed as name+"\x00"+userID to
	// avoid a nested map allocation per command.
	//
	// Memory note: userCD grows with unique (command, user) pairs. No
	// eviction is performed yet; for long-running bots with many viewers
	// this map is bounded only by user count × cooldown'd command count.
	// Eviction is future work; today's scale (one Twitch channel) keeps it
	// well below any concern.
	cdMu     sync.Mutex
	globalCD map[string]time.Time
	userCD   map[string]time.Time
}

// New constructs an [Engine] from cfg. An empty Prefix defaults to "!"; a
// nil Logger defaults to [slog.Default]; a nil Now defaults to [time.Now].
func New(cfg Config) *Engine {
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "!"
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Engine{
		prefix:   prefix,
		logger:   logger.With("component", "commands.engine"),
		now:      now,
		byName:   make(map[string]Command),
		primName: make(map[string]bool),
		globalCD: make(map[string]time.Time),
		userCD:   make(map[string]time.Time),
	}
}

// Prefix returns the configured command prefix.
func (e *Engine) Prefix() string { return e.prefix }

// Register adds c (and any aliases) to the engine. It returns an error on
// an empty Name, a nil Handler, or a name/alias that collides with an
// already-registered command. Names and aliases are stored lower-case and
// compared case-insensitively.
func (e *Engine) Register(c Command) error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("commands: command name is required")
	}
	if c.Handler == nil {
		return fmt.Errorf("commands: handler is nil for command %q", c.Name)
	}

	name := strings.ToLower(strings.TrimSpace(c.Name))
	aliases := make([]string, 0, len(c.Aliases))
	for _, a := range c.Aliases {
		la := strings.ToLower(strings.TrimSpace(a))
		if la == "" {
			return fmt.Errorf("commands: empty alias for command %q", c.Name)
		}
		aliases = append(aliases, la)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.byName[name]; exists {
		return fmt.Errorf("commands: command %q already registered", name)
	}
	for _, a := range aliases {
		if _, exists := e.byName[a]; exists {
			return fmt.Errorf("commands: alias %q already registered", a)
		}
		if a == name {
			return fmt.Errorf("commands: alias %q duplicates command name", a)
		}
	}

	stored := Command{
		Name:         name,
		Aliases:      aliases,
		Help:         c.Help,
		Handler:      c.Handler,
		MinRole:      c.MinRole,
		Cooldown:     c.Cooldown,
		UserCooldown: c.UserCooldown,
	}
	e.byName[name] = stored
	for _, a := range aliases {
		e.byName[a] = stored
	}
	e.primary = append(e.primary, stored)
	e.primName[name] = true
	return nil
}

// Handle parses msg.Text and dispatches to the matching command.
//
// Return values:
//   - (Reply{}, false): the message is NOT a command (no prefix, bare
//     prefix, or unknown name). The dispatcher should ignore it. Unknown
//     names are intentionally silent so chat is not spammed with
//     "unknown command" responses.
//   - (reply, true): the command WAS recognised. reply may be the zero
//     Reply{} when (a) the handler chose not to respond, (b) the handler
//     panicked (recovered + logged), (c) the author failed the permission
//     gate, or (d) a cooldown was active. Cases (c) and (d) are
//     intentionally silent — telling chat "you lack permission" or "wait
//     5s" invites spam and leaks information.
//
// Order: name lookup → permission gate → cooldown gate → handler. The
// cooldown timers are armed AFTER a successful handler return only;
// denied or throttled attempts do NOT reset the window.
func (e *Engine) Handle(ctx context.Context, msg Message) (Reply, bool) {
	text := strings.TrimLeft(msg.Text, " \t\r\n")
	if !strings.HasPrefix(text, e.prefix) {
		return Reply{}, false
	}
	tokens := strings.Fields(text[len(e.prefix):])
	if len(tokens) == 0 {
		return Reply{}, false
	}
	name := strings.ToLower(tokens[0])
	args := tokens[1:]

	e.mu.RLock()
	cmd, ok := e.byName[name]
	e.mu.RUnlock()
	if !ok {
		return Reply{}, false
	}

	if !msg.satisfies(cmd.MinRole) {
		return Reply{}, true
	}

	if e.onCooldown(cmd, msg) {
		return Reply{}, true
	}

	reply := e.invoke(ctx, cmd, msg, args)
	e.armCooldown(cmd, msg)
	return reply, true
}

// onCooldown reports whether either the global or per-user cooldown is
// currently active for cmd. Either dimension being active suppresses the
// invocation. A zero Cooldown / UserCooldown disables that dimension.
func (e *Engine) onCooldown(cmd Command, msg Message) bool {
	if cmd.Cooldown <= 0 && cmd.UserCooldown <= 0 {
		return false
	}
	now := e.now()
	e.cdMu.Lock()
	defer e.cdMu.Unlock()
	if cmd.Cooldown > 0 {
		if last, ok := e.globalCD[cmd.Name]; ok && now.Sub(last) < cmd.Cooldown {
			return true
		}
	}
	if cmd.UserCooldown > 0 {
		key := cmd.Name + "\x00" + msg.UserID
		if last, ok := e.userCD[key]; ok && now.Sub(last) < cmd.UserCooldown {
			return true
		}
	}
	return false
}

// armCooldown records cmd's last-fire timestamp for both dimensions. It is
// called only after a successful handler return so denied/throttled
// attempts do not extend the window.
func (e *Engine) armCooldown(cmd Command, msg Message) {
	if cmd.Cooldown <= 0 && cmd.UserCooldown <= 0 {
		return
	}
	now := e.now()
	e.cdMu.Lock()
	defer e.cdMu.Unlock()
	if cmd.Cooldown > 0 {
		e.globalCD[cmd.Name] = now
	}
	if cmd.UserCooldown > 0 {
		e.userCD[cmd.Name+"\x00"+msg.UserID] = now
	}
}

// invoke runs the handler under a deferred panic recovery so a single bad
// command cannot crash the dispatcher.
func (e *Engine) invoke(ctx context.Context, cmd Command, msg Message, args []string) (reply Reply) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("commands: handler panic recovered",
				"command", cmd.Name,
				"platform", msg.Platform,
				"channel", msg.Channel,
				"user_id", msg.UserID,
				"panic", fmt.Sprint(r),
			)
			reply = Reply{}
		}
	}()
	return cmd.Handler(ctx, msg, args)
}

// Commands returns the registered primary commands, sorted by Name. Aliases
// are NOT returned as separate entries — each Command's Aliases slice
// carries them. The returned slice is a fresh copy and safe to mutate.
func (e *Engine) Commands() []Command {
	e.mu.RLock()
	out := make([]Command, 0, len(e.primary))
	for _, c := range e.primary {
		aliasesCopy := make([]string, len(c.Aliases))
		copy(aliasesCopy, c.Aliases)
		out = append(out, Command{
			Name:         c.Name,
			Aliases:      aliasesCopy,
			Help:         c.Help,
			Handler:      c.Handler,
			MinRole:      c.MinRole,
			Cooldown:     c.Cooldown,
			UserCooldown: c.UserCooldown,
		})
	}
	e.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
