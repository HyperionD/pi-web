# User-Level pi-web Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a user-level `pi-web-config.json` beside the existing `pi-web.db`, expose it through an independent application-config API, and use it to configure the top-left brand.

**Architecture:** Keep pi-web application configuration separate from pi native settings and SQLite workspace recents. The Go backend owns file location, validation, atomic persistence, and an API that does not require a workspace ID; the browser loads and saves the returned config and renders brand text with `textContent`.

**Tech Stack:** Go `net/http`, JSON file I/O, SQLite unchanged, Astro, TypeScript, Zod, Vitest, Go `testing`.

**Spec:** `docs/superpowers/specs/2026-08-31-user-config-design.md`

## Global Constraints

- 配置按当前操作系统用户持久化，同一用户启动的 pi-web 进程共享。
- 不复用、不修改 pi 的 `settings.json`、`auth.json` 等原生配置文件，也不写入项目 `.pi` 配置。
- 配置文件路径为 `<pi session 目录的父目录>/pi-web-config.json`，默认 `~/.pi/agent/pi-web-config.json`。
- 文件和父目录分别使用 `0600`、`0700` 权限。
- 不把应用配置写入 `localStorage`、`sessionStorage` 或 URL。
- `brand` 去除首尾空白，允许 Unicode 文本，拒绝空值以外的控制字符，最大长度为 120 个 Unicode 字符。
- brand 始终作为纯文本渲染，不能通过 `innerHTML` 注入标记或脚本。
- 现有 SQLite 继续只管理最近打开的 workspace，不新增配置表。
- 本次不实现工作区允许目录限制，只保留后续配置扩展和后端校验边界。
- 验证命令必须包含 `bun run typecheck`、`bun run test`、`bun run backend:test`、`bun run build`。

---

## File Map

- Create `internal/piweb/app_config.go`: 用户级配置路径、默认值、JSON 读取、字段校验、patch 合并和原子写入。
- Create `internal/piweb/app_config_test.go`: 配置文件存储与校验的单元测试。
- Create `internal/piweb/server_app_config_handlers.go`: 独立应用配置 API handler。
- Modify `internal/piweb/server.go`: 注册 `GET/PATCH /api/app-config`。
- Create `internal/piweb/server_app_config_test.go`: 无 workspace 依赖的 API 测试。
- Modify `src/lib/api.ts`: 提供应用配置 GET/PATCH 请求函数。
- Create `src/pi-app/workspace/app-config-schema.ts`: 前端响应校验与类型定义。
- Create `src/pi-app/workspace/app-config-methods.ts`: PiApp 的应用配置加载、填充、应用和保存方法。
- Modify `src/pi-app/index.ts`: 注册应用配置方法并在启动时加载。
- Modify `src/pi-app/workspace/workspace-bootstrap-methods.ts`: 启动时并行加载 app config，失败不阻塞 workspace。
- Modify `src/pi-app/workspace/settings-methods.ts`: 让应用配置与 pi settings 分开保存、反馈错误。
- Modify `src/App.astro`: 增加 `[data-brand]` 和用户级应用设置字段。
- Modify `src/styles/parts/settings-modal.css`: 应用配置区的布局和错误状态样式。
- Create `src/pi-app/workspace/app-config-methods.test.ts`: 前端加载、纯文本渲染和保存测试。
- Modify `src/pi-app/input/input-methods-coverage.test.ts` 或新增集成测试：验证设置保存和无 workspace 场景。
- Modify `README.md`: 记录配置路径与 reset 方式。

---

### Task 1: Implement the user-level JSON storage

**Files:**
- Create: `internal/piweb/app_config.go`
- Test: `internal/piweb/app_config_test.go`

**Interfaces:**
- Produces `AppConfigPath() (string, error)` returning the parent of `DefaultPiSessionDir()` joined with `pi-web-config.json`.
- Produces `LoadAppConfig() (AppConfigResponse, error)`.
- Produces `SaveAppConfig(patch map[string]any) (AppConfigResponse, error)`.
- Produces `DefaultAppBrand = "pi.dev / web"`; the versioned default config map remains internal to the storage module.
- `AppConfigResponse` has `Config map[string]any` and `Path string` JSON fields.

- [ ] **Step 1: Write failing storage tests**

Add tests with `t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(t.TempDir(), "sessions"))` that assert:

