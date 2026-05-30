package timers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MinInterval is the smallest [Timer.Interval] the store accepts. Sub-5s
// intervals are rejected so a misconfigured timer cannot spam chat faster
// than once every five seconds; the scheduler's own tick resolution is
// coarser still.
const MinInterval = 5 * time.Second

// Sentinel errors. Callers should compare with errors.Is.
var (
	// ErrNotFound is returned by Store methods when a (tenant, channel,
	// name) lookup matches no row.
	ErrNotFound = errors.New("timers: timer not found")

	// ErrAlreadyExists is returned by Create when a row with the same
	// (tenant, channel, name) already exists.
	ErrAlreadyExists = errors.New("timers: timer already exists")

	// ErrInvalid is returned when a timer field fails validation. The
	// wrapped detail says which field was bad.
	ErrInvalid = errors.New("timers: invalid timer")
)

// nameRE constrains timer names to a tight charset so they can be pasted
// into chat without quoting and never collide with whitespace tokenisation.
var nameRE = regexp.MustCompile(`^[a-z0-9_]+$`)

// Timer is a periodic auto-announcement scoped to a (tenant, channel).
type Timer struct {
	ID           string
	TenantID     string
	Channel      string
	Name         string        // lower-cased, unique per (tenant,channel)
	Message      string        // text posted to chat
	Interval     time.Duration // minimum time between posts
	MinChatLines int           // min chat messages since last post before firing (0 = no gate)
	Enabled      bool
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Store is the persistence contract for timers. All methods are safe for
// concurrent use; uniqueness on (tenant, channel, name) is enforced
// atomically.
type Store interface {
	// Create inserts t and returns the stored timer. It returns
	// [ErrAlreadyExists] when a timer with the same (tenant, channel,
	// name) exists, or [ErrInvalid] when a field fails validation.
	Create(ctx context.Context, t Timer) (Timer, error)

	// Update mutates the message, interval, min-chat-lines and enabled
	// flag of an existing timer, bumping UpdatedAt. It returns
	// [ErrNotFound] when no matching timer exists, or [ErrInvalid] when a
	// field fails validation.
	Update(ctx context.Context, tenantID, channel, name string, message string, interval time.Duration, minChatLines int, enabled bool) (Timer, error)

	// Get returns the timer for (tenant, channel, name) or [ErrNotFound].
	Get(ctx context.Context, tenantID, channel, name string) (Timer, error)

	// Delete removes the timer for (tenant, channel, name) or returns
	// [ErrNotFound].
	Delete(ctx context.Context, tenantID, channel, name string) error

	// List returns every timer for (tenant, channel), ordered by name ASC.
	List(ctx context.Context, tenantID, channel string) ([]Timer, error)

	// ListEnabled returns every ENABLED timer across all channels for a
	// tenant, for the scheduler to load. Ordered by channel, then name.
	ListEnabled(ctx context.Context, tenantID string) ([]Timer, error)

	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring customcommands.newID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// normalizeName lower-cases, trims, and strips a leading "!" so callers may
// pass either "!rules" or "rules". Returns the canonical form.
func normalizeName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.TrimPrefix(n, "!")
	return n
}

// validate enforces field-level invariants on a Timer. The returned error
// wraps [ErrInvalid] so callers can errors.Is-match.
func validate(t Timer) error {
	if strings.TrimSpace(t.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if strings.TrimSpace(t.Channel) == "" {
		return fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	if t.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if !nameRE.MatchString(t.Name) {
		return fmt.Errorf("%w: name %q must match [a-z0-9_]+", ErrInvalid, t.Name)
	}
	if strings.TrimSpace(t.Message) == "" {
		return fmt.Errorf("%w: message is required", ErrInvalid)
	}
	if t.Interval < MinInterval {
		return fmt.Errorf("%w: interval %s is below the %s floor",
			ErrInvalid, t.Interval, MinInterval)
	}
	if t.MinChatLines < 0 {
		return fmt.Errorf("%w: min_chat_lines must be >= 0", ErrInvalid)
	}
	return nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/customcommands.sqliteStore
// conventions: WAL, foreign-keys, busy_timeout, SetMaxOpenConns(1) and a
// sync.Mutex for check-then-insert uniqueness.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger

	// mu serialises check-then-insert so duplicate Create calls produce
	// ErrAlreadyExists deterministically rather than racing a SELECT
	// against an INSERT.
	mu sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn and returns a
// ready-to-use Store. dsn may be a file path or a full modernc.org/sqlite
// DSN. Use "file::memory:?cache=shared" for tests.
//
// The returned Store has WAL journal mode, foreign-keys ON, and
// synchronous=NORMAL.
func OpenSQLiteStore(ctx context.Context, dsn string, logger *slog.Logger) (Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("timers: open sqlite: %w", err)
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
			return nil, fmt.Errorf("timers: %s: %w", p, err)
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
		return fmt.Errorf("timers: read migrations dir: %w", err)
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
			return fmt.Errorf("timers: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("timers: apply migration %s: %w", name, err)
		}
		s.log.Debug("timers: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const timerSelect = `SELECT id, tenant_id, channel, name, message, interval_ns,
                            min_chat_lines, enabled, created_by, created_at, updated_at
                     FROM timers `

func scanRow(row interface{ Scan(...any) error }) (Timer, error) {
	var (
		t                Timer
		intervalNS       int64
		enabled          int64
		created, updated int64
	)
	err := row.Scan(&t.ID, &t.TenantID, &t.Channel, &t.Name, &t.Message,
		&intervalNS, &t.MinChatLines, &enabled, &t.CreatedBy, &created, &updated)
	if err != nil {
		return Timer{}, err
	}
	t.Interval = time.Duration(intervalNS)
	t.Enabled = enabled != 0
	t.CreatedAt = time.Unix(0, created).UTC()
	t.UpdatedAt = time.Unix(0, updated).UTC()
	return t, nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (s *sqliteStore) Create(ctx context.Context, t Timer) (Timer, error) {
	t.Name = normalizeName(t.Name)
	t.Message = strings.TrimSpace(t.Message)
	if strings.TrimSpace(t.ID) == "" {
		t.ID = newID()
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	if err := validate(t); err != nil {
		return Timer{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	const dup = `SELECT 1 FROM timers
                 WHERE tenant_id = ? AND channel = ? AND name = ? LIMIT 1`
	var x int
	switch err := s.db.QueryRowContext(ctx, dup, t.TenantID, t.Channel, t.Name).Scan(&x); {
	case err == nil:
		return Timer{}, ErrAlreadyExists
	case errors.Is(err, sql.ErrNoRows):
		// not a duplicate; continue
	default:
		return Timer{}, fmt.Errorf("timers: check duplicate: %w", err)
	}

	const ins = `
INSERT INTO timers (id, tenant_id, channel, name, message, interval_ns,
                    min_chat_lines, enabled, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		t.ID, t.TenantID, t.Channel, t.Name, t.Message, int64(t.Interval),
		t.MinChatLines, boolToInt(t.Enabled), t.CreatedBy,
		t.CreatedAt.UnixNano(), t.UpdatedAt.UnixNano(),
	); err != nil {
		if isUniqueViolation(err) {
			return Timer{}, ErrAlreadyExists
		}
		return Timer{}, fmt.Errorf("timers: insert: %w", err)
	}
	return t, nil
}

func (s *sqliteStore) Update(ctx context.Context, tenantID, channel, name string, message string, interval time.Duration, minChatLines int, enabled bool) (Timer, error) {
	name = normalizeName(name)
	message = strings.TrimSpace(message)
	probe := Timer{
		TenantID: tenantID, Channel: channel, Name: name,
		Message: message, Interval: interval, MinChatLines: minChatLines,
	}
	if err := validate(probe); err != nil {
		return Timer{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().UnixNano()
	const q = `UPDATE timers
               SET message = ?, interval_ns = ?, min_chat_lines = ?, enabled = ?, updated_at = ?
               WHERE tenant_id = ? AND channel = ? AND name = ?`
	res, err := s.db.ExecContext(ctx, q,
		message, int64(interval), minChatLines, boolToInt(enabled), now,
		tenantID, channel, name)
	if err != nil {
		return Timer{}, fmt.Errorf("timers: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Timer{}, ErrNotFound
	}
	return s.getLocked(ctx, tenantID, channel, name)
}

func (s *sqliteStore) Get(ctx context.Context, tenantID, channel, name string) (Timer, error) {
	name = normalizeName(name)
	row := s.db.QueryRowContext(ctx,
		timerSelect+`WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	t, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Timer{}, ErrNotFound
	}
	if err != nil {
		return Timer{}, fmt.Errorf("timers: get: %w", err)
	}
	return t, nil
}

// getLocked is Get without re-normalising and without taking s.mu; it is
// called from Update which already holds the lock and has already
// normalised the name.
func (s *sqliteStore) getLocked(ctx context.Context, tenantID, channel, name string) (Timer, error) {
	row := s.db.QueryRowContext(ctx,
		timerSelect+`WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	t, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Timer{}, ErrNotFound
	}
	if err != nil {
		return Timer{}, fmt.Errorf("timers: get: %w", err)
	}
	return t, nil
}

func (s *sqliteStore) Delete(ctx context.Context, tenantID, channel, name string) error {
	name = normalizeName(name)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM timers WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	if err != nil {
		return fmt.Errorf("timers: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) List(ctx context.Context, tenantID, channel string) ([]Timer, error) {
	rows, err := s.db.QueryContext(ctx,
		timerSelect+`WHERE tenant_id = ? AND channel = ? ORDER BY name ASC`,
		tenantID, channel)
	if err != nil {
		return nil, fmt.Errorf("timers: list: %w", err)
	}
	defer rows.Close()
	var out []Timer
	for rows.Next() {
		t, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("timers: scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListEnabled(ctx context.Context, tenantID string) ([]Timer, error) {
	rows, err := s.db.QueryContext(ctx,
		timerSelect+`WHERE tenant_id = ? AND enabled = 1 ORDER BY channel ASC, name ASC`,
		tenantID)
	if err != nil {
		return nil, fmt.Errorf("timers: list enabled: %w", err)
	}
	defer rows.Close()
	var out []Timer
	for rows.Next() {
		t, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("timers: scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// isUniqueViolation recognises SQLite's UNIQUE constraint error in a
// driver-agnostic way (modernc.org/sqlite reports the message verbatim).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
