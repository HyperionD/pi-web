package piweb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestUserImportTemplateDownloadsExpectedHeaders(t *testing.T) {
	s, admin := accountServer(t)
	response := accountRequest(s, "GET", "/api/admin/users/import-template", "", admin)
	if response.Code != http.StatusOK {
		t.Fatalf("template status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("template content type=%q", response.Header().Get("Content-Type"))
	}
	file, err := excelize.OpenReader(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatalf("open template: %v", err)
	}
	defer func() { _ = file.Close() }()
	rows, err := file.GetRows("用户导入")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || fmt.Sprint(rows[0]) != "[账号 用户名 部门 初始密码 工作区路径 禁用]" {
		t.Fatalf("template headers=%v", rows)
	}
}

func TestUserImportCreatesUsersAndWorkspaceGrants(t *testing.T) {
	s, admin := accountServer(t)
	workspacePath := t.TempDir()
	workbook := userImportWorkbook(t, [][]string{
		{"账号", "用户名", "部门", "初始密码", "工作区路径", "禁用"},
		{"alice", "Alice", "研发部", "alice-password", workspacePath, "否"},
		{"bob", "Bob", "市场部", "bob-password", "", "是"},
	})
	response := postUserImport(t, s, admin, workbook)
	if response.Code != http.StatusCreated || response.Body.String() != `{"imported":2}`+"\n" {
		t.Fatalf("successful import status=%d body=%s", response.Code, response.Body.String())
	}
	alice, ok := s.accounts.Users["alice"]
	if !ok || alice.Department != "研发部" || len(alice.Workspaces) != 1 || len(alice.Paths) != 1 {
		t.Fatalf("alice import = %#v", alice)
	}
	bob, ok := s.accounts.Users["bob"]
	if !ok || bob.Department != "市场部" || !bob.Disabled {
		t.Fatalf("bob import = %#v", bob)
	}
}

func TestUserImportRejectsExistingAccountWithoutCreatingOtherRows(t *testing.T) {
	s, admin := accountServer(t)
	created := accountRequest(s, "POST", "/api/admin/users", `{"username":"alice","password":"alice-password","workspaces":[]}`, admin)
	if created.Code != http.StatusCreated {
		t.Fatalf("create existing user: %d %s", created.Code, created.Body.String())
	}
	workbook := userImportWorkbook(t, [][]string{
		{"账号", "用户名", "部门", "初始密码", "工作区路径", "禁用"},
		{"bob", "Bob", "市场部", "bob-password", "", "否"},
		{"ALICE", "Alice 2", "研发部", "alice-password", "", "否"},
	})
	response := postUserImport(t, s, admin, workbook)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("已存在")) {
		t.Fatalf("existing account import status=%d body=%s", response.Code, response.Body.String())
	}
	if _, exists := s.accounts.Users["bob"]; exists {
		t.Fatal("existing account import created another user")
	}
}

func TestUserImportRejectsInvalidWorkspaceWithoutRegisteringEarlierRows(t *testing.T) {
	s, admin := accountServer(t)
	validPath := t.TempDir()
	workbook := userImportWorkbook(t, [][]string{
		{"账号", "用户名", "部门", "初始密码", "工作区路径", "禁用"},
		{"alice", "Alice", "研发部", "alice-password", validPath, "否"},
		{"bob", "Bob", "市场部", "bob-password", "/path/that/does/not/exist", "否"},
	})
	response := postUserImport(t, s, admin, workbook)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid workspace status=%d body=%s", response.Code, response.Body.String())
	}
	if _, exists := s.accounts.Users["alice"]; exists {
		t.Fatal("invalid workspace import created an earlier user")
	}
	for _, workspace := range s.store.AllWorkspaces() {
		if workspace.Path == validPath {
			t.Fatal("invalid workspace import registered an earlier workspace")
		}
	}
}

func TestUserImportRejectsDuplicateRowsWithoutCreatingUsers(t *testing.T) {
	s, admin := accountServer(t)
	workbook := userImportWorkbook(t, [][]string{
		{"账号", "用户名", "部门", "初始密码", "工作区路径", "禁用"},
		{"alice", "Alice", "研发部", "alice-password", "", "否"},
		{"ALICE", "Alice 2", "研发部", "alice-password", "", "否"},
	})
	response := postUserImport(t, s, admin, workbook)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate import status=%d body=%s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte("ALICE")) {
		t.Fatalf("duplicate error omitted account: %s", response.Body.String())
	}

	users := accountRequest(s, "GET", "/api/admin/users", "", admin)
	var body struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(users.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 0 {
		t.Fatalf("duplicate import created %d users", body.Total)
	}
}

func userImportWorkbook(t *testing.T, rows [][]string) *bytes.Buffer {
	t.Helper()
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()
	for index, row := range rows {
		cell := fmt.Sprintf("A%d", index+1)
		values := make([]interface{}, len(row))
		for column, value := range row {
			values[column] = value
		}
		if err := file.SetSheetRow("Sheet1", cell, &values); err != nil {
			t.Fatal(err)
		}
	}
	var workbook bytes.Buffer
	if err := file.Write(&workbook); err != nil {
		t.Fatal(err)
	}
	return &workbook
}

func postUserImport(t *testing.T, s *Server, cookie *http.Cookie, workbook *bytes.Buffer) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "users.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workbook.WriteTo(part); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/users/import", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	return response
}
