// Package queue implements a bot-managed, per-channel FIFO song queue
// backed by SQLite (modernc.org/sqlite, pure Go).
//
// The queue is owned by the bot: viewers request songs in chat, the bot
// enqueues them, and a browser-source player promotes and plays items.
// Each item is scoped to a (tenant, channel) and ordered by a monotonic
// `position` assigned at enqueue time, so FIFO order is stable even when
// CreatedAt timestamps collide.
//
// The store mirrors internal/customcommands: WAL journal mode,
// foreign-keys ON, synchronous=NORMAL, busy_timeout 5000,
// SetMaxOpenConns(1)/SetMaxIdleConns(1), and a sync.Mutex guarding the
// check-then-write sequences (position assignment and the atomic Next
// promotion).
package queue

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
	// ErrNotFound is returned when a (tenant, channel, id) lookup or
	// delete matches no row.
	ErrNotFound = errors.New("songqueue: item not found")

	// ErrEmpty is returned by Next when no queued item exists and by
	// Current when nothing is playing.
	ErrEmpty = errors.New("songqueue: queue is empty")

	// ErrInvalid is returned when an Item fails field validation. The
	// wrapped detail says which field was bad.
	ErrInvalid = errors.New("songqueue: invalid item")
)

// Queue item statuses. A song moves queued -> playing -> played.
const (
	// StatusQueued marks an item waiting to be played (the default).
	StatusQueued = "queued"
	// StatusPlaying marks the single item currently being played.
	StatusPlaying = "playing"
	// StatusPlayed marks an item that has finished playing.
	StatusPlayed = "played"
)

// validStatuses is the closed set of status strings accepted by the store.
var validStatuses = map[string]struct{}{
	StatusQueued:  {},
	StatusPlaying: {},
	StatusPlayed:  {},
}

// Item is one queued song.
type Item struct {
	ID          string    // ULID
	TenantID    string    //
	Channel     string    //
	VideoID     string    // provider video/track id (e.g. youtube 11-char id)
	Title       string    //
	Artist      string    // channel/artist name
	DurationMS  int       //
	RequestedBy string    // viewer username
	Status      string    // "queued" | "playing" | "played"
	Position    int64     // monotonic insertion order for FIFO
	CreatedAt   time.Time //
}

