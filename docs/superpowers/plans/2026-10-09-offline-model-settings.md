# Offline Model Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 管理员在 Pi 的 `models.json` 定义统一的离线模型目录，账号跨工作区保存各自的默认选择，会话只在目录内临时切换，设置页不再暴露范围概念。

**Architecture:** Go 服务端负责读取配置文件、对照 Pi 模型列表及校验模型选择；SQLite 继续存账号偏好，Pi 会话事件继续存会话模型。前端只展示过滤后的模型及账号偏好，把非模型/凭据/应用设置留在管理员高级区。

**Tech Stack:** Go `net/http`/`testing`/SQLite、Astro/TypeScript、Vitest、Bun；使用仓库已有命令与依赖。

**Spec:** `docs/superpowers/specs/2026-10-09-offline-model-settings-design.md`

## Global Constraints

- 离线 Go 单二进制；不增加联网依赖或硬编码密钥；不回传 `models.json` 原始内容、`baseUrl`、`apiKey`。
- 保留应用配置、非模型 Pi 设置、账号权限和会话数据；原项目级模型偏好不删除但不再覆盖有效模型。
- 仅管理员配置的 provider/model 组合可选；真实模式失败时不显示伪造回退模型；mock 模式单独保留原有测试数据。
- TDD：每任务先红后绿，跑相关测试并单独提交；不要追踪已有未跟踪的 `public/file-viewer/`。
- 改动涉及前端后运行 `bun run build:single` 并同步检查 `static/`；最终运行 lint、typecheck、前后端单测。

## File Map

- `internal/piweb/model_catalog.go`：配置文件路径、非秘密目录解析、CLI 可用性求交集、成员校验；`model_catalog_test.go` 覆盖失败与过滤。
- `internal/piweb/models.go`、`server_workspace_runtime_handlers.go`：真实模型列表走目录、mock 仍用固定数据；设置 API 校验与返回状态。
- `internal/piweb/settings.go`、`user_model_settings.go`、`server_model_settings.go`：账号全局默认、继承/失效计算和运行时初始化；旧项目偏好只读保留。
- `internal/piweb/server_session_status_handlers.go`、`session_projection.go` 及会话事件查询相关文件：切换校验、恢复时使用最新会话模型、状态不覆盖真实会话模型。
- `src/components/SettingsDialog.astro`、`src/pi-app/workspace/settings-methods.ts`、`settings-schema.ts`、`src/styles/parts/settings-modal.css`、`src/pi-app/test-helper.ts`：单模型下拉、管理员高级区、提示与交互。
- `README.md`、`static/`：部署/旧覆盖说明和嵌入资源。

### Task 1: 服务端可信模型目录

**Files:** Create `internal/piweb/model_catalog.go`, `internal/piweb/model_catalog_test.go`; Modify `internal/piweb/models.go`, `internal/piweb/server_workspace_runtime_handlers.go`, `internal/piweb/server_test.go`.

**Interfaces:** Produces `configuredModels(ctx context.Context, root string) (WorkspaceModelsResponse, error)` and `catalogContains(catalog WorkspaceModelsResponse, provider, model string) bool`; only true for configured+Pi可用的组合。`WorkspaceModels` remains CLI adapter. Model list API in real mode returns HTTP 502 and `{error: ...}` on failure, mock mode keeps deterministic `zai/gpt-5.5`. Admin-only list response includes `path` and `count` for diagnostics; regular users see only model metadata/error. Use `requestActor(r)` to gate path.

- [ ] **Step 1: Write failing tests.** Use `t.Setenv("HOME", home)` and `writeFakePi(t, root, script)` (see `server_test.go:487`); create `home/.pi/agent/models.json` with `{"providers":{"local":{"api":"openai-completions","apiKey":"secret","baseUrl":"http://127.0.0.1/v1","models":[{"id":"a"},{"id":"b"}]}}}`. Fake CLI outputs `local a`, `local b`, `builtin c`. Assert exact two options and that serialized response contains neither `secret` nor `baseUrl`. Table-test missing file, invalid JSON, duplicate model IDs, empty providers and CLI failure; assert error and no fabricated options. Update the current fallback test (`server_test.go:520`) to expect 502 and no `anthropic` fallback.
Test core assertions as Go, not string-only matching:

