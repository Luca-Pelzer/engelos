package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

// PityGranter is the narrow contract the dispatcher needs from the pity
// subsystem. internal/features/pity.System satisfies it.
type PityGranter interface {
	GrantPoints(ctx context.Context, tenantID, channel, viewerID, username, reason string, amount int) (int, error)
}

// Broadcaster is the narrow contract for fanning out normalized events to
// real-time consumers (WebSocket clients, SSE clients, ...).
type Broadcaster interface {
	Broadcast(eventType string, payload any)
}

// Config configures the Dispatcher.
type Config struct {
	// TenantID is the single-tenant identifier this runtime serves.
	TenantID string

	// Platforms are the connected adapters to consume events from. Their
	// Connect method is the caller's responsibility; the dispatcher only
	// reads from each platform's Events channel until it closes.
	Platforms []adapters.Platform

	// Pity, when non-nil, receives a GrantPoints call for every chat
	// message that crosses the dispatcher. PointsPerMessage controls the
	// amount.
	Pity PityGranter

	// PointsPerMessage is the credit granted per chat message. Zero or
	// negative disables pity grants even when Pity is non-nil.
	PointsPerMessage int

	// Broadcaster, when non-nil, receives every event the dispatcher sees
	// so WebSocket / SSE consumers can render live activity.
	Broadcaster Broadcaster

	// Logger receives lifecycle and per-event debug logs. Defaults to
	// slog.Default().
	Logger *slog.Logger
}

// Dispatcher is the runtime fan-in router.
type Dispatcher struct {
	cfg       Config
	logger    *slog.Logger
	stats     stats
	started   atomic.Bool
	stopped   atomic.Bool
	doneCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

type stats struct {
	mu              sync.Mutex
	messages        int64
	subs            int64
	raids           int64
	pityGrantErrors int64
	lastEventAt     time.Time
}

// New constructs a Dispatcher.
func New(cfg Config) *Dispatcher {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.TenantID == "" {
		cfg.TenantID = "default"
	}
	return &Dispatcher{
		cfg:    cfg,
		logger: cfg.Logger.With("component", "runtime.dispatcher"),
		doneCh: make(chan struct{}),
	}
}

// Run consumes events from every configured platform until ctx is cancelled
// or all platform Events channels close. Safe to call exactly once.
func (d *Dispatcher) Run(ctx context.Context) error {
	if !d.started.CompareAndSwap(false, true) {
		return errors.New("runtime: dispatcher already started")
	}
	defer d.stopOnce.Do(func() {
		d.stopped.Store(true)
		close(d.doneCh)
	})

	if len(d.cfg.Platforms) == 0 {
		d.logger.Info("dispatcher idle (no platforms configured)")
		<-ctx.Done()
		return nil
	}

	d.logger.Info("dispatcher starting", "platforms", platformNames(d.cfg.Platforms))

	var wg sync.WaitGroup
	for _, p := range d.cfg.Platforms {
		wg.Add(1)
		go func(p adapters.Platform) {
			defer wg.Done()
			d.consume(ctx, p)
		}(p)
	}

	wg.Wait()
	d.logger.Info("dispatcher stopped",
		"messages", d.stats.snapshot().messages,
		"subs", d.stats.snapshot().subs,
		"raids", d.stats.snapshot().raids,
	)
	return nil
}

// Done returns a channel closed when Run returns.
func (d *Dispatcher) Done() <-chan struct{} { return d.doneCh }

// Stats returns a snapshot of internal counters. Safe for concurrent use.
func (d *Dispatcher) Stats() Stats { return d.stats.snapshot().public() }

// Stats is the public counter view returned by [Dispatcher.Stats].
type Stats struct {
	Messages        int64     `json:"messages"`
	Subscriptions   int64     `json:"subscriptions"`
	Raids           int64     `json:"raids"`
	PityGrantErrors int64     `json:"pity_grant_errors"`
	LastEventAt     time.Time `json:"last_event_at"`
}

func (s *stats) snapshot() statsSnap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return statsSnap{
		messages:        s.messages,
		subs:            s.subs,
		raids:           s.raids,
		pityGrantErrors: s.pityGrantErrors,
		lastEventAt:     s.lastEventAt,
	}
}

type statsSnap struct {
	messages        int64
	subs            int64
	raids           int64
	pityGrantErrors int64
	lastEventAt     time.Time
}

func (s statsSnap) public() Stats {
	return Stats{
		Messages:        s.messages,
		Subscriptions:   s.subs,
		Raids:           s.raids,
		PityGrantErrors: s.pityGrantErrors,
		LastEventAt:     s.lastEventAt,
	}
}

func (d *Dispatcher) consume(ctx context.Context, p adapters.Platform) {
	log := d.logger.With("platform", p.Name())
	events := p.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				log.Info("platform events channel closed")
				return
			}
			d.handle(ctx, ev)
		}
	}
}

func (d *Dispatcher) handle(ctx context.Context, ev adapters.Event) {
	d.stats.mu.Lock()
	d.stats.lastEventAt = ev.OccurredAt
	d.stats.mu.Unlock()

	if d.cfg.Broadcaster != nil {
		d.cfg.Broadcaster.Broadcast(string(ev.Type), ev)
	}

	switch ev.Type {
	case adapters.EventMessageCreated:
		d.onMessage(ctx, ev)
	case adapters.EventUserSubscribed, adapters.EventUserResubscribed:
		d.stats.mu.Lock()
		d.stats.subs++
		d.stats.mu.Unlock()
	case adapters.EventChannelRaided:
		d.stats.mu.Lock()
		d.stats.raids++
		d.stats.mu.Unlock()
	}
}

func (d *Dispatcher) onMessage(ctx context.Context, ev adapters.Event) {
	if ev.Message == nil {
		return
	}
	d.stats.mu.Lock()
	d.stats.messages++
	d.stats.mu.Unlock()

	if d.cfg.Pity == nil || d.cfg.PointsPerMessage <= 0 {
		return
	}
	if ev.Message.UserID == "" || ev.Channel == "" {
		return
	}

	if _, err := d.cfg.Pity.GrantPoints(ctx, d.cfg.TenantID,
		ev.Channel, ev.Message.UserID, ev.Message.Username,
		"chat:"+ev.Platform, d.cfg.PointsPerMessage); err != nil {
		d.stats.mu.Lock()
		d.stats.pityGrantErrors++
		d.stats.mu.Unlock()
		d.logger.Warn("pity grant failed",
			"platform", ev.Platform,
			"channel", ev.Channel,
			"viewer", ev.Message.UserID,
			"err", err,
		)
	}
}

func platformNames(ps []adapters.Platform) []string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name()
	}
	return names
}

// ErrAlreadyStarted is returned by Run on the second call.
var ErrAlreadyStarted = fmt.Errorf("runtime: dispatcher already started")
