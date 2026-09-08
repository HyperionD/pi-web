package piweb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func accountRequest(s *Server, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func accountServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	agentRoot := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(agentRoot, "sessions"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(agentRoot, "agent"))
	if err := setupDatabaseAdminPassword("admin-password"); err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{UserManagement: true}, NewMockStore(), NewBroker())
	login := accountRequest(s, "POST", "/api/account/login", `{"username":"admin","password":"admin-password"}`, nil)
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	return s, login.Result().Cookies()[0]
}

func TestConcurrentDatabaseAccessDoesNotReturnLocked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agentRoot := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(agentRoot, "sessions"))
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	var group sync.WaitGroup
	errors := make(chan error, 24)
	for range 24 {
		group.Add(1)
		go func() {
			defer group.Done()
			db, err := openWebDB(DefaultWebDBPath())
			if err == nil {
				_, err = db.Exec(`INSERT INTO app_settings(key,value) VALUES('parallel', '1') ON CONFLICT(key) DO UPDATE SET value='1'`)
				_ = db.Close()
			}
			if err != nil {
				errors <- err
			}
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent database access: %v", err)
	}
}

func TestAccountAuthenticationAndManagement(t *testing.T) {
	s, admin := accountServer(t)
	for _, path := range []string{"/api/workspaces", "/api/sessions/session-1", "/api/auth/providers", "/api/app-config"} {
		if w := accountRequest(s, "GET", path, "", nil); w.Code != 401 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	create := accountRequest(s, "POST", "/api/admin/users", `{"username":"alice","password":"alice-password","workspaces":[]}`, admin)
	if create.Code != 201 {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}
	login := accountRequest(s, "POST", "/api/account/login", `{"username":"alice","password":"alice-password"}`, nil)
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	for _, path := range []string{"/api/admin/users", "/api/auth/providers", "/api/system/folders"} {
		if w := accountRequest(s, "GET", path, "", cookie); w.Code != 403 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/workspaces/open", "/api/workspaces/clone"} {
		if w := accountRequest(s, "POST", path, `{}`, cookie); w.Code != 403 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	w := accountRequest(s, "GET", "/api/workspaces", "", cookie)
	var body struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Workspaces) != 0 {
		t.Fatal("unassigned workspace leaked")
	}
	if w := accountRequest(s, "POST", "/api/account/logout", `{}`, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := accountRequest(s, "GET", "/api/account/me", "", cookie); w.Code != 401 {
		t.Fatal("logout did not revoke cookie")
	}
}