```go
catalog, err := configuredModels(t.Context(), root)
if err != nil || len(catalog.Providers) != 1 || len(catalog.Providers[0].Models) != 2 {
    t.Fatalf("unexpected catalog: %+v, %v", catalog, err)
}
if catalogContains(catalog, "builtin", "c") || !catalogContains(catalog, "local", "a") {
    t.Fatal("catalog whitelist is wrong")
}
```

- [ ] **Step 2: Run red test.** `go test ./internal/piweb -run 'TestConfiguredModels|TestWorkspaceModelsEndpoint' -count=1` → FAIL (catalog not implemented / old fallback).
- [ ] **Step 3: Implement.** Read `filepath.Join(os.UserHomeDir(), ".pi", "agent", "models.json")` with a typed minimal struct containing only provider/model IDs; reject invalid/empty/duplicate IDs. Call `WorkspaceModels(ctx, root)`; build the intersection by `(provider.ID, model.ID)`, stable-sort using existing `parseListModelsOutput` ordering. Never marshal configuration or include process command output containing potential secrets in client-facing errors. Replace the real-mode fallback branch in `workspaceModels` with explicit 502; do not change mock response.
Minimal shape for the new parser (complete it with explicit validation/errors and CLI intersection):

```go
type configuredProvider struct {
    Models []struct { ID string `json:"id"` } `json:"models"`
}
type configuredFile struct {
    Providers map[string]configuredProvider `json:"providers"`
}
func catalogContains(catalog WorkspaceModelsResponse, provider, model string) bool {
    for _, entry := range catalog.Providers {
        if entry.ID != provider { continue }
        for _, item := range entry.Models { if item.ID == model { return true } }
    }
    return false
}
```

- [ ] **Step 4: Run green test.** `go test ./internal/piweb -run 'TestConfiguredModels|TestWorkspaceModelsEndpoint' -count=1` → PASS.
- [ ] **Step 5: Commit.** `git add internal/piweb/model_catalog* internal/piweb/models.go internal/piweb/server_workspace_runtime_handlers.go internal/piweb/server_test.go && git commit -m "feat(models): expose configured offline catalog"`.

### Task 2: 账号级偏好与安全写入

**Files:** Modify `internal/piweb/settings.go`, `internal/piweb/user_model_settings.go`, `internal/piweb/server_workspace_runtime_handlers.go`, `internal/piweb/account_isolation_test.go`; Create `internal/piweb/model_selection_test.go`.

**Interfaces:** Produces `resolveModelSelection(catalog WorkspaceModelsResponse, global, personal map[string]any) (ModelStartupSettings, bool)` (`bool` denotes stale personal selection). `WorkspaceSettingsResponse` adds optional `modelSelection` object `{source: "personal"|"admin"|"first", invalidPersonal: boolean, catalogError?: string}`; model fields in `Effective` become resolved pair. On catalog failure, GET settings still returns non-model settings with empty effective model and `catalogError`; PUT fails. Only `personal` + `global` accepts model patch; `null` pair clears preference. Backend validates pair and catalog before database commit.

- [ ] **Step 1: Write failing tests.** Seed 3 models and two accounts using `accountServer`, `addAccount`, `accountRequest`; save different `{"scope":"personal","location":"global","settings":{"defaultProvider":"local","defaultModel":"a"}}` vs `b` and assert isolation and cross-workspace persistence. Test global administrator default -> sorted first, stale saved model -> fallback with `invalidPersonal=true`, project file / legacy `personalProject` never override. Assert 400 for unsupported pair, split pair, `personal+project`, admin shared model patch; assert no DB mutation and no project file write. Adapt `account_isolation_test.go` legacy expectations to approved behavior; keep non-model scope tests.
Use distinct account assertions:

```go
aliceSettings := accountRequest(s, http.MethodGet, "/api/workspaces/"+workspace.ID+"/settings", "", alice)
bobSettings := accountRequest(s, http.MethodGet, "/api/workspaces/"+workspace.ID+"/settings", "", bob)
if !strings.Contains(aliceSettings.Body.String(), `"defaultModel":"a"`) ||
    !strings.Contains(bobSettings.Body.String(), `"defaultModel":"b"`) {
    t.Fatalf("preferences leaked: alice=%s bob=%s", aliceSettings.Body.String(), bobSettings.Body.String())
}
```

- [ ] **Step 2: Run red test.** `go test ./internal/piweb -run 'TestAccountModel|TestResolveModel|TestAdminModel|TestRuntimeModelStatus' -count=1` → FAIL with old project precedence / no validation.
- [ ] **Step 3: Implement.** Keep non-model merge order in `WorkspaceSettingsForUser`, but strip `modelSettingKeys` from effective file/project defaults before overlay. Resolve pair using `catalogContains`: personal global → configured global default → lexically first catalog pair. Add explicit `modelSelection` metadata to response. In `saveWorkspaceSettings`, reject shared model keys and `personal+project`; require pair or pair-null (thinking-only accepted with `thinkingLevels` check); validate against `configuredModels` before `savePersonalModelSettings`. If catalog unavailable, return error without saving. Retain old DB project rows unread/untouched. Document mock-only handling in tests explicitly rather than relaxing real-mode checks.
Core precedence, with `modelPair` extracting only nonempty string pairs:

```go
func resolveModelSelection(catalog WorkspaceModelsResponse, global, personal map[string]any) (ModelStartupSettings, bool) {
    chosen, hasPersonal := modelPair(personal)
    if hasPersonal && catalogContains(catalog, chosen.Provider, chosen.Model) { return chosen, false }
    fallback, hasGlobal := modelPair(global)
    if hasGlobal && catalogContains(catalog, fallback.Provider, fallback.Model) { return fallback, hasPersonal }
    first := catalog.Providers[0].Models[0]
    return ModelStartupSettings{Provider: first.Provider, Model: first.ID}, hasPersonal
}
```

Handle empty catalog before calling this function and retain the selected thinking level separately. `modelPair` must not silently accept a half-filled pair.

- [ ] **Step 4: Run green test.** `go test ./internal/piweb -run 'TestAccountModel|TestResolveModel|TestAdminModel|TestRuntimeModelStatus' -count=1` → PASS.
- [ ] **Step 5: Commit.** `git add internal/piweb/settings.go internal/piweb/user_model_settings.go internal/piweb/server_workspace_runtime_handlers.go internal/piweb/account_isolation_test.go internal/piweb/model_selection_test.go && git commit -m "feat(settings): enforce account-wide model preferences"`.

### Task 3: 会话切换与恢复

**Files:** Modify `internal/piweb/server_session_status_handlers.go`, `internal/piweb/server_model_settings.go`, `internal/piweb/session_event_query.go`, `internal/piweb/server_session_runtime_handlers.go`; Test `internal/piweb/server_durable_prompt_test.go`, `internal/piweb/account_isolation_test.go`; Create `internal/piweb/session_model_catalog_test.go`.

**Interfaces:** `(*SQLiteSessionEventStore).LatestSessionModel(ctx context.Context, sessionID string) (ModelStartupSettings, bool, error)` reads newest `session.model.changed` payload; server uses an optional interface assertion on `s.events` (which is typed `SessionEventStore`) for compatibility with fake stores. `modelSettingsForSession(sessionID string) (ModelStartupSettings, error)` returns last valid session model event when present, otherwise owner default. `handleSessionSetModel` rejects unlisted pair before `ConfigureModel`; thinking-only changes do not erase selected pair. Runtime status overlay must not mask a session-specific selection.

- [ ] **Step 1: Write failing tests.** Using test accounts + workspace catalog, assert POST of model outside catalog is 400, owner mismatch remains 403, thinking-only update preserves model, valid model appends `EventSessionModelChanged`; simulate runtime restart then assert model initializer receives the session model, not newly changed account default. Add status test: model from session event beats personal default. If a selected model was removed, restart fails explicitly rather than silently selecting another model; history remains readable.
Minimal rejection assertion:

