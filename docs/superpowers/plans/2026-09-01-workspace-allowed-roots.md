# Workspace Allowed Roots Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a user-level `workspace.allowedRoots` policy so pi-web workspaces can only be selected and accessed inside configured directory roots.

**Architecture:** Extend the existing `pi-web-config.json` schema with a validated `workspace.allowedRoots` string array. A backend `WorkspacePolicy` resolves real paths and is applied to folder browsing, open/clone, workspace listings, every workspace-ID route, and every session-ID route. The frontend provides a multi-root settings list and uses a separate `purpose=allowed-root` folder-picker context; frontend checks improve UX but never replace backend authorization.

**Tech Stack:** Go `net/http`, `filepath.EvalSymlinks`, existing JSON config storage, Astro/TypeScript, Zod, Vitest, Go `testing`.

**Spec:** `docs/superpowers/specs/2026-09-01-workspace-allowed-roots-design.md`

## Global Constraints

- Support multiple roots in `workspace.allowedRoots: string[]`.
- Missing `workspace`, missing `allowedRoots`, and `allowedRoots: []` all mean restriction is disabled and preserve existing unrestricted behavior.
- Non-empty roots must be non-empty existing directories; invalid PATCH leaves the old configuration unchanged.
- Save paths by trimming, expanding `~`, converting to absolute paths, cleaning, resolving symlinks, and de-duplicating real directories.
- A candidate path is allowed only when it equals a real root or is below it on a directory boundary; string-prefix-only checks are forbidden.
- Symlinked roots may be configured; candidate symlinks resolving outside an allowed root are rejected.
- `open` validates an existing real directory; `clone` validates its final destination and parent before cloning.
- Ordinary workspace folder browsing uses `purpose=workspace` or no purpose and is restricted; configuration browsing uses `purpose=allowed-root` and cannot open a workspace.
- Historical workspaces outside configured roots are hidden from lists and all workspace/session operations return `403`; missing resources remain `404`.
- Invalid non-empty policy configuration fails closed for workspace operations; it never becomes unrestricted by accident.
- App-config remains workspace-independent at `/api/app-config`; unknown top-level fields, `brand`, and existing pi settings behavior remain intact.
- Brand and path/status text are rendered as text, never with `innerHTML`.
- Do not modify or stage the pre-existing `static/` generated changes.
- Each commit message body must include exactly `Co-authored-by: JuunAI <juunai.ai.i@gmail.com>`.

## File Map

- `internal/piweb/app_config.go`: validate and normalize the new known `workspace.allowedRoots` field while preserving unknown data.
- `internal/piweb/workspace_policy.go`: resolve roots and enforce real-path/clone policy.
- `internal/piweb/workspace_policy_test.go`: unit-test policy semantics and symlink/path boundaries.
- `internal/piweb/server.go`: register policy-aware routes and folder-picker purpose handling.
- `internal/piweb/server_workspace_handlers.go`: authorize open, list, clone, and workspace responses.
- `internal/piweb/server_session_handlers.go`, `internal/piweb/server_session_status_handlers.go`: use session authorization wrappers without changing business logic.
- `internal/piweb/workspace_ops.go`: validate clone destination before invoking Git.
- `internal/piweb/store_workspace.go`, `internal/piweb/store_sessions.go`: expose the workspace path/owner lookup needed by authorization.
- `internal/piweb/server_workspace_policy.go`: central HTTP authorization wrappers and status mapping.
- `internal/piweb/*_test.go`: endpoint and regression coverage.
- `src/lib/api.ts`: send folder-picker purpose and typed app-config patches.
- `src/pi-app/workspace/app-config-schema.ts`: parse workspace config and preserve unknown fields.
- `src/pi-app/workspace/app-config-methods.ts`: manage allowed-root state, form rows, and patches.
- `src/pi-app/workspace/workspace-folder-methods.ts`: distinguish ordinary workspace browsing from allowed-root configuration browsing and handle `403`.
- `src/pi-app/workspace/workspace-methods.ts` and `workspace-bootstrap-methods.ts`: handle filtered workspaces and a workspace becoming unauthorized.
- `src/App.astro`, `src/styles/parts/settings-modal.css`: render the multi-root settings controls.
- `src/pi-app/test-helper.ts` and focused frontend tests: add deterministic DOM and request fixtures.
- `README.md`: document the new setting, semantics, and reset behavior.

