# 文件传输与上传限制 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在设置中配置工作区上传上限，使用流式上传与带进度下载，且为超限对话附件提供工作区引导。

**Architecture:** 保留现有工作区授权和 raw 下载接口；上传接口新增 multipart 流式格式，服务端使用 app config 的 `workspace.maxUploadBytes` 限制并以临时文件原子替换。浏览器端在 API 层封装 XHR 上传/下载进度，文件树负责操作菜单和状态展示，对话输入负责附件限制提示。

**Tech Stack:** Go `net/http`、JSON app config、multipart streaming、TypeScript、React、XHR、Vitest、Go testing。

**Spec:** `docs/superpowers/specs/2026-09-07-file-transfer-design.md`

## Global Constraints

- 默认工作区上传上限为 `100 MiB`。
- 配置字段为 `workspace.maxUploadBytes`，最小 `1 MiB`，最大 `2 GiB`，清除后恢复默认值。
- 新前端上传必须使用 `multipart/form-data`；服务端短期兼容旧 JSON/Base64 上传。
- 所有工作区操作继续经过 workspace access、allowed roots、`SafeJoin` 和真实路径边界检查。
- 上传必须写临时文件并在成功后替换；失败不得留下半文件。
- 不实现断点续传、分片上传、并发上传队列或自动把对话附件复制到工作区。
- 每个任务先写失败测试、确认 RED，再写最小实现、确认 GREEN。

---

### Task 1: App config 上传上限模型与校验

**Files:**
- Modify: `internal/piweb/app_config.go`
- Test: `internal/piweb/app_config_test.go`
- Modify: `src/pi-app/workspace/app-config-schema.ts`
- Test: `src/pi-app/workspace/app-config-methods.test.ts`

**Interfaces:**
- Produces `DefaultMaxUploadBytes`, `MinMaxUploadBytes`, `MaxMaxUploadBytes` 常量，以及从 app config 中读取生效上传限制的 Go helper。
- Produces `appConfigResponseSchema` 可解析的 `workspace.maxUploadBytes?: number` 字段；API patch 类型在 Task 3 与传输 API 一起更新。

- [ ] **Step 1: Write the failing Go tests**

在 `app_config_test.go` 增加以下行为测试：

```go
func TestLoadAppConfigReturnsDefaultMaxUploadBytes(t *testing.T) {
    response, err := LoadAppConfig()
    if err != nil { t.Fatal(err) }
    workspace := response.Config["workspace"].(map[string]any)
    if workspace["maxUploadBytes"] != float64(DefaultMaxUploadBytes) { t.Fatalf("unexpected default: %#v", workspace) }
}

func TestSaveAppConfigValidatesMaxUploadBytes(t *testing.T) {
    valid := []any{float64(MinMaxUploadBytes), float64(MaxMaxUploadBytes)}
    for _, value := range valid {
        response, err := SaveAppConfig(map[string]any{"workspace": map[string]any{"maxUploadBytes": value}})
        if err != nil { t.Fatalf("valid value %v: %v", value, err) }
        workspace := response.Config["workspace"].(map[string]any)
        if workspace["maxUploadBytes"] != value { t.Fatalf("saved %v as %#v", value, workspace) }
    }
    for _, value := range []any{float64(MinMaxUploadBytes - 1), float64(MaxMaxUploadBytes + 1), 1.5, "100"} {
        if _, err := SaveAppConfig(map[string]any{"workspace": map[string]any{"maxUploadBytes": value}}); err == nil { t.Fatalf("expected rejection for %#v", value) }
    }
}

func TestSaveAppConfigClearingMaxUploadBytesRestoresDefault(t *testing.T) {
    if _, err := SaveAppConfig(map[string]any{"workspace": map[string]any{"maxUploadBytes": float64(2 * MinMaxUploadBytes)}}); err != nil { t.Fatal(err) }
    response, err := SaveAppConfig(map[string]any{"workspace": map[string]any{"maxUploadBytes": nil}})
    if err != nil { t.Fatal(err) }
    workspace := response.Config["workspace"].(map[string]any)
    if workspace["maxUploadBytes"] != float64(DefaultMaxUploadBytes) { t.Fatalf("unexpected cleared value: %#v", workspace) }
}
```

