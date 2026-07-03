// Package kb is the streamer knowledge base: per-channel text entries (rules,
// schedule, games, FAQ, lore, ...) with SQLite FTS5/BM25 retrieval. It powers a
// CRUD dashboard API and the kb:lookup workflow node so a rule can answer a
// viewer question from the channel's own facts.
package kb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// maxContentBytes caps an entry body at ~4KB so a single entry cannot bloat the
// FTS index or a rendered kb:lookup context.
const maxContentBytes = 4096

// ErrNotFound is returned when an entry does not exist for the (tenant, channel,
// id). ErrInvalid wraps a validation failure (bad category, empty title, body
// over the size cap, missing tenant/channel).
var (
	ErrNotFound = errors.New("kb: entry not found")
	ErrInvalid  = errors.New("kb: invalid entry")
)

// Categories are the fixed entry kinds the dashboard offers.
var Categories = []string{"rules", "schedule", "games", "faq", "lore", "commands", "discord", "other"}

var categorySet = func() map[string]bool {
	m := make(map[string]bool, len(Categories))
	for _, c := range Categories {
		m[c] = true
	}
	return m
}()

// Entry is one knowledge-base fact scoped to a (tenant, channel).
type Entry struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Channel   string    `json:"channel"`
	Category  string    `json:"category"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store is the persistence contract for the knowledge base. All methods are safe
// for concurrent use.
type Store interface {
	// Create inserts a new entry, assigning its id and timestamps.
	Create(ctx context.Context, e Entry) (Entry, error)
	// Update overwrites category/title/content/enabled for an existing entry
	// scoped to (tenant, channel, id). Returns ErrNotFound if absent.
	Update(ctx context.Context, e Entry) (Entry, error)
	// Get returns one entry by id within (tenant, channel).
	Get(ctx context.Context, tenantID, channel, id string) (Entry, error)
	// Delete removes an entry by id within (tenant, channel).
	Delete(ctx context.Context, tenantID, channel, id string) error
	// List returns entries newest-first, optionally filtered by category
	// (empty = all), capped at limit. It includes disabled entries so the
	// dashboard can manage them.
	List(ctx context.Context, tenantID, channel, category string, limit int) ([]Entry, error)
	// Search runs an FTS5/BM25 query over enabled entries, best match first.
	// The query is untrusted chat text and is sanitized before it reaches
	// MATCH. An empty (or tokenless) query returns no rows and no error.
	Search(ctx context.Context, tenantID, channel, query, category string, limit int) ([]Entry, error)
	// Close releases the underlying database handle.
	Close() error
}

func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// validate normalises and checks an entry, returning the canonical category or
// an error wrapping ErrInvalid.
func validate(e Entry) (Entry, error) {
	e.TenantID = strings.TrimSpace(e.TenantID)
	e.Channel = strings.TrimSpace(e.Channel)
	if e.TenantID == "" {
		return Entry{}, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if e.Channel == "" {
		return Entry{}, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	e.Category = strings.ToLower(strings.TrimSpace(e.Category))
	if e.Category == "" {
		e.Category = "other"
	}
	if !categorySet[e.Category] {
		return Entry{}, fmt.Errorf("%w: category %q is not one of %s", ErrInvalid, e.Category, strings.Join(Categories, ", "))
	}
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" {
		return Entry{}, fmt.Errorf("%w: title is required", ErrInvalid)
	}
	e.Content = strings.TrimSpace(e.Content)
	if e.Content == "" {
		return Entry{}, fmt.Errorf("%w: content is required", ErrInvalid)
	}
	if len(e.Content) > maxContentBytes {
		return Entry{}, fmt.Errorf("%w: content length %d exceeds %d bytes", ErrInvalid, len(e.Content), maxContentBytes)
	}
	return e, nil
}

// sqliteStore is a pure-Go SQLite implementation backed by modernc.org/sqlite
// with FTS5 compiled in. Mirrors the counters/quotes store conventions: WAL,
// foreign-keys, busy_timeout, SetMaxOpenConns(1) and a sync.Mutex around writes.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger
	mu  sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn with WAL journal
// mode, foreign-keys ON and synchronous=NORMAL, applies the embedded FTS5
// migration, and returns a ready-to-use Store. Use "file::memory:?cache=shared"
// for tests.
func OpenSQLiteStore(ctx context.Context, dsn string, logger *slog.Logger) (Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("kb: open sqlite: %w", err)
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
			return nil, fmt.Errorf("kb: %s: %w", p, err)
		}
	}

	s := &sqliteStore{db: db, log: logger}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// migrate applies every embedded migration file in lexical order, each as a
// single ExecContext so a multi-statement file (base table + CREATE VIRTUAL
// TABLE fts5 + triggers) applies atomically.
func (s *sqliteStore) migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("kb: read migrations dir: %w", err)
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
			return fmt.Errorf("kb: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("kb: apply migration %s: %w", name, err)
		}
		s.log.Debug("kb: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const entrySelect = `SELECT id, tenant_id, channel, category, title, content, enabled, created_at, updated_at `

func scanEntry(row interface{ Scan(...any) error }) (Entry, error) {
	var (
		e                Entry
		enabled          int64
		created, updated int64
	)
	if err := row.Scan(&e.ID, &e.TenantID, &e.Channel, &e.Category, &e.Title,
		&e.Content, &enabled, &created, &updated); err != nil {
		return Entry{}, err
	}
	e.Enabled = enabled != 0
	e.CreatedAt = time.Unix(0, created).UTC()
	e.UpdatedAt = time.Unix(0, updated).UTC()
	return e, nil
}

// Create inserts a new entry, assigning its id and timestamps.
func (s *sqliteStore) Create(ctx context.Context, e Entry) (Entry, error) {
	v, err := validate(e)
	if err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	v.ID = newID()
	v.CreatedAt = now
	v.UpdatedAt = now
	const ins = `
