package workspaces

import (
	"context"
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

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MembershipView is a membership joined with its workspace, the shape the
// channel switcher and post-login routing consume in one query.
type MembershipView struct {
	Workspace Workspace        `json:"workspace"`
	Role      Role             `json:"role"`
	Source    MembershipSource `json:"source"`
}

// Store is the persistence contract for workspaces, memberships and
// invitations. All methods are safe for concurrent use.
type Store interface {
	CreateWorkspace(ctx context.Context, w Workspace) (Workspace, error)
	GetWorkspaceBySlug(ctx context.Context, tenantID, slug string) (Workspace, error)
	GetWorkspaceByID(ctx context.Context, id string) (Workspace, error)
	ListWorkspaces(ctx context.Context, tenantID string) ([]Workspace, error)
	SetAutoVerifyMods(ctx context.Context, workspaceID string, enabled bool) error

	UpsertMembership(ctx context.Context, m Membership) (Membership, error)
	GetMembership(ctx context.Context, workspaceID, userID string) (Membership, error)
	ListMembershipsForUser(ctx context.Context, userID string) ([]MembershipView, error)
	ListMembers(ctx context.Context, workspaceID string) ([]Membership, error)
	DeleteMembership(ctx context.Context, workspaceID, userID string) error

	CreateInvitation(ctx context.Context, inv Invitation) (Invitation, error)
	GetInvitationByToken(ctx context.Context, token string) (Invitation, error)
	ListPendingInvitationsForLogin(ctx context.Context, twitchLogin string) ([]Invitation, error)
	ListInvitations(ctx context.Context, workspaceID string) ([]Invitation, error)
	MarkInvitationAccepted(ctx context.Context, id string, at time.Time) error
	DeleteInvitation(ctx context.Context, id string) error

	Close() error
}

type sqliteStore struct {
	db  *sql.DB
	log *slog.Logger
	mu  sync.Mutex
}

// OpenSQLiteStore opens (or creates) a SQLite database at dsn and returns a
// ready-to-use Store with WAL journal mode, foreign-keys ON, and
// synchronous=NORMAL. Use "file::memory:?cache=shared" for tests.
func OpenSQLiteStore(ctx context.Context, dsn string, logger *slog.Logger) (Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("workspaces: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("workspaces: %s: %w", p, err)
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
		return fmt.Errorf("workspaces: read migrations dir: %w", err)
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
			return fmt.Errorf("workspaces: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("workspaces: apply migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }

const wsCols = `id, tenant_id, slug, twitch_channel_id, twitch_login, display_name, owner_user_id, auto_verify_mods, created_at, updated_at`

func scanWorkspace(row interface{ Scan(...any) error }) (Workspace, error) {
	var (
		w       Workspace
		auto    int
		created int64
		updated int64
	)
	if err := row.Scan(&w.ID, &w.TenantID, &w.Slug, &w.TwitchChannelID, &w.TwitchLogin,
		&w.DisplayName, &w.OwnerUserID, &auto, &created, &updated); err != nil {
		return Workspace{}, err
	}
	w.AutoVerifyMods = auto != 0
	w.CreatedAt = time.Unix(0, created).UTC()
	w.UpdatedAt = time.Unix(0, updated).UTC()
	return w, nil
}

func (s *sqliteStore) CreateWorkspace(ctx context.Context, w Workspace) (Workspace, error) {
	if err := w.validate(); err != nil {
		return Workspace{}, err
	}
	if strings.TrimSpace(w.ID) == "" {
		w.ID = newID()
	}
	now := time.Now().UTC()
	w.CreatedAt = now
	w.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()

	const dup = `SELECT 1 FROM workspaces WHERE tenant_id = ? AND slug = ? LIMIT 1`
	var x int
	switch err := s.db.QueryRowContext(ctx, dup, w.TenantID, w.Slug).Scan(&x); {
	case err == nil:
		return Workspace{}, ErrAlreadyExists
	case errors.Is(err, sql.ErrNoRows):
	default:
		return Workspace{}, fmt.Errorf("workspaces: check duplicate: %w", err)
	}

	const ins = `INSERT INTO workspaces (` + wsCols + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, ins,
		w.ID, w.TenantID, w.Slug, w.TwitchChannelID, w.TwitchLogin, w.DisplayName,
		w.OwnerUserID, boolToInt(w.AutoVerifyMods), now.UnixNano(), now.UnixNano()); err != nil {
		return Workspace{}, fmt.Errorf("workspaces: insert: %w", err)
	}
	return w, nil
}

func (s *sqliteStore) GetWorkspaceBySlug(ctx context.Context, tenantID, slug string) (Workspace, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+wsCols+` FROM workspaces WHERE tenant_id = ? AND slug = ?`,
		tenantID, normalizeLogin(slug))
	w, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("workspaces: get by slug: %w", err)
	}
	return w, nil
}