// Store is the persistence contract for the per-channel song queue. All
// methods are safe for concurrent use; FIFO position assignment and the
// Next promotion are serialised so ordering is deterministic.
type Store interface {
	// Enqueue appends an item (Status=queued) and returns it with
	// ID/Position/CreatedAt filled.
	Enqueue(ctx context.Context, it Item) (Item, error)
	// Next atomically moves the oldest queued item to status=playing and
	// returns it; ErrEmpty when none queued.
	Next(ctx context.Context, tenantID, channel string) (Item, error)
	// Current returns the item currently playing (status=playing), or
	// ErrEmpty.
	Current(ctx context.Context, tenantID, channel string) (Item, error)
	// MarkPlayed sets an item's status to played by id.
	MarkPlayed(ctx context.Context, tenantID, channel, id string) error
	// List returns queued items (status=queued) in FIFO order, up to
	// limit (0=all).
	List(ctx context.Context, tenantID, channel string, limit int) ([]Item, error)
	// Remove deletes a queued item by id (ErrNotFound if absent).
	Remove(ctx context.Context, tenantID, channel, id string) error
	// Clear deletes ALL items for the channel.
	Clear(ctx context.Context, tenantID, channel string) error
	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring auth.NewUserID.
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

// validate enforces field-level invariants on an Item. The returned
// error wraps ErrInvalid so callers can errors.Is-match.
func validate(it Item) error {
	if strings.TrimSpace(it.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if strings.TrimSpace(it.Channel) == "" {
		return fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	if strings.TrimSpace(it.VideoID) == "" {
		return fmt.Errorf("%w: video_id is required", ErrInvalid)
	}
	if strings.TrimSpace(it.Title) == "" {
		return fmt.Errorf("%w: title is required", ErrInvalid)
	}
	if it.DurationMS < 0 {
		return fmt.Errorf("%w: duration_ms %d must be >= 0", ErrInvalid, it.DurationMS)
	}
	if _, ok := validStatuses[it.Status]; !ok {
		return fmt.Errorf("%w: status %q is not one of queued|playing|played",
			ErrInvalid, it.Status)
	}
	return nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/customcommands conventions: WAL,
// foreign-keys, busy_timeout, SetMaxOpenConns(1) and a sync.Mutex for
// the FIFO position assignment and the atomic Next promotion.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger

	// mu serialises the read-then-write sequences (next-position lookup
	// on Enqueue, and oldest-queued select + promote on Next) so FIFO
	// ordering is deterministic rather than racing concurrent callers.
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
		return nil, fmt.Errorf("songqueue: open sqlite: %w", err)
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
			return nil, fmt.Errorf("songqueue: %s: %w", p, err)
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
		return fmt.Errorf("songqueue: read migrations dir: %w", err)
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
			return fmt.Errorf("songqueue: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("songqueue: apply migration %s: %w", name, err)
		}
		s.log.Debug("songqueue: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const sqSelect = `SELECT id, tenant_id, channel, video_id, title, artist,
                         duration_ms, requested_by, status, position, created_at
                  FROM song_queue `

func scanRow(row interface{ Scan(...any) error }) (Item, error) {
	var (
		it      Item
		created int64
	)
	err := row.Scan(&it.ID, &it.TenantID, &it.Channel, &it.VideoID, &it.Title,
		&it.Artist, &it.DurationMS, &it.RequestedBy, &it.Status, &it.Position, &created)
	if err != nil {
		return Item{}, err
	}
	it.CreatedAt = time.Unix(0, created).UTC()
	return it, nil
}

// Enqueue appends an item (Status=queued) and returns it with
// ID/Position/CreatedAt filled. Position is the next monotonic value for
// the (tenant, channel), assigned under the store mutex so concurrent
// enqueues never collide.
func (s *sqliteStore) Enqueue(ctx context.Context, it Item) (Item, error) {
	it.Channel = normalizeChannel(it.Channel)
	it.VideoID = strings.TrimSpace(it.VideoID)
	it.Title = strings.TrimSpace(it.Title)
	it.Artist = strings.TrimSpace(it.Artist)
	it.RequestedBy = strings.TrimSpace(it.RequestedBy)
	it.Status = strings.ToLower(strings.TrimSpace(it.Status))
	if it.Status == "" {
		it.Status = StatusQueued
	}
	if strings.TrimSpace(it.ID) == "" {
		it.ID = newID()
	}
	if it.CreatedAt.IsZero() {
		it.CreatedAt = time.Now().UTC()
	}
	if err := validate(it); err != nil {
		return Item{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	const nextPos = `SELECT COALESCE(MAX(position), 0) + 1
                     FROM song_queue WHERE tenant_id = ? AND channel = ?`
	if err := s.db.QueryRowContext(ctx, nextPos, it.TenantID, it.Channel).
		Scan(&it.Position); err != nil {
		return Item{}, fmt.Errorf("songqueue: next position: %w", err)
	}

	const ins = `
INSERT INTO song_queue (id, tenant_id, channel, video_id, title, artist,
                        duration_ms, requested_by, status, position, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		it.ID, it.TenantID, it.Channel, it.VideoID, it.Title, it.Artist,
		it.DurationMS, it.RequestedBy, it.Status, it.Position, it.CreatedAt.UnixNano(),
	); err != nil {
		return Item{}, fmt.Errorf("songqueue: insert: %w", err)
	}
	return it, nil
}

// Next atomically moves the oldest queued item to status=playing and
// returns it. It only promotes the oldest queued item; it does not
// demote any previously-playing item (the caller marks the old current
// played first). Returns ErrEmpty when nothing is queued.
func (s *sqliteStore) Next(ctx context.Context, tenantID, channel string) (Item, error) {
	channel = normalizeChannel(channel)

	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRowContext(ctx,
		sqSelect+`WHERE tenant_id = ? AND channel = ? AND status = ?
                  ORDER BY position ASC LIMIT 1`,
		tenantID, channel, StatusQueued)
	it, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrEmpty
	}
	if err != nil {
		return Item{}, fmt.Errorf("songqueue: next select: %w", err)
	}

	const promote = `UPDATE song_queue SET status = ?
                     WHERE tenant_id = ? AND channel = ? AND id = ?`
	if _, err := s.db.ExecContext(ctx, promote,
		StatusPlaying, tenantID, channel, it.ID); err != nil {
		return Item{}, fmt.Errorf("songqueue: next promote: %w", err)
	}
	it.Status = StatusPlaying
	return it, nil
}

// Current returns the item currently playing (status=playing) for the
// channel, or ErrEmpty when nothing is playing.
func (s *sqliteStore) Current(ctx context.Context, tenantID, channel string) (Item, error) {
	channel = normalizeChannel(channel)
	row := s.db.QueryRowContext(ctx,
		sqSelect+`WHERE tenant_id = ? AND channel = ? AND status = ?
                  ORDER BY position ASC LIMIT 1`,
		tenantID, channel, StatusPlaying)
	it, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrEmpty
	}
	if err != nil {
		return Item{}, fmt.Errorf("songqueue: current: %w", err)
	}
	return it, nil
}

// MarkPlayed sets an item's status to played by id. Returns ErrNotFound
// when no matching row exists for the (tenant, channel, id).
func (s *sqliteStore) MarkPlayed(ctx context.Context, tenantID, channel, id string) error {
	channel = normalizeChannel(channel)
	res, err := s.db.ExecContext(ctx,
		`UPDATE song_queue SET status = ?
         WHERE tenant_id = ? AND channel = ? AND id = ?`,
		StatusPlayed, tenantID, channel, id)
	if err != nil {
		return fmt.Errorf("songqueue: mark played: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns queued items (status=queued) in FIFO order (ascending
// position), up to limit items (0 means all).
func (s *sqliteStore) List(ctx context.Context, tenantID, channel string, limit int) ([]Item, error) {
	channel = normalizeChannel(channel)
	q := sqSelect + `WHERE tenant_id = ? AND channel = ? AND status = ?
                     ORDER BY position ASC`
	args := []any{tenantID, channel, StatusQueued}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("songqueue: list: %w", err)
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("songqueue: scan: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Remove deletes a queued item by id. It only removes items still in
// status=queued; returns ErrNotFound when no such queued row exists.
func (s *sqliteStore) Remove(ctx context.Context, tenantID, channel, id string) error {
	channel = normalizeChannel(channel)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM song_queue
         WHERE tenant_id = ? AND channel = ? AND id = ? AND status = ?`,
		tenantID, channel, id, StatusQueued)
	if err != nil {
		return fmt.Errorf("songqueue: remove: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Clear deletes ALL items for the channel regardless of status.
func (s *sqliteStore) Clear(ctx context.Context, tenantID, channel string) error {
	channel = normalizeChannel(channel)
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM song_queue WHERE tenant_id = ? AND channel = ?`,
		tenantID, channel)
	if err != nil {
		return fmt.Errorf("songqueue: clear: %w", err)
	}
	return nil
}
