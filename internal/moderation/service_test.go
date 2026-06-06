package moderation

import (
	"context"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/automod"
	"github.com/Luca-Pelzer/engelos/internal/automodstate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capsEngine returns an engine with only the caps filter enabled, configured to
// delete-then-escalate (TimeoutSecs 0 makes the filter floor a delete; the
// escalator drives the climb to timeouts/ban).
func capsEngine(t *testing.T, mode automod.FilterMode) *automod.Engine {
	t.Helper()
	cfg := automod.DefaultConfig()
	cfg.Mode = mode
	cfg.Caps.Enabled = true
	cfg.Caps.MinLength = 5
	cfg.Caps.MaxCapsPercent = 0.5
	cfg.Caps.TimeoutSecs = 0
	eng, err := automod.NewEngine(cfg)
	require.NoError(t, err)
	return eng
}

func newSvc(t *testing.T, eng *automod.Engine) *Service {
	t.Helper()
	return New(Config{Engine: eng, TenantID: "local"})
}

func capsMsg() Message {
	return Message{Channel: "chan", MessageID: "m1", UserID: "u1", Username: "bob", Text: "AAAAAAAA SHOUTING"}
}

func TestNew_NilEngineReturnsNil(t *testing.T) {
	assert.Nil(t, New(Config{Engine: nil}))
}

func TestEvaluate_NilServicePasses(t *testing.T) {
	var s *Service
	dec := s.Evaluate(context.Background(), capsMsg())
	assert.Equal(t, ActionNone, dec.Kind)
}

func TestEvaluate_CleanMessagePasses(t *testing.T) {
	s := newSvc(t, capsEngine(t, automod.ModeActive))
	dec := s.Evaluate(context.Background(), Message{Channel: "chan", Username: "bob", Text: "hello there friends"})
	assert.Equal(t, ActionNone, dec.Kind)
}

func TestEvaluate_ModeratorExempt(t *testing.T) {
	s := newSvc(t, capsEngine(t, automod.ModeActive))
	msg := capsMsg()
	msg.IsModerator = true
	dec := s.Evaluate(context.Background(), msg)
	assert.Equal(t, ActionNone, dec.Kind)
}

// TestEvaluate_EscalationLadder verifies a repeat caps offender climbs
// delete → 60s → 10m → 24h → ban across successive violations, driven by the
// escalator even though the filter floor is only a delete.
func TestEvaluate_EscalationLadder(t *testing.T) {
	s := newSvc(t, capsEngine(t, automod.ModeActive))
	ctx := context.Background()

	d1 := s.Evaluate(ctx, capsMsg())
	assert.Equal(t, ActionDelete, d1.Kind, "1st offense = warn/delete")

	d2 := s.Evaluate(ctx, capsMsg())
	require.Equal(t, ActionTimeout, d2.Kind, "2nd = timeout")
	assert.Equal(t, 60*time.Second, d2.Duration)

	d3 := s.Evaluate(ctx, capsMsg())
	assert.Equal(t, ActionTimeout, d3.Kind)
	assert.Equal(t, 10*time.Minute, d3.Duration)

	d4 := s.Evaluate(ctx, capsMsg())
	assert.Equal(t, ActionTimeout, d4.Kind)
	assert.Equal(t, 24*time.Hour, d4.Duration)

	d5 := s.Evaluate(ctx, capsMsg())
	assert.Equal(t, ActionBan, d5.Kind, "5th = ban")

	d6 := s.Evaluate(ctx, capsMsg())
	assert.Equal(t, ActionBan, d6.Kind, "beyond last tier clamps to ban")
}

func TestEvaluate_DryRunFlagsButStillDecides(t *testing.T) {
	s := newSvc(t, capsEngine(t, automod.ModeDryRun))
	dec := s.Evaluate(context.Background(), capsMsg())
	assert.Equal(t, ActionDelete, dec.Kind)
	assert.True(t, dec.DryRun, "dry-run mode must flag the decision")
}

func TestEvaluate_ModeOffPasses(t *testing.T) {
	s := newSvc(t, capsEngine(t, automod.ModeOff))
	dec := s.Evaluate(context.Background(), capsMsg())
	assert.Equal(t, ActionNone, dec.Kind)
}

// TestEvaluate_LinkPermitWaivesOnce verifies a granted permit suppresses a
// single link violation and is then consumed.
func TestEvaluate_LinkPermitWaivesOnce(t *testing.T) {
	cfg := automod.DefaultConfig()
	cfg.Links.Enabled = true
	cfg.Links.TimeoutSecs = 600
	eng, err := automod.NewEngine(cfg)
	require.NoError(t, err)
	s := newSvc(t, eng)
	ctx := context.Background()
	msg := Message{Channel: "chan", MessageID: "m1", UserID: "u1", Username: "bob", Text: "check evil.example.com now"}

	s.Permit("chan", "bob")
	dec := s.Evaluate(ctx, msg)
	assert.Equal(t, ActionNone, dec.Kind, "permit waives the link once")

	dec2 := s.Evaluate(ctx, msg)
	assert.NotEqual(t, ActionNone, dec2.Kind, "permit is single-use")
}

// TestEvaluate_BanFloorOverridesEscalation verifies a filter whose own verdict
// is a ban (e.g. a banned-word entry) bans on the FIRST offense regardless of
// the escalator's warn rung.
func TestEvaluate_BanFloorOverridesEscalation(t *testing.T) {
	cfg := automod.DefaultConfig()
	cfg.BannedWords.Enabled = true
	cfg.BannedWords.Entries = []automod.BannedEntry{
		{Phrase: "slur", MatchMode: automod.MatchAnywhere, Verdict: automod.VerdictBan},
	}
	eng, err := automod.NewEngine(cfg)
	require.NoError(t, err)
	s := newSvc(t, eng)
	dec := s.Evaluate(context.Background(), Message{Channel: "chan", MessageID: "m1", UserID: "u1", Username: "bob", Text: "you slur"})
	assert.Equal(t, ActionBan, dec.Kind, "ban-verdict filter bans on first offense")
}

func TestService_AuditPersists(t *testing.T) {
	ctx := context.Background()
	audit, err := automodstate.OpenSQLiteStore(ctx, "file:modsvc-audit?mode=memory&cache=shared", nil)
	require.NoError(t, err)
	defer audit.Close()

	s := New(Config{Engine: capsEngine(t, automod.ModeActive), Audit: audit, TenantID: "local"})
	s.Evaluate(ctx, capsMsg())

	rows, err := audit.List(ctx, "local", "chan", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "caps", rows[0].FilterName)
	assert.Equal(t, "delete", rows[0].Action)
}