---

### Task 1: Extend app-config validation and normalization

**Files:**
- Modify: `internal/piweb/app_config.go`
- Test: `internal/piweb/app_config_test.go`

**Interfaces:**
- Consumes: existing `LoadAppConfig()` and `SaveAppConfig(patch map[string]any)`.
- Produces: normalized stored `workspace.allowedRoots` values that Task 2 reads from the complete `AppConfigResponse.Config` map.

- [ ] **Step 1: Write failing config tests**

Add tests for the exact new patch shape and preservation behavior:

```go
func TestSaveAppConfigNormalizesAllowedRoots(t *testing.T) {
    root := t.TempDir()
    nested := filepath.Join(root, "nested")
    if err := os.Mkdir(nested, 0o755); err != nil { t.Fatal(err) }
    response, err := SaveAppConfig(map[string]any{
        "workspace": map[string]any{"allowedRoots": []any{root, nested, root}},
    })
    if err != nil { t.Fatal(err) }
    roots := response.Config["workspace"].(map[string]any)["allowedRoots"].([]any)
    if len(roots) != 2 { t.Fatalf("roots = %#v", roots) }
}

func TestSaveAppConfigRejectsInvalidAllowedRootsWithoutChangingConfig(t *testing.T) {
    valid := t.TempDir()
    if _, err := SaveAppConfig(map[string]any{
        "workspace": map[string]any{"allowedRoots": []any{valid}},
    }); err != nil { t.Fatal(err) }
    _, err := SaveAppConfig(map[string]any{
        "workspace": map[string]any{"allowedRoots": []any{filepath.Join(valid, "missing")}},
    })
    if err == nil { t.Fatal("expected invalid root error") }
    response, err := LoadAppConfig()
    if err != nil { t.Fatal(err) }
    roots := response.Config["workspace"].(map[string]any)["allowedRoots"].([]any)
    if len(roots) != 1 || roots[0] != valid { t.Fatalf("roots changed: %#v", roots) }
}
```

Also cover missing/empty arrays as unrestricted, `~` expansion, non-string entries, a non-object `workspace`, a symlink root, preservation of `brand` and an unknown nested field, and `workspace: null` removing the section.

- [ ] **Step 2: Run focused tests and verify failure**

Run:

```bash
go test ./internal/piweb -run 'TestSaveAppConfig(NormalizesAllowedRoots|RejectsInvalidAllowedRootsWithoutChangingConfig|.*Workspace)'
```

Expected: FAIL because app-config currently treats `workspace` as an arbitrary unknown value and does not normalize or validate `allowedRoots`.

- [ ] **Step 3: Implement minimal normalization**

Add a helper with an explicit contract:

```go
func normalizeStoredWorkspaceConfig(value any) (map[string]any, error)
func normalizeAllowedRoots(value any) ([]any, error)
```

`normalizeAllowedRoots` must trim and expand each string, require an existing directory, call `filepath.Abs` and `filepath.EvalSymlinks`, remove duplicate real paths, and return JSON-compatible `[]any` strings. `normalizeStoredWorkspaceConfig` must preserve unknown keys inside `workspace`, validate `allowedRoots` when present, and leave absent/empty lists valid. Update `normalizeStoredAppConfig` and `applyAppConfigPatch` so an invalid workspace patch returns before `writeAppConfig` and a successful patch does not remove unrelated config keys.

- [ ] **Step 4: Run focused and package tests**

Run:

```bash
gofmt -w internal/piweb/app_config.go internal/piweb/app_config_test.go
go test ./internal/piweb -run 'TestSaveAppConfig(NormalizesAllowedRoots|RejectsInvalidAllowedRootsWithoutChangingConfig|.*Workspace)'
go test ./internal/piweb
```

Expected: all focused and package tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/piweb/app_config.go internal/piweb/app_config_test.go
git commit -m "feat(config): validate workspace allowed roots" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 2: Build the real-path workspace policy

**Files:**
- Create: `internal/piweb/workspace_policy.go`
- Create: `internal/piweb/workspace_policy_test.go`

**Interfaces:**
- Consumes: `LoadAppConfig()` and normalized `config["workspace"]` from Task 1.
- Produces:

