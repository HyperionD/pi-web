# Office File Viewer Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** 按需接入 `@file-viewer/web` + `@file-viewer/preset-office`，为 DOCX/XLSX/PPTX 提供离线、Chrome 107 兼容的浏览器内预览，同时保留现有非 Office 预览。

**Architecture:** 新增独立 Office adapter，动态加载 File Viewer 并管理 controller 生命周期；现有 `file-preview-methods.ts` 只按文件类型委托。Office 通过 Go raw 文件 URL 或带凭据的 `fetchFile` 进入 viewer，静态 Worker/WASM/字体资源复制到 `static/file-viewer/` 并由单二进制提供。

**Tech Stack:** Astro/Vite, TypeScript, `@file-viewer/web`, `@file-viewer/preset-office`, Go `net/http`, Vitest, Playwright/Chromium.

**Spec:** `docs/superpowers/specs/2026-09-20-office-viewer-integration-design.md`

## Global Constraints

- 不使用 `@file-viewer/web-full`，不引入 CAD、3D、Archive、Adobe 等非 Office renderer。
- 首轮验收格式为 DOCX、XLSX、PPTX；PDF 继续使用现有实现。
- 兼容 Chrome 107，不使用 Chrome 107 之后的浏览器 API。
- Worker、WASM、字体和 vendor 资源必须离线、自托管并进入 Go embed 的 `static/`。
- 所有 workspace 文件 URL 必须来自现有 raw API，不能接受任意外部 URL。
- Office 文件加载失败必须保留下载回退；切换文件时必须销毁旧 controller。
- 每项行为遵循 TDD：先失败测试，再实现，再聚焦验证。

---

### Task 1: 固定 npm 依赖和 Office 资源交付边界

**Files:**
- Modify: `package.json`
- Modify: `bun.lock`
- Create: `scripts/prepare-file-viewer-assets.mjs`
- Test: `scripts/prepare-file-viewer-assets.test.mjs`
- Modify: `README.md` or `docs/` deployment notes if the existing docs have a static asset section

**Interfaces:**
- Produces a repeatable asset command that stages only the Office renderer asset set into `static/file-viewer/`.
- Pins `@file-viewer/web`, `@file-viewer/preset-office`, and the matching standard asset package to one exact version.

- [ ] **Step 1: Write the failing asset contract test**

Add a Node test that invokes the asset preparation script in a temporary target and asserts the manifest contains Office assets while excluding CAD/3D/Typst assets:

```js
assert.equal(await exists(join(target, 'vendor/docx/docx.worker.js')), true)
assert.equal(await exists(join(target, 'vendor/xlsx/sheet.worker.js')), true)
assert.equal(await exists(join(target, 'vendor/pptx/pptx.worker.js')), true)
assert.equal(await exists(join(target, 'vendor/pdf/pdf.worker.mjs')), true)
assert.equal(await exists(join(target, 'wasm/cad/libredwg-web.wasm')), false)
```

- [ ] **Step 2: Run the test and verify it fails**

Run:

```bash
bun test scripts/prepare-file-viewer-assets.test.mjs
```

Expected: FAIL because the dependency and staging script do not exist.

- [ ] **Step 3: Add exact dependencies and implement staging**

Add the published `@file-viewer/web`, `@file-viewer/preset-office`, and matching `@file-viewer/assets-standard` versions as exact dependencies. The staging script must:

1. invoke the matching assets CLI into a temporary directory;
2. select only Office groups (`pdf`, `docx`, `xlsx`, `pptx`, and their required shared fonts/assets);
3. reject path traversal and unexpected output paths;
4. atomically replace `static/file-viewer/` only after all required files exist;
5. write a small manifest containing package versions and SHA-256 values.

Do not copy `apps/viewer-demo/public` wholesale.

- [ ] **Step 4: Run the asset test and inspect size**

Run:

```bash
bun test scripts/prepare-file-viewer-assets.test.mjs
find static/file-viewer -type f -printf '%s %p\\n' | sort -nr | head
```

Expected: PASS; no CAD/3D/Typst paths; asset size is bounded and documented.

- [ ] **Step 5: Commit**

```bash
git add package.json bun.lock scripts/prepare-file-viewer-assets.mjs scripts/prepare-file-viewer-assets.test.mjs
 git commit -m "build(files): add scoped office viewer assets"
```

---

