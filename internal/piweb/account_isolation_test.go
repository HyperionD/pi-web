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
	w := accountRequest(s, "GET", "/api/workspaces/"+workspace.ID+"/sessions", "", admin)
	if w.Code != 200 || !containsSession(w.Body.String(), aSession.ID) || !containsSession(w.Body.String(), bSession.ID) {
		t.Fatalf("admin session listing incomplete: %s", w.Body.String())
	}
	reloaded := newAccountStore()
	if reloaded.dbErr != nil {
		t.Fatalf("ownership reload database error: %v", reloaded.dbErr)
	}
	if !reloaded.owns(reloaded.Users["alice"].Account, aSession.ID) {
		t.Fatalf("ownership not persisted: %#v", reloaded.Owners)
	}
	w = accountRequest(s, "DELETE", "/api/workspaces/"+workspace.ID+"/sessions", "", alice)
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

func TestGrantedWorkspaceAndSessionVisibilitySurviveCloseAndRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agentRoot := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(agentRoot, "sessions"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(agentRoot, "agent"))
	if err := setupDatabaseAdminPassword("admin-password"); err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{UserManagement: true}, NewWebStore(DefaultWebDBPath()), NewBroker())
	admin := loginAccount(t, s, "admin", "admin-password")
	workspace, err := s.store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alice := addAccount(t, s, admin, "alice", workspace.ID)
	aliceSession := createAccountSession(t, s, alice, workspace.ID)
	bob := addAccount(t, s, admin, "bob", workspace.ID)
	bobSession := createAccountSession(t, s, bob, workspace.ID)
	if closed := accountRequest(s, "DELETE", "/api/workspaces/"+workspace.ID, "", admin); closed.Code != http.StatusOK {
		t.Fatalf("close workspace: %d %s", closed.Code, closed.Body.String())
	}

	restarted := NewServer(Config{UserManagement: true}, NewWebStore(DefaultWebDBPath()), NewBroker())
	alice = loginAccount(t, restarted, "alice", "test-password")
	list := accountRequest(restarted, "GET", "/api/workspaces", "", alice)
	if list.Code != http.StatusOK || !containsSession(list.Body.String(), aliceSession.ID) || containsSession(list.Body.String(), bobSession.ID) {
		t.Fatalf("restarted visibility is incorrect: %s", list.Body.String())
	}
	if path, err := restarted.store.WorkspacePath(workspace.ID); err != nil || !sameWorkspacePath(path, workspace.Path) {
		t.Fatalf("granted workspace was not materialized: %q %v", path, err)
	}
}

func loginAccount(t *testing.T, s *Server, username, password string) *http.Cookie {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	response := accountRequest(s, "POST", "/api/account/login", body, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("login %s: %d %s", username, response.Code, response.Body.String())
	}
	return response.Result().Cookies()[0]
}

func TestClosingAdminRecentKeepsGrantedWorkspaceAvailable(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	alice := addAccount(t, s, admin, "alice", workspace.ID)
	aliceSession := createAccountSession(t, s, alice, workspace.ID)
	bob := addAccount(t, s, admin, "bob", workspace.ID)
	bobSession := createAccountSession(t, s, bob, workspace.ID)

	closed := accountRequest(s, "DELETE", "/api/workspaces/"+workspace.ID, "", admin)
	if closed.Code != http.StatusOK {
		t.Fatalf("close workspace: %d %s", closed.Code, closed.Body.String())
	}
	adminList := accountRequest(s, "GET", "/api/workspaces", "", admin)
	if adminList.Code != http.StatusOK || containsJSONIDFromBody(adminList.Body.Bytes(), workspace.ID) {
		t.Fatalf("closed workspace remained in admin recents: %s", adminList.Body.String())
	}
	aliceList := accountRequest(s, "GET", "/api/workspaces", "", alice)
	if aliceList.Code != http.StatusOK || !containsSession(aliceList.Body.String(), aliceSession.ID) || containsSession(aliceList.Body.String(), bobSession.ID) {
		t.Fatalf("granted workspace/session visibility changed after close: %s", aliceList.Body.String())
	}
	files := accountRequest(s, "GET", "/api/workspaces/"+workspace.ID+"/files", "", alice)
	if files.Code != http.StatusOK {
		t.Fatalf("closed but granted workspace is unusable: %d %s", files.Code, files.Body.String())
	}
}

func containsJSONIDFromBody(body []byte, id string) bool {
	var value any
	return json.Unmarshal(body, &value) == nil && containsJSONID(value, id)
}

func TestAdminWorkspaceAuthorizationIsNotPersisted(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	addAccount(t, s, admin, "alice", workspace.ID)

	s.accounts.mu.Lock()
	users := make(map[string]accountRecord, len(s.accounts.Users))
	for name, record := range s.accounts.Users {
		users[name] = record
	}
	adminRecord := users["admin"]
	adminRecord.Workspaces = []string{workspace.ID}
	adminRecord.Paths = map[string]string{workspace.ID: root}
	users["admin"] = adminRecord
	err = s.accounts.save(users, s.accounts.Owners)
	s.accounts.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var adminGrants int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_workspaces uw JOIN users u ON u.id = uw.user_id WHERE u.account = 'admin'`).Scan(&adminGrants); err != nil {
		t.Fatal(err)
	}
	if adminGrants != 0 {
		t.Fatalf("admin workspace grants = %d, want 0", adminGrants)
	}
	if len(s.accounts.Users["alice"].Paths) != 1 {
		t.Fatal("saving admin permissions removed another user's workspace grant")
	}
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
	resolvedPath, err := s.workspaceAccess.ResolveWorkspace(&user, workspace.ID)
	if err != nil || !sameWorkspacePath(resolvedPath, target) {
		t.Fatalf("symlink retarget changed the granted workspace: %q %v", resolvedPath, err)
	}
}
