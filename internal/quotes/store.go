package quotes

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
	// ErrNotFound is returned when a (tenant, channel, number) lookup
	// matches no row.
	ErrNotFound = errors.New("quotes: quote not found")

	// ErrInvalid is returned when quote text fails validation. The
	// wrapped detail says why.
	ErrInvalid = errors.New("quotes: invalid quote")

	// ErrEmpty is returned by GetRandom when the channel has no quotes.
	ErrEmpty = errors.New("quotes: no quotes in channel")
)

// maxQuoteLen caps stored quote text below Twitch's ~500-char chat limit,
// so a saved quote can never be crafted to exceed what the platform accepts.
const maxQuoteLen = 480

// Quote is a saved chat line scoped to a (tenant, channel). Number is a
// per-channel 1-based sequence shown to users (e.g. "!quote 3"); it is NOT
// a global primary key. ID is the internal ULID primary key.
type Quote struct {
	ID        string
	TenantID  string
	Channel   string
	Number    int // per-channel 1-based, stable once assigned
	Text      string
	CreatedBy string // user id of the mod who added it
	CreatedAt time.Time
}

// Store is the persistence contract for quotes. All methods are safe for
// concurrent use; per-channel Number assignment is serialised so concurrent
// Adds never collide.
type Store interface {
	// Add inserts a new quote and returns it with its assigned per-channel
	// Number. Returns ErrInvalid for empty/over-long text.
	Add(ctx context.Context, tenantID, channel, text, createdBy string) (Quote, error)
	// Get returns the quote with the given per-channel number. ErrNotFound if absent.
	Get(ctx context.Context, tenantID, channel string, number int) (Quote, error)
	// GetRandom returns a uniformly random quote from the channel.
	// ErrEmpty if the channel has no quotes.
	GetRandom(ctx context.Context, tenantID, channel string) (Quote, error)
	// Delete removes the quote with the given number. ErrNotFound if absent.
	// Numbers of OTHER quotes are NOT renumbered (gaps are fine and expected).
	Delete(ctx context.Context, tenantID, channel string, number int) error
	// List returns all quotes for the channel ordered by Number ASC.
	List(ctx context.Context, tenantID, channel string) ([]Quote, error)
	// Count returns the number of quotes in the channel.
	Count(ctx context.Context, tenantID, channel string) (int, error)
	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring customcommands.newID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// validateText trims and enforces non-empty / length bounds on quote text,
// returning the trimmed text or an error wrapping ErrInvalid.
func validateText(text string) (string, error) {
	t := strings.TrimSpace(text)
	if t == "" {
		return "", fmt.Errorf("%w: text is required", ErrInvalid)
	}
	if len(t) > maxQuoteLen {
		return "", fmt.Errorf("%w: text length %d exceeds %d", ErrInvalid, len(t), maxQuoteLen)
	}
	return t, nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/customcommands.sqliteStore
// conventions: WAL, foreign-keys, busy_timeout, SetMaxOpenConns(1) and a
// sync.Mutex serialising the read-max-then-insert that assigns Number.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger

	// mu serialises the MAX(number)+1 read and the subsequent INSERT so
	// concurrent Adds to the same channel cannot be assigned the same
	// Number.
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
		return nil, fmt.Errorf("quotes: open sqlite: %w", err)
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
			return nil, fmt.Errorf("quotes: %s: %w", p, err)
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
		return fmt.Errorf("quotes: read migrations dir: %w", err)
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
			return fmt.Errorf("quotes: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("quotes: apply migration %s: %w", name, err)
		}
		s.log.Debug("quotes: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const quoteSelect = `SELECT id, tenant_id, channel, number, text, created_by, created_at
                     FROM quotes `

func scanRow(row interface{ Scan(...any) error }) (Quote, error) {
	var (
		q       Quote
		created int64
	)
	err := row.Scan(&q.ID, &q.TenantID, &q.Channel, &q.Number, &q.Text,
		&q.CreatedBy, &created)
	if err != nil {
		return Quote{}, err
	}
	q.CreatedAt = time.Unix(0, created).UTC()
	return q, nil
}

func (s *sqliteStore) Add(ctx context.Context, tenantID, channel, text, createdBy string) (Quote, error) {
	if strings.TrimSpace(tenantID) == "" {
		return Quote{}, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if strings.TrimSpace(channel) == "" {
		return Quote{}, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	trimmed, err := validateText(text)
	if err != nil {
		return Quote{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// The visible Number is assigned as MAX(number)+1 for this
	// (tenant, channel), computed under s.mu so two concurrent Adds can
	// never read the same MAX and collide on the unique index. The first
	// quote in a channel gets Number 1.
	var maxNum sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(number) FROM quotes WHERE tenant_id = ? AND channel = ?`,
		tenantID, channel).Scan(&maxNum); err != nil {
		return Quote{}, fmt.Errorf("quotes: max number: %w", err)
	}
	number := int(maxNum.Int64) + 1

	q := Quote{
		ID:        newID(),
		TenantID:  tenantID,
		Channel:   channel,
		Number:    number,
		Text:      trimmed,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
	}

	const ins = `
INSERT INTO quotes (id, tenant_id, channel, number, text, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		q.ID, q.TenantID, q.Channel, q.Number, q.Text, q.CreatedBy,
		q.CreatedAt.UnixNano(),
	); err != nil {
		return Quote{}, fmt.Errorf("quotes: insert: %w", err)
	}
	return q, nil
}

func (s *sqliteStore) Get(ctx context.Context, tenantID, channel string, number int) (Quote, error) {
	row := s.db.QueryRowContext(ctx,
		quoteSelect+`WHERE tenant_id = ? AND channel = ? AND number = ?`,
		tenantID, channel, number)
	q, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Quote{}, ErrNotFound
	}
	if err != nil {
		return Quote{}, fmt.Errorf("quotes: get: %w", err)
	}
	return q, nil
}

func (s *sqliteStore) GetRandom(ctx context.Context, tenantID, channel string) (Quote, error) {
	row := s.db.QueryRowContext(ctx,
		quoteSelect+`WHERE tenant_id = ? AND channel = ? ORDER BY RANDOM() LIMIT 1`,
		tenantID, channel)
	q, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Quote{}, ErrEmpty
	}
	if err != nil {
		return Quote{}, fmt.Errorf("quotes: get random: %w", err)
	}
	return q, nil
}

func (s *sqliteStore) Delete(ctx context.Context, tenantID, channel string, number int) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM quotes WHERE tenant_id = ? AND channel = ? AND number = ?`,
		tenantID, channel, number)
	if err != nil {
		return fmt.Errorf("quotes: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) List(ctx context.Context, tenantID, channel string) ([]Quote, error) {
	rows, err := s.db.QueryContext(ctx,
		quoteSelect+`WHERE tenant_id = ? AND channel = ? ORDER BY number ASC`,
		tenantID, channel)
	if err != nil {
		return nil, fmt.Errorf("quotes: list: %w", err)
	}
	defer rows.Close()
	var out []Quote
	for rows.Next() {
		q, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("quotes: scan: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *sqliteStore) Count(ctx context.Context, tenantID, channel string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM quotes WHERE tenant_id = ? AND channel = ?`,
		tenantID, channel).Scan(&n); err != nil {
		return 0, fmt.Errorf("quotes: count: %w", err)
	}
	return n, nil
}
