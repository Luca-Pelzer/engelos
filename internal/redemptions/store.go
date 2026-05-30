package redemptions

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
	// ErrNotFound is returned when a (tenant, channel, reward_id) lookup
	// matches no row.
	ErrNotFound = errors.New("redemptions: binding not found")

	// ErrInvalid is returned when binding fields fail validation. The
	// wrapped detail says why.
	ErrInvalid = errors.New("redemptions: invalid binding")

	// ErrConflict is returned by Create when a binding for the same
	// (tenant, channel, reward_id) already exists.
	ErrConflict = errors.New("redemptions: binding already exists")
)

// Action types a binding may invoke. The set is closed and validated; later
// steps may append a const and extend the membership check by one line.
const (
	// ActionChatMessage posts a chat message; ActionParam is the template.
	ActionChatMessage = "chat_message"
	// ActionCounterIncr bumps a counter; ActionParam is the counter name.
	ActionCounterIncr = "counter_increment"
	// ActionCounterReset resets a counter; ActionParam is the counter name.
	ActionCounterReset = "counter_reset"
	// ActionNone records the reward but does nothing; ActionParam is ignored.
	ActionNone = "none"
)

// knownActions is the closed set membership-checked by validate. Adding a
// new ActionType means appending one const and one map entry here.
var knownActions = map[string]struct{}{
	ActionChatMessage:  {},
	ActionCounterIncr:  {},
	ActionCounterReset: {},
	ActionNone:         {},
}

// Field length bounds.
const (
	// maxRewardIDLen caps a Twitch reward UUID at 64 characters.
	maxRewardIDLen = 64
	// maxRewardTitleLen caps the cached reward title at 256 characters.
	maxRewardTitleLen = 256
	// maxActionParamLen caps an action parameter at 480 characters,
	// matching the codebase-wide 480-char convention.
	maxActionParamLen = 480
)

// Binding maps a Twitch Channel-Points Custom-Reward to a bot action,
// scoped to a (tenant, channel). RewardID is the Twitch reward UUID and the
// unique trigger key per (tenant, channel). RewardTitle is a cached human
// label for dashboard display (may be empty). ActionType is one of the
// Action* consts; ActionParam is its action-specific argument (may be
// empty). Enabled bindings persist but are ignored by the executor when
// false. AutoFulfill tells the future executor whether to mark the
// redemption FULFILLED/CANCELED on the Twitch side. ID is the internal
// ULID primary key.
type Binding struct {
	ID          string
	TenantID    string
	Channel     string
	RewardID    string
	RewardTitle string
	ActionType  string
	ActionParam string
	Enabled     bool
	AutoFulfill bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Store is the persistence contract for reward to action bindings. All
// methods are safe for concurrent use; the check-then-insert of Create and
// the read-modify-write of Update are serialised under a process mutex.
type Store interface {
	// Create inserts a new binding. Returns ErrConflict if a binding for
	// the same (tenant, channel, reward_id) already exists. Returns
	// ErrInvalid on validation failure.
	Create(ctx context.Context, tenantID, channel string, b Binding) (Binding, error)
	// GetByReward returns the binding for a (tenant, channel, reward_id).
	// ErrNotFound if absent.
	GetByReward(ctx context.Context, tenantID, channel, rewardID string) (Binding, error)
	// List returns all bindings for (tenant, channel) ordered by
	// reward_title then reward_id ASC.
	List(ctx context.Context, tenantID, channel string) ([]Binding, error)
	// Update replaces the mutable fields (RewardTitle, ActionType,
	// ActionParam, Enabled, AutoFulfill) of the binding identified by
	// (tenant, channel, reward_id), bumping UpdatedAt. ErrNotFound if
	// absent, ErrInvalid on bad ActionType.
	Update(ctx context.Context, tenantID, channel, rewardID string, b Binding) (Binding, error)
	// SetEnabled toggles only the Enabled flag. ErrNotFound if absent.
	SetEnabled(ctx context.Context, tenantID, channel, rewardID string, enabled bool) error
	// Delete removes the binding. ErrNotFound if absent.
	Delete(ctx context.Context, tenantID, channel, rewardID string) error
	// Close releases the underlying database handle.
	Close() error
}

// newID returns a fresh lower-cased ULID, mirroring liveops.newID.
func newID() string {
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return strings.ToLower(id.String())
}

// validate normalises and bounds-checks the binding fields, returning the
// trimmed reward_id/reward_title/action_param or an error wrapping
// ErrInvalid.
func validate(tenantID, channel string, b Binding) (rewardID, rewardTitle, actionParam string, err error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", "", "", fmt.Errorf("%w: tenant_id is required", ErrInvalid)
	}
	if strings.TrimSpace(channel) == "" {
		return "", "", "", fmt.Errorf("%w: channel is required", ErrInvalid)
	}
	rewardID = strings.TrimSpace(b.RewardID)
	if rewardID == "" {
		return "", "", "", fmt.Errorf("%w: reward_id is required", ErrInvalid)
	}
	if len(rewardID) > maxRewardIDLen {
		return "", "", "", fmt.Errorf("%w: reward_id length %d exceeds %d", ErrInvalid, len(rewardID), maxRewardIDLen)
	}
	rewardTitle = strings.TrimSpace(b.RewardTitle)
	if len(rewardTitle) > maxRewardTitleLen {
		return "", "", "", fmt.Errorf("%w: reward_title length %d exceeds %d", ErrInvalid, len(rewardTitle), maxRewardTitleLen)
	}
	if _, ok := knownActions[b.ActionType]; !ok {
		return "", "", "", fmt.Errorf("%w: unknown action_type %q", ErrInvalid, b.ActionType)
	}
	actionParam = strings.TrimSpace(b.ActionParam)
	if len(actionParam) > maxActionParamLen {
		return "", "", "", fmt.Errorf("%w: action_param length %d exceeds %d", ErrInvalid, len(actionParam), maxActionParamLen)
	}
	return rewardID, rewardTitle, actionParam, nil
}

