// Package runs is the SQLite-backed store for workflow run history: it records
// the per-node trace of every rule firing that entered the action phase and
// serves it back for the run inspector. It satisfies actions.RunRecorder and
// enforces a per-(tenant, channel) retention cap.
package runs

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store records and reads workflow run history. It satisfies
// actions.RunRecorder. Each (tenant, channel) keeps at most cap runs; older runs
// are evicted on insert.
type Store struct {
	db  *sql.DB
	cap int
	log *slog.Logger
}

// OpenStore opens (or creates) the run-history database at dsn with WAL,
// foreign-keys ON, synchronous NORMAL and a 5s busy timeout, applies the
// embedded migrations, and caps retention at capRuns per (tenant, channel).
// capRuns must be positive; retention of 0 (recording disabled) is handled by
// the caller not constructing a Store at all.
func OpenStore(ctx context.Context, dsn string, capRuns int, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if capRuns <= 0 {
		return nil, fmt.Errorf("runs: retention cap must be positive, got %d", capRuns)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("runs: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("runs: %s: %w", p, err)
		}
	}
	s := &Store{db: db, cap: capRuns, log: logger}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// migrate applies every embedded migration in lexical filename order, tolerating
// a re-run "duplicate column name" so ADD COLUMN migrations stay idempotent.
func (s *Store) migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("runs: read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("runs: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("runs: apply migration %s: %w", name, err)
		}
	}
	return nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error { return s.db.Close() }

// Record persists one run trace (header + node rows) atomically and evicts the
// oldest runs beyond the retention cap for the trace's (tenant, channel). It
// satisfies actions.RunRecorder; the engine logs and ignores any returned error
// so recording never affects the rule that fired.
func (s *Store) Record(ctx context.Context, tr actions.RunTrace) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("runs: record begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const insRun = `
INSERT INTO runs (id, tenant_id, channel, rule_name, trigger_kind, trigger_summary,
    started_at, finished_at, status, node_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := tx.ExecContext(ctx, insRun,
		tr.ID, tr.TenantID, tr.Channel, tr.RuleName, tr.TriggerKind, tr.TriggerSummary,
		tr.StartedAt.UTC().UnixNano(), tr.FinishedAt.UTC().UnixNano(), tr.Status, len(tr.Nodes)); err != nil {
		return fmt.Errorf("runs: insert run: %w", err)
	}

	const insNode = `
INSERT INTO run_nodes (run_id, seq, node_kind, type_id, status, duration_ms, error, output_summary)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	for _, n := range tr.Nodes {
		if _, err := tx.ExecContext(ctx, insNode,
			tr.ID, n.Seq, n.NodeKind, n.TypeID, n.Status, n.DurationMs, n.Error, n.OutputSummary); err != nil {
			return fmt.Errorf("runs: insert node: %w", err)
		}
	}

	// Keep only the newest cap runs for this scope. ULIDs sort lexicographically
	// by time, so ORDER BY id DESC is newest-first; the delete cascades to
	// run_nodes via the foreign key.
	const evict = `
DELETE FROM runs WHERE id IN (
    SELECT id FROM runs WHERE tenant_id = ? AND channel = ?
    ORDER BY id DESC LIMIT -1 OFFSET ?
)`
	if _, err := tx.ExecContext(ctx, evict, tr.TenantID, tr.Channel, s.cap); err != nil {
		return fmt.Errorf("runs: evict: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("runs: record commit: %w", err)
	}
	return nil
}

// ListRuns returns the newest-first runs for one rule, without node entries,
// capped at limit.
func (s *Store) ListRuns(ctx context.Context, tenantID, channel, ruleName string, limit int) ([]actions.RunTrace, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT id, tenant_id, channel, rule_name, trigger_kind, trigger_summary,
    started_at, finished_at, status, node_count
FROM runs
WHERE tenant_id = ? AND channel = ? AND rule_name = ?
ORDER BY id DESC
LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, tenantID, channel, ruleName, limit)
	if err != nil {
		return nil, fmt.Errorf("runs: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []actions.RunTrace{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRun returns one run with its ordered node entries, scoped to the owning
// rule so a run from another channel or rule is never returned. Returns
// actions.ErrRunNotFound when no matching run exists.
func (s *Store) GetRun(ctx context.Context, tenantID, channel, ruleName, runID string) (actions.RunTrace, error) {
	const q = `
SELECT id, tenant_id, channel, rule_name, trigger_kind, trigger_summary,
    started_at, finished_at, status, node_count
FROM runs
WHERE tenant_id = ? AND channel = ? AND rule_name = ? AND id = ?`
	row := s.db.QueryRowContext(ctx, q, tenantID, channel, ruleName, runID)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return actions.RunTrace{}, actions.ErrRunNotFound
	}
	if err != nil {
		return actions.RunTrace{}, err
	}
	nodes, err := s.nodes(ctx, runID)
	if err != nil {
		return actions.RunTrace{}, err
	}
	r.Nodes = nodes
	return r, nil
}

func (s *Store) nodes(ctx context.Context, runID string) ([]actions.RunNode, error) {
	const q = `
SELECT seq, node_kind, type_id, status, duration_ms, error, output_summary
FROM run_nodes WHERE run_id = ? ORDER BY seq ASC`
	rows, err := s.db.QueryContext(ctx, q, runID)
	if err != nil {
		return nil, fmt.Errorf("runs: nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []actions.RunNode{}
	for rows.Next() {
		var n actions.RunNode
		if err := rows.Scan(&n.Seq, &n.NodeKind, &n.TypeID, &n.Status, &n.DurationMs, &n.Error, &n.OutputSummary); err != nil {
			return nil, fmt.Errorf("runs: scan node: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeleteRuns clears all run history for one rule and returns how many runs were
// removed; node rows cascade away via the foreign key.
func (s *Store) DeleteRuns(ctx context.Context, tenantID, channel, ruleName string) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM runs WHERE tenant_id = ? AND channel = ? AND rule_name = ?`,
		tenantID, channel, ruleName)
	if err != nil {
		return 0, fmt.Errorf("runs: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// CountRuns returns how many runs exist for a (tenant, channel). It backs
// retention assertions in tests.
func (s *Store) CountRuns(ctx context.Context, tenantID, channel string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM runs WHERE tenant_id = ? AND channel = ?`, tenantID, channel).Scan(&n)
	return n, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRun(sc scanner) (actions.RunTrace, error) {
	var (
		r        actions.RunTrace
		started  int64
		finished int64
	)
	if err := sc.Scan(&r.ID, &r.TenantID, &r.Channel, &r.RuleName, &r.TriggerKind, &r.TriggerSummary,
		&started, &finished, &r.Status, &r.NodeCount); err != nil {
		return actions.RunTrace{}, err
	}
	r.StartedAt = time.Unix(0, started).UTC()
	r.FinishedAt = time.Unix(0, finished).UTC()
	return r, nil
}
