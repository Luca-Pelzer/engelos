package mock

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/engelos-bot/engelos/internal/adapters"
)

const defaultEventBuffer = 64

var (
	ErrNotConnected      = errors.New("mock: platform is not connected")
	ErrAlreadyConnected  = errors.New("mock: platform is already connected")
	ErrEmitAfterShutdown = errors.New("mock: EmitEvent called after disconnect")
)

// Mock is an in-memory [adapters.Platform] driven entirely from test code.
// The zero value is not usable; construct with [New].
type Mock struct {
	name   string
	logger *slog.Logger

	mu        sync.Mutex
	connected bool
	events    chan adapters.Event
	actions   []adapters.Action
	nextErr   error
	healthErr error
}

// New returns a fresh Mock with the given platform name. The mock starts
// disconnected; call Connect before EmitEvent.
func New(name string) *Mock {
	return &Mock{
		name:   name,
		logger: slog.Default().With("component", "adapters.mock", "platform", name),
	}
}

// WithLogger replaces the default slog logger. Returns the mock for chaining.
func (m *Mock) WithLogger(logger *slog.Logger) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logger = logger.With("component", "adapters.mock", "platform", m.name)
	return m
}

func (m *Mock) Name() string { return m.name }

func (m *Mock) Connect(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connected {
		return ErrAlreadyConnected
	}
	m.events = make(chan adapters.Event, defaultEventBuffer)
	m.connected = true
	m.healthErr = nil
	m.logger.Debug("mock connected")
	return nil
}

func (m *Mock) Disconnect(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return nil
	}
	close(m.events)
	m.connected = false
	m.healthErr = ErrNotConnected
	m.logger.Debug("mock disconnected")
	return nil
}

// Events returns the channel that receives emitted events. The returned
// channel is closed after Disconnect (or ForceDisconnect). Callers MUST
// re-fetch the channel after a fresh Connect; the value is not stable across
// reconnects.
func (m *Mock) Events() <-chan adapters.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events
}

// Do records the action. If SimulateError set a pending error, Do returns it
// (and clears the pending error so subsequent calls succeed again).
func (m *Mock) Do(ctx context.Context, action adapters.Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return ErrNotConnected
	}
	if m.nextErr != nil {
		err := m.nextErr
		m.nextErr = nil
		return err
	}
	m.actions = append(m.actions, action)
	return nil
}

func (m *Mock) Health() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return ErrNotConnected
	}
	return m.healthErr
}

// EmitEvent pushes e onto the events channel. It panics with
// [ErrEmitAfterShutdown] if called while disconnected, which fails tests
// loudly rather than silently dropping events.
func (m *Mock) EmitEvent(e adapters.Event) {
	m.mu.Lock()
	if !m.connected {
		m.mu.Unlock()
		panic(ErrEmitAfterShutdown)
	}
	ch := m.events
	m.mu.Unlock()
	ch <- e
}

// Actions returns a snapshot of every action recorded by Do, in the order
// they were received. The returned slice is a copy; mutating it does not
// affect the mock.
func (m *Mock) Actions() []adapters.Action {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]adapters.Action, len(m.actions))
	copy(out, m.actions)
	return out
}

// SimulateError arms the next call to Do to return err. The error is
// one-shot: after Do returns it, subsequent calls succeed again. Passing nil
// clears any previously armed error.
func (m *Mock) SimulateError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextErr = err
}

// SetHealth replaces the value returned by Health (until the next
// Connect/Disconnect transition resets it).
func (m *Mock) SetHealth(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthErr = err
}

// ForceDisconnect simulates an unexpected platform-side disconnect. It
// closes the events channel just like Disconnect but is intended to drive
// reconnect-handling code paths in tests.
func (m *Mock) ForceDisconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return
	}
	close(m.events)
	m.connected = false
	m.healthErr = ErrNotConnected
	m.logger.Debug("mock force-disconnected")
}

var _ adapters.Platform = (*Mock)(nil)