INSERT INTO kb_entries (id, tenant_id, channel, category, title, content, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		v.ID, v.TenantID, v.Channel, v.Category, v.Title, v.Content,
		boolToInt(v.Enabled), now.UnixNano(), now.UnixNano()); err != nil {
		return Entry{}, fmt.Errorf("kb: insert: %w", err)
	}
	return v, nil
}

// Update overwrites the mutable fields of an existing entry.
func (s *sqliteStore) Update(ctx context.Context, e Entry) (Entry, error) {
	if strings.TrimSpace(e.ID) == "" {
		return Entry{}, fmt.Errorf("%w: id is required", ErrInvalid)
	}
	v, err := validate(e)
	if err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	const upd = `
UPDATE kb_entries SET category = ?, title = ?, content = ?, enabled = ?, updated_at = ?
WHERE id = ? AND tenant_id = ? AND channel = ?`
	res, err := s.db.ExecContext(ctx, upd,
		v.Category, v.Title, v.Content, boolToInt(v.Enabled), now.UnixNano(),
		e.ID, v.TenantID, v.Channel)
	if err != nil {
		return Entry{}, fmt.Errorf("kb: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Entry{}, ErrNotFound
	}
	return s.getLocked(ctx, v.TenantID, v.Channel, e.ID)
}

// Get returns one entry by id within (tenant, channel).
func (s *sqliteStore) Get(ctx context.Context, tenantID, channel, id string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(ctx, tenantID, channel, id)
}

func (s *sqliteStore) getLocked(ctx context.Context, tenantID, channel, id string) (Entry, error) {
	row := s.db.QueryRowContext(ctx,
		entrySelect+`FROM kb_entries WHERE id = ? AND tenant_id = ? AND channel = ?`,
		id, tenantID, channel)
	e, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("kb: get: %w", err)
	}
	return e, nil
}

// Delete removes an entry by id within (tenant, channel).
func (s *sqliteStore) Delete(ctx context.Context, tenantID, channel, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM kb_entries WHERE id = ? AND tenant_id = ? AND channel = ?`,
		id, tenantID, channel)
	if err != nil {
		return fmt.Errorf("kb: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns entries newest-first, optionally filtered by category.
func (s *sqliteStore) List(ctx context.Context, tenantID, channel, category string, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 50
	}
	q := entrySelect + `FROM kb_entries WHERE tenant_id = ? AND channel = ?`
	args := []any{tenantID, channel}
	if c := strings.ToLower(strings.TrimSpace(category)); c != "" {
		q += ` AND category = ?`
		args = append(args, c)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	return s.query(ctx, q, args...)
}

// Search runs an FTS5/BM25 query over enabled entries, best match first. The
// title column is weighted 10x the content column so an entry whose title
// matches the query ranks above one that only mentions it in the body.
func (s *sqliteStore) Search(ctx context.Context, tenantID, channel, query, category string, limit int) ([]Entry, error) {
	match := SanitizeMatchQuery(query)
	if match == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 3
	}
	// Columns are qualified with the e alias: title/content exist in BOTH
	// kb_fts and kb_entries, so an unqualified select over the join is
	// ambiguous. bm25's first weight (10.0) boosts a title hit over a
	// content-only hit; the smaller enabled/tenant/channel predicates keep the
	// scan scoped to this channel's live entries.
	q := `SELECT e.id, e.tenant_id, e.channel, e.category, e.title, e.content, e.enabled, e.created_at, e.updated_at
FROM kb_fts JOIN kb_entries e ON e.rowid = kb_fts.rowid
WHERE kb_fts MATCH ? AND e.tenant_id = ? AND e.channel = ? AND e.enabled = 1`
	args := []any{match, tenantID, channel}
	if c := strings.ToLower(strings.TrimSpace(category)); c != "" {
		q += ` AND e.category = ?`
		args = append(args, c)
	}
	q += ` ORDER BY bm25(kb_fts, 10.0, 1.0) LIMIT ?`
	args = append(args, limit)
	return s.query(ctx, q, args...)
}

func (s *sqliteStore) query(ctx context.Context, sqlText string, args ...any) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("kb: query: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("kb: scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SanitizeMatchQuery turns untrusted chat text into a safe FTS5 MATCH
// expression: each whitespace-delimited token becomes a quoted string literal
// (with any internal double-quote doubled), and the literals are joined with the
// OR operator. Because every token is a quoted literal, FTS5 operators a user
// might type (NEAR, OR, *, ^, parentheses, unbalanced quotes) are treated as
// plain text and can neither error nor inject; joining with OR keeps recall high
// so bm25 can rank the best entry first. A tokenless input yields "".
func SanitizeMatchQuery(query string) string {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		quoted = append(quoted, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR ")
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