// sqliteStore is a pure-Go SQLite implementation backed by
// modernc.org/sqlite. Mirrors internal/liveops.sqliteStore conventions:
// WAL, foreign-keys, busy_timeout, SetMaxOpenConns(1) and a sync.Mutex
// serialising the check-then-insert and the read-modify-write.
type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger

	// mu serialises Create's duplicate check + INSERT and Update's
	// existence check + UPDATE so concurrent writers to the same
	// (tenant, channel, reward_id) cannot race; the UNIQUE constraint is
	// the backstop that makes a concurrent Create deterministically fail
	// with ErrConflict.
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
		return nil, fmt.Errorf("redemptions: open sqlite: %w", err)
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
			return nil, fmt.Errorf("redemptions: %s: %w", p, err)
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
		return fmt.Errorf("redemptions: read migrations dir: %w", err)
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
			return fmt.Errorf("redemptions: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("redemptions: apply migration %s: %w", name, err)
		}
		s.log.Debug("redemptions: migration applied", "name", name)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

const bindingSelect = `SELECT id, tenant_id, channel, reward_id, reward_title,
                              action_type, action_param, enabled, auto_fulfill,
                              created_at, updated_at
                       FROM redemption_bindings `

func scanRow(row interface{ Scan(...any) error }) (Binding, error) {
	var (
		b           Binding
		enabled     int64
		autoFulfill int64
		created     int64
		updated     int64
	)
	err := row.Scan(&b.ID, &b.TenantID, &b.Channel, &b.RewardID, &b.RewardTitle,
		&b.ActionType, &b.ActionParam, &enabled, &autoFulfill, &created, &updated)
	if err != nil {
		return Binding{}, err
	}
	b.Enabled = enabled != 0
	b.AutoFulfill = autoFulfill != 0
	b.CreatedAt = time.Unix(0, created).UTC()
	b.UpdatedAt = time.Unix(0, updated).UTC()
	return b, nil
}

