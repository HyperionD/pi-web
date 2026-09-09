package piweb

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRepositoryContract(t *testing.T) {
	root := repositoryRoot(t)
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	if got, want := server.Addr(), "127.0.0.1:8732"; got != want {
		t.Fatalf("default server address = %q, want %q", got, want)
	}

	frontendWorkflow := readRepositoryFile(t, root, ".github/workflows/frontend.yml")
	assertWorkflowCommands(t, frontendWorkflow, []string{
		"bun run lint",
		"bun run typecheck",
		"bun run test:coverage",
		"bun run build",
	})
	backendWorkflow := readRepositoryFile(t, root, ".github/workflows/backend.yml")
	for _, command := range []string{"go test ./...", "go tool cover -func=/tmp/piweb-cover.out"} {
		if !strings.Contains(backendWorkflow, command) {
			t.Errorf("backend workflow does not retain %q", command)
		}
	}

	tsconfig := readRepositoryFile(t, root, "tsconfig.json")
	if !strings.Contains(tsconfig, `"forceConsistentCasingInFileNames": true`) {
		t.Error("tsconfig must enforce consistent file-name casing")
	}

	staticReadme := readRepositoryFile(t, root, "static/README.txt")
	if !strings.Contains(staticReadme, "committed") || !strings.Contains(staticReadme, "embed:assets") {
		t.Error("static README must document committed generated assets and the refresh command")
	}
}

func TestRoutes(t *testing.T) {
	source := readRepositoryFile(t, repositoryRoot(t), "internal/piweb/server.go")
	protectedRoutes := []struct {
		methodPath string
		handler    string
		middleware string
	}{
		{"DELETE /api/workspaces/{workspaceID}", "closeWorkspace", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/sessions", "workspaceSessions", "requireWorkspaceAccess"},
		{"POST /api/workspaces/{workspaceID}/sessions", "createSession", "requireWorkspaceAccess"},
		{"DELETE /api/workspaces/{workspaceID}/sessions", "deleteWorkspaceSessions", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/files", "workspaceFiles", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/files/raw", "workspaceFileAsset", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/files/search", "searchWorkspaceFiles", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/commands", "workspaceCommands", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/models", "workspaceModels", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/runtime-status", "workspaceRuntimeStatus", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/runtime-model", "workspaceRuntimeModel", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/runtime-quota", "workspaceRuntimeQuota", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/settings", "workspaceSettings", "requireWorkspaceAccess"},
		{"PUT /api/workspaces/{workspaceID}/settings", "saveWorkspaceSettings", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/files/read", "readWorkspaceFile", "requireWorkspaceAccess"},
		{"PUT /api/workspaces/{workspaceID}/files/write", "writeWorkspaceFile", "requireWorkspaceAccess"},
		{"POST /api/workspaces/{workspaceID}/files/create", "createWorkspaceFile", "requireWorkspaceAccess"},
		{"PATCH /api/workspaces/{workspaceID}/files/rename", "renameWorkspaceFile", "requireWorkspaceAccess"},
		{"DELETE /api/workspaces/{workspaceID}/files/delete", "deleteWorkspaceFile", "requireWorkspaceAccess"},
		{"POST /api/workspaces/{workspaceID}/files/upload", "uploadWorkspaceFile", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/git/status", "gitStatus", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/git/history", "gitHistory", "requireWorkspaceAccess"},
		{"GET /api/workspaces/{workspaceID}/git/commit", "gitCommit", "requireWorkspaceAccess"},
		{"POST /api/workspaces/{workspaceID}/shell", "shellCommand", "requireWorkspaceAccess"},
		{"GET /api/sessions/{sessionID}", "session", "requireSessionAccess"},
		{"GET /api/sessions/{sessionID}/context-usage", "handleSessionContextUsage", "requireSessionAccess"},
		{"POST /api/sessions/{sessionID}/model", "handleSessionSetModel", "requireSessionAccess"},
		{"PATCH /api/sessions/{sessionID}", "renameSession", "requireSessionAccess"},
		{"DELETE /api/sessions/{sessionID}", "deleteSession", "requireSessionAccess"},
		{"POST /api/sessions/{sessionID}/prompt", "prompt", "requireSessionAccess"},
		{"POST /api/sessions/{sessionID}/ag-ui", "aguiSessionRun", "requireSessionAccess"},
		{"POST /api/sessions/{sessionID}/compact", "compactSession", "requireSessionAccess"},
		{"POST /api/sessions/{sessionID}/steer", "steerSession", "requireSessionAccess"},
		{"POST /api/sessions/{sessionID}/cancel", "cancelSession", "requireSessionAccess"},
		{"GET /api/sessions/{sessionID}/events", "sessionEvents", "requireSessionAccess"},
	}

	for _, route := range protectedRoutes {
		t.Run(route.methodPath, func(t *testing.T) {
			registration := fmt.Sprintf(
				"s.mux.HandleFunc(%q, s.%s(s.%s))",
				route.methodPath,
				route.middleware,
				route.handler,
			)
			if !strings.Contains(source, registration) {
				t.Fatalf("protected route registration missing middleware: %s", registration)
			}
		})
	}
}

func assertWorkflowCommands(t *testing.T, workflow string, commands []string) {
	t.Helper()
	position := -1
	for _, command := range commands {
		next := strings.Index(workflow[position+1:], "run: "+command)
		if next < 0 {
			t.Fatalf("workflow is missing ordered command %q", command)
		}
		position += next + 1
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository contract test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../.."))
}

func readRepositoryFile(t *testing.T, root, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}
