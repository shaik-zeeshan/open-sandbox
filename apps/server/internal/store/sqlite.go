package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	traefikcfg "github.com/shaik-zeeshan/open-sandbox/internal/traefik"
)

var ErrSandboxNotFound = errors.New("sandbox not found")
var ErrUserNotFound = errors.New("user not found")
var ErrRuntimeWorkerNotFound = errors.New("runtime worker not found")
var ErrUsernameTaken = errors.New("username already exists")
var ErrRefreshTokenNotFound = errors.New("refresh token not found")
var ErrRefreshTokenInactive = errors.New("refresh token is inactive")

type Sandbox struct {
	ID            string
	Name          string
	Image         string
	ContainerID   string
	WorkerID      string
	WorkspaceDir  string
	RepoURL       string
	PortSpecs     []string
	Status        string
	OwnerID       string
	OwnerUsername string
	ProxyConfig   map[int]traefikcfg.ServiceProxyConfig
	CreatedAt     int64
	UpdatedAt     int64
}

type RuntimeWorker struct {
	ID                  string
	Name                string
	AdvertiseAddress    string
	ExecutionMode       string
	Status              string
	Version             string
	Labels              map[string]string
	RegisteredAt        int64
	LastHeartbeatAt     int64
	HeartbeatTTLSeconds int64
	UpdatedAt           int64
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

type RefreshTokenRecord struct {
	ID                string
	TokenHash         string
	UserID            string
	ExpiresAt         int64
	CreatedAt         int64
	RotatedAt         int64
	RevokedAt         int64
	ReplacedByTokenID string
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
	worker_id TEXT NOT NULL DEFAULT 'local',
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

CREATE TABLE IF NOT EXISTS refresh_tokens (
	id TEXT PRIMARY KEY,
	token_hash TEXT NOT NULL UNIQUE,
	user_id TEXT NOT NULL,
	expires_at INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	rotated_at INTEGER NOT NULL DEFAULT 0,
	revoked_at INTEGER NOT NULL DEFAULT 0,
	replaced_by_token_id TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);

CREATE TABLE IF NOT EXISTS runtime_workers (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	advertise_address TEXT NOT NULL DEFAULT '',
	execution_mode TEXT NOT NULL,
	status TEXT NOT NULL,
	version TEXT NOT NULL DEFAULT '',
	labels_json TEXT NOT NULL DEFAULT '{}',
	registered_at INTEGER NOT NULL,
	last_heartbeat_at INTEGER NOT NULL,
	heartbeat_ttl_seconds INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_runtime_workers_last_heartbeat_at ON runtime_workers(last_heartbeat_at DESC);
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
	if err := s.ensureColumn(ctx, "sandboxes", "worker_id", "TEXT NOT NULL DEFAULT 'local'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "sandboxes", "proxy_config_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "sandboxes", "port_specs_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
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

	proxyConfigJSON, err := marshalSandboxProxyConfig(sandbox.ProxyConfig)
	if err != nil {
		return err
	}
	portSpecsJSON, err := marshalStringSlice(sandbox.PortSpecs)
	if err != nil {
		return fmt.Errorf("marshal sandbox port specs: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO sandboxes (id, name, image, container_id, worker_id, workspace_dir, repo_url, port_specs_json, status, owner_id, owner_username, proxy_config_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sandbox.ID,
		sandbox.Name,
		sandbox.Image,
		sandbox.ContainerID,
		normalizeWorkerID(sandbox.WorkerID),
		sandbox.WorkspaceDir,
		sandbox.RepoURL,
		portSpecsJSON,
		sandbox.Status,
		sandbox.OwnerID,
		sandbox.OwnerUsername,
		proxyConfigJSON,
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
		SELECT id, name, image, container_id, worker_id, workspace_dir, repo_url, port_specs_json, status, owner_id, owner_username, proxy_config_json, created_at, updated_at
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
		`SELECT id, name, image, container_id, worker_id, workspace_dir, repo_url, port_specs_json, status, owner_id, owner_username, proxy_config_json, created_at, updated_at
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

func (s *SQLiteStore) UpdateSandboxProxyConfig(ctx context.Context, sandboxID string, proxyConfig map[int]traefikcfg.ServiceProxyConfig) error {
	proxyConfigJSON, err := marshalSandboxProxyConfig(proxyConfig)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE sandboxes SET proxy_config_json = ?, updated_at = ? WHERE id = ?`,
		proxyConfigJSON,
		time.Now().Unix(),
		sandboxID,
	)
	if err != nil {
		return fmt.Errorf("update sandbox proxy config: %w", err)
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
	var portSpecsJSON string
	var proxyConfigJSON string
	if err := scanner.Scan(
		&sandbox.ID,
		&sandbox.Name,
		&sandbox.Image,
		&sandbox.ContainerID,
		&sandbox.WorkerID,
		&sandbox.WorkspaceDir,
		&sandbox.RepoURL,
		&portSpecsJSON,
		&sandbox.Status,
		&sandbox.OwnerID,
		&sandbox.OwnerUsername,
		&proxyConfigJSON,
		&sandbox.CreatedAt,
		&sandbox.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Sandbox{}, ErrSandboxNotFound
		}
		return Sandbox{}, fmt.Errorf("scan sandbox row: %w", err)
	}

	portSpecs, err := unmarshalStringSlice(portSpecsJSON)
	if err != nil {
		return Sandbox{}, fmt.Errorf("unmarshal sandbox port specs: %w", err)
	}
	sandbox.PortSpecs = portSpecs

	proxyConfig, err := unmarshalSandboxProxyConfig(proxyConfigJSON)
	if err != nil {
		return Sandbox{}, err
	}
	sandbox.ProxyConfig = proxyConfig

	return sandbox, nil
}

func normalizeWorkerID(workerID string) string {
	trimmed := strings.TrimSpace(workerID)
	if trimmed == "" {
		return "local"
	}
	return trimmed
}

// marshalSandboxProxyConfig serialises the proxy config map to JSON.
// The map keys (private port numbers) are converted to string keys for JSON
// compatibility. A nil or empty map serialises to "{}".
func marshalSandboxProxyConfig(cfg map[int]traefikcfg.ServiceProxyConfig) (string, error) {
	if len(cfg) == 0 {
		return "{}", nil
	}
	// JSON object keys must be strings; convert int keys.
	strKeyed := make(map[string]traefikcfg.ServiceProxyConfig, len(cfg))
	for port, v := range cfg {
		strKeyed[fmt.Sprintf("%d", port)] = v
	}
	encoded, err := json.Marshal(strKeyed)
	if err != nil {
		return "", fmt.Errorf("marshal sandbox proxy config: %w", err)
	}
	return string(encoded), nil
}

// unmarshalSandboxProxyConfig deserialises a JSON proxy config map.
// Empty / blank input returns an empty (non-nil) map.
func unmarshalSandboxProxyConfig(raw string) (map[int]traefikcfg.ServiceProxyConfig, error) {
	trimmed := strings.TrimSpace(raw)
	result := make(map[int]traefikcfg.ServiceProxyConfig)
	if trimmed == "" || trimmed == "{}" {
		return result, nil
	}
	strKeyed := make(map[string]traefikcfg.ServiceProxyConfig)
	if err := json.Unmarshal([]byte(trimmed), &strKeyed); err != nil {
		return nil, fmt.Errorf("unmarshal sandbox proxy config: %w", err)
	}
	for k, v := range strKeyed {
		port, ok := parseSandboxProxyConfigPort(k)
		if !ok {
			continue
		}
		result[port] = v
	}
	return result, nil
}

func parseSandboxProxyConfigPort(raw string) (int, bool) {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port <= 0 {
		return 0, false
	}
	return port, true
}

func marshalStringSlice(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func unmarshalStringSlice(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, nil
	}
	return values, nil
}

func (s *SQLiteStore) UpsertRuntimeWorker(ctx context.Context, worker RuntimeWorker) error {
	worker.ID = normalizeWorkerID(worker.ID)
	if strings.TrimSpace(worker.Name) == "" {
		worker.Name = worker.ID
	}
	if strings.TrimSpace(worker.ExecutionMode) == "" {
		worker.ExecutionMode = "docker"
	}
	if strings.TrimSpace(worker.Status) == "" {
		worker.Status = "active"
	}
	if worker.HeartbeatTTLSeconds <= 0 {
		worker.HeartbeatTTLSeconds = 30
	}
	now := time.Now().Unix()
	if worker.RegisteredAt == 0 {
		worker.RegisteredAt = now
	}
	if worker.LastHeartbeatAt == 0 {
		worker.LastHeartbeatAt = now
	}
	if worker.UpdatedAt == 0 {
		worker.UpdatedAt = now
	}

	labelsJSON, err := marshalWorkerLabels(worker.Labels)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO runtime_workers (
			id, name, advertise_address, execution_mode, status, version, labels_json,
			registered_at, last_heartbeat_at, heartbeat_ttl_seconds, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			advertise_address = excluded.advertise_address,
			execution_mode = excluded.execution_mode,
			status = excluded.status,
			version = excluded.version,
			labels_json = excluded.labels_json,
			last_heartbeat_at = excluded.last_heartbeat_at,
			heartbeat_ttl_seconds = excluded.heartbeat_ttl_seconds,
			updated_at = excluded.updated_at`,
		worker.ID,
		worker.Name,
		strings.TrimSpace(worker.AdvertiseAddress),
		worker.ExecutionMode,
		worker.Status,
		strings.TrimSpace(worker.Version),
		labelsJSON,
		worker.RegisteredAt,
		worker.LastHeartbeatAt,
		worker.HeartbeatTTLSeconds,
		worker.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime worker: %w", err)
	}

	return nil
}

func (s *SQLiteStore) GetRuntimeWorker(ctx context.Context, workerID string) (RuntimeWorker, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, advertise_address, execution_mode, status, version, labels_json,
		       registered_at, last_heartbeat_at, heartbeat_ttl_seconds, updated_at
		FROM runtime_workers
		WHERE id = ?`, normalizeWorkerID(workerID))

	worker, err := scanRuntimeWorker(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RuntimeWorker{}, ErrRuntimeWorkerNotFound
		}
		return RuntimeWorker{}, err
	}

	return worker, nil
}

func (s *SQLiteStore) ListRuntimeWorkers(ctx context.Context) ([]RuntimeWorker, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, advertise_address, execution_mode, status, version, labels_json,
		       registered_at, last_heartbeat_at, heartbeat_ttl_seconds, updated_at
		FROM runtime_workers
		ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query runtime workers: %w", err)
	}
	defer rows.Close()

	workers := make([]RuntimeWorker, 0)
	for rows.Next() {
		worker, scanErr := scanRuntimeWorker(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		workers = append(workers, worker)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime workers: %w", err)
	}

	return workers, nil
}

func (s *SQLiteStore) TouchRuntimeWorkerHeartbeat(ctx context.Context, workerID string, observedAt int64, status string, advertiseAddress string, version string, labels map[string]string) error {
	worker, err := s.GetRuntimeWorker(ctx, workerID)
	if err != nil {
		return err
	}
	if observedAt == 0 {
		observedAt = time.Now().Unix()
	}
	worker.LastHeartbeatAt = observedAt
	worker.UpdatedAt = observedAt
	if strings.TrimSpace(status) != "" {
		worker.Status = strings.TrimSpace(status)
	}
	if strings.TrimSpace(advertiseAddress) != "" {
		worker.AdvertiseAddress = strings.TrimSpace(advertiseAddress)
	}
	if strings.TrimSpace(version) != "" {
		worker.Version = strings.TrimSpace(version)
	}
	if labels != nil {
		worker.Labels = labels
	}

	return s.UpsertRuntimeWorker(ctx, worker)
}

type runtimeWorkerScanner interface {
	Scan(dest ...any) error
}

func scanRuntimeWorker(scanner runtimeWorkerScanner) (RuntimeWorker, error) {
	var worker RuntimeWorker
	var labelsJSON string
	if err := scanner.Scan(
		&worker.ID,
		&worker.Name,
		&worker.AdvertiseAddress,
		&worker.ExecutionMode,
		&worker.Status,
		&worker.Version,
		&labelsJSON,
		&worker.RegisteredAt,
		&worker.LastHeartbeatAt,
		&worker.HeartbeatTTLSeconds,
		&worker.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RuntimeWorker{}, sql.ErrNoRows
		}
		return RuntimeWorker{}, fmt.Errorf("scan runtime worker row: %w", err)
	}

	labels, err := unmarshalWorkerLabels(labelsJSON)
	if err != nil {
		return RuntimeWorker{}, err
	}
	worker.Labels = labels

	return worker, nil
}

func marshalWorkerLabels(labels map[string]string) (string, error) {
	if len(labels) == 0 {
		return "{}", nil
	}
	encoded, err := json.Marshal(labels)
	if err != nil {
		return "", fmt.Errorf("marshal runtime worker labels: %w", err)
	}
	return string(encoded), nil
}

func unmarshalWorkerLabels(raw string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]string{}, nil
	}
	labels := make(map[string]string)
	if err := json.Unmarshal([]byte(trimmed), &labels); err != nil {
		return nil, fmt.Errorf("unmarshal runtime worker labels: %w", err)
	}
	return labels, nil
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

func (s *SQLiteStore) GetUserByID(ctx context.Context, userID string) (UserRecord, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, username, password_hash, role, created_at, updated_at
		 FROM users
		 WHERE id = ?`,
		strings.TrimSpace(userID),
	)

	var user UserRecord
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.CreatedAt, &user.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserRecord{}, ErrUserNotFound
		}
		return UserRecord{}, fmt.Errorf("query user by id: %w", err)
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

func (s *SQLiteStore) CreateRefreshToken(ctx context.Context, token RefreshTokenRecord) error {
	if strings.TrimSpace(token.ID) == "" {
		return errors.New("refresh token id is required")
	}
	if strings.TrimSpace(token.TokenHash) == "" {
		return errors.New("refresh token hash is required")
	}
	if strings.TrimSpace(token.UserID) == "" {
		return errors.New("refresh token user id is required")
	}
	if token.ExpiresAt <= 0 {
		return errors.New("refresh token expires_at must be greater than zero")
	}

	if token.CreatedAt == 0 {
		token.CreatedAt = time.Now().Unix()
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO refresh_tokens (id, token_hash, user_id, expires_at, created_at, rotated_at, revoked_at, replaced_by_token_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		token.ID,
		token.TokenHash,
		token.UserID,
		token.ExpiresAt,
		token.CreatedAt,
		token.RotatedAt,
		token.RevokedAt,
		token.ReplacedByTokenID,
	)
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}

	return nil
}

func (s *SQLiteStore) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (RefreshTokenRecord, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, token_hash, user_id, expires_at, created_at, rotated_at, revoked_at, replaced_by_token_id
		 FROM refresh_tokens
		 WHERE token_hash = ?`,
		strings.TrimSpace(tokenHash),
	)

	var token RefreshTokenRecord
	if err := row.Scan(
		&token.ID,
		&token.TokenHash,
		&token.UserID,
		&token.ExpiresAt,
		&token.CreatedAt,
		&token.RotatedAt,
		&token.RevokedAt,
		&token.ReplacedByTokenID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RefreshTokenRecord{}, ErrRefreshTokenNotFound
		}
		return RefreshTokenRecord{}, fmt.Errorf("query refresh token by hash: %w", err)
	}

	return token, nil
}

