package piweb

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
		"bun run test",
		"go test ./...",
		"bun run build",
	})
	if !strings.Contains(frontendWorkflow, "uses: actions/setup-go@v6") {
		t.Error("frontend workflow must set up Go for the backend unit step")
	}
	backendWorkflow := readRepositoryFile(t, root, ".github/workflows/backend.yml")
	for _, command := range []string{"go test ./...", "go tool cover -func=/tmp/piweb-cover.out"} {
		if !strings.Contains(backendWorkflow, command) {
			t.Errorf("backend workflow does not retain %q", command)
		}
	}
	coverageCommand := "go test $(go list ./... | grep -v '/node_modules/') -coverprofile=/tmp/piweb-cover.out"
	if !strings.Contains(backendWorkflow, coverageCommand) {
		t.Errorf("backend coverage must exclude nested node_modules packages: %q", coverageCommand)
	}

	tsconfig := readRepositoryFile(t, root, "tsconfig.json")
	if !strings.Contains(tsconfig, `"forceConsistentCasingInFileNames": true`) {
		t.Error("tsconfig must enforce consistent file-name casing")
	}

	astroConfig := readRepositoryFile(t, root, "astro.config.ts")
	if !strings.Contains(astroConfig, `target: "chrome107"`) {
		t.Error("astro config must target Chrome 107")
	}

	packageJSON := readRepositoryFile(t, root, "package.json")
	var packageManifest struct {
		Scripts         map[string]string `json:"scripts"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(packageJSON), &packageManifest); err != nil {
		t.Fatalf("parse package.json: %v", err)
	}
	if packageManifest.Scripts["lint"] != "eslint ." {
		t.Errorf("lint script = %q, want %q", packageManifest.Scripts["lint"], "eslint .")
	}
	for _, dependency := range []string{"eslint", "typescript-eslint", "eslint-plugin-astro"} {
		if _, ok := packageManifest.DevDependencies[dependency]; !ok {
			t.Errorf("package.json is missing lint dependency %q", dependency)
		}
	}
	if !strings.Contains(packageManifest.Scripts["backend:test:coverage"], "grep -v '/node_modules/'") {
		t.Error("backend coverage script must exclude nested node_modules packages")
	}
	if _, err := os.Stat(filepath.Join(root, "eslint.config.js")); err != nil {
		t.Fatalf("eslint.config.js is required: %v", err)
	}

	embedScript := packageManifest.Scripts["embed:assets"]
	for _, fragment := range []string{"find static -mindepth 1", "! -name README.txt", "cp -R dist/. static/"} {
		if !strings.Contains(embedScript, fragment) {
			t.Errorf("embed:assets script must contain %q", fragment)
		}
	}
	trackedStatic := gitFiles(t, root, "static")
	if !contains(trackedStatic, "static/README.txt") || !contains(trackedStatic, "static/index.html") || !containsPrefix(trackedStatic, "static/assets/") {
		t.Error("generated static entrypoint, assets, and README must be tracked")
	}
	gitignore := readRepositoryFile(t, root, ".gitignore")
	for _, rule := range strings.Split(gitignore, "\n") {
		if strings.TrimSpace(rule) == "static/" {
			t.Error(".gitignore must not ignore committed static assets")
		}
	}
}

func TestWorkflowRunCommandsRequireExactMatch(t *testing.T) {
	workflow := "steps:\n  - run: bun run test:coverage\n"
	if contains(workflowRunCommands(workflow), "bun run test") {
		t.Fatal("workflow command matching must not treat test:coverage as test")
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
	actual := workflowRunCommands(workflow)
	position := -1
	for _, command := range commands {
		next := -1
		for index := position + 1; index < len(actual); index++ {
			if actual[index] == command {
				next = index
				break
			}
		}
		if next < 0 {
			t.Fatalf("workflow is missing ordered command %q", command)
		}
		position = next
	}
}

func workflowRunCommands(workflow string) []string {
	var commands []string
	for _, line := range strings.Split(workflow, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if strings.HasPrefix(line, "run:") {
			commands = append(commands, strings.TrimSpace(strings.TrimPrefix(line, "run:")))
		}
	}
	return commands
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

func gitFiles(t *testing.T, root, path string) []string {
	t.Helper()
	output, err := exec.Command("git", "-C", root, "ls-files", path).Output()
	if err != nil {
		t.Fatalf("list tracked files under %s: %v", path, err)
	}
	return strings.Fields(string(output))
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
