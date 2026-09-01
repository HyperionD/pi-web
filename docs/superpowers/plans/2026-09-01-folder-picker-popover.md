# Folder Picker Popover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将工作区打开流程收敛为锚定目录选择 popover，移除手动路径和 Git clone 前端入口，同时保留后端路径安全边界。

**Architecture:** 复用现有 `listFolders(path, purpose)` 与 `openWorkspace(path)`，把目录浏览器移动到 picker 卡片内的锚定 popover。普通 workspace picker 只在用户点击按钮后加载；设置中的 allowed-root picker 继续复用同一目录浏览 UI，但使用 `purpose=allowed-root` 和现有返回设置流程。clone 后端 API 保留，前端 import、表单和提交方法删除。

**Tech Stack:** Astro template, TypeScript custom element methods, CSS, Vitest, existing Go folder/workspace APIs.

**Spec:** `docs/superpowers/specs/2026-09-01-folder-picker-popover-design.md`

## Global Constraints

- 后端继续作为最终安全边界，普通 picker 使用 `purpose=workspace`。
- 不改变 `workspace.allowedRoots` 语义、后端授权逻辑或目录 API。
- 移除手动路径输入和 Git clone 的前端入口；保留后端 Git clone API 兼容已有调用。
- 路径、目录名、错误和状态只使用安全文本节点渲染。
- 支持 Escape、点击外部关闭和键盘聚焦，不引入浏览器存储或新依赖。
- 不修改或提交既有 `static/` 构建产物。

---

## 文件映射

- Modify: `src/App.astro` — 删除路径/clone 表单，增加 popover 触发按钮和共享目录选择结构。
- Modify: `src/styles/parts/workspace-picker.css` — 删除旧 input/clone/browser 布局，增加 anchored popover、loading、focus 和窄屏样式。
- Modify: `src/pi-app/workspace/workspace-folder-methods.ts` — 增加 popover 打开/关闭/聚焦方法，收敛 folder browsing/open 流程，删除两个 submit 方法和 clone import。
- Modify: `src/pi-app/index.ts` — 删除 path/clone submit listener，注册 popover outside click，并让 click handler 处理新的 open/close actions。
- Modify: `src/pi-app/status/layout-methods.ts` — picker route 只聚焦触发按钮，不再自动展开目录浏览器；Escape 关闭 popover 后再处理既有 modal。
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts` — picker bootstrap 不再自动请求目录列表。
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.test.ts` — 删除 clone submit 覆盖，更新 picker bootstrap 断言。
- Modify: `src/pi-app/workspace/workspace-methods-coverage.test.ts` — 删除 clone/path submit 覆盖，增加 popover open/close/navigation/selection 分支。
- Modify: `src/pi-app/input/input-actions-coverage.test.ts` — 更新 folder action fixture 和新的 popover action 分支。
- Modify: `src/pi-app/core-events.test.ts` — 更新 DOM binding 断言，移除 path/clone form 事件，覆盖 popover keyboard/outside close。
- Modify: `src/pi-app/workspace/workspace-navigation.test.ts` — 增加 picker button 与最近工作区共存的回归断言。

---

### Task 1: 先建立 popover 交互的失败测试

**Files:**
- Modify: `src/pi-app/workspace/workspace-methods-coverage.test.ts`
- Modify: `src/pi-app/input/input-actions-coverage.test.ts`
- Modify: `src/pi-app/core-events.test.ts`

**Interfaces:**
- Consumes: 现有 `connectPiApp()`, `installPiAppFixture()`, `listFolders()` fetch mock 和 `handleAppClick()`。
- Produces: 明确约束 `openFolderPopover()`, `closeFolderPopover()`, `folder-open`, `folder-close` 与 `folder-open-current` 的测试契约。

- [ ] **Step 1: 为 fixture 增加最小 popover DOM，并写行为测试**

在 `workspace-methods-coverage.test.ts` 增加一个测试，使用以下结构并断言初始打开行为：

