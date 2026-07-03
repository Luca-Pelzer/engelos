package actions

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store is the persistence contract for action rules. All methods are safe for
// concurrent use. ListEnabled is on the engine's hot path and returns only
// enabled rules for a channel so the engine never filters.
type Store interface {
	Create(ctx context.Context, r Rule) (Rule, error)
	Update(ctx context.Context, r Rule) (Rule, error)
	Get(ctx context.Context, tenantID, channel, name string) (Rule, error)
	Delete(ctx context.Context, tenantID, channel, name string) error
	List(ctx context.Context, tenantID, channel string) ([]Rule, error)
	ListEnabled(ctx context.Context, tenantID, channel string) ([]Rule, error)
	ListTimerRules(ctx context.Context, tenantID string) ([]Rule, error)
	ListEventChannels(ctx context.Context, tenantID, eventType string) ([]string, error)
	SetEnabled(ctx context.Context, tenantID, channel, name string, enabled bool) error
	Close() error
}

// sqliteStore is a pure-Go SQLite implementation backed by modernc.org/sqlite,
// mirroring internal/counters.sqliteStore conventions: WAL, foreign-keys,
// busy_timeout, a single open connection, and a sync.Mutex around the
// check-then-insert that Create performs.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger
	mu  sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn and returns a
// ready-to-use Store with WAL journal mode, foreign-keys ON, and
// synchronous=NORMAL. Use "file::memory:?cache=shared" for tests.
func OpenSQLiteStore(ctx context.Context, dsn string, logger *slog.Logger) (Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("actions: open sqlite: %w", err)
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
			return nil, fmt.Errorf("actions: %s: %w", p, err)
		}
	}

	s := &sqliteStore{db: db, log: logger}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *sqliteStore) migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("actions: read migrations dir: %w", err)
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
			return fmt.Errorf("actions: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("actions: apply migration %s: %w", name, err)
		}
		s.log.Debug("actions: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const ruleSelect = `SELECT id, tenant_id, channel, name, enabled, trigger_kind,
                    trigger_filter, conditions, actions, schema_version,
                    created_at, updated_at FROM rules `

func scanRule(row interface{ Scan(...any) error }) (Rule, error) {
	var (
		r              Rule
		enabled        int
		triggerKind    string
		triggerFilter  string
		conditionsJSON string
		actionsJSON    string
		created        int64
		updated        int64
	)
	if err := row.Scan(&r.ID, &r.TenantID, &r.Channel, &r.Name, &enabled,
		&triggerKind, &triggerFilter, &conditionsJSON, &actionsJSON,
		&r.SchemaVersion, &created, &updated); err != nil {
		return Rule{}, err
	}
	r.Enabled = enabled != 0
	r.TriggerKind = TriggerKind(triggerKind)
	if triggerFilter != "" {
		r.TriggerFilter = json.RawMessage(triggerFilter)
	}
	if err := json.Unmarshal([]byte(conditionsJSON), &r.Conditions); err != nil {
		return Rule{}, fmt.Errorf("actions: decode conditions: %w", err)
	}
	if err := json.Unmarshal([]byte(actionsJSON), &r.Actions); err != nil {
		return Rule{}, fmt.Errorf("actions: decode actions: %w", err)
	}
	r.CreatedAt = time.Unix(0, created).UTC()
	r.UpdatedAt = time.Unix(0, updated).UTC()
	return r, nil
}

func marshalRule(r Rule) (triggerFilter, conditions, actions string, err error) {
	tf := ""
	if len(r.TriggerFilter) > 0 {
		tf = string(r.TriggerFilter)
	}
	cond, err := json.Marshal(r.Conditions)
	if err != nil {
		return "", "", "", fmt.Errorf("actions: encode conditions: %w", err)
	}
	act, err := json.Marshal(r.Actions)
	if err != nil {
		return "", "", "", fmt.Errorf("actions: encode actions: %w", err)
	}
	return tf, string(cond), string(act), nil
}

func (s *sqliteStore) Create(ctx context.Context, r Rule) (Rule, error) {
	if err := r.validate(); err != nil {
		return Rule{}, err
	}
	if strings.TrimSpace(r.ID) == "" {
		r.ID = newID()
	}
	now := time.Now().UTC()
	r.CreatedAt = now
	r.UpdatedAt = now
	r.SchemaVersion = SchemaVersion

	tf, cond, act, err := marshalRule(r)
	if err != nil {
		return Rule{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	const dup = `SELECT 1 FROM rules WHERE tenant_id = ? AND channel = ? AND name = ? LIMIT 1`
	var x int
	switch err := s.db.QueryRowContext(ctx, dup, r.TenantID, r.Channel, r.Name).Scan(&x); {
	case err == nil:
		return Rule{}, ErrAlreadyExists
	case errors.Is(err, sql.ErrNoRows):
	default:
		return Rule{}, fmt.Errorf("actions: check duplicate: %w", err)
	}

	const ins = `INSERT INTO rules
(id, tenant_id, channel, name, enabled, trigger_kind, trigger_filter,
 conditions, actions, schema_version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		r.ID, r.TenantID, r.Channel, r.Name, boolToInt(r.Enabled),
		string(r.TriggerKind), tf, cond, act, r.SchemaVersion,
		now.UnixNano(), now.UnixNano()); err != nil {
		return Rule{}, fmt.Errorf("actions: insert: %w", err)
	}
	return r, nil
}

