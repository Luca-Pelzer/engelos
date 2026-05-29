package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// Message is the platform-neutral command-invocation context the engine
// needs. The runtime dispatcher fills it from an adapters.Event before
// calling [Engine.Handle]. Username is informational only — routing keys
// off Channel/UserID/Text.
type Message struct {
	Platform string
	Channel  string
	UserID   string
	Username string
	Text     string
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

// Command is a registered command: its primary Name (without prefix),
// optional Aliases, a one-line Help string, and the Handler. Names and
// aliases are compared case-insensitively.
type Command struct {
	Name    string
	Aliases []string
	Help    string
	Handler Handler
}

// Config configures an [Engine].
type Config struct {
	// Prefix is the command prefix, e.g. "!". When empty, [New] defaults
	// to "!".
	Prefix string
	// Logger receives handler-panic and registration logs. When nil,
	// [slog.Default] is used.
	Logger *slog.Logger
}

// Engine parses prefixed chat messages and dispatches them to registered
// [Command]s. Handle is safe for concurrent use; Register and Handle are
// serialised via an RWMutex so late registration is race-free.
//
// See the package doc for parsing rules and the silent-unknown-command
// rationale.
type Engine struct {
	prefix string
	logger *slog.Logger

	mu       sync.RWMutex
	primary  []Command
	byName   map[string]Command
	primName map[string]bool
}

// New constructs an [Engine] from cfg. An empty Prefix defaults to "!"; a
// nil Logger defaults to [slog.Default].
func New(cfg Config) *Engine {
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "!"
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		prefix:   prefix,
		logger:   logger.With("component", "commands.engine"),
		byName:   make(map[string]Command),
		primName: make(map[string]bool),
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
		Name:    name,
		Aliases: aliases,
		Help:    c.Help,
		Handler: c.Handler,
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
//   - (reply, true): the command ran. reply may be the zero Reply{} when
//     the handler chose not to respond or when the handler panicked
//     (recovered + logged).
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

	reply := e.invoke(ctx, cmd, msg, args)
	return reply, true
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
			Name:    c.Name,
			Aliases: aliasesCopy,
			Help:    c.Help,
			Handler: c.Handler,
		})
	}
	e.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