### Task 2: Add the Office viewer adapter

**Files:**
- Create: `src/lib/office-preview.ts`
- Create: `src/lib/office-preview.test.ts`

**Interfaces:**

```ts
export type OfficePreviewIdentity = { workspaceId: string; path: string; version?: string };
export type OfficePreviewHandle = { destroy(): void; ready: Promise<void> };
export function isOfficePreviewPath(path: string): boolean;
export function mountOfficePreview(
  container: HTMLElement,
  identity: OfficePreviewIdentity,
  options?: { onDownload?: () => void; signal?: AbortSignal },
): Promise<OfficePreviewHandle>;
```

- [ ] **Step 1: Write failing adapter tests**

Mock the dynamic File Viewer import and assert:

- `.docx`, `.xlsx`, `.pptx` are Office paths;
- `.md`, `.pdf`, `.png`, `.txt` are not;
- mount passes a raw workspace URL and `credentials: 'include'` through the fetch adapter;
- a stale abort signal prevents mounting;
- `destroy()` calls the viewer controller destroy method;
- loader errors reject with a user-actionable error.

- [ ] **Step 2: Run focused tests and verify failure**

```bash
bunx vitest run src/lib/office-preview.test.ts
```

Expected: FAIL because the adapter does not exist.

- [ ] **Step 3: Implement the minimal adapter**

Use a lazy import of `@file-viewer/web` and `@file-viewer/preset-office`. Mount with `rendererMode: 'replace'`, `preset: officePreset`, `builtinRenderers: 'none'`, and `autoRenderers: false` so this adapter does not silently load unrelated renderers.

The fetch adapter must call the existing raw URL with `credentials: 'include'`, preserve the response MIME and filename, and return a `File`/`Blob` accepted by File Viewer. Check `signal.aborted` before and after fetch. Do not expose arbitrary URL input.

- [ ] **Step 4: Run focused tests**

```bash
bunx vitest run src/lib/office-preview.test.ts
bun run typecheck
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/lib/office-preview.ts src/lib/office-preview.test.ts
 git commit -m "feat(files): add lazy office viewer adapter"
```

---

### Task 3: Integrate Office lifecycle into the file preview panel

**Files:**
- Modify: `src/pi-app/editor/file-preview-methods.ts`
- Modify: `src/pi-app/editor/file-preview.test.ts`
- Modify: `src/App.astro`
- Modify: `src/pi-app/test-helper.ts`
- Modify: `src/pi-app/input/input-methods.ts` only if an explicit retry action is required
- Modify: `src/styles/parts/prompt-composer.css`

**Interfaces:**
- `filePreview.officeHandle?: OfficePreviewHandle` is owned by the current preview state.
- `renderFilePreviewBody()` routes Office files to `mountOfficePreview()`.
- `destroyFilePreviewEditor()` also destroys the Office handle.

- [ ] **Step 1: Write failing DOM/lifecycle tests**

Add tests for:

1. DOCX renders an Office host instead of CodeMirror/fallback text.
2. XLSX and PPTX use the same route.
3. switching from DOCX to Markdown destroys the first controller.
4. closing the preview destroys the controller.
5. an Office load failure shows an error and keeps a download action.
6. stale Office completion cannot replace a newer selected file.

- [ ] **Step 2: Run focused tests and verify failure**

```bash
bunx vitest run src/pi-app/editor/file-preview.test.ts -t 'Office'
```

Expected: FAIL because the preview panel has no Office route.

- [ ] **Step 3: Implement lifecycle integration**

Add a `.fp-office-viewer` host and stable `data-file-preview-office` hook. Before rendering a new file, destroy any existing Office handle. For Office files, show a loading state, mount the adapter, and check `workspaceId`, path, and request ID before storing the handle. On error, render a localized failure message and expose the existing download action. Keep current media/PDF/HTML/text branches unchanged.

- [ ] **Step 4: Run focused and existing editor tests**

```bash
bunx vitest run src/pi-app/editor/file-preview.test.ts src/pi-app/editor/file-editor.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/pi-app/editor/file-preview-methods.ts src/pi-app/editor/file-preview.test.ts src/App.astro src/pi-app/test-helper.ts src/styles/parts/prompt-composer.css
 git commit -m "feat(files): route office files through viewer"
```

---

### Task 4: Add Office-specific backend and security regression coverage

