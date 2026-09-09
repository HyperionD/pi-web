package piweb

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestBatchAssignWorkspacePathAddsGrantToSelectedUsers(t *testing.T) {
	s, admin := accountServer(t)
	initialPath := t.TempDir()
	for _, username := range []string{"alice", "bob"} {
		created := accountRequest(s, "POST", "/api/admin/users", `{"account":"`+username+`","username":"`+username+`","password":"password-123","workspacePaths":["`+initialPath+`"]}`, admin)
		if created.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", username, created.Code, created.Body.String())
		}
	}
	workspacePath := t.TempDir()
	body := `{"usernames":["alice","bob"],"workspaces":[],"workspacePaths":["` + workspacePath + `"]}`
	response := accountRequest(s, "POST", "/api/admin/users/batch-workspaces", body, admin)
	if response.Code != http.StatusOK {
		t.Fatalf("batch assign: %d %s", response.Code, response.Body.String())
	}
	var result struct {
		Updated int `json:"updated"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Updated != 2 {
		t.Fatalf("updated = %d, want 2", result.Updated)
	}
	for _, username := range []string{"alice", "bob"} {
		record := s.accounts.Users[username]
		if len(record.Paths) != 2 || !hasWorkspacePath(record.Paths, initialPath) || !hasWorkspacePath(record.Paths, workspacePath) {
			t.Fatalf("%s grants = %#v", username, record.Paths)
		}
	}
}

func hasWorkspacePath(paths map[string]string, expected string) bool {
	for _, path := range paths {
		if path == expected {
			return true
		}
	}
	return false
}
