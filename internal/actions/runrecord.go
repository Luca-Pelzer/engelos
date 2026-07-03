package actions

import (
	"context"
	"errors"
	"time"
)

// ErrRunNotFound is returned by a run store when a run lookup matches no row.
var ErrRunNotFound = errors.New("actions: run not found")

// NewRunID mints a fresh run id (a lower-cased ULID). A caller uses it to
// pre-allocate the id a firing will be recorded under - e.g. the manual-fire
// endpoint correlating its response with the eventual run.
func NewRunID() string { return newID() }

// RunRecorder persists a completed rule-firing trace. The engine calls Record
// once per firing that entered the action phase, in the worker goroutine after
// execution. A nil recorder (the default) disables recording at zero cost.
// A returned error is logged and ignored so a recording failure never affects
// the rule that fired.
type RunRecorder interface {
	Record(ctx context.Context, trace RunTrace) error
}

// RunTrace is one recorded rule firing: the run header plus its ordered node
// entries. Every summary field is already redacted by the engine before it
// reaches a recorder.
type RunTrace struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	Channel        string    `json:"channel"`
	RuleName       string    `json:"rule_name"`
	TriggerKind    string    `json:"trigger_kind"`
	TriggerSummary string    `json:"trigger_summary"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	Status         string    `json:"status"`
	NodeCount      int       `json:"node_count"`
	Nodes          []RunNode `json:"nodes,omitempty"`
}

// RunNode is one condition or action entry within a run.
type RunNode struct {
	Seq           int    `json:"seq"`
	NodeKind      string `json:"node_kind"`
	TypeID        string `json:"type_id"`
	Status        string `json:"status"`
	DurationMs    int64  `json:"duration_ms"`
	Error         string `json:"error,omitempty"`
	OutputSummary string `json:"output_summary,omitempty"`
}

// Run status values.
const (
	RunStatusOK      = "ok"
	RunStatusPartial = "partial"
	RunStatusError   = "error"
	RunStatusStopped = "stopped"
)

// Node status and kind values.
const (
	nodeStatusOK      = "ok"
	nodeStatusFail    = "fail"
	nodeStatusSkipped = "skipped"
	nodeStatusStopped = "stopped"

	nodeKindCondition = "condition"
	nodeKindAction    = "action"

	errorMaxRunes = 300
)

// traceBuilder accumulates a RunTrace during a single rule firing. It is used by
// exactly one worker goroutine, so it needs no locking.
type traceBuilder struct {
	trace     RunTrace
	seq       int
	ranCount  int
	failCount int
	hasStop   bool
}

// newTraceBuilder starts a trace for one firing, reusing the trigger's
// pre-allocated RunID (manual fire correlation) when present, else minting one.
func newTraceBuilder(r Rule, t Trigger) *traceBuilder {
	id := t.RunID
	if id == "" {
		id = newID()
	}
	return &traceBuilder{trace: RunTrace{
		ID:             id,
		TenantID:       r.TenantID,
		Channel:        r.Channel,
		RuleName:       r.Name,
		TriggerKind:    string(t.Kind),
		TriggerSummary: summarizeTrigger(t),
		StartedAt:      time.Now().UTC(),
	}}
}

func (tb *traceBuilder) nextSeq() int {
	s := tb.seq
	tb.seq++
	return s
}

func (tb *traceBuilder) addCondition(typeID string, passed bool, dur time.Duration) {
	status := nodeStatusOK
	if !passed {
		status = nodeStatusFail
	}
	tb.trace.Nodes = append(tb.trace.Nodes, RunNode{
		Seq:        tb.nextSeq(),
		NodeKind:   nodeKindCondition,
		TypeID:     typeID,
		Status:     status,
		DurationMs: dur.Milliseconds(),
	})
}

func (tb *traceBuilder) addAction(typeID, status string, err error, dur time.Duration, outputs map[string]any) {
	tb.trace.Nodes = append(tb.trace.Nodes, RunNode{
		Seq:           tb.nextSeq(),
		NodeKind:      nodeKindAction,
		TypeID:        typeID,
		Status:        status,
		DurationMs:    dur.Milliseconds(),
		Error:         truncateRunes(errString(err), errorMaxRunes),
		OutputSummary: summarizeOutputs(typeID, outputs),
	})
	switch status {
	case nodeStatusOK:
		tb.ranCount++
	case nodeStatusFail:
		tb.ranCount++
		tb.failCount++
	}
}

// markStopped records the actions left unrun after an explicit Stop as stopped
// (or skipped when disabled) and flags the whole run stopped.
func (tb *traceBuilder) markStopped(remaining []ActionInstance) {
	tb.hasStop = true
	for _, ai := range remaining {
		status := nodeStatusStopped
		if !ai.Enabled {
			status = nodeStatusSkipped
		}
		tb.trace.Nodes = append(tb.trace.Nodes, RunNode{
			Seq:      tb.nextSeq(),
			NodeKind: nodeKindAction,
			TypeID:   ai.TypeID,
			Status:   status,
		})
	}
}

// finish stamps the end time and derives the overall run status: a Stop wins,
// then all-actions-failed is an error, any failure is partial, else ok.
func (tb *traceBuilder) finish() RunTrace {
	tb.trace.FinishedAt = time.Now().UTC()
	tb.trace.NodeCount = len(tb.trace.Nodes)
	switch {
	case tb.hasStop:
		tb.trace.Status = RunStatusStopped
	case tb.ranCount > 0 && tb.failCount == tb.ranCount:
		tb.trace.Status = RunStatusError
	case tb.failCount > 0:
		tb.trace.Status = RunStatusPartial
	default:
		tb.trace.Status = RunStatusOK
	}
	return tb.trace
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
