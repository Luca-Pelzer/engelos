package loyalty

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
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Sentinel errors. Callers should compare with errors.Is.
var (
	// ErrNotFound is returned when a (tenant, channel, viewer) lookup
	// matches no account row.
	ErrNotFound = errors.New("loyalty: account not found")

	// ErrInvalid is returned when an argument fails validation (a
	// non-positive amount, a missing identity field, or a self-transfer).
	// The wrapped detail says why.
	ErrInvalid = errors.New("loyalty: invalid request")

	// ErrInsufficient is returned by Spend and Transfer when the source
	// account's balance is less than the requested amount. The balance is
	// left unchanged.
	ErrInsufficient = errors.New("loyalty: insufficient balance")
)

// Account is a viewer's loyalty standing in a channel: a spendable points
// balance keyed by (TenantID, Channel, ViewerID). ViewerID is the stable
// platform user id; Username is the last-seen display name. Balance is
// never negative.
type Account struct {
	ID        string
	TenantID  string
	Channel   string
	ViewerID  string
	Username  string
	Balance   int64
	UpdatedAt time.Time
}

// Store is the persistence contract for the loyalty economy. All methods
// are safe for concurrent use. Earn's increment is atomic so concurrent
// earns never lose an update; Spend and Transfer run in single-writer
// transactions that refuse to overdraw an account.
type Store interface {
	// Balance returns the viewer's account. ErrNotFound if they have none
	// yet.
	Balance(ctx context.Context, tenantID, channel, viewerID string) (Account, error)
	// Earn adds amount (must be > 0) to the viewer, creating the account at
	// 0 first if absent (upsert), refreshes Username, and returns the NEW
	// account. amount <= 0 returns ErrInvalid.
	Earn(ctx context.Context, tenantID, channel, viewerID, username string, amount int64) (Account, error)
	// Spend deducts amount (must be > 0) from the viewer and returns the
	// NEW account. Returns ErrInsufficient (and leaves the balance intact)
	// when balance < amount, ErrNotFound when the account does not exist,
	// and ErrInvalid when amount <= 0.
	Spend(ctx context.Context, tenantID, channel, viewerID string, amount int64) (Account, error)
	// Transfer moves amount (must be > 0) from one viewer to another
	// atomically — the "!give" command. The sender must already exist and
	// hold enough funds (else ErrInsufficient); the recipient account is
	// created if needed. A self-transfer (from == to) returns ErrInvalid,
	// as does amount <= 0.
	Transfer(ctx context.Context, tenantID, channel, fromViewerID, toViewerID, toUsername string, amount int64) (from Account, to Account, err error)
	// Leaderboard returns the top accounts for (tenant, channel) ordered by
	// Balance DESC, ties broken by Username ASC. limit is clamped to
	// [1, 100].
	Leaderboard(ctx context.Context, tenantID, channel string, limit int) ([]Account, error)
	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring counters.newID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// normalizeChannel lower-cases, trims, and strips a leading "#" so callers
// may pass either "#cohh" or "cohh" — mirrors how the twitch adapter and
// commands engine canonicalise channel logins.
func normalizeChannel(channel string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(channel)), "#")
}

