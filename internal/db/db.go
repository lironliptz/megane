package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DefaultSQLitePathRelative is DB_PATH when unset: single SQLite file under ./.db/
// (repository root; keep WAL/SHM siblings next to the main file).
const DefaultSQLitePathRelative = "./.db/jump-starter.db"

// DefaultSQLitePathForSlug returns the default DB_PATH for a sprout (Jump-Start generated .env).
func DefaultSQLitePathForSlug(slug string) string {
	return "./.db/" + strings.TrimSpace(slug) + ".db"
}

// ResolvePath returns path when non-empty, otherwise DefaultSQLitePathRelative.
func ResolvePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return DefaultSQLitePathRelative
	}
	return path
}

// EnsureSQLiteDir creates the parent directory for a SQLite file path (no-op for :memory:).
func EnsureSQLiteDir(dbPath string) error {
	if strings.TrimSpace(dbPath) == "" || dbPath == ":memory:" {
		return nil
	}
	dir := filepath.Dir(dbPath)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir sqlite dir %q: %w", dir, err)
	}
	return nil
}

// versionedMigrations is the source of truth for schema history.
// Add new entries at the end; never edit existing ones.
var versionedMigrations = []struct {
	version int
	sql     string
}{
	{1, `CREATE TABLE IF NOT EXISTS users (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		email         TEXT    NOT NULL UNIQUE,
		password_hash TEXT    NOT NULL,
		role          TEXT    NOT NULL DEFAULT 'user',
		created_at    DATETIME NOT NULL
	)`},
	{2, `CREATE TABLE IF NOT EXISTS projects (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id       INTEGER NOT NULL REFERENCES users(id),
		filename      TEXT    NOT NULL,
		original_name TEXT    NOT NULL,
		mime_type     TEXT    NOT NULL,
		status        TEXT    NOT NULL DEFAULT 'uploaded',
		created_at    DATETIME NOT NULL
	)`},
	{3, `CREATE TABLE IF NOT EXISTS pipeline_events (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER NOT NULL REFERENCES projects(id),
		stage      TEXT    NOT NULL,
		message    TEXT    NOT NULL,
		created_at DATETIME NOT NULL
	)`},
	{4, `ALTER TABLE projects ADD COLUMN llm_model TEXT NOT NULL DEFAULT ''`},
	{5, `ALTER TABLE projects ADD COLUMN completed_at DATETIME`},
	{6, `UPDATE projects SET completed_at = (
		SELECT MAX(e.created_at) FROM pipeline_events e
		WHERE e.project_id = projects.id AND e.stage = 'complete'
	) WHERE status = 'complete' AND completed_at IS NULL`},
	{7, `CREATE TABLE IF NOT EXISTS crawler_run_stats (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		source        TEXT     NOT NULL,
		started_at    DATETIME NOT NULL,
		ended_at      DATETIME NOT NULL,
		navigations   INTEGER  NOT NULL DEFAULT 0,
		history_backs INTEGER  NOT NULL DEFAULT 0,
		clicks        INTEGER  NOT NULL DEFAULT 0,
		form_fills    INTEGER  NOT NULL DEFAULT 0,
		downloads     INTEGER  NOT NULL DEFAULT 0
	)`},
	{8, `CREATE INDEX IF NOT EXISTS crawler_run_stats_started_at_idx ON crawler_run_stats(started_at)`},
	{9, `ALTER TABLE pipeline_events ADD COLUMN outcome TEXT NOT NULL DEFAULT ''`},
	{10, `ALTER TABLE pipeline_events ADD COLUMN result_snippet TEXT NOT NULL DEFAULT ''`},
	{11, `ALTER TABLE projects ADD COLUMN file_size INTEGER NOT NULL DEFAULT 0`},
	{12, `CREATE TABLE IF NOT EXISTS dm_file_types (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT    NOT NULL,
		slug       TEXT    NOT NULL UNIQUE,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`},
	{13, `CREATE TABLE IF NOT EXISTS dm_schemas (
		id                INTEGER PRIMARY KEY AUTOINCREMENT,
		file_type_id      INTEGER NOT NULL REFERENCES dm_file_types(id),
		version           INTEGER NOT NULL,
		schema_json       TEXT    NOT NULL,
		stats_json        TEXT    NOT NULL DEFAULT '{}',
		convergence_score REAL    NOT NULL DEFAULT 0.0,
		is_finalized      INTEGER NOT NULL DEFAULT 0,
		created_at        DATETIME NOT NULL
	)`},
	{14, `CREATE UNIQUE INDEX IF NOT EXISTS dm_schemas_type_version ON dm_schemas(file_type_id, version)`},
	{15, `CREATE TABLE IF NOT EXISTS dm_analyses (
		id                 INTEGER PRIMARY KEY AUTOINCREMENT,
		file_type_id       INTEGER NOT NULL REFERENCES dm_file_types(id),
		schema_version     INTEGER NOT NULL,
		file_id            INTEGER NOT NULL REFERENCES projects(id),
		result_json        TEXT    NOT NULL,
		manual_answers     TEXT    NOT NULL DEFAULT '[]',
		processing_time_ms INTEGER NOT NULL DEFAULT 0,
		llm_token_usage    TEXT    NOT NULL DEFAULT '{}',
		created_at         DATETIME NOT NULL
	)`},
	{16, `CREATE INDEX IF NOT EXISTS dm_analyses_file_type_idx ON dm_analyses(file_type_id, created_at DESC)`},
	{17, `CREATE TABLE IF NOT EXISTS dm_convergence_metrics (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		file_type_id  INTEGER NOT NULL REFERENCES dm_file_types(id),
		metric_type   TEXT    NOT NULL,
		value         REAL    NOT NULL,
		recorded_at   DATETIME NOT NULL
	)`},
	{18, `CREATE INDEX IF NOT EXISTS dm_convergence_metrics_type_idx ON dm_convergence_metrics(file_type_id, metric_type, recorded_at)`},
	{19, `CREATE TABLE IF NOT EXISTS dm_jobs (
		id           TEXT PRIMARY KEY,
		status       TEXT NOT NULL DEFAULT 'pending',
		request_json TEXT NOT NULL,
		result_json  TEXT NOT NULL DEFAULT '',
		error_json   TEXT NOT NULL DEFAULT '',
		created_at   DATETIME NOT NULL,
		updated_at   DATETIME NOT NULL
	)`},
	{20, `CREATE INDEX IF NOT EXISTS dm_jobs_status_idx ON dm_jobs(status, created_at DESC)`},
	{21, `ALTER TABLE dm_jobs ADD COLUMN progress_json TEXT NOT NULL DEFAULT '[]'`},
	{22, `ALTER TABLE dm_file_types ADD COLUMN extraction_brief TEXT NOT NULL DEFAULT ''`},
	{23, `ALTER TABLE dm_file_types ADD COLUMN field_hints_json TEXT NOT NULL DEFAULT '[]'`},
	{24, `ALTER TABLE dm_file_types ADD COLUMN excluded_fields_json TEXT NOT NULL DEFAULT '[]'`},
	{25, `CREATE TABLE IF NOT EXISTS dm_type_samples (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		file_type_id    INTEGER NOT NULL REFERENCES dm_file_types(id),
		file_id         INTEGER NOT NULL REFERENCES projects(id),
		original_name   TEXT    NOT NULL,
		size_bytes      INTEGER NOT NULL DEFAULT 0,
		content_hash    TEXT    NOT NULL DEFAULT '',
		first_seen_at   DATETIME NOT NULL,
		last_seen_at    DATETIME NOT NULL,
		analysis_count  INTEGER NOT NULL DEFAULT 1
	)`},
	{26, `CREATE INDEX IF NOT EXISTS dm_type_samples_type_idx ON dm_type_samples(file_type_id, last_seen_at DESC)`},
	{27, `CREATE UNIQUE INDEX IF NOT EXISTS dm_type_samples_type_hash_uq ON dm_type_samples(file_type_id, content_hash) WHERE content_hash != ''`},
	{28, `INSERT INTO dm_type_samples (file_type_id, file_id, original_name, size_bytes, content_hash, first_seen_at, last_seen_at, analysis_count)
		SELECT a.file_type_id, MIN(a.file_id), p.original_name, COALESCE(p.file_size, 0), '', MIN(a.created_at), MAX(a.created_at), COUNT(*)
		FROM dm_analyses a
		JOIN projects p ON p.id = a.file_id
		GROUP BY a.file_type_id, p.original_name, COALESCE(p.file_size, 0)`},
	{29, `CREATE TABLE IF NOT EXISTS dm_build_configs (
		id                      INTEGER PRIMARY KEY AUTOINCREMENT,
		file_type_id            INTEGER NOT NULL UNIQUE REFERENCES dm_file_types(id),
		status                  TEXT    NOT NULL DEFAULT 'draft',
		field_overrides_json    TEXT    NOT NULL DEFAULT '[]',
		type_rules              TEXT    NOT NULL DEFAULT '',
		production_prompt       TEXT    NOT NULL DEFAULT '',
		mime_types_json         TEXT    NOT NULL DEFAULT '[]',
		built_at                DATETIME,
		built_artifacts_json    TEXT    NOT NULL DEFAULT '{}',
		updated_at              DATETIME NOT NULL DEFAULT (datetime('now'))
	)`},
	{30, `CREATE INDEX IF NOT EXISTS idx_dm_build_configs_file_type ON dm_build_configs(file_type_id)`},
	{31, `CREATE INDEX IF NOT EXISTS idx_dm_build_configs_status    ON dm_build_configs(status)`},
}

