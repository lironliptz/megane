---
description: >
  Add a new database table (and optional query helpers) to the SQLite schema.
  Use when the user says "store X in the database", "I need a new table for Y",
  "add persistence for Z", or "track W across sessions".
---

## What you do

Add a migration, write typed query helpers, and (optionally) expose a JSON API for the new table.

## Files to touch

| File | Change |
|------|--------|
| `internal/db/migrations.sql` | Append `CREATE TABLE IF NOT EXISTS …` |
| `internal/db/db.go` | Add query methods on `*DB` |
| `internal/handlers/` or `internal/admin/` | (optional) New API endpoint |
| `internal/handlers/router.go` | (optional) Register the endpoint |

## Rules for migrations

1. **Append only** — never edit an existing `CREATE TABLE` statement. SQLite runs the file on
   startup via a versioned migration runner (`schema_migrations` table); existing statements are
   already applied.
2. Always use `CREATE TABLE IF NOT EXISTS` — safe to re-run.
3. Use `REFERENCES` for FK relationships but don't add `FOREIGN KEY PRAGMA` — SQLite FK
   enforcement is off by default in this template.
4. Use `DATETIME NOT NULL` for timestamps and `INTEGER NOT NULL DEFAULT 0` for counters.

### Migration template

```sql
CREATE TABLE IF NOT EXISTS my_entity (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id),
    project_id  INTEGER REFERENCES projects(id),   -- nullable FK
    kind        TEXT    NOT NULL,
    value       TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS my_entity_user_id_idx ON my_entity(user_id);
```

## Writing query helpers

Add methods to the `DB` struct in `internal/db/db.go`. Follow the existing pattern:

```go
// MyEntity mirrors the my_entity DB row.
type MyEntity struct {
    ID        int64     `json:"id"`
    UserID    int64     `json:"user_id"`
    ProjectID *int64    `json:"project_id,omitempty"`
    Kind      string    `json:"kind"`
    Value     string    `json:"value"`
    CreatedAt time.Time `json:"created_at"`
}

func (d *DB) InsertMyEntity(userID int64, projectID *int64, kind, value string) (int64, error) {
    res, err := d.db.Exec(
        `INSERT INTO my_entity (user_id, project_id, kind, value, created_at) VALUES (?,?,?,?,?)`,
        userID, projectID, kind, value, time.Now().UTC(),
    )
    if err != nil {
        return 0, err
    }
    return res.LastInsertId()
}

func (d *DB) GetMyEntitiesByUser(userID int64) ([]MyEntity, error) {
    rows, err := d.db.Query(
        `SELECT id, user_id, project_id, kind, value, created_at
         FROM my_entity WHERE user_id = ? ORDER BY created_at DESC`, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []MyEntity
    for rows.Next() {
        var e MyEntity
        if err := rows.Scan(&e.ID, &e.UserID, &e.ProjectID, &e.Kind, &e.Value, &e.CreatedAt); err != nil {
            return nil, err
        }
        out = append(out, e)
    }
    return out, rows.Err()
}
```

## Exposing via API

To add a read endpoint (user-scoped):

```go
// internal/handlers/myentity.go
func (h *Handlers) GetMyEntities(c *gin.Context) {
    userID := auth.UserIDFromCtx(c)
    items, err := h.DB.GetMyEntitiesByUser(userID)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }
    c.JSON(200, gin.H{"data": items, "error": nil})
}
```

Register in `internal/handlers/router.go` inside the `/api` group:
```go
api.GET("/my-entities", h.GetMyEntities)
```

For admin-only data, use `internal/admin/admin.go` + the `/api/admin/*` group with `auth.AdminRequired()`.

## Verify

```bash
go build ./...   # confirm clean
make run         # migrations run at startup; check log for errors
# Use test-pipeline skill or curl to exercise the new endpoint
```

To inspect the schema after migration:
```
!sqlite3 ./.db/jump-starter.db ".schema my_entity"
```