```go
response := accountRequest(s, http.MethodPost, "/api/sessions/"+session.ID+"/model",
    `{"provider":"other","model":"unlisted"}`, alice)
if response.Code != http.StatusBadRequest { t.Fatalf("outside catalog: %d %s", response.Code, response.Body.String()) }
```

- [ ] **Step 2: Run red test.** `go test ./internal/piweb -run 'TestSessionModelCatalog|TestRuntimeModelStatus|TestDurable' -count=1` → FAIL.
- [ ] **Step 3: Implement.** On POST, resolve session workspace, validate pair against `configuredModels` before runtime call. Implement `LatestSessionModel` in `session_event_query.go` with `SELECT payload_json FROM session_events WHERE session_id=? AND event_type=? ORDER BY seq DESC LIMIT 1`, using `EventSessionModelChanged`, `sql.ErrNoRows`, and `json.Unmarshal` into `ModelStartupSettings`. Distinguish no event from an explicit selection; if latest event is thinking-only, retrieve the preceding event that sets a pair. Select event pair only if still in catalog, else return explicit error; replace `overlayPersonalRuntimeModel`'s unconditional account overwrite with event-aware selection. Keep session authorization checks in place and avoid changing unrelated RPC commands.

```go
func (s *SQLiteSessionEventStore) LatestSessionModel(ctx context.Context, sessionID string) (ModelStartupSettings, bool, error) {
    rows, err := s.db.QueryContext(ctx,
        `SELECT payload_json FROM session_events WHERE session_id=? AND event_type=? ORDER BY seq DESC`,
        sessionID, EventSessionModelChanged)
    if err != nil { return ModelStartupSettings{}, false, err }
    defer rows.Close()
    for rows.Next() {
        var payload []byte
        if err := rows.Scan(&payload); err != nil { return ModelStartupSettings{}, false, err }
        var selected ModelStartupSettings
        if err := json.Unmarshal(payload, &selected); err != nil { return ModelStartupSettings{}, false, err }
        if selected.Provider != "" && selected.Model != "" { return selected, true, nil }
    }
    return ModelStartupSettings{}, false, rows.Err()
}
```
- [ ] **Step 4: Run green test.** `go test ./internal/piweb -run 'TestSessionModelCatalog|TestRuntimeModelStatus|TestDurable' -count=1` → PASS.
- [ ] **Step 5: Commit.** Stage only changed session Go files and tests, then `git commit -m "fix(session): retain catalog-validated session model"`.

### Task 4: 日常设置 UI 简化

**Files:** Modify `src/components/SettingsDialog.astro`, `src/pi-app/workspace/settings-methods.ts`, `src/pi-app/workspace/settings-schema.ts`, `src/pi-app/test-helper.ts`, `src/styles/parts/settings-modal.css`, `src/pi-app/input/controls.test.ts`, `src/pi-app/workspace/app-config-methods.test.ts`; add focused `src/pi-app/workspace/offline-model-settings.test.ts`.

**Interfaces:** Current `getWorkspaceModels` and `getWorkspaceSettings` remain typed client calls; `saveWorkspaceSettings(workspaceId, "personal", patch, "global")` is the only model-save path. `modelSelection` metadata parsed in `settings-schema.ts`; `data-action="apply-session-model"` explicitly applies current account selection, never on Save.

- [ ] **Step 1: Write failing tests.** Fixture returns catalog `local/a`, `local/b`, effective `local/a` and `modelSelection:{source:"first",invalidPersonal:false}`. Assert model dropdown has only `local/a`,`local/b` plus “使用管理员默认”; `custom`/scope toolbar absent from daily area. Choosing `local/b` sends `scope:personal,location:global` with both IDs. Selecting inherit sends both `null`. Save does not POST `/sessions/s1/model`; explicit apply button does. Missing catalog disables Save; `invalidPersonal` displays warning; regular user cannot see app config/auth; admin advanced details can. Update old tests asserting project scope/custom free text to the new contract.
Frontend request assertion:

