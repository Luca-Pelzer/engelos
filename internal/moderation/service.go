// Package moderation wires the stateless automod filter engine together with
// the stateful escalation, permit and audit-log layers into a single Service
// the runtime dispatcher can consult for every chat message.
//
// The two underlying packages are deliberately decoupled (internal/automod is
// pure detection logic; internal/automodstate holds escalation + audit state),
// and neither imports the other. This package is the only place that knows
// about both, keeping the dependency graph a clean tree.
package moderation

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/automod"
	"github.com/Luca-Pelzer/engelos/internal/automodstate"
)

// ActionKind is the enforcement the dispatcher should carry out on a message.
type ActionKind int

const (
	// ActionNone means the message is clean; do nothing.
	ActionNone ActionKind = iota
	// ActionDelete removes the message without timing the user out.
	ActionDelete
	// ActionTimeout removes the message and times the user out for Duration.
	ActionTimeout
	// ActionBan permanently bans the user.
	ActionBan
)

// Decision is the moderation verdict for a single message. When Kind is
// ActionNone the dispatcher proceeds normally (commands, points, streak). For
// any other Kind the dispatcher should enforce the action (unless DryRun) and
// then stop processing the message.
type Decision struct {
	Kind     ActionKind
	Duration time.Duration // for ActionTimeout
	Reason   string        // human-readable, safe to show in chat
	Filter   string        // which filter fired
	DryRun   bool          // true when the engine is in dry-run (shadow) mode
}

// Message is the neutral input the dispatcher hands to the Service. It mirrors
// the fields the filters need without coupling to the adapter layer.
type Message struct {
	Channel       string
	MessageID     string
	UserID        string
	Username      string
	Text          string
	EmoteCount    int
	FirstMsg      bool
	IsModerator   bool
	IsVIP         bool
	IsSubscriber  bool
	IsBroadcaster bool
}

// Service evaluates messages against the filter engine, applies escalation to
// pick the punishment severity, honours link permits, and records every action
// to the audit log. A nil *Service is safe: Evaluate returns ActionNone, so the
// dispatcher can hold an optional Moderator without nil checks at the call site.
type Service struct {
	mu       sync.RWMutex
	engine   *automod.Engine // guarded by mu; swapped wholesale on SetConfig
	escal    *automodstate.Escalator
	permits  *automodstate.PermitTracker
	audit    *automodstate.AuditStore
	tenantID string
	logger   *slog.Logger
}

// Config bundles the dependencies for New. Audit may be nil (actions then go
// unlogged but enforcement still works); engine must be non-nil.
type Config struct {
	Engine   *automod.Engine
	Escal    *automodstate.Escalator
	Permits  *automodstate.PermitTracker
	Audit    *automodstate.AuditStore
	TenantID string
	Logger   *slog.Logger
}