// validateIdentity normalises channel and checks that tenant, channel and
// viewer id are all present, returning the canonical channel or an error
// wrapping ErrInvalid. viewerID is used verbatim (a stable platform id).
func validateIdentity(tenantID, channel, viewerID string) (string, error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	ch := normalizeChannel(channel)
	if ch == "" {
		return "", fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	if strings.TrimSpace(viewerID) == "" {
		return "", fmt.Errorf("%w: viewer_id is required", ErrInvalid)
	}
	return ch, nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/counters.sqliteStore conventions:
// WAL, foreign-keys ON, busy_timeout, SetMaxOpenConns(1). Because there is
// a single writer connection, the read-then-write transactions in Spend
// and Transfer are serialised by SQLite and need no extra process mutex.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn and returns a
// ready-to-use Store. dsn may be a file path or a full modernc.org/sqlite
// DSN. Use "file:loyalty?mode=memory&cache=shared" for tests.
//
// The returned Store has WAL journal mode, foreign-keys ON,
// synchronous=NORMAL, and a single pooled connection so writes serialise.
func OpenSQLiteStore(ctx context.Context, dsn string, logger *slog.Logger) (Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("loyalty: open sqlite: %w", err)
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
			return nil, fmt.Errorf("loyalty: %s: %w", p, err)
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
		return fmt.Errorf("loyalty: read migrations dir: %w", err)
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
			return fmt.Errorf("loyalty: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("loyalty: apply migration %s: %w", name, err)
		}
		s.log.Debug("loyalty: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const accountSelect = `SELECT id, tenant_id, channel, viewer_id, username, balance, updated_at
                       FROM loyalty_accounts `

// scanRow materialises one Account, converting the Unix-seconds updated_at
// column back to a UTC time.Time.
func scanRow(row interface{ Scan(...any) error }) (Account, error) {
	var (
		a       Account
		updated int64
	)
	err := row.Scan(&a.ID, &a.TenantID, &a.Channel, &a.ViewerID, &a.Username, &a.Balance, &updated)
	if err != nil {
		return Account{}, err
	}
	a.UpdatedAt = time.Unix(updated, 0).UTC()
	return a, nil
}

// Balance returns the viewer's account. ErrNotFound if they have none yet.
func (s *sqliteStore) Balance(ctx context.Context, tenantID, channel, viewerID string) (Account, error) {
	ch, err := validateIdentity(tenantID, channel, viewerID)
	if err != nil {
		return Account{}, err
	}
	row := s.db.QueryRowContext(ctx,
		accountSelect+`WHERE tenant_id = ? AND channel = ? AND viewer_id = ?`,
		tenantID, ch, viewerID)
	a, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("loyalty: balance: %w", err)
	}
	return a, nil
}

// Earn adds amount to the viewer via an atomic upsert and returns the new
// account.
func (s *sqliteStore) Earn(ctx context.Context, tenantID, channel, viewerID, username string, amount int64) (Account, error) {
	ch, err := validateIdentity(tenantID, channel, viewerID)
	if err != nil {
		return Account{}, err
	}
	if amount <= 0 {
		return Account{}, fmt.Errorf("%w: earn amount %d must be positive", ErrInvalid, amount)
	}
	uname := strings.TrimSpace(username)

	now := time.Now().UTC().Unix()
	// The credit is a single atomic upsert: a brand-new account is INSERTed
	// at amount (from a base of 0), while an existing one has
	// "balance = balance + amount" applied in the same statement. Because the
	// read and write happen inside one SQL statement, two concurrent Earns
	// cannot read the same old balance and lose an increment.
	const up = `
INSERT INTO loyalty_accounts (id, tenant_id, channel, viewer_id, username, balance, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel, viewer_id)
DO UPDATE SET balance = balance + excluded.balance,
              username = excluded.username,
              updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, up,
		newID(), tenantID, ch, viewerID, uname, amount, now); err != nil {
		return Account{}, fmt.Errorf("loyalty: earn: %w", err)
	}
	return s.balanceVerbatim(ctx, tenantID, ch, viewerID)
}

// Spend deducts amount from the viewer inside a transaction, refusing to
// overdraw.
func (s *sqliteStore) Spend(ctx context.Context, tenantID, channel, viewerID string, amount int64) (Account, error) {
	ch, err := validateIdentity(tenantID, channel, viewerID)
	if err != nil {
		return Account{}, err
	}
	if amount <= 0 {
		return Account{}, fmt.Errorf("%w: spend amount %d must be positive", ErrInvalid, amount)
	}

	// Read-check-write under one transaction. With SetMaxOpenConns(1) the
	// single writer connection serialises this against every other writer,
	// so the balance read here cannot be invalidated by a concurrent Spend
	// before the UPDATE lands — no overdraw is possible.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, fmt.Errorf("loyalty: spend begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	cur, err := scanRow(tx.QueryRowContext(ctx,
		accountSelect+`WHERE tenant_id = ? AND channel = ? AND viewer_id = ?`,
		tenantID, ch, viewerID))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("loyalty: spend read: %w", err)
	}
	if cur.Balance < amount {
		return Account{}, ErrInsufficient
	}

	now := time.Now().UTC().Unix()
	if _, err := tx.ExecContext(ctx,
		`UPDATE loyalty_accounts SET balance = balance - ?, updated_at = ?
         WHERE tenant_id = ? AND channel = ? AND viewer_id = ?`,
		amount, now, tenantID, ch, viewerID); err != nil {
		return Account{}, fmt.Errorf("loyalty: spend update: %w", err)
	}

	out, err := scanRow(tx.QueryRowContext(ctx,
		accountSelect+`WHERE tenant_id = ? AND channel = ? AND viewer_id = ?`,
		tenantID, ch, viewerID))
	if err != nil {
		return Account{}, fmt.Errorf("loyalty: spend reread: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Account{}, fmt.Errorf("loyalty: spend commit: %w", err)
	}
	return out, nil
}