**Files:**
- Modify: `internal/piweb/server_workspace_file_preview_test.go`
- Modify: `internal/piweb/files_test.go`
- Modify: `internal/piweb/server_workspace_file_handlers.go` only if the tests expose a missing contract
- Modify: `src/lib/api.test.ts` if URL behavior needs an explicit Office assertion

**Interfaces:**
- Existing raw API remains the sole source for Office bytes.
- No new arbitrary proxy endpoint is introduced.

- [ ] **Step 1: Write failing/strengthening tests**

Cover:

- Office raw responses use the expected MIME and `inline` disposition;
- `download=1` uses safe attachment disposition;
- Range requests return 206 and invalid ranges return 416;
- traversal paths are rejected;
- no Office URL can escape the workspace root;
- required `nosniff` and `Accept-Ranges` headers remain present.

- [ ] **Step 2: Run focused Go tests**

```bash
go test ./internal/piweb -run 'Test(WorkspaceFileAsset|WorkspaceFileList|ReadWorkspaceFile)' -count=1
```

Expected: PASS for the existing implementation or FAIL if an Office-specific MIME/security gap is found.

- [ ] **Step 3: Make only contract-preserving fixes**

Do not add a second file proxy. Any fix must preserve SafeJoin/access checks, Range semantics, download behavior, and current non-Office tests.

- [ ] **Step 4: Run the package tests and race check**

```bash
go test ./internal/piweb -race -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/piweb/server_workspace_file_preview_test.go internal/piweb/files_test.go internal/piweb/server_workspace_file_handlers.go src/lib/api.test.ts
 git commit -m "test(files): lock office raw asset contract"
```

---

### Task 5: Add Chrome 107-compatible browser smoke coverage

**Files:**
- Create: `e2e/file-preview-office.spec.ts`
- Modify: `playwright.config.ts` only if a Chromium 107 executable/project can be configured without changing the default project
- Modify: `package.json` with a non-destructive `test:e2e:office` script if needed
- Modify: `docs/superpowers/specs/2026-09-20-office-viewer-integration-design.md` with observed compatibility results

**Interfaces:**
- Browser smoke tests use local fixtures and the same-origin Go raw API; no external service or CDN.

- [ ] **Step 1: Write the failing browser tests**

Cover DOCX/XLSX/PPTX fixture load, Chinese text visibility, Worker/WASM request URLs, resource MIME, file switch cleanup, and download fallback. Add a separate run target for Chrome 107; do not replace the default modern Chromium project.

- [ ] **Step 2: Run the test and record the first failures**

```bash
bun run test:e2e:office
```

Expected: FAIL until fixtures, selectors, and the adapter are complete.

- [ ] **Step 3: Implement only stable hooks and compatibility fixes**

Use `data-file-preview-office`, `data-file-preview-error`, and existing download hooks. Do not add browser APIs newer than Chrome 107 or broaden the renderer set.

- [ ] **Step 4: Run Chrome 107 and modern Chromium tests**

```bash
bun run test:e2e:office
bun run test:e2e -- e2e/file-preview-office.spec.ts
```

Expected: both pass; all Worker/WASM/font requests are local and successful.

- [ ] **Step 5: Commit**

```bash
git add e2e/file-preview-office.spec.ts playwright.config.ts package.json docs/superpowers/specs/2026-09-20-office-viewer-integration-design.md
 git commit -m "test(files): verify office viewer on chrome 107"
```

---

### Task 6: Final validation and embedded asset synchronization

**Files:**
- Modify: `static/file-viewer/**` generated assets only
- Modify: `docs/` deployment notes if the final asset command differs from the spec

- [ ] **Step 1: Run the full validation**

```bash
bun run lint
bun run typecheck
bun run test
bun run backend:test
bun run build
bun run check:embedded-assets
git diff --check
```

- [ ] **Step 2: Synchronize embedded assets**

Run the scoped asset preparation command, rebuild, and rerun `check:embedded-assets`. Confirm no generated cache, full demo sample, CAD, 3D, or secret files entered `static/`.

- [ ] **Step 3: Inspect final diff and working tree**

```bash
git diff --stat HEAD~6..HEAD
git status --short --branch
```

- [ ] **Step 4: Commit generated assets and final documentation**

```bash
git add static/file-viewer docs
 git commit -m "chore(files): embed office viewer assets"
```