- [ ] **Step 2: Run the focused Go tests and verify RED**

Run: `go test ./internal/piweb -run 'Test(LoadAppConfigReturnsDefaultMaxUploadBytes|SaveAppConfigValidatesMaxUploadBytes|SaveAppConfigClearingMaxUploadBytesRestoresDefault)$'`

Expected: FAIL because the default field, constants, or nested validation do not exist.

- [ ] **Step 3: Implement minimal Go config support**

Add constants in `app_config.go`, add `workspace.maxUploadBytes` to `defaultAppConfig`, normalize it in `normalizeStoredWorkspaceConfig`, merge nested workspace defaults in `appConfigResponse`, and reject non-integer values outside `[MinMaxUploadBytes, MaxMaxUploadBytes]`. Preserve `allowedRoots` normalization and delete the field when patch value is `nil`.

- [ ] **Step 4: Run the focused Go tests and verify GREEN**

Run the same `go test` command. Expected: PASS.

- [ ] **Step 5: Add the failing TypeScript schema test**

Extend `src/pi-app/workspace/app-config-methods.test.ts` with a response containing `workspace.maxUploadBytes` and assert `parseAppConfigResponse` preserves the number.

- [ ] **Step 6: Run the focused frontend test and verify RED**

Run: `bunx vitest run src/pi-app/workspace/app-config-methods.test.ts`

Expected: FAIL because the schema and form helper do not expose `maxUploadBytes`.

- [ ] **Step 7: Implement the TypeScript schema field**

Add `workspace: { allowedRoots?: string[]; maxUploadBytes?: number }` to `appConfigPatchSchema` and the response schema; keep `.passthrough()` for forward-compatible app config keys.

- [ ] **Step 8: Run the focused frontend test and verify GREEN**

Run: `bunx vitest run src/pi-app/workspace/app-config-methods.test.ts`. Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/piweb/app_config.go internal/piweb/app_config_test.go src/pi-app/workspace/app-config-schema.ts src/pi-app/workspace/app-config-methods.test.ts
git commit -m "feat: add configurable workspace upload limit" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 2: 流式工作区上传与服务端兼容

**Files:**
- Modify: `internal/piweb/server_workspace_handlers.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/files.go`
- Test: `internal/piweb/server_workspace_handlers_test.go` (create if absent)
- Test: `internal/piweb/files_test.go`

**Interfaces:**
- Produces multipart upload handling on `POST /api/workspaces/{workspaceID}/files/upload` with fields `path`, `overwrite`, `file`.
- Preserves the existing JSON/Base64 request shape for legacy callers, but applies the configured byte limit.
- Returns `413` with a readable configured-limit error when the file exceeds the limit.

- [ ] **Step 1: Write failing backend tests**

Add tests that set `workspace.maxUploadBytes` to a small value and assert:

```go
func TestUploadWorkspaceFileEndpointAcceptsMultipartAndReportsTooLarge(t *testing.T) {
    // multipart body: path=docs/a.bin, overwrite=false, file=large payload
    response := testServer.Handler().ServeHTTP(...)
    if response.Code != http.StatusRequestEntityTooLarge { t.Fatalf("status=%d body=%s", response.Code, response.Body.String()) }
    if !strings.Contains(response.Body.String(), "max") { t.Fatal("missing readable limit") }
}

func TestUploadWorkspaceFileEndpointWritesMultipartFileAndSupportsOverwrite(t *testing.T) {
    // upload docs/a.txt with "first", assert file contents, upload overwrite=false and assert conflict,
    // upload overwrite=true with "second", assert replacement.
}
```

Add a direct file-operation test that forces a write failure/oversize path and asserts no `.tmp` file remains in the target directory.

- [ ] **Step 2: Run focused Go tests and verify RED**

Run: `go test ./internal/piweb -run 'TestUploadWorkspaceFileEndpoint|TestUploadWorkspaceFile.*Temp'`

