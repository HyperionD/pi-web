package piweb

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func DefaultWebDBPath() string {
	sessionDir := DefaultPiSessionDir()
	if sessionDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionDir), "pi-web.db")
}

func openWebDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`
PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS workspaces (
  path TEXT PRIMARY KEY,
  workspace_id TEXT,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_opened_at TEXT NOT NULL,
  hidden INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_workspaces_visible_last_opened ON workspaces(hidden, last_opened_at DESC);
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, account TEXT NOT NULL UNIQUE, username TEXT NOT NULL, department TEXT NOT NULL DEFAULT '', role TEXT NOT NULL, password_hash TEXT NOT NULL, disabled INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS user_workspaces (user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, workspace_id TEXT NOT NULL, workspace_path TEXT NOT NULL, PRIMARY KEY(user_id, workspace_id));
CREATE TABLE IF NOT EXISTS app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS session_owners (session_id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE);
`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`ALTER TABLE workspaces ADD COLUMN workspace_id TEXT`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		_ = db.Close()
		return nil, err
	}
	if err := migrateWorkspaceIDs(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN account TEXT`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN department TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`UPDATE users SET account=username WHERE account IS NULL OR account=''`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_account ON users(account)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrateWorkspaceIDs(db *sql.DB) error {
	var version string
	if err := db.QueryRow(`SELECT value FROM app_settings WHERE key='workspaceCatalogModel'`).Scan(&version); err == nil && version == "v1" {
		return nil
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_workspaces_id`); err != nil {
		return err
	}
	if err := normalizeWorkspaceIDs(db); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_id ON workspaces(workspace_id) WHERE workspace_id IS NOT NULL`); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT INTO app_settings(key,value) VALUES('workspaceCatalogModel','v1') ON CONFLICT(key) DO UPDATE SET value='v1'`)
	return err
}

type workspaceIDMigration struct {
	path      string
	storedID  string
	grantedID string
}

func normalizeWorkspaceIDs(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err = normalizeWorkspacePaths(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.Exec(`
INSERT OR IGNORE INTO workspaces(path, name, created_at, last_opened_at, hidden)
SELECT DISTINCT workspace_path, workspace_path, ?, ?, 1 FROM user_workspaces
`, now, now); err != nil {
		_ = tx.Rollback()
		return err
	}
	records, err := workspaceIDMigrations(tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	used := map[string]int{}
	for _, record := range records {
		id := migratedWorkspaceID(record, used)
		if _, err = tx.Exec(`UPDATE workspaces SET workspace_id=? WHERE path=?`, id, record.path); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.Exec(`UPDATE user_workspaces SET workspace_id=? WHERE workspace_path=?`, id, record.path); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func normalizeWorkspacePaths(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT path FROM workspaces ORDER BY path`)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			_ = rows.Close()
			return err
		}
		paths = append(paths, path)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, path := range paths {
		canonical := canonicalWorkspacePath(path)
		if canonical == path {
			continue
		}
		if err := mergeWorkspacePath(tx, path, canonical); err != nil {
			return err
		}
	}
	return nil
}

func mergeWorkspacePath(tx *sql.Tx, oldPath, canonical string) error {
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE path=?`, canonical).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		if _, err := tx.Exec(`
UPDATE user_workspaces SET workspace_path=? WHERE workspace_path=?
`, canonical, oldPath); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE workspaces SET path=? WHERE path=?`, canonical, oldPath)
		return err
	}
	if _, err := tx.Exec(`
DELETE FROM user_workspaces
WHERE workspace_path=? AND EXISTS (
  SELECT 1 FROM user_workspaces kept
  WHERE kept.user_id=user_workspaces.user_id AND kept.workspace_path=?
)
`, oldPath, canonical); err != nil {
		return err
	}
	if _, err := tx.Exec(`
UPDATE user_workspaces SET workspace_path=? WHERE workspace_path=?
`, canonical, oldPath); err != nil {
		return err
	}
	if _, err := tx.Exec(`
UPDATE workspaces
SET hidden=MIN(hidden, (SELECT hidden FROM workspaces WHERE path=?)),
    last_opened_at=MAX(last_opened_at, (SELECT last_opened_at FROM workspaces WHERE path=?))
WHERE path=?
`, oldPath, oldPath, canonical); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM workspaces WHERE path=?`, oldPath)
	return err
}

func workspaceIDMigrations(tx *sql.Tx) ([]workspaceIDMigration, error) {
	rows, err := tx.Query(`
SELECT path, COALESCE(workspace_id, ''), COALESCE((
  SELECT MIN(user_workspaces.workspace_id)
  FROM user_workspaces
  WHERE user_workspaces.workspace_path = workspaces.path
), '')
FROM workspaces ORDER BY path
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []workspaceIDMigration
	for rows.Next() {
		var record workspaceIDMigration
		if err := rows.Scan(&record.path, &record.storedID, &record.grantedID); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func migratedWorkspaceID(record workspaceIDMigration, used map[string]int) string {
	id := record.storedID
	if id == "" {
		id = record.grantedID
	}
	if id == "" {
		id = slug(filepath.Base(record.path))
	}
	if id == "" {
		id = "workspace"
	}
	if used[id] > 0 {
		id += "-" + shortPathHash(record.path)
	}
	return uniqueID(id, used)
}

type webWorkspaceRecord struct {
	ID     string
	Path   string
	Recent bool
}

func LoadWebWorkspacePaths(dbPath string) []string {
	records := loadWebWorkspaceRecents(dbPath)
	if records == nil {
		return nil
	}
	paths := make([]string, 0, len(records))
	for _, record := range records {
		paths = append(paths, record.Path)
	}
	return paths
}

func loadWebWorkspaceRecents(dbPath string) []webWorkspaceRecord {
	catalog := loadWebWorkspaceCatalog(dbPath)
	if catalog == nil {
		return nil
	}
	recents := make([]webWorkspaceRecord, 0, len(catalog))
	for _, record := range catalog {
		if record.Recent {
			recents = append(recents, record)
		}
	}
	return recents
}

func loadWebWorkspaceCatalog(dbPath string) []webWorkspaceRecord {
	if dbPath == "" {
		return nil
	}
	db, err := openWebDB(dbPath)
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`
SELECT COALESCE(workspace_id, ''), path, hidden = 0
FROM workspaces
ORDER BY hidden ASC, last_opened_at DESC, path ASC
`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var records []webWorkspaceRecord
	for rows.Next() {
		var record webWorkspaceRecord
		if rows.Scan(&record.ID, &record.Path, &record.Recent) == nil {
			records = append(records, record)
		}
	}
	return records
}

func loadWebWorkspaceID(dbPath, path string) (string, error) {
	if dbPath == "" {
		return "", nil
	}
	db, err := openWebDB(dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var id sql.NullString
	err = db.QueryRow(`SELECT workspace_id FROM workspaces WHERE path = ?`, path).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id.String, err
}

func saveWebWorkspaceOpened(dbPath string, record webWorkspaceRecord) error {
	if dbPath == "" {
		return nil
	}
	db, err := openWebDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`
INSERT INTO workspaces(path, workspace_id, name, created_at, last_opened_at, hidden)
VALUES (?, ?, ?, ?, ?, 0)
ON CONFLICT(path) DO UPDATE SET
  workspace_id=excluded.workspace_id,
  name=excluded.name,
  last_opened_at=excluded.last_opened_at,
  hidden=0
`, record.Path, record.ID, filepath.Base(record.Path), now, now)
	return err
}

func deleteWebWorkspaceRegistration(dbPath, path string) error {
	db, err := openWebDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DELETE FROM workspaces WHERE path=? AND hidden=1`, path)
	return err
}

func saveWebWorkspaceClosed(dbPath, path string) error {
	if dbPath == "" {
		return nil
	}
	db, err := openWebDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`UPDATE workspaces SET hidden=1 WHERE path=?`, path)
	return err
}

func saveWebWorkspaceRegistered(dbPath string, record webWorkspaceRecord) error {
	if dbPath == "" {
		return nil
	}
	db, err := openWebDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`
INSERT INTO workspaces(path, workspace_id, name, created_at, last_opened_at, hidden)
VALUES (?, ?, ?, ?, ?, 1)
ON CONFLICT(path) DO UPDATE SET
  workspace_id=COALESCE(workspaces.workspace_id, excluded.workspace_id),
  name=excluded.name
`, record.Path, record.ID, filepath.Base(record.Path), now, now)
	return err
}