```go
func TestAppConfigPathSharesPiAgentParentWithoutUsingPiSettingsName(t *testing.T) {
    sessionDir := filepath.Join(t.TempDir(), "agent", "sessions")
    t.Setenv("PI_CODING_AGENT_SESSION_DIR", sessionDir)

    path, err := AppConfigPath()

    if err != nil || path != filepath.Join(filepath.Dir(sessionDir), "pi-web-config.json") {
        t.Fatalf("path=%q err=%v", path, err)
    }
}

func TestLoadAppConfigReturnsDefaultWithoutCreatingFile(t *testing.T) {
    root := t.TempDir()
    t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))

    response, err := LoadAppConfig()

    if err != nil || response.Config["brand"] != DefaultAppBrand {
        t.Fatalf("response=%+v err=%v", response, err)
    }
    if _, err := os.Stat(filepath.Join(root, "pi-web-config.json")); !errors.Is(err, os.ErrNotExist) {
        t.Fatalf("expected no file creation, got %v", err)
    }
}
```

Also cover: valid save, `brand: nil` reset, unknown-key preservation, wrong type, control character, 121-rune brand, empty session-dir error, and file permissions.

- [ ] **Step 2: Run the focused tests and confirm expected failure**

Run:

```bash
go test ./internal/piweb -run 'Test(AppConfig|LoadAppConfig)'
```

Expected: FAIL because `AppConfigPath`, `LoadAppConfig`, `SaveAppConfig`, and `DefaultAppBrand` do not exist.

- [ ] **Step 3: Implement the minimal storage module**

Implement these rules in `app_config.go`:

- Derive the path from `DefaultPiSessionDir`; return an explicit error if it is empty.
- Use a default map `{ "version": float64(1), "brand": DefaultAppBrand }` and clone maps before merging.
- Read a missing or zero-length file as an empty map merged over defaults; reject malformed JSON with the file path in the error.
- Validate only known `brand` values: accept string or `nil`; trim strings; reject control characters and `utf8.RuneCountInString(value) > 120`; a blank string or nil deletes the explicit `brand` key so the effective response uses the default.
- Preserve unknown top-level keys while setting `version` to `1` when absent.
- Create parent directories with `0700`; write a temporary file in the same directory with `0600`, encode indented JSON, call `Sync`, close, and replace the target with `os.Rename`.
- Return the effective response and path after every save.

- [ ] **Step 4: Run storage tests and confirm they pass**

Run:

```bash
go test ./internal/piweb -run 'Test(AppConfig|LoadAppConfig)'
```

Expected: all focused app-config tests PASS.

- [ ] **Step 5: Commit the storage unit**