Expected: FAIL because the handler only decodes JSON and has no configured multipart limit.

- [ ] **Step 3: Implement configured byte-limit helpers**

Add a helper that loads `LoadAppConfig`, reads `workspace.maxUploadBytes`, and returns the default on a valid effective response. Add a bounded streaming reader using `http.MaxBytesReader` or an equivalent `limit+1` reader so one extra byte can be detected without buffering the full file.

- [ ] **Step 4: Implement multipart upload transaction**

In `uploadWorkspaceFile`, branch on `Content-Type`: multipart reads `path`, `overwrite`, and `file`, validates `path` with `SafeJoin` before creating the temp file, streams to a `0600` temp file in the destination directory, rejects over-limit input with `413`, then calls a file helper that applies existing/overwrite semantics and atomically renames the temp file. Keep the old JSON branch and replace its fixed body parsing with a configured limit only for this endpoint. Ensure every failure closes and removes the temp file.

- [ ] **Step 5: Run focused Go tests and verify GREEN**

Run: `go test ./internal/piweb -run 'TestUploadWorkspaceFileEndpoint|TestUploadWorkspaceFile.*Temp'`. Expected: PASS.

- [ ] **Step 6: Run existing file/server tests**

Run: `go test ./internal/piweb -run 'Test(UploadWorkspaceFile|Server|WorkspaceFile|WorkspacePolicy)'`. Expected: PASS; unrelated environment-dependent failures must be recorded rather than hidden.

- [ ] **Step 7: Commit**

