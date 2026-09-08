package piweb

import "testing"

func TestWorkspaceAccessIgnoresInvalidGrantForValidSession(t *testing.T) {
	s, admin := accountServer(t)
	workspace, err := s.store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cookie := addAccount(t, s, admin, "alice", workspace.ID)
	login, ok := s.accounts.authenticate(cookie.Value)
	if !ok {
		t.Fatal("alice login missing")
	}
	session := createAccountSession(t, s, cookie, workspace.ID)

	s.accounts.mu.Lock()
	record := s.accounts.Users["alice"]
	record.Workspaces = append(record.Workspaces, "missing")
	record.Paths["missing"] = t.TempDir() + "/gone"
	s.accounts.Users["alice"] = record
	s.accounts.mu.Unlock()

	workspaces, err := s.workspaceAccess.List(&login.User)
	if err != nil || len(workspaces) != 1 || workspaces[0].ID != workspace.ID {
		t.Fatalf("invalid grant blocked valid workspace: %#v %v", workspaces, err)
	}
	if _, err := s.workspaceAccess.ResolveSession(&login.User, session.ID); err != nil {
		t.Fatalf("invalid grant blocked valid session: %v", err)
	}
}