```go
type WorkspacePolicy struct { /* private normalized roots and restriction state */ }
func NewWorkspacePolicy(config map[string]any) (WorkspacePolicy, error)
func LoadWorkspacePolicy() (WorkspacePolicy, error)
func (p WorkspacePolicy) CheckExistingDirectory(path string) error
func (p WorkspacePolicy) CheckFolderPath(path string) error
func (p WorkspacePolicy) ValidateCloneDestination(parentPath, name string) (string, error)
```

Use sentinel errors that callers can classify without matching error strings:

```go
var ErrWorkspaceForbidden = errors.New("workspace path is outside allowed roots")
var ErrWorkspacePolicyInvalid = errors.New("workspace policy is invalid")
```

- [ ] **Step 1: Write failing policy tests**

Cover exact boundary cases:

```go
func policyForRoots(t *testing.T, roots ...string) WorkspacePolicy {
    t.Helper()
    values := make([]any, len(roots))
    for i, root := range roots { values[i] = root }
    policy, err := NewWorkspacePolicy(map[string]any{
        "workspace": map[string]any{"allowedRoots": values},
    })
    if err != nil { t.Fatal(err) }
    return policy
}

func TestWorkspacePolicyUsesDirectoryBoundaries(t *testing.T) {
    root := t.TempDir()
    sibling := root + "-sibling"
    if err := os.Mkdir(sibling, 0o755); err != nil { t.Fatal(err) }
    child := filepath.Join(root, "child")
    if err := os.Mkdir(child, 0o755); err != nil { t.Fatal(err) }
    policy := policyForRoots(t, root)
    if err := policy.CheckExistingDirectory(root); err != nil { t.Fatal(err) }
    if err := policy.CheckExistingDirectory(filepath.Join(root, "child")); err != nil {
        t.Fatal(err)
    }
    if err := policy.CheckExistingDirectory(sibling); !errors.Is(err, ErrWorkspaceForbidden) {
        t.Fatalf("sibling error = %v", err)
    }
}
```

Add tests for unrestricted missing/empty config, multiple roots, `~`, root symlink resolution, candidate symlink outside rejection, nonexistent clone destination under an allowed parent, clone destination outside rejection, invalid policy fail-closed, and `.`/`..` boundary handling.

- [ ] **Step 2: Run policy tests and verify failure**

Run:

```bash
go test ./internal/piweb -run 'TestWorkspacePolicy'
```

Expected: FAIL because `WorkspacePolicy`, `LoadWorkspacePolicy`, and the sentinel errors do not exist.

- [ ] **Step 3: Implement policy helpers**

Implement root extraction from the complete config and real-path checks. Use `filepath.Rel` for the boundary test rather than string prefixes:

```go
func pathWithinRoot(root, candidate string) bool {
    relative, err := filepath.Rel(root, candidate)
    return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
```

`CheckExistingDirectory` must expand, clean, `Abs`, `EvalSymlinks`, `os.Stat`, require a directory, and return `ErrWorkspaceForbidden` when no root matches. `CheckFolderPath` must apply the same policy to an existing directory. `ValidateCloneDestination` must validate the existing parent, reject separators/`.`/`..` in `name`, join the destination, and validate the existing destination when present; for a new destination, the validated parent plus one child segment is sufficient.

- [ ] **Step 4: Run focused tests, race tests, and package tests**

Run:

