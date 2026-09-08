package piweb

import (
	"path/filepath"
	"testing"
)

func TestAssignedWorkspaceAllowsItsStorePath(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil { t.Fatal(err) }
	cookie := addAccount(t, s, admin, "alice", workspace.ID)
	user, ok := s.accounts.authenticate(cookie.Value)
	if !ok { t.Fatal("login missing") }
	storePath, err := s.store.WorkspacePath(workspace.ID)
	if err != nil { t.Fatal(err) }
	record := s.accounts.Users["alice"]
	t.Logf("workspace=%q assigned=%q", storePath, record.Paths[workspace.ID])
	if filepath.Clean(storePath) != filepath.Clean(record.Paths[workspace.ID]) { t.Fatalf("path mismatch") }
	if !s.accounts.allows(user.User, workspace.ID, storePath) { t.Fatal("assigned workspace denied") }
}
