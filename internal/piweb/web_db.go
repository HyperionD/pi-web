package piweb

import (
	"database/sql"
	"os"
	"path/filepath"
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
	if _, err := db.Exec(`
PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS workspaces (
  path TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_opened_at TEXT NOT NULL,
  hidden INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_workspaces_visible_last_opened ON workspaces(hidden, last_opened_at DESC);
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, role TEXT NOT NULL, password_hash TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS user_workspaces (user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, workspace_id TEXT NOT NULL, workspace_path TEXT NOT NULL, PRIMARY KEY(user_id, workspace_id));
CREATE TABLE IF NOT EXISTS app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS session_owners (session_id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS user_workspaces (
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL,
  workspace_path TEXT NOT NULL,
  PRIMARY KEY(user_id, workspace_id)
);
CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS session_owners (
  session_id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE
);
`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func LoadWebWorkspacePaths(dbPath string) []string {
	if dbPath == "" {
		return nil
	}
	db, err := openWebDB(dbPath)
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`SELECT path FROM workspaces WHERE hidden = 0 ORDER BY last_opened_at DESC, path ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if rows.Scan(&path) == nil {
			paths = append(paths, path)
		}
	}
	return paths
}

func SaveWebWorkspacePaths(dbPath string, paths []string) error {
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
	for i, path := range paths {
		lastOpened := now
		if i > 0 {
			lastOpened = time.Now().UTC().Add(-time.Duration(i) * time.Nanosecond).Format(time.RFC3339Nano)
		}
		if _, err := tx.Exec(`
INSERT INTO workspaces(path, name, created_at, last_opened_at, hidden)
VALUES (?, ?, ?, ?, 0)
ON CONFLICT(path) DO UPDATE SET name = excluded.name, last_opened_at = excluded.last_opened_at, hidden = 0
`, path, filepath.Base(path), now, lastOpened); err != nil {
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
