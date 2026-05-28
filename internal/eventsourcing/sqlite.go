package eventsourcing

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type SQLiteStore struct {
	db     *sql.DB
	logger *slog.Logger
}

type SQLiteOption func(*sqliteConfig)

type sqliteConfig struct {
	logger          *slog.Logger
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
	busyTimeout     time.Duration
}

func WithLogger(l *slog.Logger) SQLiteOption {
	return func(c *sqliteConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

func WithMaxOpenConns(n int) SQLiteOption {
	return func(c *sqliteConfig) {
		if n > 0 {
			c.maxOpenConns = n
		}
	}
}

func WithBusyTimeout(d time.Duration) SQLiteOption {
	return func(c *sqliteConfig) {
		if d > 0 {
			c.busyTimeout = d
		}
	}
}

func OpenSQLite(ctx context.Context, dsn string, opts ...SQLiteOption) (*SQLiteStore, error) {
	cfg := sqliteConfig{
		logger:          slog.Default(),
		maxOpenConns:    1,
		maxIdleConns:    1,
		connMaxLifetime: 0,
		busyTimeout:     5 * time.Second,
	}
	for _, o := range opts {
		o(&cfg)
	}

	connStr, err := buildSQLiteDSN(dsn, cfg.busyTimeout)
	if err != nil {
		return nil, fmt.Errorf("eventsourcing: build dsn: %w", err)
	}

	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("eventsourcing: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(cfg.maxOpenConns)
	db.SetMaxIdleConns(cfg.maxIdleConns)
	if cfg.connMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.connMaxLifetime)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("eventsourcing: ping: %w", err)
	}

	store := &SQLiteStore{db: db, logger: cfg.logger}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("eventsourcing: migrate: %w", err)
	}
	return store, nil
}

func buildSQLiteDSN(raw string, busyTimeout time.Duration) (string, error) {
	if raw == "" {
		return "", errors.New("empty dsn")
	}

	pragmas := map[string]string{
		"_pragma": "",
	}
	_ = pragmas

	base := raw
	var existingQuery string
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		base = raw[:i]
		existingQuery = raw[i+1:]
	}

	q, err := url.ParseQuery(existingQuery)
	if err != nil {
		return "", fmt.Errorf("parse dsn query: %w", err)
	}

	existing := map[string]struct{}{}
	for _, p := range q["_pragma"] {
		name := strings.SplitN(p, "=", 2)[0]
		existing[strings.TrimSpace(strings.ToLower(name))] = struct{}{}
	}

	add := func(pragma string) {
		name := strings.SplitN(pragma, "=", 2)[0]
		if _, ok := existing[strings.TrimSpace(strings.ToLower(name))]; ok {
			return
		}
		q.Add("_pragma", pragma)
	}

	add("journal_mode=WAL")
	add("foreign_keys=ON")
	add("synchronous=NORMAL")
	add(fmt.Sprintf("busy_timeout=%d", busyTimeout.Milliseconds()))

	return base + "?" + q.Encode(), nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) DB() *sql.DB { return s.db }

