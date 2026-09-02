package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func saveAllowedRootsForTest(t *testing.T, roots ...string) {
	t.Helper()
	values := make([]any, len(roots))
	for i, root := range roots {
		values[i] = root
	}
	if _, err := SaveAppConfig(map[string]any{
		"workspace": map[string]any{"allowedRoots": values},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCloneGitWorkspaceRejectsForbiddenDestinationBeforeClone(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	outside := t.TempDir()
	marker := filepath.Join(t.TempDir(), "clone-ran")
	bin := t.TempDir()
	git := filepath.Join(bin, "git")
	if err := os.WriteFile(git, []byte("#!/bin/sh\ntouch \"$CLONE_MARKER\"\nmkdir -p \"$3\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLONE_MARKER", marker)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	policy := WorkspacePolicy{allowedRoots: []string{allowed}, restricted: true}

	_, _, err := CloneGitWorkspace(context.Background(), emptyStore(""), CloneWorkspaceRequest{
		ParentPath: outside,
		GitURL:     "https://example.com/repo.git",
		Name:       "repo",
	}, policy)
	if !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("clone error = %v, want %v", err, ErrWorkspaceForbidden)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("git clone marker error = %v, clone should not execute", err)
	}
}

func TestCloneGitWorkspaceRejectsSymlinkSwapAfterValidation(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	outside := t.TempDir()
	bin := t.TempDir()
	git := filepath.Join(bin, "git")
	if err := os.WriteFile(git, []byte("#!/bin/sh\nrm -rf \"$3\"\nln -s \"$CLONE_OUTSIDE\" \"$3\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLONE_OUTSIDE", outside)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	store := emptyStore(filepath.Join(t.TempDir(), "web.db"))
	policy := WorkspacePolicy{allowedRoots: []string{allowed}, restricted: true}

	workspace, output, err := CloneGitWorkspace(context.Background(), store, CloneWorkspaceRequest{
		ParentPath: allowed,
		GitURL:     "https://example.com/repo.git",
		Name:       "repo",
	}, policy)
	if err == nil {
		t.Fatalf("clone unexpectedly succeeded: workspace=%+v output=%q", workspace, output)
	}
	if !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("clone error = %v, want %v", err, ErrWorkspaceForbidden)
	}
	if _, err := os.Lstat(filepath.Join(allowed, "repo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe clone target was not cleaned: %v", err)
	}
	if workspaces := store.Workspaces(); len(workspaces) != 0 {
		t.Fatalf("unsafe clone target was registered: %+v", workspaces)
	}
}

func TestWorkspaceSelectionHonorsAllowedRoots(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	outside := t.TempDir()
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	saveAllowedRootsForTest(t, allowed)

	open := httptest.NewRecorder()
	server.Handler().ServeHTTP(open, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/open", strings.NewReader(`{"path":"`+outside+`"}`)))
	if open.Code != http.StatusForbidden {
		t.Fatalf("open status = %d, body = %s", open.Code, open.Body.String())
	}

	folders := httptest.NewRecorder()
	server.Handler().ServeHTTP(folders, httptest.NewRequest(http.MethodGet,
		"/api/system/folders?path="+url.QueryEscape(outside), nil))
	if folders.Code != http.StatusOK || !strings.Contains(folders.Body.String(), allowed) {
		t.Fatalf("folders status = %d, body = %s", folders.Code, folders.Body.String())
	}
}

func TestFolderPickerShowsAllowedRootFromOutsidePath(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	child := filepath.Join(allowed, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	saveAllowedRootsForTest(t, allowed)

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet,
		"/api/system/folders?path=%7E", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("root entry status = %d, body = %s", response.Code, response.Body.String())
	}
	var rootListing FolderListing
	if err := json.Unmarshal(response.Body.Bytes(), &rootListing); err != nil {
		t.Fatal(err)
	}
	var rootMarker struct {
		RootListing bool `json:"rootListing"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &rootMarker); err != nil {
		t.Fatal(err)
	}
	if !rootMarker.RootListing {
		t.Fatalf("root listing marker missing: %s", response.Body.String())
	}
	if len(rootListing.Folders) != 1 || rootListing.Folders[0].Path != allowed {
		t.Fatalf("root entries = %+v, want only %q", rootListing.Folders, allowed)
	}

	childResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(childResponse, httptest.NewRequest(http.MethodGet,
		"/api/system/folders?path="+url.QueryEscape(allowed), nil))
	if childResponse.Code != http.StatusOK || !strings.Contains(childResponse.Body.String(), "child") {
		t.Fatalf("allowed root listing status = %d, body = %s", childResponse.Code, childResponse.Body.String())
	}
}

func TestWorkspaceSelectionAllowsChildInsideAllowedRoot(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	child := filepath.Join(allowed, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	saveAllowedRootsForTest(t, allowed)

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/open", strings.NewReader(`{"path":"`+child+`"}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("open status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestWorkspaceSelectionCloneHonorsAllowedRoots(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	outside := t.TempDir()
	source := filepath.Join(t.TempDir(), "source.git")
	if output, err := exec.Command("git", "init", "--bare", source).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, output)
	}
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	saveAllowedRootsForTest(t, allowed)

	inside := httptest.NewRecorder()
	server.Handler().ServeHTTP(inside, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/clone", strings.NewReader(`{"parentPath":"`+allowed+`","gitUrl":"`+source+`","name":"inside"}`)))
	if inside.Code != http.StatusCreated {
		t.Fatalf("inside clone status = %d, body = %s", inside.Code, inside.Body.String())
	}

	outsideResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(outsideResponse, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/clone", strings.NewReader(`{"parentPath":"`+outside+`","gitUrl":"`+source+`","name":"outside"}`)))
	if outsideResponse.Code != http.StatusForbidden {
		t.Fatalf("outside clone status = %d, body = %s", outsideResponse.Code, outsideResponse.Body.String())
	}
}

func TestFolderPurposeControlsBrowsingScope(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	saveAllowedRootsForTest(t, allowed)

	ordinary := httptest.NewRecorder()
	server.Handler().ServeHTTP(ordinary, httptest.NewRequest(http.MethodGet,
		"/api/system/folders?path="+url.QueryEscape(outside), nil))
	if ordinary.Code != http.StatusOK || !strings.Contains(ordinary.Body.String(), allowed) {
		t.Fatalf("ordinary folder status = %d, body = %s", ordinary.Code, ordinary.Body.String())
	}

	allowedRoot := httptest.NewRecorder()
	server.Handler().ServeHTTP(allowedRoot, httptest.NewRequest(http.MethodGet,
		"/api/system/folders?path="+url.QueryEscape(outside)+"&purpose=allowed-root", nil))
	if allowedRoot.Code != http.StatusOK || !strings.Contains(allowedRoot.Body.String(), "child") {
		t.Fatalf("allowed-root folder status = %d, body = %s", allowedRoot.Code, allowedRoot.Body.String())
	}

	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodGet,
		"/api/system/folders?path="+url.QueryEscape(allowed)+"&purpose=other", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid purpose status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
}

func TestWorkspaceListFiltersForbiddenPaths(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	outside := t.TempDir()
	store := emptyStore(filepath.Join(t.TempDir(), "web.db"))
	if _, err := store.OpenWorkspace(allowed); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenWorkspace(outside); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, store, NewBroker())
	saveAllowedRootsForTest(t, allowed)

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/workspaces", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("workspaces status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Workspaces) != 1 || body.Workspaces[0].Path != allowed {
		t.Fatalf("unexpected workspaces: %+v", body.Workspaces)
	}
}

func TestWorkspaceSelectionReportsMalformedPolicyAsServerError(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	configPath, err := AppConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"workspace":{"allowedRoots":[42]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, NewMockStore(), NewBroker())

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/open", strings.NewReader(`{"path":"`+t.TempDir()+`"}`)))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("malformed policy status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestWorkspaceSelectionKeepsMalformedRequestAsBadRequest(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	server := NewServer(Config{}, NewMockStore(), NewBroker())

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/open", strings.NewReader(`{"path":`)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed request status = %d, body = %s", response.Code, response.Body.String())
	}
}