```ts
const trigger = document.createElement("button");
trigger.dataset.action = "folder-open";
const popover = document.createElement("div");
popover.dataset.folderPopover = "";
popover.hidden = true;
popover.innerHTML = '<button data-action="folder-close"></button><span data-folder-path></span><div data-folder-list></div><button data-action="folder-open-current"></button>';
app.append(trigger, popover);
app.apiConnected = true;
globalThis.fetch = vi.fn(async () => okJson({ path: "/allowed", parent: "/", folders: [{ name: "child", path: "/allowed/child" }] }));

await app.openFolderPopover();
expect(popover.hidden).toBe(false);
expect(app.folderBrowsePurpose).toBe("workspace");
expect(globalThis.fetch).toHaveBeenCalledWith(expect.stringContaining("path=~&purpose=workspace"), expect.any(Object));
expect(popover.querySelector("[data-folder-path]").textContent).toBe("/allowed");
```

增加导航和选择断言：目录 button 点击后请求其 path；确认按钮调用 `openWorkspacePath(currentFolder)`。

- [ ] **Step 2: 写关闭与删除入口测试**

在 `core-events.test.ts` 中移除对 `[data-path-form]` 和 `[data-clone-form]` submit 的断言，增加 `folder-open`/`folder-close` action 点击覆盖，并断言 `bindDomEvents()` 不注册旧 submit handler。覆盖 `window` Escape 和外部 click 后 popover `hidden === true`，以及触发按钮恢复 focus。

- [ ] **Step 3: 运行定向测试确认失败**

Run:

```bash
bunx vitest run src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/input/input-actions-coverage.test.ts src/pi-app/core-events.test.ts
```

Expected: FAIL，因为 `openFolderPopover`、`closeFolderPopover` 和新的 DOM 结构尚未实现；失败必须来自这些缺失行为，而不是 fixture 语法错误。

- [ ] **Step 4: Commit tests**

```bash
git add src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/input/input-actions-coverage.test.ts src/pi-app/core-events.test.ts
git commit -m "test: define folder picker popover behavior" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 2: 实现 popover DOM、folder methods 和事件生命周期

**Files:**
- Modify: `src/App.astro`
- Modify: `src/pi-app/workspace/workspace-folder-methods.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `src/pi-app/status/layout-methods.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts`

**Interfaces:**
- Consumes: Task 1 的测试契约；现有 `listFolders(path, purpose)`, `openWorkspacePath(path)`, `browseFolder(purpose)` 和 `recoverForbiddenWorkspace(error)`。
- Produces: `openFolderPopover(purpose = "workspace")`, `closeFolderPopover()`, `focusFolderPopover()`，以及 `data-folder-open`, `data-folder-close`, `data-folder-open-current` action。

- [ ] **Step 1: 替换 Astro picker markup**

在 `src/App.astro` 的“打开文件夹”卡片中删除 `data-path-form`、路径 input、`data-clone-form`、clone inputs/buttons 和旧说明文字。新增：

```astro
<div class="folder-picker-anchor">
  <button class="open-folder-btn" type="button" data-action="folder-open" aria-haspopup="dialog" aria-expanded="false">
    <Folder size={14} strokeWidth={1.75} aria-hidden="true" /> 选择文件夹
  </button>
  <div class="folder-popover" data-folder-popover role="dialog" aria-label="选择工作区" hidden>
    <div class="folder-popover-head">
      <strong>选择工作区</strong>
      <button type="button" data-action="folder-close" aria-label="关闭目录选择器">×</button>
    </div>
    <div class="folder-browser" data-folder-browser>
      <div class="folder-browser-head">
        <button type="button" data-action="folder-up"><ArrowUp size={12} strokeWidth={1.75} aria-hidden="true" /> 上一级</button>
        <span data-folder-path>~</span>
      </div>
      <div class="folder-list" data-folder-list></div>
      <button type="button" class="folder-confirm" data-action="folder-open-current">选择此文件夹</button>
    </div>
  </div>
</div>
```

保持 `data-folder-browser`、`data-folder-list`、`data-folder-path` 这些共享选择器，确保设置的 allowed-root picker 仍可复用。普通 popover 初始为 hidden；不要在 SSR 中渲染用户路径到 HTML。

- [ ] **Step 2: 实现打开/关闭与 focus 管理**

在 `workspace-folder-methods.ts` 增加以下逻辑：

```ts
async openFolderPopover(purpose = "workspace") {
  this.folderBrowsePurpose = purpose;
  const popover = this.querySelector("[data-folder-popover]");
  const trigger = this.querySelector("[data-action='folder-open']");
  popover?.removeAttribute("hidden");
  trigger?.setAttribute("aria-expanded", "true");
  await this.loadFolder("~", purpose);
  this.focusFolderPopover();
},

closeFolderPopover() {
  const popover = this.querySelector("[data-folder-popover]");
  const trigger = this.querySelector("[data-action='folder-open']");
  popover?.setAttribute("hidden", "");
  trigger?.setAttribute("aria-expanded", "false");
  trigger?.focus();
},

focusFolderPopover() {
  this.querySelector("[data-folder-popover] [data-folder-list] button, [data-folder-popover] [data-action='folder-open-current']")?.focus();
},
```

