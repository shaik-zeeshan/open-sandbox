package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrSandboxNotFound = errors.New("sandbox not found")
var ErrUserNotFound = errors.New("user not found")
var ErrUsernameTaken = errors.New("username already exists")

type Sandbox struct {
	ID            string
	Name          string
	Image         string
	ContainerID   string
	WorkspaceDir  string
	RepoURL       string
	Status        string
	OwnerID       string
	OwnerUsername string
	CreatedAt     int64
	UpdatedAt     int64
}

type User struct {
	ID        string
	Username  string
	Role      string
	CreatedAt int64
	UpdatedAt int64
}

type UserRecord struct {
	User
	PasswordHash string
}

func (s *SQLiteStore) HasUsers(ctx context.Context) (bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count > 0, nil
}

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLiteStore, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		trimmed = "open-sandbox.db"
	}

	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite parent directory: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", filepath.ToSlash(absPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	const migration = `
CREATE TABLE IF NOT EXISTS sandboxes (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	image TEXT NOT NULL,
	container_id TEXT NOT NULL UNIQUE,
	workspace_dir TEXT NOT NULL,
	repo_url TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	username TEXT NOT NULL UNIQUE COLLATE NOCASE,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sandboxes_created_at ON sandboxes(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sandboxes_container_id ON sandboxes(container_id);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
`

	if _, err := s.db.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("run sqlite migrations: %w", err)
	}
	if err := s.ensureColumn(ctx, "sandboxes", "owner_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "sandboxes", "owner_username", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	return nil
}

func (s *SQLiteStore) ensureColumn(ctx context.Context, table string, column string, definition string) error {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("inspect sqlite table %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan sqlite table info for %s: %w", table, err)
		}
		if strings.EqualFold(name, column) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sqlite table info for %s: %w", table, err)
	}

	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}

	return nil
}

func (s *SQLiteStore) CreateSandbox(ctx context.Context, sandbox Sandbox) error {
	if sandbox.ID == "" {
		return errors.New("sandbox id is required")
	}
	now := time.Now().Unix()
	if sandbox.CreatedAt == 0 {
		sandbox.CreatedAt = now
	}
	if sandbox.UpdatedAt == 0 {
		sandbox.UpdatedAt = sandbox.CreatedAt
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO sandboxes (id, name, image, container_id, workspace_dir, repo_url, status, owner_id, owner_username, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sandbox.ID,
		sandbox.Name,
		sandbox.Image,
		sandbox.ContainerID,
		sandbox.WorkspaceDir,
		sandbox.RepoURL,
		sandbox.Status,
		sandbox.OwnerID,
		sandbox.OwnerUsername,
		sandbox.CreatedAt,
		sandbox.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert sandbox: %w", err)
	}

	return nil
}

func (s *SQLiteStore) ListSandboxes(ctx context.Context) ([]Sandbox, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, image, container_id, workspace_dir, repo_url, status, owner_id, owner_username, created_at, updated_at
		FROM sandboxes
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query sandboxes: %w", err)
	}
	defer rows.Close()

	out := make([]Sandbox, 0)
	for rows.Next() {
		sandbox, scanErr := scanSandbox(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, sandbox)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sandboxes: %w", err)
	}

	return out, nil
}

func (s *SQLiteStore) GetSandbox(ctx context.Context, sandboxID string) (Sandbox, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, name, image, container_id, workspace_dir, repo_url, status, owner_id, owner_username, created_at, updated_at
		 FROM sandboxes
		 WHERE id = ?`,
		sandboxID,
	)

	sandbox, err := scanSandbox(row)
	if err != nil {
		return Sandbox{}, err
	}

	return sandbox, nil
}

