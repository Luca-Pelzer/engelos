package customcommands

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

// Sentinel errors. Callers should compare with errors.Is.
var (
	// ErrNotFound is returned by Store methods when a (tenant, channel,
	// name) lookup matches no row.
	ErrNotFound = errors.New("customcommands: command not found")

	// ErrAlreadyExists is returned by Create when a row with the same
	// (tenant, channel, name) already exists.
	ErrAlreadyExists = errors.New("customcommands: command already exists")

	// ErrInvalid is returned when Name, Response or MinRole fail
	// validation. The wrapped detail says which field was bad.
	ErrInvalid = errors.New("customcommands: invalid command")
)

// validRoles is the closed set of MinRole strings accepted by the store.
// The order matches internal/commands.Role but is duplicated here to
// avoid importing the engine package (which would create a cycle).
var validRoles = map[string]struct{}{
	"everyone":    {},
	"subscriber":  {},
	"vip":         {},
	"moderator":   {},
	"broadcaster": {},
}

// nameRE constrains command names to a tight charset so triggers can be
// pasted into chat without quoting and never collide with whitespace
// tokenisation by the commands engine.
var nameRE = regexp.MustCompile(`^[a-z0-9_]+$`)

// maxResponseLen caps stored response text below Twitch's ~500-char chat limit,
// so a stored command can never be crafted to exceed what the platform accepts.
const maxResponseLen = 480