func (s *SQLiteStore) RotateRefreshToken(ctx context.Context, currentTokenID string, replacement RefreshTokenRecord, rotatedAt int64) error {
	if strings.TrimSpace(currentTokenID) == "" {
		return errors.New("current refresh token id is required")
	}
	if rotatedAt <= 0 {
		rotatedAt = time.Now().Unix()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin refresh token rotation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(
		ctx,
		`UPDATE refresh_tokens
		 SET rotated_at = ?, replaced_by_token_id = ?
		 WHERE id = ?
		   AND revoked_at = 0
		   AND rotated_at = 0
		   AND expires_at > ?`,
		rotatedAt,
		replacement.ID,
		strings.TrimSpace(currentTokenID),
		rotatedAt,
	)
	if err != nil {
		return fmt.Errorf("rotate refresh token: %w", err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read refresh token rotation rows affected: %w", err)
	}
	if changed == 0 {
		return ErrRefreshTokenInactive
	}

	if replacement.CreatedAt == 0 {
		replacement.CreatedAt = rotatedAt
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO refresh_tokens (id, token_hash, user_id, expires_at, created_at, rotated_at, revoked_at, replaced_by_token_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		replacement.ID,
		replacement.TokenHash,
		replacement.UserID,
		replacement.ExpiresAt,
		replacement.CreatedAt,
		replacement.RotatedAt,
		replacement.RevokedAt,
		replacement.ReplacedByTokenID,
	); err != nil {
		return fmt.Errorf("insert replacement refresh token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit refresh token rotation transaction: %w", err)
	}

	return nil
}

func (s *SQLiteStore) RevokeRefreshTokenByHash(ctx context.Context, tokenHash string, revokedAt int64) error {
	if strings.TrimSpace(tokenHash) == "" {
		return ErrRefreshTokenNotFound
	}
	if revokedAt <= 0 {
		revokedAt = time.Now().Unix()
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE refresh_tokens
		 SET revoked_at = ?
		 WHERE token_hash = ?
		   AND revoked_at = 0
		   AND rotated_at = 0
		   AND expires_at > ?`,
		revokedAt,
		strings.TrimSpace(tokenHash),
		revokedAt,
	)
	if err != nil {
		return fmt.Errorf("revoke refresh token by hash: %w", err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read refresh token revoke rows affected: %w", err)
	}
	if changed == 0 {
		return ErrRefreshTokenNotFound
	}

	return nil
}
