package piweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type watchTestWriter struct {
	header http.Header
	mu     sync.Mutex
	body   strings.Builder
	wake   chan struct{}
}

func newWatchTestWriter() *watchTestWriter {
	return &watchTestWriter{header: http.Header{}, wake: make(chan struct{}, 8)}
}

func (w *watchTestWriter) Header() http.Header { return w.header }
func (w *watchTestWriter) WriteHeader(int)     {}
func (w *watchTestWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.body.Write(data)
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return n, err
}
func (w *watchTestWriter) Flush() {}
func (w *watchTestWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func TestWorkspaceFileWatchEmitsConnectedAndChange(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewWebStore(filepath.Join(t.TempDir(), "web.db"))
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	saveAllowedRootsForTest(t, root)
	server := NewServer(Config{}, store, NewBroker())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/files/watch?path=note.txt", nil).WithContext(ctx)
	writer := newWatchTestWriter()
	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(writer, request)
		close(done)
	}()
	waitForWatchBody(t, writer, "event: connected")
	if err := os.WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForWatchBody(t, writer, "event: change")
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watch handler did not stop after request cancellation")
	}
}

func TestWorkspaceFileWatchRejectsDirectories(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := NewWebStore(filepath.Join(t.TempDir(), "web.db"))
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	saveAllowedRootsForTest(t, root)
	server := NewServer(Config{}, store, NewBroker())
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/files/watch?path=docs", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func waitForWatchBody(t *testing.T, writer *watchTestWriter, want string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if strings.Contains(writer.String(), want) {
			return
		}
		select {
		case <-writer.wake:
		case <-deadline:
			t.Fatalf("watch body did not contain %q: %s", want, writer.String())
		}
	}
}