// Transfer moves amount from one viewer to another atomically.
func (s *sqliteStore) Transfer(ctx context.Context, tenantID, channel, fromViewerID, toViewerID, toUsername string, amount int64) (Account, Account, error) {
	ch, err := validateIdentity(tenantID, channel, fromViewerID)
	if err != nil {
		return Account{}, Account{}, err
	}
	if strings.TrimSpace(toViewerID) == "" {
		return Account{}, Account{}, fmt.Errorf("%w: recipient viewer_id is required", ErrInvalid)
	}
	if amount <= 0 {
		return Account{}, Account{}, fmt.Errorf("%w: transfer amount %d must be positive", ErrInvalid, amount)
	}
	// Reject self-transfer before touching the db: gifting to yourself is a
	// no-op the caller almost certainly did not intend.
	if fromViewerID == toViewerID {
		return Account{}, Account{}, fmt.Errorf("%w: cannot transfer to self", ErrInvalid)
	}
	toUname := strings.TrimSpace(toUsername)

	// One transaction for the whole move: verify the sender exists and has
	// funds, debit the sender, then upsert+credit the recipient. The single
	// writer connection makes the debit-then-credit indivisible, so the two
	// balances can never be observed mid-transfer and funds are conserved.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, Account{}, fmt.Errorf("loyalty: transfer begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	sender, err := scanRow(tx.QueryRowContext(ctx,
		accountSelect+`WHERE tenant_id = ? AND channel = ? AND viewer_id = ?`,
		tenantID, ch, fromViewerID))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, Account{}, fmt.Errorf("loyalty: transfer read sender: %w", err)
	}
	if sender.Balance < amount {
		return Account{}, Account{}, ErrInsufficient
	}

	now := time.Now().UTC().Unix()
	if _, err := tx.ExecContext(ctx,
		`UPDATE loyalty_accounts SET balance = balance - ?, updated_at = ?
         WHERE tenant_id = ? AND channel = ? AND viewer_id = ?`,
		amount, now, tenantID, ch, fromViewerID); err != nil {
		return Account{}, Account{}, fmt.Errorf("loyalty: transfer debit: %w", err)
	}

	// Credit the recipient with the same atomic upsert Earn uses, creating
	// the account at amount if they had none.
	const credit = `
INSERT INTO loyalty_accounts (id, tenant_id, channel, viewer_id, username, balance, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, channel, viewer_id)
DO UPDATE SET balance = balance + excluded.balance,
              username = excluded.username,
              updated_at = excluded.updated_at`
	if _, err := tx.ExecContext(ctx, credit,
		newID(), tenantID, ch, toViewerID, toUname, amount, now); err != nil {
		return Account{}, Account{}, fmt.Errorf("loyalty: transfer credit: %w", err)
	}

	from, err := scanRow(tx.QueryRowContext(ctx,
		accountSelect+`WHERE tenant_id = ? AND channel = ? AND viewer_id = ?`,
		tenantID, ch, fromViewerID))
	if err != nil {
		return Account{}, Account{}, fmt.Errorf("loyalty: transfer reread sender: %w", err)
	}
	to, err := scanRow(tx.QueryRowContext(ctx,
		accountSelect+`WHERE tenant_id = ? AND channel = ? AND viewer_id = ?`,
		tenantID, ch, toViewerID))
	if err != nil {
		return Account{}, Account{}, fmt.Errorf("loyalty: transfer reread recipient: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Account{}, Account{}, fmt.Errorf("loyalty: transfer commit: %w", err)
	}
	return from, to, nil
}

// Leaderboard returns the top accounts by balance, clamping limit to
// [1, 100].
func (s *sqliteStore) Leaderboard(ctx context.Context, tenantID, channel string, limit int) ([]Account, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	ch := normalizeChannel(channel)
	if ch == "" {
		return nil, fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		accountSelect+`WHERE tenant_id = ? AND channel = ?
                       ORDER BY balance DESC, username ASC
                       LIMIT ?`,
		tenantID, ch, limit)
	if err != nil {
		return nil, fmt.Errorf("loyalty: leaderboard: %w", err)
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("loyalty: scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// balanceVerbatim reads an account without re-normalising channel; callers
// have already validated tenant/channel/viewer.
func (s *sqliteStore) balanceVerbatim(ctx context.Context, tenantID, channel, viewerID string) (Account, error) {
	row := s.db.QueryRowContext(ctx,
		accountSelect+`WHERE tenant_id = ? AND channel = ? AND viewer_id = ?`,
		tenantID, channel, viewerID)
	a, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("loyalty: balance: %w", err)
	}
	return a, nil
}
