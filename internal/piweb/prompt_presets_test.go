package piweb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func promptPresetRequest(
	s *Server, method, path, body string, cookie *http.Cookie, adminPassword string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if adminPassword != "" {
		request.Header.Set(adminPasswordHeader, adminPassword)
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	return response
}

func TestPromptPresetCRUDAllowsOptionalSkill(t *testing.T) {
	server, admin := accountServer(t)
	create := promptPresetRequest(server, http.MethodPost, "/api/prompt-presets", `{
		"label":"解释代码","skill":"","prompt":"请解释这段代码。","order":20
	}`, admin, "admin-password")
	if create.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}

	var created struct {
		Preset PromptPreset `json:"preset"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Preset.ID == "" || created.Preset.Skill != "" || created.Preset.Label != "解释代码" {
		t.Fatalf("created preset = %#v", created.Preset)
	}
	duplicate := promptPresetRequest(server, http.MethodPost, "/api/prompt-presets", `{
		"id":"`+created.Preset.ID+`","label":"重复","prompt":"test"
	}`, admin, "admin-password")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate: %d %s", duplicate.Code, duplicate.Body.String())
	}

	update := promptPresetRequest(server, http.MethodPatch, "/api/prompt-presets/"+created.Preset.ID,
		`{"label":"解释当前代码","prompt":"请解释当前代码。","enabled":false}`, admin, "admin-password")
	if update.Code != http.StatusOK {
		t.Fatalf("update: %d %s", update.Code, update.Body.String())
	}

	list := promptPresetRequest(server, http.MethodGet, "/api/prompt-presets", "", admin, "")
	if list.Code != http.StatusOK {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var listed struct {
		Presets []PromptPreset `json:"presets"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Presets) != 1 || listed.Presets[0].Label != "解释当前代码" || listed.Presets[0].Enabled {
		t.Fatalf("listed presets = %#v", listed.Presets)
	}

	remove := promptPresetRequest(
		server, http.MethodDelete, "/api/prompt-presets/"+created.Preset.ID,
		"", admin, "admin-password",
	)
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", remove.Code, remove.Body.String())
	}
}

func TestPromptPresetMutationAllowsAuthenticatedAdminWithoutPassword(t *testing.T) {
	server, admin := accountServer(t)
	create := promptPresetRequest(server, http.MethodPost, "/api/prompt-presets", `{"label":"管理员预设","prompt":"test"}`, admin, "")
	if create.Code != http.StatusCreated {
		t.Fatalf("admin create without password: %d %s", create.Code, create.Body.String())
	}
}

func TestPromptPresetMutationRequiresAdmin(t *testing.T) {
	server, admin := accountServer(t)
	createUser := accountRequest(
		server, http.MethodPost, "/api/admin/users",
		`{"username":"alice","password":"alice-password","workspaces":[]}`, admin,
	)
	if createUser.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", createUser.Code, createUser.Body.String())
	}
	login := accountRequest(
		server, http.MethodPost, "/api/account/login",
		`{"username":"alice","password":"alice-password"}`, nil,
	)
	if login.Code != http.StatusOK {
		t.Fatalf("login user: %d %s", login.Code, login.Body.String())
	}
	userCookie := login.Result().Cookies()[0]

	list := promptPresetRequest(server, http.MethodGet, "/api/prompt-presets", "", userCookie, "")
	if list.Code != http.StatusOK {
		t.Fatalf("user list: %d %s", list.Code, list.Body.String())
	}
	create := promptPresetRequest(
		server, http.MethodPost, "/api/prompt-presets",
		`{"label":"不应成功","prompt":"test"}`, userCookie, "admin-password",
	)
	if create.Code != http.StatusForbidden {
		t.Fatalf("user create: %d %s", create.Code, create.Body.String())
	}
}
