package moments

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

// Sentinel errors. Callers should compare with errors.Is.
var (
	// ErrActiveExists is returned by Open when a moment with status='open'
	// already exists for the (tenant, channel). At most one moment may be
	// open per channel at a time.
	ErrActiveExists = errors.New("moments: an active moment already exists")

	// ErrNoActive is returned by Active, Join and End when the (tenant,
	// channel) has no currently-open moment.
	ErrNoActive = errors.New("moments: no active moment")

	// ErrClosed is returned by Join when the active moment's window has
	// already elapsed (now is at or past ClosesAt). The moment row may not
	// yet be marked closed, but it no longer accepts participants.
	ErrClosed = errors.New("moments: moment window has closed")

	// ErrAlreadyJoined is returned by Join when the viewer has already
	// recorded a participant entry for the active moment.
	ErrAlreadyJoined = errors.New("moments: viewer already joined")

	// ErrNotFound is returned when a moment lookup by id matches no row.
	ErrNotFound = errors.New("moments: moment not found")

	// ErrInvalid is returned when an argument fails validation (empty
	// required field, or a non-positive window). The wrapped detail says
	// which field was bad. Callers should compare with errors.Is.
	ErrInvalid = errors.New("moments: invalid argument")
)

// Rarity is the tier a closed moment earns, derived from how many viewers
// joined before it ended.
type Rarity string

// Rarity tiers a closed moment can earn, by participant count.
const (
	// RarityCommon is the tier for moments below RareThreshold participants.
	RarityCommon Rarity = "common"
	// RarityRare is the tier for moments with at least RareThreshold but
	// fewer than LegendaryThreshold participants.
	RarityRare Rarity = "rare"
	// RarityLegendary is the tier for moments with at least
	// LegendaryThreshold participants.
	RarityLegendary Rarity = "legendary"
)

// Rarity thresholds. A closed moment's tier is the highest threshold whose
// participant count it meets or exceeds:
//
//	participants >= LegendaryThreshold (50) -> RarityLegendary
//	participants >= RareThreshold      (15) -> RarityRare
//	otherwise                                -> RarityCommon
const (
	// RareThreshold is the minimum participant count for RarityRare.
	RareThreshold = 15
	// LegendaryThreshold is the minimum participant count for RarityLegendary.
	LegendaryThreshold = 50
)

// RarityFor maps a participant count to its rarity tier using
// RareThreshold and LegendaryThreshold. See the threshold constants for
// the tiering rules.
func RarityFor(participants int) Rarity {
	switch {
	case participants >= LegendaryThreshold:
		return RarityLegendary
	case participants >= RareThreshold:
		return RarityRare
	default:
		return RarityCommon
	}
}

// Moment is one BeReal-style moment alert scoped to a (tenant, channel).
// While Status is "open" the moment accepts participants until ClosesAt;
// once ended its Status is "closed", Rarity is set from the final
// participant count, and ClosedAt records when it ended. Participants holds
// the participant count, filled on read.
type Moment struct {
	ID           string
	TenantID     string
	Channel      string
	Title        string
	Status       string // "open" | "closed"
	Rarity       Rarity // set when closed ("" while open)
	Participants int    // count, filled on read
	OpenedBy     string // mod username
	OpenedAt     time.Time
	ClosesAt     time.Time // OpenedAt + window
	ClosedAt     time.Time // zero until closed
}

// Status constants for the moments.status column.
const (
	statusOpen   = "open"
	statusClosed = "closed"
)

