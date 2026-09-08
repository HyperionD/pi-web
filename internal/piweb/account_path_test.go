package piweb

import (
	"path/filepath"
	"testing"
)

func TestAssignedWorkspaceAllowsItsStorePath(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	cookie := addAccount(t, s, admin, "alice", workspace.ID)
	user, ok := s.accounts.authenticate(cookie.Value)
	if !ok {
		t.Fatal("login missing")
	}
	storePath, err := s.store.WorkspacePath(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	record := s.accounts.Users["alice"]
	if filepath.Clean(storePath) != filepath.Clean(record.Paths[workspace.ID]) {
		t.Fatalf("path mismatch")
	}
	if !s.accounts.allows(user.User, workspace.ID, storePath) {
		t.Fatal("assigned workspace denied")
	}
	if response := accountRequest(s, "GET", "/api/workspaces/"+workspace.ID+"/settings", "", cookie); response.Code != 200 {
		t.Fatalf("assigned workspace settings should be readable: %d %s", response.Code, response.Body.String())
	}
	if response := accountRequest(s, "PUT", "/api/workspaces/"+workspace.ID+"/settings", `{}`, cookie); response.Code != 403 {
		t.Fatalf("workspace settings mutation should remain admin-only: %d", response.Code)
	}
}
