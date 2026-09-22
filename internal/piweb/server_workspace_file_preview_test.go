package piweb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListWorkspaceFilesReturnsImmediateEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "readme.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "report.pdf"), []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := ListWorkspaceFiles(root, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %#v", len(entries), entries)
	}
	if entries[0].Path == "" || entries[0].Size <= 0 || entries[0].Modified == "" {
		t.Fatalf("missing entry metadata: %#v", entries[0])
	}
	if _, err := ListWorkspaceFiles(root, "../outside"); err == nil {
		t.Fatal("directory traversal was accepted")
	}
}

func TestWorkspaceFileListEndpointReturnsJSONEntries(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewWebStore(filepath.Join(t.TempDir(), "web.db"))
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	saveAllowedRootsForTest(t, root)
	server := NewServer(Config{}, store, NewBroker())
	request := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/files/list?path=.", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Entries []WorkspaceFileEntry `json:"entries"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 1 || body.Entries[0].Path != "note.txt" {
		t.Fatalf("unexpected entries: %#v", body.Entries)
	}
}

func TestWorkspaceFileAssetSupportsRangeAndDownloadDisposition(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "clip.mp4"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewWebStore(filepath.Join(t.TempDir(), "web.db"))
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	saveAllowedRootsForTest(t, root)
	server := NewServer(Config{}, store, NewBroker())

	request := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/files/raw?path=clip.mp4", nil)
	request.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || response.Body.String() != "2345" {
		t.Fatalf("range response = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("Content-Disposition") == "" || response.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("missing preview headers: %#v", response.Header())
	}

	download := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/files/raw?path=clip.mp4&download=1", nil)
	downloadResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(downloadResponse, download)
	if downloadResponse.Code != http.StatusOK || !strings.HasPrefix(downloadResponse.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("download response = %d %q", downloadResponse.Code, downloadResponse.Header().Get("Content-Disposition"))
	}
}