func SaveWebWorkspacePaths(dbPath string, paths []string) error {
	records := make([]webWorkspaceRecord, 0, len(paths))
	for _, path := range paths {
		records = append(records, webWorkspaceRecord{Path: path})
	}
	return saveWebWorkspaceRecents(dbPath, records)
}

func saveWebWorkspaceRecents(dbPath string, records []webWorkspaceRecord) error {
	if dbPath == "" {
		return nil
	}
	db, err := openWebDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`UPDATE workspaces SET hidden = 1`); err != nil {
		_ = tx.Rollback()
		return err
	}
	for i, record := range records {
		lastOpened := now
		if i > 0 {
			lastOpened = time.Now().UTC().Add(-time.Duration(i) * time.Nanosecond).Format(time.RFC3339Nano)
		}
		if _, err := tx.Exec(`
INSERT INTO workspaces(path, workspace_id, name, created_at, last_opened_at, hidden)
VALUES (?, NULLIF(?, ''), ?, ?, ?, 0)
ON CONFLICT(path) DO UPDATE SET
  workspace_id = COALESCE(excluded.workspace_id, workspaces.workspace_id),
  name = excluded.name,
  last_opened_at = excluded.last_opened_at,
  hidden = 0
`, record.Path, record.ID, filepath.Base(record.Path), now, lastOpened); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// PrepareUserManagementDB performs the one-time reset required by the new authorization model.
// It intentionally keeps Pi session files on disk; only the application index and permissions reset.
func PrepareUserManagementDB() error {
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	var version string
	if err := db.QueryRow(`SELECT value FROM app_settings WHERE key='authorizationModel'`).Scan(&version); err == nil && version == "v1" {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, table := range []string{"user_workspaces", "session_owners", "users", "workspaces", "app_settings"} {
		if _, err = tx.Exec(`DELETE FROM ` + table); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	_, err = tx.Exec(`INSERT INTO app_settings(key,value) VALUES('authorizationModel','v1')`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