// CustomCommand is a streamer-defined text command scoped to a
// (tenant, channel). Name is the trigger WITHOUT the prefix, lower-cased.
// Response is the raw text emitted (may contain $user/$channel/$args
// placeholders that the commands engine expands at send time - this
// package stores them raw).
type CustomCommand struct {
	ID        string
	TenantID  string
	Channel   string
	Name      string
	Response  string
	MinRole   string
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store is the persistence contract for custom commands. All methods
// are safe for concurrent use; uniqueness on (tenant, channel, name) is
// enforced atomically.
type Store interface {
	Create(ctx context.Context, c CustomCommand) (CustomCommand, error)
	Update(ctx context.Context, tenantID, channel, name, response, minRole string) (CustomCommand, error)
	Get(ctx context.Context, tenantID, channel, name string) (CustomCommand, error)
	Delete(ctx context.Context, tenantID, channel, name string) error
	List(ctx context.Context, tenantID, channel string) ([]CustomCommand, error)
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring auth.NewUserID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// normalizeName lower-cases, trims, and strips a leading "!" so callers
// may pass either "!hello" or "hello". Returns the canonical form.
func normalizeName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.TrimPrefix(n, "!")
	return n
}

// validate enforces field-level invariants on a CustomCommand. The
// returned error wraps ErrInvalid so callers can errors.Is-match.
func validate(c CustomCommand) error {
	if strings.TrimSpace(c.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if strings.TrimSpace(c.Channel) == "" {
		return fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	if c.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if !nameRE.MatchString(c.Name) {
		return fmt.Errorf("%w: name %q must match [a-z0-9_]+", ErrInvalid, c.Name)
	}
	if strings.TrimSpace(c.Response) == "" {
		return fmt.Errorf("%w: response is required", ErrInvalid)
	}
	if len(c.Response) > maxResponseLen {
		return fmt.Errorf("%w: response length %d exceeds %d", ErrInvalid, len(c.Response), maxResponseLen)
	}
	if _, ok := validRoles[c.MinRole]; !ok {
		return fmt.Errorf("%w: min_role %q is not one of everyone|subscriber|vip|moderator|broadcaster",
			ErrInvalid, c.MinRole)
	}
	return nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/auth.sqliteStore conventions:
// WAL, foreign-keys, busy_timeout, SetMaxOpenConns(1) and a sync.Mutex
// for check-then-insert uniqueness.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger

	// mu serialises check-then-insert so duplicate Create calls produce
	// ErrAlreadyExists deterministically rather than racing a SELECT
	// against an INSERT.
	mu sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn and
// returns a ready-to-use Store. dsn may be a file path or a full
// modernc.org/sqlite DSN. Use "file::memory:?cache=shared" for tests.
//
// The returned Store has WAL journal mode, foreign-keys ON, and
// synchronous=NORMAL.
func OpenSQLiteStore(ctx context.Context, dsn string, logger *slog.Logger) (Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("customcommands: open sqlite: %w", err)
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
			return nil, fmt.Errorf("customcommands: %s: %w", p, err)
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
		return fmt.Errorf("customcommands: read migrations dir: %w", err)
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
			return fmt.Errorf("customcommands: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("customcommands: apply migration %s: %w", name, err)
		}
		s.log.Debug("customcommands: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const ccSelect = `SELECT id, tenant_id, channel, name, response, min_role,
                         created_by, created_at, updated_at
                  FROM custom_commands `

func scanRow(row interface{ Scan(...any) error }) (CustomCommand, error) {
	var (
		c                CustomCommand
		created, updated int64
	)
	err := row.Scan(&c.ID, &c.TenantID, &c.Channel, &c.Name, &c.Response,
		&c.MinRole, &c.CreatedBy, &created, &updated)
	if err != nil {
		return CustomCommand{}, err
	}
	c.CreatedAt = time.Unix(0, created).UTC()
	c.UpdatedAt = time.Unix(0, updated).UTC()
	return c, nil
}

func (s *sqliteStore) Create(ctx context.Context, c CustomCommand) (CustomCommand, error) {
	c.Name = normalizeName(c.Name)
	c.Response = strings.TrimSpace(c.Response)
	c.MinRole = strings.ToLower(strings.TrimSpace(c.MinRole))
	if c.MinRole == "" {
		c.MinRole = "everyone"
	}
	if strings.TrimSpace(c.ID) == "" {
		c.ID = newID()
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = now
	}
	if err := validate(c); err != nil {
		return CustomCommand{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	const dup = `SELECT 1 FROM custom_commands
                 WHERE tenant_id = ? AND channel = ? AND name = ? LIMIT 1`
	var x int
	switch err := s.db.QueryRowContext(ctx, dup, c.TenantID, c.Channel, c.Name).Scan(&x); {
	case err == nil:
		return CustomCommand{}, ErrAlreadyExists
	case errors.Is(err, sql.ErrNoRows):
		// not a duplicate; continue
	default:
		return CustomCommand{}, fmt.Errorf("customcommands: check duplicate: %w", err)
	}

	const ins = `
INSERT INTO custom_commands (id, tenant_id, channel, name, response, min_role,
                             created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		c.ID, c.TenantID, c.Channel, c.Name, c.Response, c.MinRole,
		c.CreatedBy, c.CreatedAt.UnixNano(), c.UpdatedAt.UnixNano(),
	); err != nil {
		if isUniqueViolation(err) {
			return CustomCommand{}, ErrAlreadyExists
		}
		return CustomCommand{}, fmt.Errorf("customcommands: insert: %w", err)
	}
	return c, nil
}

func (s *sqliteStore) Update(ctx context.Context, tenantID, channel, name, response, minRole string) (CustomCommand, error) {
	name = normalizeName(name)
	response = strings.TrimSpace(response)
	minRole = strings.ToLower(strings.TrimSpace(minRole))
	if minRole == "" {
		minRole = "everyone"
	}
	probe := CustomCommand{
		TenantID: tenantID, Channel: channel,
		Name: name, Response: response, MinRole: minRole,
	}
	if err := validate(probe); err != nil {
		return CustomCommand{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().UnixNano()
	const q = `UPDATE custom_commands
               SET response = ?, min_role = ?, updated_at = ?
               WHERE tenant_id = ? AND channel = ? AND name = ?`
	res, err := s.db.ExecContext(ctx, q, response, minRole, now, tenantID, channel, name)
	if err != nil {
		return CustomCommand{}, fmt.Errorf("customcommands: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return CustomCommand{}, ErrNotFound
	}
	return s.getLocked(ctx, tenantID, channel, name)
}

func (s *sqliteStore) Get(ctx context.Context, tenantID, channel, name string) (CustomCommand, error) {
	name = normalizeName(name)
	row := s.db.QueryRowContext(ctx,
		ccSelect+`WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	c, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomCommand{}, ErrNotFound
	}
	if err != nil {
		return CustomCommand{}, fmt.Errorf("customcommands: get: %w", err)
	}
	return c, nil
}

// getLocked is Get without re-normalising and without taking s.mu; it
// is called from Update which already holds the lock and has already
// normalised the name.
func (s *sqliteStore) getLocked(ctx context.Context, tenantID, channel, name string) (CustomCommand, error) {
	row := s.db.QueryRowContext(ctx,
		ccSelect+`WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	c, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomCommand{}, ErrNotFound
	}
	if err != nil {
		return CustomCommand{}, fmt.Errorf("customcommands: get: %w", err)
	}
	return c, nil
}

func (s *sqliteStore) Delete(ctx context.Context, tenantID, channel, name string) error {
	name = normalizeName(name)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM custom_commands WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	if err != nil {
		return fmt.Errorf("customcommands: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) List(ctx context.Context, tenantID, channel string) ([]CustomCommand, error) {
	rows, err := s.db.QueryContext(ctx,
		ccSelect+`WHERE tenant_id = ? AND channel = ? ORDER BY name ASC`,
		tenantID, channel)
	if err != nil {
		return nil, fmt.Errorf("customcommands: list: %w", err)
	}
	defer rows.Close()
	var out []CustomCommand
	for rows.Next() {
		c, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("customcommands: scan: %w", err)
		}
		out = append(out, c)
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