```bash
git add internal/piweb/server_workspace_handlers.go internal/piweb/server.go internal/piweb/files.go internal/piweb/server_workspace_handlers_test.go internal/piweb/files_test.go
git commit -m "feat: stream workspace file uploads" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 3: 浏览器上传/下载 API 与进度事件

**Files:**
- Modify: `src/lib/api.ts`
- Test: `src/lib/api.test.ts`
- Create: `src/lib/file-transfer.ts`
- Test: `src/lib/file-transfer.test.ts`

**Interfaces:**
- Produces `uploadWorkspaceFile(workspaceId: string, path: string, file: File, overwrite: boolean, onProgress?: (loaded: number, total?: number) => void): Promise<unknown>` using multipart XHR.
- Produces `downloadWorkspaceFile(workspaceId: string, path: string, onProgress?: (loaded: number, total?: number) => void): Promise<{ blob: Blob; filename: string }>` using XHR.
- Produces `formatTransferBytes(bytes: number): string` and `transferPercent(loaded: number, total?: number): number | undefined` for shared UI formatting.

- [ ] **Step 1: Write failing transport tests**

In `file-transfer.test.ts`, install a deterministic fake `XMLHttpRequest` and test that upload opens the workspace endpoint, appends `path`, `overwrite`, and `file` to `FormData`, forwards `loaded/total`, resolves JSON on `2xx`, and rejects with the server error on `413`. Test download forwards `loaded/total`, resolves a Blob, and revokes its object URL only in the UI consumer.

- [ ] **Step 2: Run focused frontend tests and verify RED**

Run: `bunx vitest run src/lib/file-transfer.test.ts`. Expected: FAIL because the module and XHR functions do not exist.

- [ ] **Step 3: Implement XHR transport**

Create a small XHR wrapper that applies `apiBase()`, sets `Content-Type` only for JSON helpers (never manually for `FormData`), parses JSON error bodies, reports upload progress from `xhr.upload.onprogress`, reports download progress from `xhr.onprogress`, and rejects non-2xx responses. Keep the existing fetch-based `request` helper unchanged for other APIs.

- [ ] **Step 4: Route public API functions through the helper**

Update `src/lib/api.ts` to export the upload and download functions. Keep a compatibility test for existing JSON API helpers and replace the old upload test’s Base64 body expectation with a multipart invocation expectation.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run: `bunx vitest run src/lib/file-transfer.test.ts src/lib/api.test.ts`. Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src/lib/api.ts src/lib/api.test.ts src/lib/file-transfer.ts src/lib/file-transfer.test.ts
git commit -m "feat: add progress-aware file transfer client" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 4: 设置界面配置上传上限

**Files:**
- Modify: `src/App.astro`
- Modify: `src/pi-app/workspace/app-config-methods.ts`
- Modify: `src/pi-app/workspace/app-config-methods.test.ts`
- Modify: `src/styles/parts/settings-modal.css`

**Interfaces:**
- Consumes `appConfigState.config.workspace.maxUploadBytes` and `saveAppConfig`.
- Produces a numeric `data-app-setting="max-upload-mib"` control that loads/saves MiB and retains the existing admin-password prompt.

- [ ] **Step 1: Write the failing form tests**

Add tests for `fillAppConfigForm` showing `100` for the default, showing a saved value such as `256` for `268435456` bytes, and `appConfigPatchFromForm` returning `{ workspace: { maxUploadBytes: 268435456 } }` when the value changes. Add invalid input tests for blank, below `1`, above `2048`, and non-numeric values.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `bunx vitest run src/pi-app/workspace/app-config-methods.test.ts`. Expected: FAIL because the control and conversion helpers do not exist.

- [ ] **Step 3: Add the settings field and conversion helpers**

Add a labeled numeric input under the existing `pi-web 应用设置` section with hint `MiB；1–2048，保存后立即生效`. Add `maxUploadBytesToMiB` and `maxUploadMiBToBytes` helpers using integer checks; invalid values should make the settings save show a clear error instead of sending a malformed patch. Preserve allowed roots and brand patch behavior.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run: `bunx vitest run src/pi-app/workspace/app-config-methods.test.ts`. Expected: PASS.

- [ ] **Step 5: Run typecheck**

Run: `bun run typecheck`. Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src/App.astro src/pi-app/workspace/app-config-methods.ts src/pi-app/workspace/app-config-methods.test.ts src/styles/parts/settings-modal.css
git commit -m "feat: expose workspace upload limit in settings" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 5: 文件树上传/下载进度与下载操作

**Files:**
- Modify: `src/components/WorkspaceFileTree.tsx`
- Modify: `src/components/WorkspaceFileTree.test.tsx`
- Modify: `src/styles/parts/tree-preview-status.css`
- Modify: `src/App.astro` only if the status region needs a shared mount point

**Interfaces:**
- Consumes the Task 3 transport callbacks.
- Produces file menu action `download`, sequential multi-file upload progress, and an accessible live status region with no unsafe HTML interpolation.

- [ ] **Step 1: Write failing component tests**

Add tests that open a file row menu and assert “下载”; mock the transport callback to emit `loaded=50,total=100` and assert `50%` plus the filename; assert upload passes the actual `File` rather than a Base64 string; assert a failed upload/download displays a readable error and clears the progress state.

- [ ] **Step 2: Run focused component tests and verify RED**

Run: `bunx vitest run src/components/WorkspaceFileTree.test.tsx`. Expected: FAIL because the menu, transport callbacks, and status region do not exist.

- [ ] **Step 3: Implement file tree transfer state**

Add a `TransferState` union and local state. Make `TreeActionMenu` render download only for `target.kind === "file"`; invoke `downloadWorkspaceFile`, create an object URL, click a hidden anchor with the basename, and revoke the URL in `finally`. Replace `fileToBase64` upload with the Task 3 multipart function, process selected files sequentially, and update progress for each file. Keep overwrite confirmation and refresh behavior.

- [ ] **Step 4: Add accessible progress markup and styles**

Render a `role="status"`/`aria-live="polite"` region with loaded/total bytes and percent when known; use text nodes/React text interpolation for filenames. Add disabled state for duplicate transfer actions and styles for success/error/progress states without changing unrelated tree layout.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run: `bunx vitest run src/components/WorkspaceFileTree.test.tsx`. Expected: PASS.

- [ ] **Step 6: Run related frontend tests**

Run: `bunx vitest run src/components/WorkspaceFileTree.test.tsx src/pi-app/workspace`. Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add src/components/WorkspaceFileTree.tsx src/components/WorkspaceFileTree.test.tsx src/styles/parts/tree-preview-status.css src/App.astro
git commit -m "feat: show workspace file transfer progress" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 6: 对话附件限制提示与工作区引导

**Files:**
- Modify: `src/pi-app/input/attachment-methods.ts`
- Modify: `src/pi-app/input/input-methods.ts`
- Modify: `src/App.astro`
- Modify: `src/styles/parts/prompt-composer.css`
- Modify: `src/pi-app/input/input.test.ts`
- Modify: `src/pi-app/input/input-methods-coverage.test.ts`

**Interfaces:**
- Produces visible attachment guidance text containing the current inline/request limit and `请先上传到工作区`.
- `addFiles(files)` rejects image/combined attachments that cannot fit the known prompt body budget and records a user-visible warning; ordinary files over `256 KiB` show an explicit non-sendable state rather than silently inserting only a placeholder.

- [ ] **Step 1: Write failing attachment tests**

Add tests for a `256 KiB + 1` ordinary file and an oversized image: assert no sendable attachment is added, the prompt composer shows a message containing `上传到工作区`, and the send button remains disabled when no valid text/attachment exists. Keep the existing small image paste test green.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `bunx vitest run src/pi-app/input/input.test.ts src/pi-app/input/input-methods-coverage.test.ts`. Expected: FAIL because oversized files currently become placeholder/image data attachments without guidance.

- [ ] **Step 3: Implement explicit limits and warning state**

Keep `INLINE_LIMIT = 256 * 1024` for ordinary file content. Add a conservative prompt request attachment budget derived from the server JSON body limit, reject oversized image/aggregate attachments before Base64 conversion, and expose a `setAttachmentWarning(message)` helper that writes to a dedicated `aria-live` element. Do not use `innerHTML` for filenames or errors. Ensure warnings clear after removing the invalid file or adding a valid file.

- [ ] **Step 4: Add static guidance copy and styles**

Add a compact hint below the attachment controls: `对话附件有限制；较大文件请先上传到工作区。普通文件超过 256 KiB 不会内联。` Style it as secondary help text and keep it visible on mobile and desktop.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run: `bunx vitest run src/pi-app/input/input.test.ts src/pi-app/input/input-methods-coverage.test.ts`. Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src/pi-app/input/attachment-methods.ts src/pi-app/input/input-methods.ts src/App.astro src/styles/parts/prompt-composer.css src/pi-app/input/input.test.ts src/pi-app/input/input-methods-coverage.test.ts
git commit -m "feat: guide oversized prompt attachments to workspace" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 7: 集成验证与发布前检查

**Files:**
- Modify: only files needed to fix verified regressions; do not update generated `static/` assets until source checks pass.

- [ ] **Step 1: Run targeted backend tests**

Run: `go test ./internal/piweb -run 'Test(AppConfig|UploadWorkspaceFile|ServerWorkspace|WorkspaceFile)'`. Record any pre-existing environment-dependent failures separately.

- [ ] **Step 2: Run targeted frontend tests**

Run: `bunx vitest run src/lib/file-transfer.test.ts src/lib/api.test.ts src/components/WorkspaceFileTree.test.tsx src/pi-app/input/input.test.ts src/pi-app/input/input-methods-coverage.test.ts src/pi-app/workspace/app-config-methods.test.ts`.

- [ ] **Step 3: Run static checks**

Run: `bun run typecheck` and `git diff --check`. Expected: both exit `0`.

- [ ] **Step 4: Run production build**

Run: `bun run build`. Expected: exit `0`; do not commit generated `static/` changes unless the project’s release workflow explicitly requires them.

- [ ] **Step 5: Review the final diff**

Run: `git status --short`, `git diff --stat HEAD~6..HEAD`, and inspect all changed source/test files. Verify no secrets, API keys, generated unrelated assets, or temporary files are included.

- [ ] **Step 6: Commit any verification-only fix**

If a regression is found, add its failing test first, fix it, rerun the affected command, then commit with the required footer. If no regression is found, leave the tree clean.