// DB wraps sql.DB with helper methods.
type DB struct {
	*sql.DB
}

// User represents a user row.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

// Project represents an uploaded project/file row.
type Project struct {
	ID           int64
	UserID       int64
	Filename     string
	OriginalName string
	MimeType     string
	LLMModel     string `json:"llm_model"` // Gemini model id; empty = server default
	Status       string
	FileSize     int64      `json:"file_size"`
	CreatedAt    time.Time
	CompletedAt  *time.Time `json:"completed_at,omitempty"` // set when status is complete or error
}

// PipelineEvent represents a single event in the pipeline lifecycle.
type PipelineEvent struct {
	ID            int64
	ProjectID     int64
	Stage         string
	Message       string
	Outcome       string `json:"outcome,omitempty"`
	ResultSnippet string `json:"result_snippet,omitempty"`
	CreatedAt     time.Time
}

// Open opens the SQLite database and runs migrations.
func Open(path string) (*DB, error) {
	if err := EnsureSQLiteDir(path); err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open("sqlite3", path+"?_journal=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	d := &DB{sqlDB}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func (d *DB) migrate() error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, m := range versionedMigrations {
		var exists int
		_ = d.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, m.version).Scan(&exists)
		if exists == 1 {
			continue
		}
		if _, err := d.Exec(m.sql); err != nil {
			// ALTER TABLE "duplicate column" is expected on existing DBs — treat as applied.
			if isDuplicateColumn(err) {
				// fall through to record it
			} else {
				return fmt.Errorf("migration v%d: %w", m.version, err)
			}
		}
		if _, err := d.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, m.version, time.Now().UTC()); err != nil {
			return fmt.Errorf("record migration v%d: %w", m.version, err)
		}
	}
	return nil
}