```bash
gofmt -w internal/piweb/workspace_policy.go internal/piweb/workspace_policy_test.go
go test ./internal/piweb -run 'TestWorkspacePolicy'
go test -race ./internal/piweb -run 'TestWorkspacePolicy'
go test ./internal/piweb
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/piweb/workspace_policy.go internal/piweb/workspace_policy_test.go
git commit -m "feat(workspace): add allowed roots policy" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 3: Enforce policy for folders, open, clone, and workspace lists

**Files:**
- Modify: `internal/piweb/folders.go`
- Modify: `internal/piweb/workspace_ops.go`
- Modify: `internal/piweb/server_workspace_handlers.go`
- Modify: `internal/piweb/server.go`
- Create: `internal/piweb/server_workspace_selection_test.go`

**Interfaces:**
- Consumes: `LoadWorkspacePolicy`, `CheckFolderPath`, `CheckExistingDirectory`, `ValidateCloneDestination`, and `NewWorkspacePolicy` from Task 2.
- Produces: restricted ordinary folder browsing, explicit `purpose=allowed-root` browsing, filtered workspace lists, and policy-aware open/clone routes.

The clone helper has this exact signature after this task:

```go
func CloneGitWorkspace(ctx context.Context, store *Store, req CloneWorkspaceRequest, policy WorkspacePolicy) (Workspace, string, error)
```

- [ ] **Step 1: Write failing endpoint tests**

Add a server fixture with two sibling directories and requests that prove the backend boundary:

```go
func saveAllowedRootsForTest(t *testing.T, roots ...string) {
    t.Helper()
    values := make([]any, len(roots))
    for i, root := range roots { values[i] = root }
    if _, err := SaveAppConfig(map[string]any{
        "workspace": map[string]any{"allowedRoots": values},
    }); err != nil { t.Fatal(err) }
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
    if open.Code != http.StatusForbidden { t.Fatalf("open status = %d", open.Code) }

    folders := httptest.NewRecorder()
    server.Handler().ServeHTTP(folders, httptest.NewRequest(http.MethodGet,
        "/api/system/folders?path="+url.QueryEscape(outside), nil))
    if folders.Code != http.StatusForbidden { t.Fatalf("folders status = %d", folders.Code) }
}
```

Also test allowed child open, clone parent inside/outside, ordinary folder listing, `purpose=allowed-root` listing outside the current roots, and `/api/workspaces` hiding a pre-existing outside workspace. Verify malformed policy returns `500` for workspace selection instead of becoming unrestricted.

- [ ] **Step 2: Run endpoint tests and verify failure**

Run:

```bash
go test ./internal/piweb -run 'TestWorkspaceSelectionHonorsAllowedRoots|Test.*AllowedRoot|Test.*Workspace.*Filter'
```

Expected: FAIL because handlers currently call `ListFolders`, `OpenWorkspace`, `CloneGitWorkspace`, and `Store.Workspaces` without policy checks.

- [ ] **Step 3: Implement policy-aware selection**

Add an explicit purpose parser in the folder handler:

```go
purpose := r.URL.Query().Get("purpose")
if purpose == "" { purpose = "workspace" }
if purpose != "workspace" && purpose != "allowed-root" {
    writeError(w, http.StatusBadRequest, errors.New("invalid folder purpose"))
    return
}
```

For `workspace`, call `LoadWorkspacePolicy` and reject an outside query path before `ListFolders`. For `allowed-root`, retain the existing filesystem listing behavior and never call `OpenWorkspace` or clone logic. In `openWorkspace`, check the requested existing directory before `Store.OpenWorkspace`. Change `CloneGitWorkspace` to receive a `WorkspacePolicy` and call `ValidateCloneDestination` before `git clone`; use the validated parent as `cmd.Dir` and the validated destination for `Store.OpenWorkspace`. Filter `/api/workspaces` by checking each workspace path and omit forbidden entries while returning a policy configuration error as `500`.

Map `ErrWorkspaceForbidden` to `403` and `ErrWorkspacePolicyInvalid`/filesystem errors to `500`; keep malformed request bodies as `400`.

- [ ] **Step 4: Run endpoint and regression tests**

Run:

```bash
gofmt -w internal/piweb/folders.go internal/piweb/workspace_ops.go internal/piweb/server_workspace_handlers.go internal/piweb/server.go
go test ./internal/piweb -run 'TestWorkspaceSelectionHonorsAllowedRoots|Test.*AllowedRoot|Test.*Workspace.*Filter'
go test ./internal/piweb
```

Expected: PASS, with all existing workspace endpoint tests unchanged in behavior when roots are missing/empty.

- [ ] **Step 5: Commit**

```bash
git add internal/piweb/folders.go internal/piweb/workspace_ops.go internal/piweb/server_workspace_handlers.go internal/piweb/server.go internal/piweb/server_workspace_selection_test.go
git commit -m "feat(api): restrict workspace selection" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 4: Authorize every existing workspace and session route