func (s *SQLiteStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            name TEXT PRIMARY KEY,
            applied_at_ns INTEGER NOT NULL
        ) STRICT;
    `); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var applied int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM schema_migrations WHERE name = ?`, name,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (name, applied_at_ns) VALUES (?, ?)`,
			name, time.Now().UTC().UnixNano(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		s.logger.Info("eventsourcing: applied migration", "name", name)
	}
	return nil
}

func (s *SQLiteStore) Append(ctx context.Context, e Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	return s.insertOne(ctx, s.db, e)
}

func (s *SQLiteStore) AppendBatch(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	for i, e := range events {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("eventsourcing: event[%d]: %w", i, err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("eventsourcing: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for i, e := range events {
		if err := s.insertOne(ctx, tx, e); err != nil {
			return fmt.Errorf("eventsourcing: event[%d]: %w", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("eventsourcing: commit batch: %w", err)
	}
	return nil
}

type sqlExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *SQLiteStore) insertOne(ctx context.Context, x sqlExec, e Event) error {
	var corr, caus any
	if e.CorrelationID != nil {
		corr = e.CorrelationID.String()
	}
	if e.CausationID != nil {
		caus = e.CausationID.String()
	}
	_, err := x.ExecContext(ctx, `
        INSERT INTO events
            (id, type, tenant_id, occurred_at_ns, payload, version, correlation_id, causation_id)
        VALUES
            (?, ?, ?, ?, ?, ?, ?, ?)
    `,
		e.ID.String(),
		e.Type,
		e.TenantID,
		e.OccurredAt.UTC().UnixNano(),
		[]byte(e.Payload),
		int64(e.Version),
		corr,
		caus,
	)
	if err != nil {
		return fmt.Errorf("eventsourcing: insert event %s: %w", e.ID, err)
	}
	return nil
}

func (s *SQLiteStore) Read(ctx context.Context, opts ReadOptions) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if err := validateReadOpts(opts); err != nil {
			yield(Event{}, err)
			return
		}

		query, args := buildSelectQuery(opts, false)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			yield(Event{}, fmt.Errorf("eventsourcing: query: %w", err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			ev, err := scanEvent(rows)
			if err != nil {
				yield(Event{}, err)
				return
			}
			if !yield(ev, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(Event{}, fmt.Errorf("eventsourcing: rows: %w", err))
		}
	}
}

func (s *SQLiteStore) Count(ctx context.Context, opts ReadOptions) (int64, error) {
	if err := validateReadOpts(opts); err != nil {
		return 0, err
	}
	query, args := buildSelectQuery(opts, true)
	var n int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("eventsourcing: count: %w", err)
	}
	return n, nil
}

func validateReadOpts(opts ReadOptions) error {
	if opts.TenantID == "" {
		return fmt.Errorf("eventsourcing: tenant id is required for reads")
	}
	if opts.AfterID != "" {
		if _, err := ulid.ParseStrict(opts.AfterID); err != nil {
			return fmt.Errorf("eventsourcing: invalid after_id: %w", err)
		}
	}
	if opts.Limit < 0 {
		return fmt.Errorf("eventsourcing: limit must be >= 0")
	}
	return nil
}

func buildSelectQuery(opts ReadOptions, count bool) (string, []any) {
	var b strings.Builder
	args := make([]any, 0, 8)

	if count {
		b.WriteString("SELECT COUNT(1) FROM events WHERE tenant_id = ?")
	} else {
		b.WriteString(`SELECT id, type, tenant_id, occurred_at_ns, payload, version, correlation_id, causation_id
            FROM events WHERE tenant_id = ?`)
	}
	args = append(args, opts.TenantID)

	if opts.AfterID != "" {
		b.WriteString(" AND id > ?")
		args = append(args, opts.AfterID)
	}
	if len(opts.Types) > 0 {
		b.WriteString(" AND type IN (")
		for i, t := range opts.Types {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("?")
			args = append(args, t)
		}
		b.WriteString(")")
	}
	if !opts.OccurredAfter.IsZero() {
		b.WriteString(" AND occurred_at_ns >= ?")
		args = append(args, opts.OccurredAfter.UTC().UnixNano())
	}
	if !opts.OccurredBefore.IsZero() {
		b.WriteString(" AND occurred_at_ns < ?")
		args = append(args, opts.OccurredBefore.UTC().UnixNano())
	}

	if !count {
		b.WriteString(" ORDER BY id ASC")
		if opts.Limit > 0 {
			b.WriteString(" LIMIT ?")
			args = append(args, opts.Limit)
		}
	}
	return b.String(), args
}

func scanEvent(rows *sql.Rows) (Event, error) {
	var (
		idStr       string
		typ         string
		tenantID    string
		occurredNs  int64
		payload     []byte
		version     int64
		correlation sql.NullString
		causation   sql.NullString
	)
	if err := rows.Scan(&idStr, &typ, &tenantID, &occurredNs, &payload, &version, &correlation, &causation); err != nil {
		return Event{}, fmt.Errorf("eventsourcing: scan: %w", err)
	}
	id, err := ulid.ParseStrict(idStr)
	if err != nil {
		return Event{}, fmt.Errorf("eventsourcing: parse id %q: %w", idStr, err)
	}
	ev := Event{
		ID:         id,
		Type:       typ,
		TenantID:   tenantID,
		OccurredAt: time.Unix(0, occurredNs).UTC(),
		Payload:    append([]byte(nil), payload...),
		Version:    uint32(version),
	}
	if correlation.Valid {
		cid, err := ulid.ParseStrict(correlation.String)
		if err != nil {
			return Event{}, fmt.Errorf("eventsourcing: parse correlation %q: %w", correlation.String, err)
		}
		ev.CorrelationID = &cid
	}
	if causation.Valid {
		cid, err := ulid.ParseStrict(causation.String)
		if err != nil {
			return Event{}, fmt.Errorf("eventsourcing: parse causation %q: %w", causation.String, err)
		}
		ev.CausationID = &cid
	}
	return ev, nil
}