```ts
expect(JSON.parse(putCall[1].body)).toEqual({
  scope: "personal", location: "global",
  settings: { defaultProvider: "local", defaultModel: "b" },
});
expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/sessions/s1/model"))).toBe(false);
```

- [ ] **Step 2: Run red test.** `bun run test -- src/pi-app/workspace/offline-model-settings.test.ts src/pi-app/input/controls.test.ts src/pi-app/workspace/app-config-methods.test.ts` → FAIL.
- [ ] **Step 3: Implement.** Keep existing settings field selectors where needed by tests, but replace provider+model free text inputs with a combined visible selector keyed by provider/model pair. On save split it into two validated patch fields, always target personal/global; moving application settings to admin `<details>` must not change admin password gate. Place non-model Pi scope controls inside admin advanced area; hide existing credential controls there too. Add diagnostic state without printing credentials or showing a fake option on error. Preserve keyboard labels/focus. Update test fixture and Zod schema; avoid changing unrelated UI.
Keep the combined option value unambiguous even when IDs contain `/`:

```ts
const optionValue = (provider: string, model: string) => JSON.stringify([provider, model]);
const [provider, model] = JSON.parse(modelControl.value) as [string, string];
const patch = parseSettingsPatch({ defaultProvider: provider, defaultModel: model });
await saveWorkspaceSettings(workspaceId, "personal", patch, "global");
```

Validate that the parsed pair occurs in `modelState.providers` before sending and handle the inherit option separately.

- [ ] **Step 4: Run green test.** `bun run test -- src/pi-app/workspace/offline-model-settings.test.ts src/pi-app/input/controls.test.ts src/pi-app/workspace/app-config-methods.test.ts && bun run typecheck` → PASS.
- [ ] **Step 5: Commit.** Stage only changed `src/` files and `git commit -m "feat(ui): simplify offline model settings"`.

### Task 5: 文档、回归和嵌入资源

**Files:** Modify `README.md`, generated `static/` contents only as required by build; update affected contract tests in `internal/piweb/server_test.go` and frontend tests if earlier tasks expose uncovered legacy assumptions.

**Interfaces:** No new runtime API; existing published single-binary behavior stays intact.

- [ ] **Step 1: Add failing acceptance/regression checks.** Run full `go test ./...` and `bun run test` to expose old tests expecting project model overrides; ensure tests enforce 3 configured candidates -> exactly 3 visible, different users retain own choices across workspaces and restart. Add the missing assertion to the relevant Task 1/2/4 test file before fixing any failure.
- [ ] **Step 2: Run red check.** Run targeted test for each added assertion and confirm its failure is caused by the missing behavior, not test setup.
- [ ] **Step 3: Implement minimal fixes and docs.** In `README.md` document `~/.pi/agent/models.json` ownership, supported Pi custom provider format (do not include a real key), `settings.json` admin default, per-account SQLite selection, no project-level model override and failure mode. Update prior tests without weakening authorization or catalog assertions.
README example must contain no real credential:

```json
{"providers":{"offline":{"baseUrl":"http://127.0.0.1:8000/v1","api":"openai-completions","apiKey":"OFFLINE_API_KEY","models":[{"id":"example-model"}]}}}
```

Check Pi's existing supported model schema and environment-variable convention before publishing that example; omit example rather than publishing inaccurate syntax. On missing/invalid catalog document explicit error behavior.

- [ ] **Step 4: Verify full build.** `bun run lint && bun run typecheck && bun run test && bun run backend:test && bun run build:single && bun run check:embedded-assets`; inspect output and remediate failures. Then `git diff --check && git status --short --branch`; inspect `static/` asset changes and exclude `public/file-viewer/`.
- [ ] **Step 5: Commit.** Stage `README.md`, relevant tests and required `static/` files only; `git commit -m "docs: explain offline model configuration"` (if static assets are included, use a separate `chore(assets): sync embedded frontend` commit). Report exact verification evidence and remaining blockers.
