package actions

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// RuleSource supplies the engine with the rules to evaluate for a channel. The
// store satisfies it; tests use an in-memory fake. ListEnabled returns only
// enabled rules so the hot path never filters.
type RuleSource interface {
	ListEnabled(ctx context.Context, tenantID, channel string) ([]Rule, error)
}

// Config configures an [Engine].
type Config struct {
	TenantID string
	Source   RuleSource
	Registry *Registry

	// Workers bounds how many rule firings run concurrently. The dispatcher
	// hands events off without blocking, so a slow action (an http request)
	// never stalls event consumption. Defaults to 4.
	Workers int

	// ActionTimeout bounds a single action's run time. Defaults to 15s.
	ActionTimeout time.Duration

	Logger *slog.Logger
}

// Engine matches fired triggers against stored rules and runs the matching
// rules' actions. It owns a bounded worker pool plus a set of named serial
// queues so a rule can opt its action list into strict ordering.
type Engine struct {
	tenantID      string
	source        RuleSource
	registry      *Registry
	actionTimeout time.Duration
	workers       int
	log           *slog.Logger

	jobs chan job
	wg   sync.WaitGroup

	queuesMu sync.Mutex
	queues   map[string]chan job

	stopOnce sync.Once
	stopped  chan struct{}
}

type job struct {
	rule    Rule
	trigger Trigger
}

const (
	defaultWorkers       = 4
	defaultActionTimeout = 15 * time.Second
	queueBuffer          = 64
	jobBuffer            = 256
)

// New constructs an Engine. Source and Registry are required.
func New(cfg Config) (*Engine, error) {
	if cfg.Source == nil {
		return nil, &configError{"source is required"}
	}
	if cfg.Registry == nil {
		return nil, &configError{"registry is required"}
	}
	if cfg.TenantID == "" {
		cfg.TenantID = "local"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = defaultWorkers
	}
	if cfg.ActionTimeout <= 0 {
		cfg.ActionTimeout = defaultActionTimeout
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		tenantID:      cfg.TenantID,
		source:        cfg.Source,
		registry:      cfg.Registry,
		actionTimeout: cfg.ActionTimeout,
		log:           logger.With("component", "actions.engine"),
		jobs:          make(chan job, jobBuffer),
		queues:        make(map[string]chan job),
		stopped:       make(chan struct{}),
		workers:       cfg.Workers,
	}, nil
}

// Start spins up the worker pool. It is safe to call once.
func (e *Engine) Start() {
	for i := 0; i < e.workers; i++ {
		e.wg.Add(1)
		go e.worker()
	}
}

// Stop signals shutdown and blocks until every worker and queue goroutine has
// returned. Shutdown is signalled solely by closing e.stopped: the jobs channel
// and per-name queues are never closed, so a concurrent Fire that is mid-send
// can never panic with "send on closed channel". After Stop the engine must not
// be reused.
func (e *Engine) Stop() {
	e.stopOnce.Do(func() {
		close(e.stopped)
	})
	e.wg.Wait()
}

// Fire matches t against the channel's enabled rules and schedules every match
// for execution. It never blocks on action work: matching is synchronous (cheap
// in-memory predicate checks) but execution is handed to the worker pool. A
// full job buffer drops the firing with a warning rather than stalling the
// dispatcher, which must keep consuming platform events.
func (e *Engine) Fire(ctx context.Context, t Trigger) {
	if t.Channel == "" {
		return
	}
	rules, err := e.source.ListEnabled(ctx, e.tenantID, t.Channel)
	if err != nil {
		e.log.Warn("list enabled rules failed", "channel", t.Channel, "err", err)
		return
	}
	for _, r := range rules {
		if !e.matches(r, t) {
			continue
		}
		e.schedule(job{rule: r, trigger: t})
	}
}

// RunRule schedules one already-selected rule for execution, bypassing the
// channel-wide ListEnabled match that Fire performs. A timer tick and a manual
// fire each target a single rule, so firing the whole channel's rules of a kind
// would be wrong; RunRule reuses the same schedule path so conditions, actions,
// the named queue and the per-action timeout still apply. It is safe to call
// after Stop: schedule's stopped-guard drops the job rather than panicking.
func (e *Engine) RunRule(rule Rule, t Trigger) {
	e.schedule(job{rule: rule, trigger: t})
}

// matches reports whether rule r should fire for trigger t. The trigger kind
// must agree and the rule's kind-specific TriggerFilter must accept the event.
func (e *Engine) matches(r Rule, t Trigger) bool {
	if r.TriggerKind != t.Kind {
		return false
	}
	return matchTriggerFilter(r, t)
}