```bash
git add internal/piweb/app_config.go internal/piweb/app_config_test.go
git commit -m "feat(config): add user-level pi-web config storage" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 2: Expose the independent application-config API

**Files:**
- Create: `internal/piweb/server_app_config_handlers.go`
- Create: `internal/piweb/server_app_config_test.go`
- Modify: `internal/piweb/server.go:routes`

**Interfaces:**
- `GET /api/app-config` calls `LoadAppConfig` and returns `{ "config": ..., "path": ... }`.
- `PATCH /api/app-config` decodes one JSON object into `map[string]any`, calls `SaveAppConfig`, and returns the same response shape.
- The handler never calls `Store.WorkspacePath` and works with an empty `Store`.

- [ ] **Step 1: Write failing API tests**

Create a server with `NewServer(Config{}, NewWebStore(filepath.Join(t.TempDir(), "web.db")), NewBroker())`, set `PI_CODING_AGENT_SESSION_DIR`, and assert:

```go
func TestAppConfigAPIWorksWithoutWorkspace(t *testing.T) {
    server := NewServer(Config{}, NewWebStore(filepath.Join(t.TempDir(), "web.db")), NewBroker())

    get := httptest.NewRecorder()
    server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/app-config", nil))
    if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"brand":"pi.dev / web"`) {
        t.Fatalf("GET %d %s", get.Code, get.Body.String())
    }

    put := httptest.NewRecorder()
    request := httptest.NewRequest(http.MethodPatch, "/api/app-config", strings.NewReader(`{"brand":"JuunAI / web"}`))
    server.Handler().ServeHTTP(put, request)
    if put.Code != http.StatusOK || !strings.Contains(put.Body.String(), `"brand":"JuunAI / web"`) {
        t.Fatalf("PATCH %d %s", put.Code, put.Body.String())
    }
}
```

Add tests for malformed JSON (`400`), invalid brand (`400`), and storage failure (`500`).

- [ ] **Step 2: Run API tests and confirm expected failure**

Run:

```bash
go test ./internal/piweb -run TestAppConfigAPI
```

Expected: FAIL with route not found or missing handler.

- [ ] **Step 3: Register and implement the handlers**

Add:

```go
s.mux.HandleFunc("GET /api/app-config", s.appConfig)
s.mux.HandleFunc("PATCH /api/app-config", s.patchAppConfig)
```

`appConfig` maps `LoadAppConfig` errors to `500`. `patchAppConfig` uses `readJSON` with `map[string]any`, rejects a nil top-level object, maps validation errors to `400`, and maps file I/O errors to `500`.

- [ ] **Step 4: Run API tests and the existing server tests**

Run:

```bash
go test ./internal/piweb -run 'TestAppConfigAPI|Test.*Endpoint'
```

Expected: app-config API and existing endpoint tests PASS.

- [ ] **Step 5: Commit the API unit**

```bash
git add internal/piweb/server_app_config_handlers.go internal/piweb/server_app_config_test.go internal/piweb/server.go
git commit -m "feat(api): expose user-level app config" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 3: Add frontend app-config API, schema, and state methods

**Files:**
- Modify: `src/lib/api.ts`
- Create: `src/pi-app/workspace/app-config-schema.ts`
- Create: `src/pi-app/workspace/app-config-methods.ts`
- Test: `src/pi-app/workspace/app-config-methods.test.ts`

**Interfaces:**
- `getAppConfig()` calls `GET /api/app-config`.
- `saveAppConfig(config)` calls `PATCH /api/app-config` with `JSON.stringify(config)`.
- `parseAppConfigResponse(value)` validates `{ config: { version: number, brand: string, ... }, path: string }` and preserves unknown keys.
- `appConfigMethods.loadAppConfigState()` stores `this.appConfigState` and calls `applyBrand`.
- `appConfigMethods.applyBrand(brand)` writes the normalized value to `[data-brand]` using `textContent` and falls back to `DEFAULT_APP_BRAND`.
- `appConfigMethods.fillAppConfigForm()` and `appConfigMethods.appConfigPatchFromForm()` operate on `[data-app-setting='brand']` only.

- [ ] **Step 1: Write failing frontend tests**

Create a jsdom fixture containing `<span data-brand></span>` and a brand input. Assert:

```ts
it("loads the server brand without a workspace id", async () => {
  const app = await connectPiApp();
  globalThis.fetch = vi.fn(async () => ok({
    config: { version: 1, brand: "JuunAI / web" },
    path: "/home/me/.pi/agent/pi-web-config.json",
  }));

  await app.loadAppConfigState();

  expect(app.querySelector("[data-brand]").textContent).toBe("JuunAI / web");
  expect(globalThis.fetch.mock.calls[0][0]).toBe("http://backend.test/api/app-config");
});

it("renders brand as text rather than HTML", async () => {
  const app = await connectPiApp();
  app.applyBrand("<b>unsafe</b>");

  expect(app.querySelector("[data-brand]").textContent).toBe("<b>unsafe</b>");
  expect(app.querySelector("[data-brand] b")).toBeNull();
});
```

Also test malformed response fallback, PATCH body `{ brand: "next" }`, and blank brand restoring `DEFAULT_APP_BRAND`.

- [ ] **Step 2: Run the focused tests and confirm expected failure**

Run:

```bash
bunx vitest run src/pi-app/workspace/app-config-methods.test.ts
```

Expected: FAIL because the API functions, schema, methods, and `[data-brand]` fixture support do not exist.

- [ ] **Step 3: Implement the frontend API and methods**

Add the API functions alongside existing `request` wrappers. Use Zod `passthrough()` for unknown config keys. In `loadAppConfigState`, catch only at the bootstrap caller; the method itself should throw so the caller can report the failure. Normalize `brand` with `trim()` and use the default when empty or non-string. Never use `innerHTML` for brand.

- [ ] **Step 4: Run the focused frontend tests and confirm they pass**

Run:

```bash
bunx vitest run src/pi-app/workspace/app-config-methods.test.ts
```

Expected: all app-config frontend tests PASS.

- [ ] **Step 5: Commit the frontend state unit**

```bash
git add src/lib/api.ts src/pi-app/workspace/app-config-schema.ts src/pi-app/workspace/app-config-methods.ts src/pi-app/workspace/app-config-methods.test.ts
git commit -m "feat(ui): add app config client state" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 4: Wire brand loading and independent settings UI

**Files:**
- Modify: `src/App.astro`
- Modify: `src/pi-app/index.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts`
- Modify: `src/pi-app/workspace/settings-methods.ts`
- Modify: `src/styles/parts/settings-modal.css`
- Modify: `src/pi-app/test-helper.ts`
- Test: `src/pi-app/workspace/app-config-methods.test.ts`
- Test: `src/pi-app/input/input-methods-coverage.test.ts`

**Interfaces:**
- The top bar contains one `[data-brand]` text node initialized to `pi.dev / web` plus the existing cursor decoration.
- `bootstrapAPI` starts app-config loading without waiting for workspace selection; a failed app-config request leaves the default brand and does not skip workspace loading.
- Settings modal contains a clearly labeled user-level pi-web section with `data-app-setting="brand"` and `data-app-config-path`.
- `saveSettingsForm` saves app config through `saveAppConfig` independently of the pi settings scope; no workspace ID is sent to that request.

- [ ] **Step 1: Extend fixtures and write failing integration tests**

Add `[data-brand]`, `data-app-setting="brand"`, and `data-app-config-path` to `installPiAppFixture`. Add tests that:

- call `bootstrapAPI` with a successful app-config response and a workspace response, then assert brand text and both requests occurred;
- make app-config GET fail and workspace GET succeed, then assert workspace bootstrap still runs and the default brand remains;
- set the app-config input to `Custom`, submit settings without an active workspace, and assert one PATCH to `/api/app-config` with `{ brand: "Custom" }`;
- switch the pi settings scope between project/global and assert the app-config PATCH remains user-level and unchanged;
- save `<img src=x onerror=alert(1)>` and assert the top bar contains that literal text and no `img` element.

- [ ] **Step 2: Run the integration tests and confirm expected failure**

Run:

```bash
bunx vitest run src/pi-app/workspace/app-config-methods.test.ts src/pi-app/input/input-methods-coverage.test.ts -t 'app config|brand|user-level'
```

Expected: FAIL because the Astro markup, bootstrap wiring, and save coordination are not present.

- [ ] **Step 3: Implement the UI wiring**

Make these changes without changing pi settings semantics:

- Replace the fixed split brand markup with a `[data-brand]` text span initialized to `pi.dev / web`; keep the cursor block.
- Mix `appConfigMethods` into `PiApp` in `src/pi-app/index.ts`.
- In `bootstrapAPI`, start `loadAppConfigState()` in parallel with `getWorkspaces()` and catch its error by keeping the default brand; do not enter the outer workspace-failure catch for an app-config-only error.
- Add a `pi-web 应用设置` section outside the project/global pi settings field groups. Show the config path returned by the API and make its input independent from `[data-settings-scope]`.
- On opening settings, load app config even when `activeWorkspaceId` is empty. On submit, save the app-config patch separately; save pi settings only when a workspace and pi settings patch are available. Report each failure in the existing settings status and only show “saved” after all requested saves succeed.
- Use `textContent` for every brand update and preserve the default on read failures.
- Add only the required CSS for the section, path hint, disabled/loading state, and existing `.err` treatment.

- [ ] **Step 4: Run the integration tests and confirm they pass**

Run:

```bash
bunx vitest run src/pi-app/workspace/app-config-methods.test.ts src/pi-app/input/input-methods-coverage.test.ts
```

Expected: all selected app-config and existing input tests PASS.

- [ ] **Step 5: Commit the UI wiring**

```bash
git add src/App.astro src/pi-app/index.ts src/pi-app/workspace/workspace-bootstrap-methods.ts src/pi-app/workspace/settings-methods.ts src/styles/parts/settings-modal.css src/pi-app/test-helper.ts src/pi-app/workspace/app-config-methods.test.ts src/pi-app/input/input-methods-coverage.test.ts
git commit -m "feat(ui): configure user-level brand" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 5: Document the config contract and run the full verification suite

**Files:**
- Modify: `README.md`
- Review: `docs/superpowers/specs/2026-08-31-user-config-design.md`

- [ ] **Step 1: Document the user-level path and reset behavior**

Add a concise configuration note to README covering:

```text
Default: ~/.pi/agent/pi-web-config.json
Custom session directory: <parent of PI_CODING_AGENT_SESSION_DIR>/pi-web-config.json
Reset: delete pi-web-config.json; pi-web will use the default brand.
```

State explicitly that pi native `settings.json` and `auth.json`, project `.pi` files, browser storage, and SQLite are not used for this brand setting.

- [ ] **Step 2: Run frontend typecheck and tests**

Run:

```bash
bun run typecheck
bun run test
```

Expected: typecheck succeeds and Vitest reports zero failures.

- [ ] **Step 3: Run backend tests**

Run:

```bash
bun run backend:test
```

Expected: all Go packages pass, including storage permissions, malformed JSON, API status, and no-workspace cases.

- [ ] **Step 4: Build the static frontend**

Run:

```bash
bun run build
```

Expected: Astro completes successfully. Existing chunk-size warnings may remain, but the command must exit 0.

- [ ] **Step 5: Review the final diff and commit documentation**

Run:

```bash
git diff --check -- README.md internal/piweb src/lib src/pi-app src/App.astro src/styles/parts/settings-modal.css
git status --short
```

Do not stage the pre-existing generated `static/` changes. Then commit:

```bash
git add README.md
git commit -m "docs: document pi-web user config" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

- [ ] **Step 6: Confirm the final requirement checklist**

Verify from the diff and tests that:

- `pi-web-config.json` is beside `pi-web.db` and does not reuse pi config filenames.
- Brand is shared by processes of the same OS user and is not browser-session state.
- API works before any workspace is opened.
- Brand is rendered as escaped text.
- Existing SQLite and pi settings behavior is unchanged.
- No workspace allowlist behavior was added prematurely.