调整 `browseFolder(purpose)`：`allowed-root` 时保留现有 settings route/return 状态；workspace purpose 改为调用 `openFolderPopover("workspace")` 的等价逻辑。为避免重复请求，增加简单的 `folderLoadPending` guard，`finally` 恢复状态。

调整 `renderFolderListing()`：更新路径文本、上一级按钮 disabled 状态、目录 button；列表为空时保留文本空状态。`folderRow()` 不使用 `innerHTML`，逐个创建 span 并通过 `textContent` 写入名称和路径。

调整 `folder-open-current` 分支：allowed-root 继续调用 `addAllowedRootFromFolder(currentFolder)`；workspace 成功调用 `openWorkspacePath(currentFolder)` 并关闭 popover。

删除 `cloneWorkspaceRequest` import、`submitCloneWorkspace()` 和 `submitWorkspacePath()`；保留 `openWorkspacePath()`。

- [ ] **Step 3: 更新全局事件绑定和 route/bootstrap 行为**

在 `index.ts` 删除两个旧 form submit listener。`handleAppClick()` 增加：

```ts
if (action === "folder-open") void this.openFolderPopover("workspace");
if (action === "folder-close") this.closeFolderPopover();
```

保留 `folder-enter`、`folder-up` 和 `folder-open-current`，确保它们只操作共享 picker。window click handler 在点击不属于 `.folder-popover, [data-action='folder-open']` 时调用 `closeFolderPopover()`；不要影响 session menu 关闭逻辑。

在 `layout-methods.ts` 的 `route("picker")` 删除路径 input focus 和自动 `browseFolder()`，改为聚焦 `[data-action='folder-open']`。在 `workspace-bootstrap-methods.ts` 删除 picker bootstrap 中的 `await this.browseFolder()`；用户点击按钮或设置“添加目录”时才加载目录。

Escape 处理应先关闭可见 folder popover并阻止继续关闭无关 modal；若 popover 已关闭，再走既有 `closeModals()`。

- [ ] **Step 4: 运行定向测试确认通过**

Run:

```bash
bunx vitest run src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/input/input-actions-coverage.test.ts src/pi-app/core-events.test.ts
```

Expected: PASS；如果失败，修正实现或测试契约，不绕过断言。

- [ ] **Step 5: Commit implementation**