func (s *sqliteStore) GetWorkspaceByID(ctx context.Context, id string) (Workspace, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+wsCols+` FROM workspaces WHERE id = ?`, id)
	w, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("workspaces: get by id: %w", err)
	}
	return w, nil
}

func (s *sqliteStore) ListWorkspaces(ctx context.Context, tenantID string) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+wsCols+` FROM workspaces WHERE tenant_id = ? ORDER BY slug ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("workspaces: list: %w", err)
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, fmt.Errorf("workspaces: scan: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SetAutoVerifyMods(ctx context.Context, workspaceID string, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE workspaces SET auto_verify_mods = ?, updated_at = ? WHERE id = ?`,
		boolToInt(enabled), time.Now().UTC().UnixNano(), workspaceID)
	if err != nil {
		return fmt.Errorf("workspaces: set auto_verify_mods: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) UpsertMembership(ctx context.Context, m Membership) (Membership, error) {
	if err := m.validate(); err != nil {
		return Membership{}, err
	}
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	const q = `INSERT INTO memberships (workspace_id, user_id, role, source, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(workspace_id, user_id) DO UPDATE SET role = excluded.role, source = excluded.source, updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, q,
		m.WorkspaceID, m.UserID, string(m.Role), string(m.Source),
		m.CreatedAt.UnixNano(), m.UpdatedAt.UnixNano()); err != nil {
		return Membership{}, fmt.Errorf("workspaces: upsert membership: %w", err)
	}
	return m, nil
}

func scanMembership(row interface{ Scan(...any) error }) (Membership, error) {
	var (
		m       Membership
		role    string
		source  string
		created int64
		updated int64
	)
	if err := row.Scan(&m.WorkspaceID, &m.UserID, &role, &source, &created, &updated); err != nil {
		return Membership{}, err
	}
	m.Role = Role(role)
	m.Source = MembershipSource(source)
	m.CreatedAt = time.Unix(0, created).UTC()
	m.UpdatedAt = time.Unix(0, updated).UTC()
	return m, nil
}

func (s *sqliteStore) GetMembership(ctx context.Context, workspaceID, userID string) (Membership, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT workspace_id, user_id, role, source, created_at, updated_at FROM memberships WHERE workspace_id = ? AND user_id = ?`,
		workspaceID, userID)
	m, err := scanMembership(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	if err != nil {
		return Membership{}, fmt.Errorf("workspaces: get membership: %w", err)
	}
	return m, nil
}

func (s *sqliteStore) ListMembershipsForUser(ctx context.Context, userID string) ([]MembershipView, error) {
	q := `SELECT ` + wsColsPrefixed("w") + `, m.role, m.source
FROM memberships m JOIN workspaces w ON w.id = m.workspace_id
WHERE m.user_id = ? ORDER BY w.slug ASC`
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("workspaces: list memberships for user: %w", err)
	}
	defer rows.Close()
	var out []MembershipView
	for rows.Next() {
		var (
			w       Workspace
			auto    int
			created int64
			updated int64
			role    string
			source  string
		)
		if err := rows.Scan(&w.ID, &w.TenantID, &w.Slug, &w.TwitchChannelID, &w.TwitchLogin,
			&w.DisplayName, &w.OwnerUserID, &auto, &created, &updated, &role, &source); err != nil {
			return nil, fmt.Errorf("workspaces: scan membership view: %w", err)
		}
		w.AutoVerifyMods = auto != 0
		w.CreatedAt = time.Unix(0, created).UTC()
		w.UpdatedAt = time.Unix(0, updated).UTC()
		out = append(out, MembershipView{Workspace: w, Role: Role(role), Source: MembershipSource(source)})
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListMembers(ctx context.Context, workspaceID string) ([]Membership, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT workspace_id, user_id, role, source, created_at, updated_at FROM memberships WHERE workspace_id = ? ORDER BY created_at ASC`,
		workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspaces: list members: %w", err)
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, fmt.Errorf("workspaces: scan member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteMembership(ctx context.Context, workspaceID, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM memberships WHERE workspace_id = ? AND user_id = ?`, workspaceID, userID)
	if err != nil {
		return fmt.Errorf("workspaces: delete membership: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) CreateInvitation(ctx context.Context, inv Invitation) (Invitation, error) {
	if err := inv.validate(); err != nil {
		return Invitation{}, err
	}
	if strings.TrimSpace(inv.ID) == "" {
		inv.ID = newID()
	}
	if strings.TrimSpace(inv.Token) == "" {
		tok, err := newToken()
		if err != nil {
			return Invitation{}, err
		}
		inv.Token = tok
	}
	inv.CreatedAt = time.Now().UTC()
	const q = `INSERT INTO invitations (id, workspace_id, twitch_login, role, token, invited_by, accepted_at, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, q,
		inv.ID, inv.WorkspaceID, inv.TwitchLogin, string(inv.Role), inv.Token, inv.InvitedBy,
		nullableUnix(inv.AcceptedAt), nullableUnix(inv.ExpiresAt), inv.CreatedAt.UnixNano()); err != nil {
		return Invitation{}, fmt.Errorf("workspaces: insert invitation: %w", err)
	}
	return inv, nil
}

const invCols = `id, workspace_id, twitch_login, role, token, invited_by, accepted_at, expires_at, created_at`

func scanInvitation(row interface{ Scan(...any) error }) (Invitation, error) {
	var (
		inv      Invitation
		role     string
		accepted sql.NullInt64
		expires  sql.NullInt64
		created  int64
	)
	if err := row.Scan(&inv.ID, &inv.WorkspaceID, &inv.TwitchLogin, &role, &inv.Token,
		&inv.InvitedBy, &accepted, &expires, &created); err != nil {
		return Invitation{}, err
	}
	inv.Role = Role(role)
	if accepted.Valid {
		t := time.Unix(0, accepted.Int64).UTC()
		inv.AcceptedAt = &t
	}
	if expires.Valid {
		t := time.Unix(0, expires.Int64).UTC()
		inv.ExpiresAt = &t
	}
	inv.CreatedAt = time.Unix(0, created).UTC()
	return inv, nil
}

func (s *sqliteStore) GetInvitationByToken(ctx context.Context, token string) (Invitation, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+invCols+` FROM invitations WHERE token = ?`, token)
	inv, err := scanInvitation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, ErrNotFound
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("workspaces: get invitation: %w", err)
	}
	return inv, nil
}

func (s *sqliteStore) ListPendingInvitationsForLogin(ctx context.Context, twitchLogin string) ([]Invitation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+invCols+` FROM invitations WHERE twitch_login = ? AND accepted_at IS NULL ORDER BY created_at ASC`,
		normalizeLogin(twitchLogin))
	if err != nil {
		return nil, fmt.Errorf("workspaces: list pending invitations: %w", err)
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, fmt.Errorf("workspaces: scan invitation: %w", err)
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListInvitations(ctx context.Context, workspaceID string) ([]Invitation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+invCols+` FROM invitations WHERE workspace_id = ? ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspaces: list invitations: %w", err)
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, fmt.Errorf("workspaces: scan invitation: %w", err)
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *sqliteStore) MarkInvitationAccepted(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE invitations SET accepted_at = ? WHERE id = ?`, at.UnixNano(), id)
	if err != nil {
		return fmt.Errorf("workspaces: mark invitation accepted: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) DeleteInvitation(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM invitations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("workspaces: delete invitation: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func wsColsPrefixed(alias string) string {
	parts := strings.Split(wsCols, ", ")
	for i, p := range parts {
		parts[i] = alias + "." + p
	}
	return strings.Join(parts, ", ")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableUnix(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixNano()
}