// New constructs a Service. It returns nil when cfg.Engine is nil so callers can
// treat "no engine configured" as "moderation disabled".
func New(cfg Config) *Service {
	if cfg.Engine == nil {
		return nil
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Escal == nil {
		cfg.Escal = automodstate.NewEscalator(automodstate.DefaultTiers(), 24*time.Hour)
	}
	if cfg.Permits == nil {
		cfg.Permits = automodstate.NewPermitTracker(60 * time.Second)
	}
	tenant := strings.TrimSpace(cfg.TenantID)
	if tenant == "" {
		tenant = "local"
	}
	return &Service{
		engine:   cfg.Engine,
		escal:    cfg.Escal,
		permits:  cfg.Permits,
		audit:    cfg.Audit,
		tenantID: tenant,
		logger:   logger,
	}
}

// Permit grants the user a one-shot link permit (the `!permit <user>` flow).
// Safe to call on a nil Service.
func (s *Service) Permit(channel, user string) {
	if s == nil {
		return
	}
	s.permits.Grant(channel, user)
}

// Offenses reports the current escalation count for a (channel, user, filter),
// for dashboard display. Safe on a nil Service.
func (s *Service) Offenses(channel, user, filter string) int {
	if s == nil {
		return 0
	}
	return s.escal.Offenses(channel, user, filter)
}

// Config returns the engine's current filter configuration, for the management
// API to render. Safe on a nil Service (returns the zero Config).
func (s *Service) Config() automod.Config {
	if s == nil {
		return automod.Config{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.engine.Config()
}

// SetConfig validates and atomically swaps in a new filter configuration by
// rebuilding the engine. It returns an error (and keeps the old config) when
// the new config has an invalid banned-word regex. Safe for concurrent use.
func (s *Service) SetConfig(cfg automod.Config) error {
	if s == nil {
		return nil
	}
	engine, err := automod.NewEngine(cfg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.engine = engine
	s.mu.Unlock()
	return nil
}

// AuditList returns recent mod actions for a channel, newest first. Safe on a
// nil Service or nil audit store (returns nil).
func (s *Service) AuditList(ctx context.Context, channel string, limit int) ([]automodstate.ModAction, error) {
	if s == nil || s.audit == nil {
		return nil, nil
	}
	return s.audit.List(ctx, s.tenantID, channel, limit)
}

// Evaluate runs the filters, applies escalation, and (for a violation) records
// an audit row, returning the Decision the dispatcher should enforce. A nil
// Service or a clean message yields ActionNone.
func (s *Service) Evaluate(ctx context.Context, msg Message) Decision {
	if s == nil {
		return Decision{Kind: ActionNone}
	}

	s.mu.RLock()
	engine := s.engine
	s.mu.RUnlock()

	result := engine.Evaluate(
		automod.Message{
			Text:       msg.Text,
			Username:   msg.Username,
			EmoteCount: msg.EmoteCount,
			FirstMsg:   msg.FirstMsg,
		},
		automod.UserContext{
			Role:           roleOf(msg),
			AccountAgeDays: -1,
			FollowAgeDays:  -1,
		},
	)
	if result.Verdict == automod.VerdictPass {
		return Decision{Kind: ActionNone}
	}

	// A link violation can be waived once by a moderator-granted permit.
	if result.FilterName == "links" && s.permits.Consume(msg.Channel, msg.Username) {
		return Decision{Kind: ActionNone}
	}

	dryRun := engine.Config().Mode == automod.ModeDryRun

	// Escalation decides the ACTUAL severity from the user's history; the
	// filter's own verdict is the floor. A repeat offender on a delete-only
	// filter still climbs the ladder to timeouts and eventually a ban.
	action, duration := s.escal.Record(msg.Channel, msg.Username, result.FilterName)
	kind, dur := mergeVerdict(result, action, duration)

	dec := Decision{
		Kind:     kind,
		Duration: dur,
		Reason:   result.Reason,
		Filter:   result.FilterName,
		DryRun:   dryRun,
	}

	s.recordAudit(ctx, msg, result, dec)
	return dec
}

// mergeVerdict combines the filter's intrinsic verdict (the floor) with the
// escalation ladder's action (which climbs with repeat offenses), returning the
// more severe of the two so neither can soften the other.
func mergeVerdict(result automod.FilterResult, escAction automodstate.Action, escDur time.Duration) (ActionKind, time.Duration) {
	// Map the filter floor.
	floorKind := ActionDelete
	floorDur := time.Duration(0)
	switch result.Verdict {
	case automod.VerdictTimeout:
		floorKind = ActionTimeout
		floorDur = result.Timeout
	case automod.VerdictBan:
		floorKind = ActionBan
	}

	// Map the escalation action.
	escKind := ActionDelete
	switch escAction {
	case automodstate.ActionWarn:
		escKind = ActionDelete
	case automodstate.ActionTimeout:
		escKind = ActionTimeout
	case automodstate.ActionBan:
		escKind = ActionBan
	}

	// Take the more severe kind.
	kind := floorKind
	if escKind > kind {
		kind = escKind
	}
	// For a timeout, take the longer of the two durations so escalation can
	// only lengthen, never shorten, the floor.
	dur := time.Duration(0)
	if kind == ActionTimeout {
		dur = floorDur
		if escDur > dur {
			dur = escDur
		}
		if dur <= 0 {
			dur = 60 * time.Second // safety floor for a timeout with no duration
		}
	}
	return kind, dur
}

// recordAudit persists the enforcement to the audit log. Failures are logged
// but never block enforcement.
func (s *Service) recordAudit(ctx context.Context, msg Message, result automod.FilterResult, dec Decision) {
	if s.audit == nil {
		return
	}
	if _, err := s.audit.Log(ctx, automodstate.ModAction{
		TenantID:    s.tenantID,
		Channel:     msg.Channel,
		UserID:      msg.UserID,
		Username:    msg.Username,
		MessageID:   msg.MessageID,
		MessageText: msg.Text,
		FilterName:  result.FilterName,
		Reason:      result.Reason,
		MatchedText: result.MatchedText,
		Action:      kindString(dec.Kind),
		DurationSec: int(dec.Duration / time.Second),
		DryRun:      dec.DryRun,
	}); err != nil {
		s.logger.WarnContext(ctx, "automod audit log failed", slog.Any("err", err))
	}
}

// roleOf maps the dispatcher's badge booleans onto the automod role ladder,
// highest-first.
func roleOf(msg Message) automod.Role {
	switch {
	case msg.IsBroadcaster:
		return automod.RoleBroadcaster
	case msg.IsModerator:
		return automod.RoleModerator
	case msg.IsVIP:
		return automod.RoleVIP
	case msg.IsSubscriber:
		return automod.RoleSubscriber
	default:
		return automod.RoleEveryone
	}
}

func kindString(k ActionKind) string {
	switch k {
	case ActionDelete:
		return "delete"
	case ActionTimeout:
		return "timeout"
	case ActionBan:
		return "ban"
	default:
		return "none"
	}
}