**Files:**
- Create: `internal/piweb/server_workspace_policy.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/store_workspace.go`
- Modify: `internal/piweb/store_sessions.go`
- Modify: `internal/piweb/server_workspace_handlers.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Modify: `internal/piweb/server_session_status_handlers.go`
- Create: `internal/piweb/server_workspace_policy_test.go`

The test file imports `io`, `net/http`, and `net/http/httptest` for the request helper below.

**Interfaces:**
- Consumes: `LoadWorkspacePolicy` and `Store.WorkspacePath` from earlier tasks.
- Produces:

```go
func (s *Server) requireWorkspaceAccess(next http.HandlerFunc) http.HandlerFunc
func (s *Server) requireSessionAccess(next http.HandlerFunc) http.HandlerFunc
```

- [ ] **Step 1: Write failing authorization tests**

Test representative routes from every family and a session route:

```go
func serverWithAllowedAndForbiddenWorkspace(t *testing.T) (*Server, Workspace, Workspace) {
    t.Helper()
    t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
    allowedRoot := t.TempDir()
    outsideRoot := t.TempDir()
    store := NewMockStore()
    allowed, err := store.OpenWorkspace(allowedRoot)
    if err != nil { t.Fatal(err) }
    outside, err := store.OpenWorkspace(outsideRoot)
    if err != nil { t.Fatal(err) }
    saveAllowedRootsForTest(t, allowedRoot)
    return NewServer(Config{}, store, NewBroker()), allowed, outside
}

func TestWorkspaceRoutesRejectForbiddenWorkspace(t *testing.T) {
    server, workspace, outside := serverWithAllowedAndForbiddenWorkspace(t)
    cases := []struct{ method, suffix string }{
        {http.MethodGet, "/files"},
        {http.MethodGet, "/settings"},
        {http.MethodPost, "/shell"},
    }
    for _, tc := range cases {
        requestPath := "/api/workspaces/" + outside.ID + tc.suffix
        response := serveRequest(server, tc.method, requestPath, nil)
        if response.Code != http.StatusForbidden { t.Fatalf("%s %s: %d", tc.method, workspace.ID, response.Code) }
    }
}

func serveRequest(server *Server, method, path string, body io.Reader) *httptest.ResponseRecorder {
    request := httptest.NewRequest(method, path, body)
    response := httptest.NewRecorder()
    server.Handler().ServeHTTP(response, request)
    return response
}
```

Cover workspace delete/session/file/git/runtime/shell routes, session read/prompt/AG-UI/events/status routes, allowed resources, and missing IDs. Assert forbidden is `403` and missing remains `404`.

- [ ] **Step 2: Run authorization tests and verify failure**

Run:

```bash
go test ./internal/piweb -run 'TestWorkspaceRoutesRejectForbiddenWorkspace|TestSessionRoutesRejectForbiddenWorkspace'
```

Expected: FAIL because current routes execute business logic without checking the current policy.

- [ ] **Step 3: Implement centralized route wrappers**

Implement wrappers that resolve the resource before invoking the handler:

```go
func (s *Server) requireWorkspaceAccess(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        path, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
        if err != nil { writeStoreError(w, err); return }
        policy, err := LoadWorkspacePolicy()
        if err != nil { writeError(w, http.StatusInternalServerError, err); return }
        if err := policy.CheckExistingDirectory(path); err != nil {
            writeWorkspacePolicyError(w, err)
            return
        }
        next.ServeHTTP(w, r)
    }
}
```

Register every `/api/workspaces/{workspaceID}/...` route through this wrapper. For session routes, resolve `session.Workspace` using a store method that preserves `404`, then check that workspace path with the same policy before invoking the existing handler. Do not duplicate checks inside business methods. Keep `GET /api/workspaces` filtering from Task 3.

- [ ] **Step 4: Run full backend tests and race tests**

Run:

```bash
gofmt -w internal/piweb/server_workspace_policy.go internal/piweb/server.go internal/piweb/store_workspace.go internal/piweb/store_sessions.go internal/piweb/server_workspace_handlers.go internal/piweb/server_session_handlers.go internal/piweb/server_session_status_handlers.go internal/piweb/server_workspace_policy_test.go
go test ./internal/piweb -run 'TestWorkspaceRoutesRejectForbiddenWorkspace|TestSessionRoutesRejectForbiddenWorkspace'
go test ./internal/piweb
go test -race ./internal/piweb
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/piweb/server_workspace_policy.go internal/piweb/server.go internal/piweb/store_workspace.go internal/piweb/store_sessions.go internal/piweb/server_workspace_handlers.go internal/piweb/server_session_handlers.go internal/piweb/server_session_status_handlers.go internal/piweb/server_workspace_policy_test.go
git commit -m "feat(api): authorize existing workspace access" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 5: Add typed frontend config state and multi-root settings UI

