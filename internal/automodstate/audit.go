package automodstate

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ModAction is one logged enforcement action: a full snapshot of what AutoMod
// did (or, when DryRun, would have done) so a streamer can later review or
// reverse it. Every field is captured at decision time; nothing is looked up
// after the fact.
type ModAction struct {
	// ID is a lower-cased ULID assigned on Log if empty.
	ID string
	// TenantID scopes the row to a deployment; self-hosted uses "local".
	TenantID string
	// Channel is the chat channel the action occurred in.
	Channel string
	// UserID is the platform user id of the offender (may be empty).
	UserID string
	// Username is the human-readable offender name (used for per-user history).
	Username string
	// MessageID is the platform message id that triggered the action.
	MessageID string
	// MessageText is the offending message content, stored for review.
	MessageText string
	// FilterName identifies which detection filter fired (e.g. "caps", "links").
	FilterName string
	// Reason is a human-readable explanation shown to mods.
	Reason string
	// MatchedText is the specific substring that tripped the filter.
	MatchedText string
	// Action is the enforcement taken: "warn"|"timeout"|"ban"|"delete".
	Action string
	// DurationSec is the timeout length in seconds; 0 for warn/ban/delete.
	DurationSec int
	// DryRun is true when the action was logged but not executed (shadow mode).
	DryRun bool
	// CreatedAt is when the action occurred (UTC); assigned on Log if zero.
	CreatedAt time.Time
}

// AuditStore persists ModAction rows in SQLite. It mirrors the conventions of
// internal/counters: WAL journal, single connection, embedded migrations. All
// methods are safe for concurrent use.
type AuditStore struct {
	db  *sql.DB
	log *slog.Logger
}

// newID returns a fresh lower-cased ULID, mirroring counters.newID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// defaultListLimit caps List/ListByUser when the caller passes <= 0.
const defaultListLimit = 100

// maxListLimit is the hard ceiling on rows returned by a single query, so a
// caller cannot accidentally pull an unbounded result set into memory.
const maxListLimit = 500

// OpenSQLiteStore opens (or creates) a SQLite database at dsn and returns a
// ready-to-use AuditStore. dsn may be a file path or a full modernc.org/sqlite
// DSN; use "file:...?mode=memory&cache=shared" for tests.
//
// The returned store has WAL journal mode, foreign-keys ON, synchronous=NORMAL
// and a 5s busy timeout, and runs all embedded migrations before returning.
func OpenSQLiteStore(ctx context.Context, dsn string, logger *slog.Logger) (*AuditStore, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("automodstate: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("automodstate: %s: %w", p, err)
		}
	}

	s := &AuditStore{db: db, log: logger}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// migrate applies every embedded migration in lexical filename order.
func (s *AuditStore) migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("automodstate: read migrations dir: %w", err)
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
			return fmt.Errorf("automodstate: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("automodstate: apply migration %s: %w", name, err)
		}
		s.log.Debug("automodstate: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *AuditStore) Close() error { return s.db.Close() }

// Log inserts one ModAction and returns the stored row. A missing ID is filled
// with a fresh ULID and a zero CreatedAt is set to the current UTC time, so
// callers may pass a partially-populated struct. CreatedAt is persisted as Unix
// seconds, matching the second-granularity used by the audit table.
func (s *AuditStore) Log(ctx context.Context, a ModAction) (ModAction, error) {
	if strings.TrimSpace(a.ID) == "" {
		a.ID = newID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	// Normalise to whole seconds in UTC so the in-memory value matches what a
	// subsequent read returns (the column stores Unix seconds, dropping any
	// sub-second precision).
	a.CreatedAt = a.CreatedAt.UTC().Truncate(time.Second)

	dryRun := 0
	if a.DryRun {
		dryRun = 1
	}

	const ins = `
INSERT INTO automod_audit (
    id, tenant_id, channel, user_id, username, message_id, message_text,
    filter_name, reason, matched_text, action, duration_sec, dry_run, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		a.ID, a.TenantID, a.Channel, a.UserID, a.Username, a.MessageID,
		a.MessageText, a.FilterName, a.Reason, a.MatchedText, a.Action,
		a.DurationSec, dryRun, a.CreatedAt.Unix()); err != nil {
		return ModAction{}, fmt.Errorf("automodstate: log: %w", err)
	}
	return a, nil
}

const auditSelect = `SELECT id, tenant_id, channel, user_id, username, message_id,
       message_text, filter_name, reason, matched_text, action, duration_sec,
       dry_run, created_at
FROM automod_audit `

// clampLimit applies the default-and-ceiling policy shared by List/ListByUser.
func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

// scanAction scans one row into a ModAction, converting the stored Unix-seconds
// timestamp back to a UTC time.Time and the 0/1 flag back to a bool.
func scanAction(row interface{ Scan(...any) error }) (ModAction, error) {
	var (
		a       ModAction
		dryRun  int
		created int64
	)
	err := row.Scan(&a.ID, &a.TenantID, &a.Channel, &a.UserID, &a.Username,
		&a.MessageID, &a.MessageText, &a.FilterName, &a.Reason, &a.MatchedText,
		&a.Action, &a.DurationSec, &dryRun, &created)
	if err != nil {
		return ModAction{}, err
	}
	a.DryRun = dryRun != 0
	a.CreatedAt = time.Unix(created, 0).UTC()
	return a, nil
}

// List returns the most recent actions for (tenantID, channel), newest first.
// limit is clamped to [1, 500] with a default of 100 when non-positive.
func (s *AuditStore) List(ctx context.Context, tenantID, channel string, limit int) ([]ModAction, error) {
	limit = clampLimit(limit)
	rows, err := s.db.QueryContext(ctx,
		auditSelect+`WHERE tenant_id = ? AND channel = ?
ORDER BY created_at DESC, id DESC LIMIT ?`,
		tenantID, channel, limit)
	if err != nil {
		return nil, fmt.Errorf("automodstate: list: %w", err)
	}
	defer rows.Close()
	return scanActions(rows)
}

// ListByUser returns the most recent actions for one username within a channel,
// newest first, for the per-user offense history view. limit is clamped to
// [1, 500] with a default of 100 when non-positive.
func (s *AuditStore) ListByUser(ctx context.Context, tenantID, channel, username string, limit int) ([]ModAction, error) {
	limit = clampLimit(limit)
	rows, err := s.db.QueryContext(ctx,
		auditSelect+`WHERE tenant_id = ? AND channel = ? AND username = ?
ORDER BY created_at DESC, id DESC LIMIT ?`,
		tenantID, channel, username, limit)
	if err != nil {
		return nil, fmt.Errorf("automodstate: list by user: %w", err)
	}
	defer rows.Close()
	return scanActions(rows)
}

// scanActions drains a *sql.Rows into a slice of ModAction.
func scanActions(rows *sql.Rows) ([]ModAction, error) {
	var out []ModAction
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, fmt.Errorf("automodstate: scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