func (s *SQLiteStore) UpdateSandboxStatus(ctx context.Context, sandboxID string, status string) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE sandboxes SET status = ?, updated_at = ? WHERE id = ?`,
		status,
		time.Now().Unix(),
		sandboxID,
	)
	if err != nil {
		return fmt.Errorf("update sandbox status: %w", err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if changed == 0 {
		return ErrSandboxNotFound
	}

	return nil
}

func (s *SQLiteStore) UpdateSandboxStatusByContainerID(ctx context.Context, containerID string, status string) error {
	if strings.TrimSpace(containerID) == "" {
		return nil
	}

	_, err := s.db.ExecContext(
		ctx,
		`UPDATE sandboxes SET status = ?, updated_at = ? WHERE container_id = ?`,
		status,
		time.Now().Unix(),
		containerID,
	)
	if err != nil {
		return fmt.Errorf("update sandbox status by container id: %w", err)
	}

	return nil
}

func (s *SQLiteStore) DeleteSandbox(ctx context.Context, sandboxID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sandboxes WHERE id = ?`, sandboxID)
	if err != nil {
		return fmt.Errorf("delete sandbox: %w", err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if changed == 0 {
		return ErrSandboxNotFound
	}

	return nil
}

func (s *SQLiteStore) DeleteSandboxByContainerID(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `DELETE FROM sandboxes WHERE container_id = ?`, containerID)
	if err != nil {
		return fmt.Errorf("delete sandbox by container id: %w", err)
	}

	return nil
}

type sandboxScanner interface {
	Scan(dest ...any) error
}

func scanSandbox(scanner sandboxScanner) (Sandbox, error) {
	var sandbox Sandbox
	if err := scanner.Scan(
		&sandbox.ID,
		&sandbox.Name,
		&sandbox.Image,
		&sandbox.ContainerID,
		&sandbox.WorkspaceDir,
		&sandbox.RepoURL,
		&sandbox.Status,
		&sandbox.OwnerID,
		&sandbox.OwnerUsername,
		&sandbox.CreatedAt,
		&sandbox.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Sandbox{}, ErrSandboxNotFound
		}
		return Sandbox{}, fmt.Errorf("scan sandbox row: %w", err)
	}

	return sandbox, nil
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func (s *SQLiteStore) CreateUser(ctx context.Context, user UserRecord) (User, error) {
	username := normalizeUsername(user.Username)
	if username == "" {
		return User{}, errors.New("username is required")
	}
	if user.PasswordHash == "" {
		return User{}, errors.New("password hash is required")
	}
	if user.Role == "" {
		return User{}, errors.New("role is required")
	}
	if user.ID == "" {
		return User{}, errors.New("user id is required")
	}

	now := time.Now().Unix()
	if user.CreatedAt == 0 {
		user.CreatedAt = now
	}
	if user.UpdatedAt == 0 {
		user.UpdatedAt = user.CreatedAt
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO users (id, username, password_hash, role, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		user.ID,
		username,
		user.PasswordHash,
		user.Role,
		user.CreatedAt,
		user.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return User{}, ErrUsernameTaken
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	return User{ID: user.ID, Username: username, Role: user.Role, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}, nil
}

func (s *SQLiteStore) EnsureUser(ctx context.Context, user UserRecord) (User, error) {
	existing, err := s.GetUserByUsername(ctx, user.Username)
	if err == nil {
		return existing.User, nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		return User{}, err
	}
	return s.CreateUser(ctx, user)
}

func (s *SQLiteStore) GetUserByUsername(ctx context.Context, username string) (UserRecord, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, username, password_hash, role, created_at, updated_at
		 FROM users
		 WHERE username = ?`,
		normalizeUsername(username),
	)

	var user UserRecord
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.CreatedAt, &user.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserRecord{}, ErrUserNotFound
		}
		return UserRecord{}, fmt.Errorf("query user by username: %w", err)
	}

	return user, nil
}

func (s *SQLiteStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, role, created_at, updated_at
		FROM users
		ORDER BY username ASC`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan user row: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	return users, nil
}

func (s *SQLiteStore) UpdateUserPasswordHash(ctx context.Context, userID string, passwordHash string) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash,
		time.Now().Unix(),
		userID,
	)
	if err != nil {
		return fmt.Errorf("update user password hash: %w", err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if changed == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (s *SQLiteStore) DeleteUser(ctx context.Context, userID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if changed == 0 {
		return ErrUserNotFound
	}

	return nil
}