**Files:**
- Modify: `src/lib/api.ts`
- Modify: `src/pi-app/workspace/app-config-schema.ts`
- Modify: `src/pi-app/workspace/app-config-methods.ts`
- Modify: `src/App.astro`
- Modify: `src/styles/parts/settings-modal.css`
- Modify: `src/pi-app/test-helper.ts`
- Test: `src/pi-app/workspace/app-config-methods.test.ts`

**Interfaces:**
- Consumes: backend PATCH `/api/app-config` with `{workspace:{allowedRoots:string[]}}`.
- Produces:

```ts
export type AppConfigPatch = {
  brand?: string | null;
  workspace?: { allowedRoots: string[] } | null;
};
export function listFolders(path?: string, purpose?: "workspace" | "allowed-root"): Promise<FolderListing>;
```

- [ ] **Step 1: Write failing frontend tests**

Add tests for parsing, form state, and request shape:

```ts
it("preserves multiple allowed roots and sends only the app-config patch", async () => {
  const app = await connectPiApp();
  const response = parseAppConfigResponse({
    config: { version: 1, brand: "x", workspace: { allowedRoots: ["/a", "/b"] } },
    path: "/config",
  });
  expect(response.config.workspace.allowedRoots).toEqual(["/a", "/b"]);
  app.querySelector("[data-app-setting='allowed-roots']").value = "/a\n/b";
  await app.saveSettingsForm(new Event("submit"));
  expect(fetch).toHaveBeenCalledWith(expect.stringContaining("/api/app-config"), expect.objectContaining({
    method: "PATCH",
    body: JSON.stringify({ workspace: { allowedRoots: ["/a", "/b"] } }),
  }));
});
```

Cover add/remove rows, `purpose=allowed-root`, empty list as unrestricted, invalid response, and independence from project/global pi settings.

- [ ] **Step 2: Run focused tests and verify failure**

Run:

```bash
bunx vitest run src/pi-app/workspace/app-config-methods.test.ts -t 'allowed roots|workspace config|allowed-root'
```

Expected: FAIL because the schema, API purpose argument, form methods, and markup do not support allowed roots.

- [ ] **Step 3: Implement schema, API, and UI state**

Extend the Zod schema with a passthrough workspace object and narrow the API wrapper:

```ts
const appConfigPatchSchema = z.object({
  brand: z.string().nullable().optional(),
  workspace: z.object({ allowedRoots: z.array(z.string()) }).nullable().optional(),
});

export function saveAppConfig(config: AppConfigPatch) {
  return request("/api/app-config", { method: "PATCH", body: JSON.stringify(config) });
}
```

Add a labeled list section with `data-app-setting="allowed-roots"`, one text row per root, add/remove controls, an unrestricted hint for zero rows, and `data-app-config-path`. Add methods to render rows, collect trimmed non-empty roots, and apply the server response after save. Use `textContent` for path/status/row labels. Keep brand behavior and pi settings scope unchanged.

- [ ] **Step 4: Run frontend tests and typecheck**

Run:

