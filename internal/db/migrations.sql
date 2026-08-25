CREATE TABLE IF NOT EXISTS users (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    email        TEXT    NOT NULL UNIQUE,
    password_hash TEXT   NOT NULL,
    role         TEXT    NOT NULL DEFAULT 'user',
    created_at   DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id),
    filename      TEXT    NOT NULL,
    original_name TEXT    NOT NULL,
    mime_type     TEXT    NOT NULL,
    status        TEXT    NOT NULL DEFAULT 'uploaded',
    created_at    DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS pipeline_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id),
    stage      TEXT    NOT NULL,
    message    TEXT    NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS crawler_run_stats (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    source        TEXT     NOT NULL,
    started_at    DATETIME NOT NULL,
    ended_at      DATETIME NOT NULL,
    navigations   INTEGER  NOT NULL DEFAULT 0,
    history_backs INTEGER  NOT NULL DEFAULT 0,
    clicks        INTEGER  NOT NULL DEFAULT 0,
    form_fills    INTEGER  NOT NULL DEFAULT 0,
    downloads     INTEGER  NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS crawler_run_stats_started_at_idx ON crawler_run_stats(started_at);

-- v9+: pipeline LLM audit columns (applied via schema_migrations in db.go)
-- ALTER TABLE pipeline_events ADD COLUMN outcome TEXT NOT NULL DEFAULT '';
-- ALTER TABLE pipeline_events ADD COLUMN result_snippet TEXT NOT NULL DEFAULT '';
-- ALTER TABLE projects ADD COLUMN file_size INTEGER NOT NULL DEFAULT 0;
