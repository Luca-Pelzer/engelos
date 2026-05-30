package rewards

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
	ErrNotFound = errors.New("rewards: reward not found")

	// ErrAlreadyExists is returned by Create when a row with the same
	// (tenant, channel, name) already exists.
	ErrAlreadyExists = errors.New("rewards: reward already exists")

	// ErrInvalid is returned when Name, Cost or Description fail
	// validation. The wrapped detail says which field was bad.
	ErrInvalid = errors.New("rewards: invalid reward")
)

// nameRE constrains reward names to a tight charset so they can be
// pasted into chat as the !redeem argument without quoting and never
// collide with whitespace tokenisation by the commands engine.
var nameRE = regexp.MustCompile(`^[a-z0-9_]+$`)

// maxNameLen caps reward names so a single chat token stays short.
const maxNameLen = 32

// maxDescriptionLen caps stored description text so the !rewards listing
// stays well within Twitch's ~500-char chat limit per reward.
const maxDescriptionLen = 200

// Reward is a streamer-defined item viewers can buy with loyalty points,
// scoped to a (tenant, channel). Name is the !redeem argument (lower-cased,
// unique per channel). Cost is in points (>0). Description is shown in
// !rewards.
type Reward struct {
	ID          string
	TenantID    string
	Channel     string
	Name        string
	Description string
	Cost        int64
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Store is the persistence contract for rewards. All methods are safe
// for concurrent use; uniqueness on (tenant, channel, name) is enforced
// atomically.
type Store interface {
	// Create adds a reward. ErrAlreadyExists if (tenant,channel,name) exists.
	Create(ctx context.Context, r Reward) (Reward, error)
	// Update changes cost+description of an existing reward (by name). ErrNotFound if absent.
	Update(ctx context.Context, tenantID, channel, name string, cost int64, description string) (Reward, error)
	// Get returns a reward by (tenant,channel,name). ErrNotFound if absent.
	Get(ctx context.Context, tenantID, channel, name string) (Reward, error)
	// Delete removes a reward. ErrNotFound if absent.
	Delete(ctx context.Context, tenantID, channel, name string) error
	// List returns all rewards for (tenant,channel) ordered by Cost ASC, then Name ASC.
	List(ctx context.Context, tenantID, channel string) ([]Reward, error)
	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring auth.NewUserID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// normalizeName lower-cases and trims a reward name so callers may pass
// "Coffee" or "  coffee  ". Returns the canonical form.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// normalizeChannel lower-cases, trims, and strips a leading "#" so
// callers may pass either "#chan" or "chan". Returns the canonical form.
func normalizeChannel(channel string) string {
	c := strings.ToLower(strings.TrimSpace(channel))
	return strings.TrimPrefix(c, "#")
}

// validate enforces field-level invariants on a Reward. The returned
// error wraps ErrInvalid so callers can errors.Is-match.
func validate(r Reward) error {
	if strings.TrimSpace(r.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if strings.TrimSpace(r.Channel) == "" {
		return fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	if r.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(r.Name) > maxNameLen {
		return fmt.Errorf("%w: name length %d exceeds %d", ErrInvalid, len(r.Name), maxNameLen)
	}
	if !nameRE.MatchString(r.Name) {
		return fmt.Errorf("%w: name %q must match [a-z0-9_]+", ErrInvalid, r.Name)
	}
	if r.Cost <= 0 {
		return fmt.Errorf("%w: cost %d must be greater than 0", ErrInvalid, r.Cost)
	}
	if len(r.Description) > maxDescriptionLen {
		return fmt.Errorf("%w: description length %d exceeds %d", ErrInvalid, len(r.Description), maxDescriptionLen)
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
		return nil, fmt.Errorf("rewards: open sqlite: %w", err)
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
			return nil, fmt.Errorf("rewards: %s: %w", p, err)
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
		return fmt.Errorf("rewards: read migrations dir: %w", err)
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
			return fmt.Errorf("rewards: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("rewards: apply migration %s: %w", name, err)
		}
		s.log.Debug("rewards: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const rewardSelect = `SELECT id, tenant_id, channel, name, description, cost,
                             created_by, created_at, updated_at
                      FROM rewards `

func scanRow(row interface{ Scan(...any) error }) (Reward, error) {
	var (
		r                Reward
		created, updated int64
	)
	err := row.Scan(&r.ID, &r.TenantID, &r.Channel, &r.Name, &r.Description,
		&r.Cost, &r.CreatedBy, &created, &updated)
	if err != nil {
		return Reward{}, err
	}
	r.CreatedAt = time.Unix(created, 0).UTC()
	r.UpdatedAt = time.Unix(updated, 0).UTC()
	return r, nil
}

func (s *sqliteStore) Create(ctx context.Context, r Reward) (Reward, error) {
	r.Name = normalizeName(r.Name)
	r.Channel = normalizeChannel(r.Channel)
	r.Description = strings.TrimSpace(r.Description)
	r.CreatedBy = strings.TrimSpace(r.CreatedBy)
	if strings.TrimSpace(r.ID) == "" {
		r.ID = newID()
	}
	now := time.Now().UTC().Truncate(time.Second)
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}
	if err := validate(r); err != nil {
		return Reward{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	const dup = `SELECT 1 FROM rewards
                 WHERE tenant_id = ? AND channel = ? AND name = ? LIMIT 1`
	var x int
	switch err := s.db.QueryRowContext(ctx, dup, r.TenantID, r.Channel, r.Name).Scan(&x); {
	case err == nil:
		return Reward{}, ErrAlreadyExists
	case errors.Is(err, sql.ErrNoRows):
		// not a duplicate; continue
	default:
		return Reward{}, fmt.Errorf("rewards: check duplicate: %w", err)
	}

	const ins = `
INSERT INTO rewards (id, tenant_id, channel, name, description, cost,
                     created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		r.ID, r.TenantID, r.Channel, r.Name, r.Description, r.Cost,
		r.CreatedBy, r.CreatedAt.Unix(), r.UpdatedAt.Unix(),
	); err != nil {
		if isUniqueViolation(err) {
			return Reward{}, ErrAlreadyExists
		}
		return Reward{}, fmt.Errorf("rewards: insert: %w", err)
	}
	return r, nil
}

func (s *sqliteStore) Update(ctx context.Context, tenantID, channel, name string, cost int64, description string) (Reward, error) {
	channel = normalizeChannel(channel)
	name = normalizeName(name)
	description = strings.TrimSpace(description)
	probe := Reward{
		TenantID: tenantID, Channel: channel,
		Name: name, Cost: cost, Description: description,
	}
	if err := validate(probe); err != nil {
		return Reward{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Truncate(time.Second).Unix()
	const q = `UPDATE rewards
               SET cost = ?, description = ?, updated_at = ?
               WHERE tenant_id = ? AND channel = ? AND name = ?`
	res, err := s.db.ExecContext(ctx, q, cost, description, now, tenantID, channel, name)
	if err != nil {
		return Reward{}, fmt.Errorf("rewards: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Reward{}, ErrNotFound
	}
	return s.getLocked(ctx, tenantID, channel, name)
}

func (s *sqliteStore) Get(ctx context.Context, tenantID, channel, name string) (Reward, error) {
	channel = normalizeChannel(channel)
	name = normalizeName(name)
	row := s.db.QueryRowContext(ctx,
		rewardSelect+`WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	r, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Reward{}, ErrNotFound
	}
	if err != nil {
		return Reward{}, fmt.Errorf("rewards: get: %w", err)
	}
	return r, nil
}

// getLocked is Get without re-normalising and without taking s.mu; it is
// called from Update which already holds the lock and has already
// normalised the channel and name.
func (s *sqliteStore) getLocked(ctx context.Context, tenantID, channel, name string) (Reward, error) {
	row := s.db.QueryRowContext(ctx,
		rewardSelect+`WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	r, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Reward{}, ErrNotFound
	}
	if err != nil {
		return Reward{}, fmt.Errorf("rewards: get: %w", err)
	}
	return r, nil
}

func (s *sqliteStore) Delete(ctx context.Context, tenantID, channel, name string) error {
	channel = normalizeChannel(channel)
	name = normalizeName(name)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM rewards WHERE tenant_id = ? AND channel = ? AND name = ?`,
		tenantID, channel, name)
	if err != nil {
		return fmt.Errorf("rewards: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) List(ctx context.Context, tenantID, channel string) ([]Reward, error) {
	channel = normalizeChannel(channel)
	rows, err := s.db.QueryContext(ctx,
		rewardSelect+`WHERE tenant_id = ? AND channel = ? ORDER BY cost ASC, name ASC`,
		tenantID, channel)
	if err != nil {
		return nil, fmt.Errorf("rewards: list: %w", err)
	}
	defer rows.Close()
	var out []Reward
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("rewards: scan: %w", err)
		}
		out = append(out, r)
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
