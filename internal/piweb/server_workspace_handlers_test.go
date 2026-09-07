package piweb

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadWorkspaceFileEndpointAcceptsMultipartAndReportsTooLarge(t *testing.T) {
	root, server, workspace := newUploadTestServer(t, MinMaxUploadBytes)
	body, contentType := multipartUploadBody(t, "docs/a.bin", false, bytes.Repeat([]byte("x"), MinMaxUploadBytes+1))

	request := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspace.ID+"/files/upload", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "max") {
		t.Fatalf("missing readable limit: %s", response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "a.bin")); !os.IsNotExist(err) {
		t.Fatalf("oversized upload created target: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pi-upload-") {
			t.Fatalf("oversized upload left staging temp: %s", entry.Name())
		}
	}
}

func TestUploadWorkspaceFileEndpointWritesMultipartFileAndSupportsOverwrite(t *testing.T) {
	root, server, workspace := newUploadTestServer(t, MinMaxUploadBytes)

	upload := func(content string, overwrite bool) *httptest.ResponseRecorder {
		body, contentType := multipartUploadBody(t, "docs/a.txt", overwrite, []byte(content))
		request := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspace.ID+"/files/upload", body)
		request.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		return response
	}

	if response := upload("first", false); response.Code != http.StatusOK {
		t.Fatalf("first upload: %d %s", response.Code, response.Body.String())
	}
	path := filepath.Join(root, "docs", "a.txt")
	if data, err := os.ReadFile(path); err != nil || string(data) != "first" {
		t.Fatalf("first contents=%q err=%v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat uploaded file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("uploaded mode=%v", info.Mode().Perm())
	}
	if response := upload("conflict", false); response.Code == http.StatusOK || !strings.Contains(response.Body.String(), "path already exists") {
		t.Fatalf("overwrite=false response=%d body=%s", response.Code, response.Body.String())
	}
	if response := upload("second", true); response.Code != http.StatusOK {
		t.Fatalf("overwrite upload: %d %s", response.Code, response.Body.String())
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "second" {
		t.Fatalf("replacement contents=%q err=%v", data, err)
	}
}

func TestUploadWorkspaceFileEndpointValidatesPathBeforeFileBytes(t *testing.T) {
	_, server, workspace := newUploadTestServer(t, MinMaxUploadBytes)
	payload := bytes.Repeat([]byte("x"), 512*1024)
	body, contentType := multipartUploadBody(t, "../outside.txt", false, payload)
	counted := &countingReader{Reader: body}
	request := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspace.ID+"/files/upload", counted)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if counted.read >= int64(len(payload)/2) {
		t.Fatalf("invalid path consumed file payload: %d bytes", counted.read)
	}
}

func TestUploadWorkspaceFileEndpointBoundsUnknownMultipartPart(t *testing.T) {
	_, server, workspace := newUploadTestServer(t, MinMaxUploadBytes)
	payload := bytes.Repeat([]byte("x"), 4*1024*1024)
	body, contentType := multipartUploadBodyWithUnknownPart(t, payload)
	counted := &countingReader{Reader: body}
	request := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspace.ID+"/files/upload", counted)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "multipart part is too large") {
		t.Fatalf("unexpected unknown-part error: %s", response.Body.String())
	}
	if counted.read >= int64(len(payload)/2) {
		t.Fatalf("unknown part consumed too much payload: %d bytes", counted.read)
	}
}

func TestUploadWorkspaceFileEndpointAppliesLimitToLegacyJSON(t *testing.T) {
	_, server, workspace := newUploadTestServer(t, MinMaxUploadBytes)
	content := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), MinMaxUploadBytes+1))
	request := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspace.ID+"/files/upload", strings.NewReader(`{"path":"docs/a.bin","content":"`+content+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "max") {
		t.Fatalf("missing readable limit: %s", response.Body.String())
	}
}

func TestUploadWorkspaceFileEndpointAcceptsLegacyJSONAtConfiguredLimit(t *testing.T) {
	root, server, workspace := newUploadTestServer(t, MinMaxUploadBytes)
	data := bytes.Repeat([]byte("x"), MinMaxUploadBytes)
	content := base64.StdEncoding.EncodeToString(data)
	request := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspace.ID+"/files/upload", strings.NewReader(`{"path":"docs/a.bin","content":"`+content+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	stored, err := os.ReadFile(filepath.Join(root, "docs", "a.bin"))
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatalf("stored bytes=%d err=%v", len(stored), err)
	}
}

func TestUploadWorkspaceFileEndpointRejectsFileFirstWithoutConsumingPayload(t *testing.T) {
	_, server, workspace := newUploadTestServer(t, MinMaxUploadBytes)
	payload := bytes.Repeat([]byte("x"), 4*1024*1024)
	body, contentType := multipartUploadBodyFileFirst(t, "../outside.txt", true, payload)
	counted := &countingReader{Reader: body}
	request := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspace.ID+"/files/upload", counted)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "path and overwrite must precede file") {
		t.Fatalf("unexpected file-first error: %s", response.Body.String())
	}
	if counted.read >= int64(len(payload)/2) {
		t.Fatalf("file-first request consumed too much payload: %d bytes", counted.read)
	}
}

type countingReader struct {
	io.Reader
	read int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += int64(n)
	return n, err
}

func TestUploadWorkspaceFileStreamRemovesTempAfterOversize(t *testing.T) {
	root := t.TempDir()
	limit := int64(MinMaxUploadBytes)
	_, err := UploadWorkspaceFileStream(root, "docs/a.bin", bytes.NewReader(bytes.Repeat([]byte("x"), int(limit)+1)), limit, false)
	if err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("oversize error=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary files remain: %#v", entries)
	}
}

func newUploadTestServer(t *testing.T, maxBytes int) (string, *Server, Workspace) {
	t.Helper()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(t.TempDir(), "sessions"))
	root := t.TempDir()
	if _, err := SaveAppConfig(map[string]any{
		"workspace": map[string]any{
			"allowedRoots":   []any{root},
			"maxUploadBytes": float64(maxBytes),
		},
	}); err != nil {
		t.Fatal(err)
	}
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, NewServer(Config{}, store, NewBroker()), workspace
}

func multipartUploadBodyFileFirst(t *testing.T, path string, overwrite bool, content []byte) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("overwrite", map[bool]string{true: "true", false: "false"}[overwrite]); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("path", path); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func multipartUploadBodyWithUnknownPart(t *testing.T, content []byte) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormField("unknown")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func multipartUploadBody(t *testing.T, path string, overwrite bool, content []byte) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("path", path); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("overwrite", map[bool]string{true: "true", false: "false"}[overwrite]); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}