// boolToInt maps a Go bool to the INTEGER 0/1 the schema stores.
func boolToInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func (s *sqliteStore) Create(ctx context.Context, tenantID, channel string, b Binding) (Binding, error) {
	rewardID, rewardTitle, actionParam, err := validate(tenantID, channel, b)
	if err != nil {
		return Binding{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	const dup = `SELECT 1 FROM redemption_bindings
                 WHERE tenant_id = ? AND channel = ? AND reward_id = ? LIMIT 1`
	var x int
	switch err := s.db.QueryRowContext(ctx, dup, tenantID, channel, rewardID).Scan(&x); {
	case err == nil:
		return Binding{}, ErrConflict
	case errors.Is(err, sql.ErrNoRows):
		// not a duplicate; continue
	default:
		return Binding{}, fmt.Errorf("redemptions: check duplicate: %w", err)
	}

	now := time.Now().UTC()
	out := Binding{
		ID:          newID(),
		TenantID:    tenantID,
		Channel:     channel,
		RewardID:    rewardID,
		RewardTitle: rewardTitle,
		ActionType:  b.ActionType,
		ActionParam: actionParam,
		Enabled:     b.Enabled,
		AutoFulfill: b.AutoFulfill,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	const ins = `
INSERT INTO redemption_bindings (id, tenant_id, channel, reward_id, reward_title,
                                 action_type, action_param, enabled, auto_fulfill,
                                 created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		out.ID, out.TenantID, out.Channel, out.RewardID, out.RewardTitle,
		out.ActionType, out.ActionParam, boolToInt(out.Enabled), boolToInt(out.AutoFulfill),
		out.CreatedAt.UnixNano(), out.UpdatedAt.UnixNano(),
	); err != nil {
		if isUniqueViolation(err) {
			return Binding{}, ErrConflict
		}
		return Binding{}, fmt.Errorf("redemptions: insert: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) GetByReward(ctx context.Context, tenantID, channel, rewardID string) (Binding, error) {
	row := s.db.QueryRowContext(ctx,
		bindingSelect+`WHERE tenant_id = ? AND channel = ? AND reward_id = ?`,
		tenantID, channel, strings.TrimSpace(rewardID))
	b, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrNotFound
	}
	if err != nil {
		return Binding{}, fmt.Errorf("redemptions: get: %w", err)
	}
	return b, nil
}

// getLocked is GetByReward without taking s.mu; it is called from Update
// which already holds the lock and has already trimmed the reward_id.
func (s *sqliteStore) getLocked(ctx context.Context, tenantID, channel, rewardID string) (Binding, error) {
	row := s.db.QueryRowContext(ctx,
		bindingSelect+`WHERE tenant_id = ? AND channel = ? AND reward_id = ?`,
		tenantID, channel, rewardID)
	b, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrNotFound
	}
	if err != nil {
		return Binding{}, fmt.Errorf("redemptions: get: %w", err)
	}
	return b, nil
}

func (s *sqliteStore) List(ctx context.Context, tenantID, channel string) ([]Binding, error) {
	rows, err := s.db.QueryContext(ctx,
		bindingSelect+`WHERE tenant_id = ? AND channel = ?
                       ORDER BY reward_title ASC, reward_id ASC`,
		tenantID, channel)
	if err != nil {
		return nil, fmt.Errorf("redemptions: list: %w", err)
	}
	defer rows.Close()
	var out []Binding
	for rows.Next() {
		b, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("redemptions: scan: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqliteStore) Update(ctx context.Context, tenantID, channel, rewardID string, b Binding) (Binding, error) {
	b.RewardID = rewardID
	rid, rewardTitle, actionParam, err := validate(tenantID, channel, b)
	if err != nil {
		return Binding{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().UnixNano()
	const q = `UPDATE redemption_bindings
               SET reward_title = ?, action_type = ?, action_param = ?,
                   enabled = ?, auto_fulfill = ?, updated_at = ?
               WHERE tenant_id = ? AND channel = ? AND reward_id = ?`
	res, err := s.db.ExecContext(ctx, q,
		rewardTitle, b.ActionType, actionParam,
		boolToInt(b.Enabled), boolToInt(b.AutoFulfill), now,
		tenantID, channel, rid)
	if err != nil {
		return Binding{}, fmt.Errorf("redemptions: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Binding{}, ErrNotFound
	}
	return s.getLocked(ctx, tenantID, channel, rid)
}

func (s *sqliteStore) SetEnabled(ctx context.Context, tenantID, channel, rewardID string, enabled bool) error {
	now := time.Now().UTC().UnixNano()
	res, err := s.db.ExecContext(ctx,
		`UPDATE redemption_bindings SET enabled = ?, updated_at = ?
         WHERE tenant_id = ? AND channel = ? AND reward_id = ?`,
		boolToInt(enabled), now, tenantID, channel, strings.TrimSpace(rewardID))
	if err != nil {
		return fmt.Errorf("redemptions: set enabled: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) Delete(ctx context.Context, tenantID, channel, rewardID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM redemption_bindings WHERE tenant_id = ? AND channel = ? AND reward_id = ?`,
		tenantID, channel, strings.TrimSpace(rewardID))
	if err != nil {
		return fmt.Errorf("redemptions: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
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
