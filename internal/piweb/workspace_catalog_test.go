package piweb

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceIDMigrationMergesLegacySymlinkPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "pi-web.db")
	db, err := openWebDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
DROP INDEX idx_workspaces_id;
DELETE FROM app_settings WHERE key='workspaceCatalogModel'
`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`
INSERT INTO users(id,account,username,role,password_hash,created_at)
VALUES('user-id','user','user','user','hash',?)
`, now); err != nil {
		t.Fatal(err)
	}
	for _, record := range []struct {
		path   string
		id     string
		hidden int
	}{{target, "target-id", 1}, {link, "legacy-id", 0}} {
		if _, err := db.Exec(`
INSERT INTO workspaces(path,workspace_id,name,created_at,last_opened_at,hidden)
VALUES(?,?,?,?,?,?)
`, record.path, record.id, "target", now, now, record.hidden); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
INSERT INTO user_workspaces(user_id,workspace_id,workspace_path)
VALUES('user-id','legacy-id',?)
`, link); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = openWebDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var workspaceCount int
	var workspaceID, grantPath, grantID string
	if err := db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE path=?`, target).Scan(&workspaceCount); err != nil {
		t.Fatal(err)
	}
	if workspaceCount != 1 {
		t.Fatalf("canonical workspace count = %d", workspaceCount)
	}
	if err := db.QueryRow(`SELECT workspace_id FROM workspaces WHERE path=?`, target).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
SELECT workspace_id,workspace_path FROM user_workspaces WHERE user_id='user-id'
`).Scan(&grantID, &grantPath); err != nil {
		t.Fatal(err)
	}
	if workspaceID != grantID || grantPath != target {
		t.Fatalf("merged workspace = %q/%q, grant = %q/%q", workspaceID, target, grantID, grantPath)
	}
}

func TestWorkspaceIDMigrationRepairsLegacyCollisions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pi-web.db")
	db, err := openWebDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX idx_workspaces_id`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM app_settings WHERE key='workspaceCatalogModel'`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	paths := []string{filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "project")}
	for index, path := range paths {
		userID := []string{"alice-id", "bob-id"}[index]
		account := []string{"alice", "bob"}[index]
		if _, err := db.Exec(`
INSERT INTO workspaces(path,name,created_at,last_opened_at,hidden)
VALUES(?,?,?,?,?)
`, path, "project", now, now, index); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
INSERT INTO users(id,account,username,role,password_hash,created_at)
VALUES(?,?,?,?,?,?)
`, userID, account, account, "user", "hash", now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
INSERT INTO user_workspaces(user_id,workspace_id,workspace_path)
VALUES(?,?,?)
`, userID, "project", path); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = openWebDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ids := map[string]bool{}
	for _, path := range paths {
		var workspaceID, grantID string
		if err := db.QueryRow(`SELECT workspace_id FROM workspaces WHERE path=?`, path).Scan(&workspaceID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`
SELECT workspace_id FROM user_workspaces WHERE workspace_path=?
`, path).Scan(&grantID); err != nil {
			t.Fatal(err)
		}
		if workspaceID == "" || workspaceID != grantID || ids[workspaceID] {
			t.Fatalf("invalid migrated IDs: workspace=%q grant=%q seen=%v", workspaceID, grantID, ids)
		}
		ids[workspaceID] = true
	}
}