// schedule routes a job to its named serial queue when the rule requests one,
// otherwise onto the shared worker pool.
func (e *Engine) schedule(j job) {
	if q := strings.TrimSpace(j.rule.Actions.QueueID); q != "" {
		e.enqueueSerial(q, j)
		return
	}
	select {
	case <-e.stopped:
		return
	default:
	}
	select {
	case e.jobs <- j:
	case <-e.stopped:
	default:
		e.log.Warn("action job buffer full, dropping firing",
			"rule", j.rule.Name, "channel", j.rule.Channel)
	}
}

// enqueueSerial lazily creates a per-name queue with a single draining
// goroutine so all firings sharing a QueueID run strictly one after another.
func (e *Engine) enqueueSerial(name string, j job) {
	select {
	case <-e.stopped:
		return
	default:
	}

	e.queuesMu.Lock()
	q, ok := e.queues[name]
	if !ok {
		// Re-check shutdown while holding the lock so we never create a new
		// queue goroutine (wg.Add) after Stop has begun its wg.Wait.
		select {
		case <-e.stopped:
			e.queuesMu.Unlock()
			return
		default:
		}
		q = make(chan job, queueBuffer)
		e.queues[name] = q
		e.wg.Add(1)
		go e.drainQueue(q)
	}
	e.queuesMu.Unlock()

	select {
	case q <- j:
	case <-e.stopped:
	default:
		e.log.Warn("action queue full, dropping firing",
			"queue", name, "rule", j.rule.Name)
	}
}

func (e *Engine) drainQueue(q chan job) {
	defer e.wg.Done()
	for {
		select {
		case j := <-q:
			e.run(j)
		case <-e.stopped:
			return
		}
	}
}

func (e *Engine) worker() {
	defer e.wg.Done()
	for {
		select {
		case j := <-e.jobs:
			e.run(j)
		case <-e.stopped:
			return
		}
	}
}

// run evaluates a matched rule's conditions and, if they pass, executes its
// actions in order. Each firing gets a fresh ExecutionContext so outputs never
// bleed between firings.
func (e *Engine) run(j job) {
	ctx := context.Background()
	ec := newExecutionContext(ctx, j.rule, j.trigger)

	pass, err := e.evaluateConditions(ec, j.rule.Conditions)
	if err != nil {
		e.log.Warn("condition evaluation failed",
			"rule", j.rule.Name, "err", err)
		return
	}
	if !pass {
		return
	}
	e.executeActions(ec, j.rule.Actions)
}

// evaluateConditions applies the rule's condition list under its combine mode.
// An empty list always passes. A missing plugin fails closed (the rule does not
// fire) so a deleted or unregistered condition can never silently open a gate.
func (e *Engine) evaluateConditions(ec *ExecutionContext, list ConditionList) (bool, error) {
	if len(list.Conditions) == 0 {
		return true, nil
	}
	mode := list.Mode
	if mode == "" {
		mode = ConditionModeAll
	}
	anyPassed := false
	for _, ci := range list.Conditions {
		plugin, ok := e.registry.Condition(ci.TypeID)
		if !ok {
			e.log.Warn("unknown condition type, failing closed",
				"type_id", ci.TypeID, "rule", ec.RuleName)
			return false, nil
		}
		ok, err := plugin.Evaluate(ec, ci.Config)
		if err != nil {
			return false, err
		}
		switch mode {
		case ConditionModeAll:
			if !ok {
				return false, nil
			}
		case ConditionModeAny:
			if ok {
				return true, nil
			}
		case ConditionModeNone:
			if ok {
				return false, nil
			}
		}
		anyPassed = anyPassed || ok
	}
	switch mode {
	case ConditionModeAny:
		return false, nil
	default:
		return true, nil
	}
}

// executeActions runs each enabled action in order, applying variable
// substitution to its config first. A failing action is logged and skipped so
// one bad step never aborts the rest; an action may explicitly Stop the list.
func (e *Engine) executeActions(ec *ExecutionContext, list ActionList) {
	for _, ai := range list.Actions {
		if !ai.Enabled {
			continue
		}
		plugin, ok := e.registry.Action(ai.TypeID)
		if !ok {
			e.log.Warn("unknown action type, skipping",
				"type_id", ai.TypeID, "rule", ec.RuleName)
			continue
		}
		cfg := substituteRaw(ec, ai.Config)

		actx, cancel := context.WithTimeout(ec.Ctx, e.actionTimeout)
		res, err := plugin.Execute(ec.withCtx(actx), cfg)
		cancel()
		if err != nil {
			e.log.Warn("action failed",
				"type_id", ai.TypeID, "rule", ec.RuleName, "err", err)
			continue
		}
		if res != nil {
			for k, v := range res.Outputs {
				ec.SetOutput(k, v)
			}
			if res.Stop {
				return
			}
		}
	}
}

type configError struct{ msg string }

func (e *configError) Error() string { return "actions: " + e.msg }
