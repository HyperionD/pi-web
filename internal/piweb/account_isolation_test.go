package piweb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func addAccount(t *testing.T, s *Server, admin *http.Cookie, username, workspaceID string) *http.Cookie {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":"test-password","workspaces":[%q]}`, username, workspaceID)
	w := accountRequest(s, "POST", "/api/admin/users", body, admin)
	if w.Code != 201 {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}
	w = accountRequest(s, "POST", "/api/account/login", fmt.Sprintf(`{"username":%q,"password":"test-password"}`, username), nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func createAccountSession(t *testing.T, s *Server, cookie *http.Cookie, workspaceID string) Session {
	t.Helper()
	w := accountRequest(s, "POST", "/api/workspaces/"+workspaceID+"/sessions", `{}`, cookie)
	if w.Code != 201 {
		t.Fatalf("session: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Session Session `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Session
}

func TestAccountSessionIsolationAndPersistence(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	alice := addAccount(t, s, admin, "alice", workspace.ID)
	bob := addAccount(t, s, admin, "bob", workspace.ID)
	aSession := createAccountSession(t, s, alice, workspace.ID)
	bSession := createAccountSession(t, s, bob, workspace.ID)
	for _, suffix := range []string{"", "/events", "/context-usage"} {
		w := accountRequest(s, "GET", "/api/sessions/"+aSession.ID+suffix, "", bob)
		if w.Code != 404 {
			t.Fatalf("read other session %s: %d", suffix, w.Code)
		}
	}
	for _, method := range []string{"PATCH", "DELETE"} {
		w := accountRequest(s, method, "/api/sessions/"+aSession.ID, `{"title":"stolen"}`, bob)
		if w.Code != 404 {
			t.Fatalf("mutate other session: %d", w.Code)
		}
	}
	for _, suffix := range []string{"prompt", "ag-ui", "steer", "cancel", "compact", "model"} {
		if w := accountRequest(s, "POST", "/api/sessions/"+aSession.ID+"/"+suffix, `{}`, bob); w.Code != 404 {
			t.Fatalf("%s: %d", suffix, w.Code)
		}
	}
	for _, path := range []string{"/api/workspaces", "/api/workspaces/" + workspace.ID + "/sessions"} {
		w := accountRequest(s, "GET", path, "", alice)
		if w.Code != 200 || !containsSession(w.Body.String(), aSession.ID) || containsSession(w.Body.String(), bSession.ID) {
			t.Fatalf("session listing leak: %s", w.Body.String())
		}
	}
	reloaded := newAccountStore()
	if reloaded.err != nil || !reloaded.owns(reloaded.Users["alice"].Account, aSession.ID) {
		t.Fatal("ownership not persisted")
	}
	w := accountRequest(s, "DELETE", "/api/workspaces/"+workspace.ID+"/sessions", "", alice)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = accountRequest(s, "GET", "/api/sessions/"+bSession.ID, "", bob); w.Code != 200 {
		t.Fatal("bulk deletion affected another user")
	}
	oldLogin, _ := s.accounts.authenticate(bob.Value)
	w = accountRequest(s, "PATCH", "/api/admin/users/bob", `{"workspaces":[]}`, admin)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	select {
	case <-oldLogin.Context.Done():
	default:
		t.Fatal("in-flight requests not revoked")
	}
	if w = accountRequest(s, "GET", "/api/workspaces", "", bob); w.Code != 401 {
		t.Fatal("assignment change did not revoke login")
	}
}

func containsSession(body, id string) bool {
	var value any
	if json.Unmarshal([]byte(body), &value) != nil {
		return false
	}
	return containsJSONID(value, id)
}

func containsJSONID(value any, id string) bool {
	switch value := value.(type) {
	case map[string]any:
		if value["id"] == id {
			return true
		}
		for _, child := range value {
			if containsJSONID(child, id) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if containsJSONID(child, id) {
				return true
			}
		}
	}
	return false
}

func TestAccountUnassignedAndSymlinkWorkspaceDenied(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	target := filepath.Join(root, "target")
	other := filepath.Join(root, "other")
	link := filepath.Join(root, "link")
	for _, path := range []string{target, other} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	workspace, err := s.store.OpenWorkspace(link)
	if err != nil {
		t.Fatal(err)
	}
	alice := addAccount(t, s, admin, "alice", workspace.ID)
	forbidden, err := s.store.OpenWorkspace(other)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"files", "files/raw?path=x", "sessions", "settings", "git/status"} {
		w := accountRequest(s, "GET", "/api/workspaces/"+forbidden.ID+"/"+suffix, "", alice)
		if w.Code != 404 && w.Code != 403 {
			t.Fatalf("unassigned %s: %d", suffix, w.Code)
		}
	}
	user := s.accounts.Users["alice"].Account
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	if s.accounts.allows(user, workspace.ID, link) {
		t.Fatal("symlink retarget bypass")
	}
}