```bash
bunx vitest run src/pi-app/workspace/app-config-methods.test.ts
bun run typecheck
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/lib/api.ts src/pi-app/workspace/app-config-schema.ts src/pi-app/workspace/app-config-methods.ts src/App.astro src/styles/parts/settings-modal.css src/pi-app/test-helper.ts src/pi-app/workspace/app-config-methods.test.ts
git commit -m "feat(ui): configure workspace allowed roots" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 6: Integrate restricted folder browsing and unauthorized-workspace recovery

**Files:**
- Modify: `src/pi-app/workspace/workspace-folder-methods.ts`
- Modify: `src/pi-app/workspace/workspace-methods.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts`
- Modify: `src/pi-app/index.ts`
- Test: `src/pi-app/workspace/workspace-methods-coverage.test.ts`
- Test: `src/pi-app/workspace/workspace-navigation.test.ts`
- Test: `src/pi-app/workspace/workspace-bootstrap-coverage.test.ts`
- Test: `src/pi-app/input/input-actions-coverage.test.ts`

**Interfaces:**
- Consumes: `listFolders(path, purpose)`, app-config state, and backend `403` errors from Tasks 3–5.
- Produces: ordinary picker requests with `purpose=workspace`, config picker requests with `purpose=allowed-root`, and safe recovery when the active workspace is no longer allowed.

- [ ] **Step 1: Write failing picker/recovery tests**

Add deterministic assertions:

```ts
it("uses the restricted purpose for ordinary folder browsing", async () => {
  await app.loadFolder("/allowed");
  expect(fetch).toHaveBeenCalledWith(expect.stringContaining("purpose=workspace"), expect.any(Object));
});

it("returns to picker when the active workspace becomes forbidden", async () => {
  globalThis.fetch = vi.fn(async () => ({ ok: false, status: 403, statusText: "Forbidden", json: async () => ({ error: "outside allowed roots" }) }));
  await expect(app.refreshWorkspaces()).resolves.toBeUndefined();
  expect(app.querySelector('[data-view="picker"]').hidden).toBe(false);
  expect(app.dataset.activeWorkspaceId).toBe("");
});
```

Also assert configuration root browsing sends `purpose=allowed-root`, forbidden messages are visible, filtered workspace lists do not render outside entries, and bootstrap config errors do not prevent workspace loading.

- [ ] **Step 2: Run focused tests and verify failure**

Run:

```bash
bunx vitest run src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/input/input-actions-coverage.test.ts -t 'allowed|forbidden|purpose|picker'
```

Expected: FAIL because current folder methods do not send a purpose and workspace refresh does not recover from `403`.

- [ ] **Step 3: Implement picker and recovery behavior**

Update `listFolders` calls so ordinary browsing uses `purpose=workspace` and settings root selection uses `purpose=allowed-root`. Preserve server error text instead of replacing it with a generic connection error. When a workspace or session request returns `403`, clear `data-active-workspace-id`, stop workspace-only loading, and route to picker; do not delete the SQLite record. Keep app-config loading independent of workspace selection.

- [ ] **Step 4: Run focused frontend tests and typecheck**

Run:

```bash
bunx vitest run src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/input/input-actions-coverage.test.ts
bun run typecheck
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/pi-app/workspace/workspace-folder-methods.ts src/pi-app/workspace/workspace-methods.ts src/pi-app/workspace/workspace-bootstrap-methods.ts src/pi-app/index.ts src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/input/input-actions-coverage.test.ts
git commit -m "feat(ui): recover from restricted workspaces" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 7: Document and run final verification

**Files:**
- Modify: `README.md`
- Test: existing backend and frontend suites

**Interfaces:**
- Consumes: final app-config, API, policy, and UI behavior from Tasks 1–6.
- Produces: user documentation and a verified branch without staged `static/` artifacts.

- [ ] **Step 1: Update README**

Add an exact example and behavior summary:

```text
"workspace": {
  "allowedRoots": ["~/code", "/workspaces"]
}
```

Document that missing or empty `allowedRoots` disables the restriction, non-empty roots are real existing directories, symlink escapes are rejected, historical outside workspaces are hidden/rejected, and deleting `pi-web-config.json` restores defaults.

- [ ] **Step 2: Run documentation diff check**

Run:

```bash
git diff --check -- README.md
```

Expected: PASS.

- [ ] **Step 3: Run all required verification**

Run each command separately and record its exit code:

```bash
bun run typecheck
bun run test
go test ./...
bun run backend:test
bun run build
```

Expected: all commands exit 0; build may emit the existing chunk-size warning. `git status --short` may show only pre-existing `static/` changes.

- [ ] **Step 4: Verify no generated artifacts are staged**

Run:

```bash
git diff --cached --name-only
 git diff --check -- . ':(exclude)static'
```

Expected: no staged `static/` path and no whitespace errors in feature files.

- [ ] **Step 5: Commit documentation**

```bash
git add README.md
git commit -m "docs: document workspace allowed roots" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

After this task, run a final review against the spec and all deferred findings before offering merge/PR/keep-branch options.