```bash
git add src/App.astro src/pi-app/workspace/workspace-folder-methods.ts src/pi-app/index.ts src/pi-app/status/layout-methods.ts src/pi-app/workspace/workspace-bootstrap-methods.ts
git commit -m "feat: add folder picker popover" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 3: 更新 popover 视觉样式和可访问性回归

**Files:**
- Modify: `src/styles/parts/workspace-picker.css`
- Modify: `src/pi-app/workspace/workspace-methods-coverage.test.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.test.ts`
- Modify: `src/pi-app/workspace/workspace-navigation.test.ts`

**Interfaces:**
- Consumes: Task 2 的 `.folder-picker-anchor`, `.folder-popover`, `.folder-confirm` DOM 和 folder methods。
- Produces: 响应式 anchored popover，保留设置 allowed-root picker 的共享 browser 样式。

- [ ] **Step 1: 删除旧表单样式并添加 popover 样式**

删除 `.input-row`、`.clone-row` 及其 input/button 专属样式。保留 shell row 样式。新增以下规则并按现有 token 调整：

```css
.folder-picker-anchor { position: relative; display: inline-block; }
.open-folder-btn,
.folder-confirm {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 1px solid var(--border-hi);
  border-radius: var(--radius-1);
  background: var(--accent);
  color: #002211;
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  font-weight: 600;
  padding: 8px 12px;
}
.open-folder-btn:hover,
.folder-confirm:hover { filter: brightness(1.1); }
.open-folder-btn:disabled,
.folder-confirm:disabled { cursor: not-allowed; opacity: .55; }
.folder-popover {
  position: absolute;
  z-index: 20;
  top: calc(100% + 8px);
  left: 0;
  width: min(560px, calc(100vw - 32px));
  border: 1px solid var(--border-hi);
  background: var(--bg-2);
  box-shadow: 0 12px 30px rgba(0, 0, 0, .28);
}
.folder-popover-head { display: flex; align-items: center; justify-content: space-between; }
.folder-popover [data-folder-browser] { margin: 0; border: 0; }
.folder-list button:focus-visible,
.folder-popover button:focus-visible { outline: 1px solid var(--accent); outline-offset: 1px; }
@media (max-width: 640px) {
  .folder-popover { position: fixed; left: 16px; right: 16px; width: auto; }
}
```

不要改变设置 modal 的 layout；allowed-root 复用 browser 时仍应可见并能滚动。

- [ ] **Step 2: 增加 loading/error/disabled 状态测试**

在 `workspace-methods-coverage.test.ts` 覆盖：API 未连接时打开不会抛错且设置 error；请求失败时 `.folder-error` 通过 `textContent` 显示；空目录显示“暂无文件夹”；请求期间触发按钮或确认按钮 disabled，完成后恢复。

在 `workspace-bootstrap-methods.test.ts` 更新 bootstrap 断言：picker route 不应自动调用 `browseFolder`，而普通 picker 初始页面仍可显示打开按钮。

- [ ] **Step 3: 增加 picker 与最近工作区共存回归**

在 `workspace-navigation.test.ts` 的 picker 相关测试中断言 picker 同时保留 `[data-folder-popover]` 触发按钮和 `[data-recent-workspaces]`，最近工作区 button 仍调用 `openWorkspace`。

- [ ] **Step 4: 运行前端全量测试**

Run:

```bash
bun run test
```

Expected: 所有测试通过。旧 coverage 测试中构造 `data-path-form` 或 `data-clone-form` 的分支应被删除，并由 popover 打开、导航、确认、错误和关闭分支覆盖同等用户行为。

- [ ] **Step 5: Commit styles and regression tests**

```bash
git add src/styles/parts/workspace-picker.css src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-bootstrap-methods.test.ts src/pi-app/workspace/workspace-navigation.test.ts
git commit -m "style: refine folder picker popover" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 4: 全量验证、静态产物隔离与收尾

**Files:**
- Read: `docs/superpowers/specs/2026-09-01-folder-picker-popover-design.md` to verify implementation coverage; no source change is expected.

**Interfaces:**
- Consumes: Tasks 1–3 的已提交实现。
- Produces: 可审计的验证结果和干净的源代码 diff；`static/` 不进入提交。

- [ ] **Step 1: 检查未跟踪和既有 static 变更**

Run:

```bash
git status --short
git diff --check -- . ':(exclude)static'
```

确认 `static/` 的删除、修改和新增文件不被 staging；只允许源代码、测试、样式和计划/spec 变更进入后续提交。

- [ ] **Step 2: 运行完整验证命令**

Run:

```bash
PI_CODING_AGENT_SESSION_DIR="$(mktemp -d)/sessions" go test ./...
bun run backend:test
bun run typecheck
bun run test
bun run build
```

Expected: Go tests通过，前端测试全部通过，typecheck 无输出错误，build 成功。build 产生的 `static/` 变化保持未提交；记录已有 chunk size warning（如仍存在）但不扩大本任务范围。

- [ ] **Step 3: 验证关键行为和 diff**

Run:

```bash
rg -n 'data-path-form|data-clone-form|submitWorkspacePath|submitCloneWorkspace|cloneWorkspaceRequest' src || true
git diff --check -- . ':(exclude)static'
git status --short --branch
```

Expected：普通 picker source 中不再存在 path/clone UI 和前端提交方法；后端 clone API source 仍存在；只有预期文件变更。

- [ ] **Step 4: 若验证发现实现偏差，修正后重新验证并提交**

只在验证发现真实实现偏差时修改对应源文件；随后重复受影响的测试命令，并使用实际文件列表提交：

```bash
git add src/App.astro src/styles/parts/workspace-picker.css src/pi-app/index.ts src/pi-app/status/layout-methods.ts src/pi-app/workspace/workspace-folder-methods.ts src/pi-app/workspace/workspace-bootstrap-methods.ts src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-bootstrap-methods.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/input/input-actions-coverage.test.ts src/pi-app/core-events.test.ts
git commit -m "fix: close folder picker popover edge case" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

没有偏差时不要创建空提交。