// Store is the persistence contract for moment alerts. All methods are
// safe for concurrent use; the "at most one open moment per (tenant,
// channel)" invariant is enforced atomically.
type Store interface {
	// Open starts a new moment for (tenant, channel) with the given title,
	// opener and window. Returns ErrActiveExists if one is already open for
	// that channel, or ErrInvalid if any field is empty or window <= 0.
	Open(ctx context.Context, tenantID, channel, title, openedBy string, window time.Duration) (Moment, error)
	// Active returns the channel's currently-open moment, or ErrNoActive.
	Active(ctx context.Context, tenantID, channel string) (Moment, error)
	// Join records a participant for the channel's active moment (idempotent
	// per viewer). Returns ErrNoActive if none open, ErrClosed if the window
	// has elapsed (now at or past ClosesAt), or ErrAlreadyJoined if this
	// viewer already joined. On success it returns the updated participant
	// count.
	Join(ctx context.Context, tenantID, channel, viewerID, username string, now time.Time) (int, error)
	// End ends the channel's active moment, computes its rarity from the
	// participant count, and returns the closed moment. Returns ErrNoActive
	// if none open.
	End(ctx context.Context, tenantID, channel string, now time.Time) (Moment, error)
	// History returns the most recent closed moments for the channel,
	// newest first, up to limit (clamped to [1, 50]).
	History(ctx context.Context, tenantID, channel string, limit int) ([]Moment, error)
	// Participants returns the usernames who joined a moment by id, ordered
	// by join time. Returns ErrNotFound if no such moment exists for the
	// (tenant, channel).
	Participants(ctx context.Context, tenantID, channel, momentID string) ([]string, error)
	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring customcommands.newID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// normalizeChannel lower-cases, trims, and strips a leading "#" so callers
// may pass either "#channel" or "channel". Returns the canonical form.
func normalizeChannel(channel string) string {
	c := strings.ToLower(strings.TrimSpace(channel))
	c = strings.TrimPrefix(c, "#")
	return c
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/customcommands.sqliteStore
// conventions: WAL, foreign-keys, busy_timeout, SetMaxOpenConns(1) and a
// sync.Mutex around the check-then-write sequences that Open, Join and End
// perform.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger

	// mu serialises the check-then-write sequences (Open's "is one already
	// open?" check, Join's find-then-insert, End's find-then-update) so the
	// single-open-moment and idempotent-join invariants hold deterministically
	// rather than racing a SELECT against an INSERT/UPDATE.
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
		return nil, fmt.Errorf("moments: open sqlite: %w", err)
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
			return nil, fmt.Errorf("moments: %s: %w", p, err)
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
		return fmt.Errorf("moments: read migrations dir: %w", err)
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
			return fmt.Errorf("moments: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("moments: apply migration %s: %w", name, err)
		}
		s.log.Debug("moments: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const momentSelect = `SELECT id, tenant_id, channel, title, status, rarity,
                             opened_by, opened_at, closes_at, closed_at
                      FROM moments `

// scanMoment scans a single moments row into a Moment. Participants is NOT
// filled here; callers fill it from a separate count.
func scanMoment(row interface{ Scan(...any) error }) (Moment, error) {
	var (
		m                      Moment
		rarity                 string
		opened, closes, closed int64
	)
	err := row.Scan(&m.ID, &m.TenantID, &m.Channel, &m.Title, &m.Status,
		&rarity, &m.OpenedBy, &opened, &closes, &closed)
	if err != nil {
		return Moment{}, err
	}
	m.Rarity = Rarity(rarity)
	m.OpenedAt = time.Unix(0, opened).UTC()
	m.ClosesAt = time.Unix(0, closes).UTC()
	if closed != 0 {
		m.ClosedAt = time.Unix(0, closed).UTC()
	}
	return m, nil
}

// countParticipantsLocked returns the participant count for a moment id.
func (s *sqliteStore) countParticipants(ctx context.Context, momentID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM moment_participants WHERE moment_id = ?`,
		momentID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("moments: count participants: %w", err)
	}
	return n, nil
}

// Open starts a new moment for (tenant, channel), enforcing the
// single-open-moment invariant under s.mu.
func (s *sqliteStore) Open(ctx context.Context, tenantID, channel, title, openedBy string, window time.Duration) (Moment, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return Moment{}, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c := normalizeChannel(channel)
	if c == "" {
		return Moment{}, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	ti := strings.TrimSpace(title)
	if ti == "" {
		return Moment{}, fmt.Errorf("%w: title is required", ErrInvalid)
	}
	ob := strings.TrimSpace(openedBy)
	if ob == "" {
		return Moment{}, fmt.Errorf("%w: opened_by is required", ErrInvalid)
	}
	if window <= 0 {
		return Moment{}, fmt.Errorf("%w: window must be positive", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	const dup = `SELECT 1 FROM moments
                 WHERE tenant_id = ? AND channel = ? AND status = ? LIMIT 1`
	var x int
	switch err := s.db.QueryRowContext(ctx, dup, t, c, statusOpen).Scan(&x); {
	case err == nil:
		return Moment{}, ErrActiveExists
	case errors.Is(err, sql.ErrNoRows):
		// no open moment; continue
	default:
		return Moment{}, fmt.Errorf("moments: check active: %w", err)
	}

	m := Moment{
		ID:       newID(),
		TenantID: t,
		Channel:  c,
		Title:    ti,
		Status:   statusOpen,
		OpenedBy: ob,
		OpenedAt: time.Now().UTC(),
	}
	m.ClosesAt = m.OpenedAt.Add(window)

	const ins = `
INSERT INTO moments (id, tenant_id, channel, title, status, rarity,
                     opened_by, opened_at, closes_at, closed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		m.ID, m.TenantID, m.Channel, m.Title, m.Status, "",
		m.OpenedBy, m.OpenedAt.UnixNano(), m.ClosesAt.UnixNano(), int64(0),
	); err != nil {
		if isUniqueViolation(err) {
			return Moment{}, ErrActiveExists
		}
		return Moment{}, fmt.Errorf("moments: insert: %w", err)
	}
	return m, nil
}

// Active returns the channel's currently-open moment, or ErrNoActive.
func (s *sqliteStore) Active(ctx context.Context, tenantID, channel string) (Moment, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return Moment{}, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c := normalizeChannel(channel)
	if c == "" {
		return Moment{}, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	row := s.db.QueryRowContext(ctx,
		momentSelect+`WHERE tenant_id = ? AND channel = ? AND status = ?`,
		t, c, statusOpen)
	m, err := scanMoment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Moment{}, ErrNoActive
	}
	if err != nil {
		return Moment{}, fmt.Errorf("moments: active: %w", err)
	}
	n, err := s.countParticipants(ctx, m.ID)
	if err != nil {
		return Moment{}, err
	}
	m.Participants = n
	return m, nil
}

// Join records a participant for the channel's active moment.
func (s *sqliteStore) Join(ctx context.Context, tenantID, channel, viewerID, username string, now time.Time) (int, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return 0, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c := normalizeChannel(channel)
	if c == "" {
		return 0, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	v := strings.TrimSpace(viewerID)
	if v == "" {
		return 0, fmt.Errorf("%w: viewer_id is required", ErrInvalid)
	}
	u := strings.TrimSpace(username)
	if u == "" {
		return 0, fmt.Errorf("%w: username is required", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRowContext(ctx,
		momentSelect+`WHERE tenant_id = ? AND channel = ? AND status = ?`,
		t, c, statusOpen)
	m, err := scanMoment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoActive
	}
	if err != nil {
		return 0, fmt.Errorf("moments: join find active: %w", err)
	}
	if !now.UTC().Before(m.ClosesAt) {
		return 0, ErrClosed
	}

	const ins = `
INSERT INTO moment_participants (id, moment_id, viewer_id, username, joined_at)
VALUES (?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		newID(), m.ID, v, u, now.UTC().UnixNano(),
	); err != nil {
		if isUniqueViolation(err) {
			return 0, ErrAlreadyJoined
		}
		return 0, fmt.Errorf("moments: join insert: %w", err)
	}

	n, err := s.countParticipants(ctx, m.ID)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// End ends the channel's active moment, computes its rarity from the
// participant count, and returns the closed moment.
func (s *sqliteStore) End(ctx context.Context, tenantID, channel string, now time.Time) (Moment, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return Moment{}, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c := normalizeChannel(channel)
	if c == "" {
		return Moment{}, fmt.Errorf("%w: channel is required", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRowContext(ctx,
		momentSelect+`WHERE tenant_id = ? AND channel = ? AND status = ?`,
		t, c, statusOpen)
	m, err := scanMoment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Moment{}, ErrNoActive
	}
	if err != nil {
		return Moment{}, fmt.Errorf("moments: end find active: %w", err)
	}

	n, err := s.countParticipants(ctx, m.ID)
	if err != nil {
		return Moment{}, err
	}
	rarity := RarityFor(n)
	closedAt := now.UTC()

	const upd = `UPDATE moments
                 SET status = ?, rarity = ?, closed_at = ?
                 WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, upd,
		statusClosed, string(rarity), closedAt.UnixNano(), m.ID,
	); err != nil {
		return Moment{}, fmt.Errorf("moments: end update: %w", err)
	}

	m.Status = statusClosed
	m.Rarity = rarity
	m.ClosedAt = closedAt
	m.Participants = n
	return m, nil
}

// History returns the most recent closed moments for the channel, newest
// first, up to limit (clamped to [1, 50]).
func (s *sqliteStore) History(ctx context.Context, tenantID, channel string, limit int) ([]Moment, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c := normalizeChannel(channel)
	if c == "" {
		return nil, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx,
		momentSelect+`WHERE tenant_id = ? AND channel = ? AND status = ?
                      ORDER BY closed_at DESC LIMIT ?`,
		t, c, statusClosed, limit)
	if err != nil {
		return nil, fmt.Errorf("moments: history: %w", err)
	}
	defer rows.Close()
	var out []Moment
	for rows.Next() {
		m, err := scanMoment(rows)
		if err != nil {
			return nil, fmt.Errorf("moments: scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		n, err := s.countParticipants(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Participants = n
	}
	return out, nil
}

// Participants returns the usernames who joined a moment by id, ordered by
// join time.
func (s *sqliteStore) Participants(ctx context.Context, tenantID, channel, momentID string) ([]string, error) {
	t := strings.TrimSpace(tenantID)
	if t == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	c := normalizeChannel(channel)
	if c == "" {
		return nil, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	id := strings.TrimSpace(momentID)
	if id == "" {
		return nil, fmt.Errorf("%w: moment_id is required", ErrInvalid)
	}

	// Confirm the moment exists and belongs to this (tenant, channel) so a
	// stray id from another channel cannot leak participant names.
	var x int
	switch err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM moments WHERE id = ? AND tenant_id = ? AND channel = ? LIMIT 1`,
		id, t, c).Scan(&x); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("moments: participants lookup: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT username FROM moment_participants
         WHERE moment_id = ? ORDER BY joined_at ASC, id ASC`,
		id)
	if err != nil {
		return nil, fmt.Errorf("moments: participants: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, fmt.Errorf("moments: scan participant: %w", err)
		}
		out = append(out, u)
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