func (s *sqliteStore) Update(ctx context.Context, r Rule) (Rule, error) {
	if err := r.validate(); err != nil {
		return Rule{}, err
	}
	r.UpdatedAt = time.Now().UTC()
	r.SchemaVersion = SchemaVersion

	tf, cond, act, err := marshalRule(r)
	if err != nil {
		return Rule{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	const upd = `UPDATE rules SET
enabled = ?, trigger_kind = ?, trigger_filter = ?, conditions = ?,
actions = ?, schema_version = ?, updated_at = ?
WHERE tenant_id = ? AND channel = ? AND name = ?`
	res, err := s.db.ExecContext(ctx, upd,
		boolToInt(r.Enabled), string(r.TriggerKind), tf, cond, act,
		r.SchemaVersion, r.UpdatedAt.UnixNano(),
		r.TenantID, r.Channel, r.Name)
	if err != nil {
		return Rule{}, fmt.Errorf("actions: update: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return Rule{}, ErrNotFound
	}
	return s.getLocked(ctx, r.TenantID, r.Channel, r.Name)
}

func (s *sqliteStore) Get(ctx context.Context, tenantID, channel, name string) (Rule, error) {
	row := s.db.QueryRowContext(ctx,
		ruleSelect+`WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, normalizeName(name))
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, ErrNotFound
	}
	if err != nil {
		return Rule{}, fmt.Errorf("actions: get: %w", err)
	}
	return r, nil
}

func (s *sqliteStore) getLocked(ctx context.Context, tenantID, channel, name string) (Rule, error) {
	row := s.db.QueryRowContext(ctx,
		ruleSelect+`WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, ErrNotFound
	}
	if err != nil {
		return Rule{}, fmt.Errorf("actions: get: %w", err)
	}
	return r, nil
}

func (s *sqliteStore) Delete(ctx context.Context, tenantID, channel, name string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM rules WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, normalizeName(name))
	if err != nil {
		return fmt.Errorf("actions: delete: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) SetEnabled(ctx context.Context, tenantID, channel, name string, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE rules SET enabled = ?, updated_at = ? WHERE tenant_id = ? AND channel = ? AND name = ?`,
		boolToInt(enabled), time.Now().UTC().UnixNano(), tenantID, channel, normalizeName(name))
	if err != nil {
		return fmt.Errorf("actions: set enabled: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) List(ctx context.Context, tenantID, channel string) ([]Rule, error) {
	return s.query(ctx,
		ruleSelect+`WHERE tenant_id = ? AND channel = ? ORDER BY name ASC`,
		tenantID, channel)
}

func (s *sqliteStore) ListEnabled(ctx context.Context, tenantID, channel string) ([]Rule, error) {
	return s.query(ctx,
		ruleSelect+`WHERE tenant_id = ? AND channel = ? AND enabled = 1 ORDER BY name ASC`,
		tenantID, channel)
}

// ListTimerRules returns every enabled timer-kind rule across all channels of a
// tenant so the scheduler can arm one ticker per rule without a separate
// list-all-channels query.
func (s *sqliteStore) ListTimerRules(ctx context.Context, tenantID string) ([]Rule, error) {
	return s.query(ctx,
		ruleSelect+`WHERE tenant_id = ? AND enabled = 1 AND trigger_kind = 'timer' ORDER BY channel ASC, name ASC`,
		tenantID)
}

// ListEventChannels returns the distinct channels of a tenant that own at least
// one enabled event-kind rule whose trigger filter accepts eventType. It powers
// fan-out for platform events that carry no channel of their own (donations),
// applying the same event_type predicate the engine uses at fire time so a
// channel is included exactly when one of its rules would match.
func (s *sqliteStore) ListEventChannels(ctx context.Context, tenantID, eventType string) ([]string, error) {
	rules, err := s.query(ctx,
		ruleSelect+`WHERE tenant_id = ? AND enabled = 1 AND trigger_kind = 'event' ORDER BY channel ASC`,
		tenantID)
	if err != nil {
		return nil, err
	}
	probe := Trigger{Kind: TriggerEvent, EventType: eventType}
	seen := make(map[string]struct{}, len(rules))
	var out []string
	for _, r := range rules {
		if _, ok := seen[r.Channel]; ok {
			continue
		}
		if matchTriggerFilter(r, probe) {
			seen[r.Channel] = struct{}{}
			out = append(out, r.Channel)
		}
	}
	return out, nil
}

func (s *sqliteStore) query(ctx context.Context, sqlText string, args ...any) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("actions: list: %w", err)
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, fmt.Errorf("actions: scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