func isDuplicateColumn(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}

// CreateUser inserts a new user and returns the inserted ID.
func (d *DB) CreateUser(email, passwordHash, role string) (int64, error) {
	res, err := d.Exec(
		`INSERT INTO users (email, password_hash, role, created_at) VALUES (?, ?, ?, ?)`,
		email, passwordHash, role, time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetUserByEmail returns the user with the given email, or sql.ErrNoRows.
func (d *DB) GetUserByEmail(email string) (*User, error) {
	row := d.QueryRow(
		`SELECT id, email, password_hash, role, created_at FROM users WHERE email = ?`, email,
	)
	u := &User{}
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserByID returns the user with the given ID.
func (d *DB) GetUserByID(id int64) (*User, error) {
	row := d.QueryRow(
		`SELECT id, email, password_hash, role, created_at FROM users WHERE id = ?`, id,
	)
	u := &User{}
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, nil
}

// GetAllUsers returns all users.
func (d *DB) GetAllUsers() ([]*User, error) {
	rows, err := d.Query(`SELECT id, email, password_hash, role, created_at FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// DeleteUser deletes a user by ID.
func (d *DB) DeleteUser(id int64) error {
	_, err := d.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// UserCount returns the total number of users.
func (d *DB) UserCount() (int, error) {
	var count int
	err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// CreateProject inserts a new project row. llmModel may be empty to use the server default.
func (d *DB) CreateProject(userID int64, filename, originalName, mimeType, llmModel string, fileSize int64) (int64, error) {
	res, err := d.Exec(
		`INSERT INTO projects (user_id, filename, original_name, mime_type, llm_model, file_size, status, created_at) VALUES (?, ?, ?, ?, ?, ?, 'uploaded', ?)`,
		userID, filename, originalName, mimeType, llmModel, fileSize, time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetProjectsByUser returns all projects for a given user.
func (d *DB) GetProjectsByUser(userID int64) ([]*Project, error) {
	rows, err := d.Query(
		`SELECT id, user_id, filename, original_name, mime_type, llm_model, status, file_size, created_at, completed_at FROM projects WHERE user_id = ? ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjects(rows)
}

// GetAllProjects returns all projects across all users.
func (d *DB) GetAllProjects() ([]*Project, error) {
	rows, err := d.Query(
		`SELECT id, user_id, filename, original_name, mime_type, llm_model, status, file_size, created_at, completed_at FROM projects ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjects(rows)
}

// GetProjectByID returns a single project.
func (d *DB) GetProjectByID(id int64) (*Project, error) {
	row := d.QueryRow(
		`SELECT id, user_id, filename, original_name, mime_type, llm_model, status, file_size, created_at, completed_at FROM projects WHERE id = ?`, id,
	)
	p := &Project{}
	var completed sql.NullTime
	if err := row.Scan(&p.ID, &p.UserID, &p.Filename, &p.OriginalName, &p.MimeType, &p.LLMModel, &p.Status, &p.FileSize, &p.CreatedAt, &completed); err != nil {
		return nil, err
	}
	if completed.Valid {
		t := completed.Time
		p.CompletedAt = &t
	}
	return p, nil
}

// UpdateProjectStatus sets the status column for a project.
// Terminal statuses (complete, error) record completed_at; other statuses clear it.
func (d *DB) UpdateProjectStatus(projectID int64, status string) error {
	switch status {
	case "complete", "error":
		_, err := d.Exec(`UPDATE projects SET status = ?, completed_at = ? WHERE id = ?`, status, time.Now().UTC(), projectID)
		return err
	default:
		_, err := d.Exec(`UPDATE projects SET status = ?, completed_at = NULL WHERE id = ?`, status, projectID)
		return err
	}
}

// DeleteProject removes a project and its pipeline events.
func (d *DB) DeleteProject(projectID int64) error {
	if _, err := d.Exec(`DELETE FROM pipeline_events WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	res, err := d.Exec(`DELETE FROM projects WHERE id = ?`, projectID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AddEvent appends a pipeline event row.
func (d *DB) AddEvent(projectID int64, stage, message string) error {
	return d.AddEventEx(projectID, stage, message, "", "")
}

// AddEventEx appends a pipeline event with optional outcome and result snippet (LLM audit JSON).
func (d *DB) AddEventEx(projectID int64, stage, message, outcome, resultSnippet string) error {
	_, err := d.Exec(
		`INSERT INTO pipeline_events (project_id, stage, message, outcome, result_snippet, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		projectID, stage, message, outcome, resultSnippet, time.Now().UTC(),
	)
	return err
}

// GetEvents returns all pipeline events for a project in chronological order.
func (d *DB) GetEvents(projectID int64) ([]*PipelineEvent, error) {
	rows, err := d.Query(
		`SELECT id, project_id, stage, message, outcome, result_snippet, created_at FROM pipeline_events WHERE project_id = ? ORDER BY created_at ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*PipelineEvent
	for rows.Next() {
		e := &PipelineEvent{}
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Stage, &e.Message, &e.Outcome, &e.ResultSnippet, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetRecentEvents returns the last N events across all projects.
func (d *DB) GetRecentEvents(limit int) ([]*PipelineEvent, error) {
	rows, err := d.Query(
		`SELECT id, project_id, stage, message, outcome, result_snippet, created_at FROM pipeline_events ORDER BY created_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*PipelineEvent
	for rows.Next() {
		e := &PipelineEvent{}
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Stage, &e.Message, &e.Outcome, &e.ResultSnippet, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// ProjectCountByStatus returns a map of status -> count.
func (d *DB) ProjectCountByStatus() (map[string]int, error) {
	rows, err := d.Query(`SELECT status, COUNT(*) FROM projects GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		result[status] = count
	}
	return result, rows.Err()
}

func scanProjects(rows *sql.Rows) ([]*Project, error) {
	var projects []*Project
	for rows.Next() {
		p := &Project{}
		var completed sql.NullTime
		if err := rows.Scan(&p.ID, &p.UserID, &p.Filename, &p.OriginalName, &p.MimeType, &p.LLMModel, &p.Status, &p.FileSize, &p.CreatedAt, &completed); err != nil {
			return nil, err
		}
		if completed.Valid {
			t := completed.Time
			p.CompletedAt = &t
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}
