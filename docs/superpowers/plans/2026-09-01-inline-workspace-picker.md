# Inline Workspace Picker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除全屏 picker 页面，让侧栏 `+ 打开` 和其他入口在主窗口内按点击位置打开共享目录 popover。

**Architecture:** 将现有 `[data-folder-popover]` 提升到 `pi-app` 根层，所有普通 workspace 入口通过 `openFolderPopover(purpose, event)` 共享它。popover 使用 fixed 坐标并在视口内避让；`route("picker")` 不再作为普通选择流程，主 route 保持 workspace，无工作区时显示现有 empty main。

**Tech Stack:** Astro template, TypeScript custom element methods, CSS, Vitest, existing folder/workspace APIs.

**Spec:** `docs/superpowers/specs/2026-09-01-inline-workspace-picker-design.md`

## Global Constraints

- 后端仍是最终安全边界；普通 picker 使用 `purpose=workspace`。
- 不改变 `workspace.allowedRoots`、folder API、open API 或后端授权逻辑。
- 取消全屏 picker 页面；没有工作区时显示主窗口 empty state，不自动弹出 popover。
- popover 以点击坐标为锚点，必须限制在视口内并支持独立滚动。
- allowed-root 使用 `purpose=allowed-root`，完成/取消后恢复 settings modal 与焦点。
- 路径、目录名、错误和状态只使用安全文本节点渲染。
- 不修改或提交既有 `static/` 构建产物，不修改用户配置，不引入新依赖。

---

## 文件映射

- Modify: `src/App.astro` — 删除 picker section，提升共享 popover 到 `pi-app` 根层，保留 session/empty 主窗口。
- Modify: `src/App.stories.ts` — 移除 picker 页面 story，使用 workspace/empty 状态展示。
- Modify: `src/pi-app/input/input-methods.ts` — `route-picker` 直接打开 popover，并传递 click event。
- Modify: `src/pi-app/index.ts` — outside click 识别所有 popover trigger，保留 session menu 行为。
- Modify: `src/pi-app/workspace/workspace-folder-methods.ts` — 坐标定位/视口避让、共享 trigger、allowed-root flow。
- Modify: `src/pi-app/workspace/app-config-methods.ts` — allowed-root picker 不再依赖 picker section，传递添加按钮事件。
- Modify: `src/pi-app/status/layout-methods.ts` — 删除 picker view 切换，route 只维护 workspace 主视图。
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts` — 无 workspace 显示 empty main；403 回 workspace empty，不进入 picker。
- Modify: `src/styles/parts/workspace-picker.css` — 删除全屏 picker/recent card 样式，保留共享 browser 并改 fixed popover。
- Modify: `src/pi-app/test-helper.ts` — fixture 使用根层 popover、sidebar trigger 和 empty main。
- Modify: `src/pi-app/input/input-actions-coverage.test.ts` — 更新 route-picker action 为直接打开 popover。
- Modify: `src/pi-app/core-events.test.ts` — 更新 route、outside click、popover trigger 断言。
- Modify: `src/pi-app/input/controls.test.ts` — 删除全屏 picker route 可见性断言，覆盖 workspace route 与 popover。
- Modify: `src/pi-app/workspace/workspace-bootstrap-coverage.test.ts` — 更新 bootstrap/403 empty state 断言。
- Modify: `src/pi-app/workspace/workspace-methods-coverage.test.ts` — 增加坐标、边界、trigger 和 allowed-root 回归。
- Modify: `src/pi-app/workspace/workspace-navigation.test.ts` — 验证侧栏入口不改变 route。

---

### Task 1: 建立真实入口与坐标定位的失败测试

**Files:**
- Modify: `src/pi-app/input/input-actions-coverage.test.ts`
- Modify: `src/pi-app/core-events.test.ts`
- Modify: `src/pi-app/workspace/workspace-methods-coverage.test.ts`
- Modify: `src/pi-app/workspace/workspace-navigation.test.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-coverage.test.ts`
- Modify: `src/pi-app/input/controls.test.ts`

**Interfaces:**
- Consumes: 现有 `connectPiApp()`, `installPiAppFixture()`, `handleAppClick()`, folder API mock 和 `showEmptyMain()`。
- Produces: `openFolderPopover(purpose, event)`, `positionFolderPopover(event, trigger)`、route-picker direct-open 和 no-picker-route 测试契约。

- [ ] **Step 1: 更新 fixture 结构契约测试**

让测试明确要求：popover 不依赖 `[data-view="picker"]`，侧栏 `[data-action="route-picker"]` 点击后 `data-route` 保持 `workspace`、popover 可见，并请求 `purpose=workspace`。无 workspace bootstrap 后断言 `[data-main="empty"]` 可见。

- [ ] **Step 2: 添加坐标和视口边界的失败测试**

用固定 viewport mock 和 click event 断言 popover 写入可视位置：

```ts
Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
Object.defineProperty(window, "innerHeight", { configurable: true, value: 768 });
const trigger = app.querySelector("[data-action='route-picker']");
trigger.getBoundingClientRect = () => ({ left: 40, top: 80, right: 120, bottom: 112, width: 80, height: 32 });
app.openFolderPopover = vi.fn();
app.handleAppClick({
  target: trigger,
  clientX: 90,
  clientY: 112,
});
expect(app.openFolderPopover).toHaveBeenCalledWith("workspace", expect.objectContaining({ clientX: 90, clientY: 112 }));
```

增加 `positionFolderPopover()` 的边界契约：鼠标坐标靠近右/下边缘时，最终 `left/top` 不得超出 `innerWidth - popover.offsetWidth - 16` 与 `innerHeight - popover.offsetHeight - 16`；键盘事件坐标为 0 时使用 trigger rect。

- [ ] **Step 3: 更新 route/403/allowed-root 失败测试**

删除“picker section 可见”的断言，改为：`route("picker")` 不显示任何 picker section；403 recovery 调用 `route("workspace")`、显示 empty main、popover hidden、trigger `aria-expanded=false`。allowed-root 添加按钮打开根层 popover，选择/取消后 settings modal 和 add button focus 恢复。

- [ ] **Step 4: 运行定向测试确认 RED**

```bash
bunx vitest run src/pi-app/input/input-actions-coverage.test.ts src/pi-app/core-events.test.ts src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/input/controls.test.ts
```

Expected：由于 route-picker 仍 route 到 picker、popover 仍嵌在 picker、坐标方法和 empty recovery 尚未实现而失败；不得修改生产代码来绕过 RED。

- [ ] **Step 5: Commit tests**

```bash
git add src/pi-app/input/input-actions-coverage.test.ts src/pi-app/core-events.test.ts src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/input/controls.test.ts
git commit -m "test: define inline workspace picker behavior" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 2: 移除 picker 页面并接入共享 popover trigger

**Files:**
- Modify: `src/App.astro`
- Modify: `src/App.stories.ts`
- Modify: `src/pi-app/input/input-methods.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `src/pi-app/status/layout-methods.ts`
- Modify: `src/pi-app/workspace/app-config-methods.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts`
- Modify: `src/pi-app/workspace/workspace-folder-methods.ts`
- Modify: `src/pi-app/test-helper.ts`

**Interfaces:**
- Consumes: Task 1 的 trigger、route、empty、coordinates 测试契约；现有 `showEmptyMain()`、`recoverForbiddenWorkspace()`、`restoreAllowedRootPicker()`。
- Produces: `openFolderPopover(purpose = "workspace", event?)`, `positionFolderPopover(event, trigger)`, `closeFolderPopover()`；`route-picker` 不再改变 route。

- [ ] **Step 1: 移除 Astro picker section，提升 popover**

删除 `src/App.astro` 中整个 `<section data-view="picker">`（包括最近工作区 card）。将其中 folder popover 提升为 `pi-app` 根层、header 后的 sibling。popover 保留 `data-folder-popover`、`data-folder-browser`、`data-folder-list`、`data-folder-path` 和确认按钮，但不再位于隐藏视图内。

删除 `pickerVisible`、`pickerHidden` 和 `workspaceVisible = initialRoute !== "picker"` 的计算；workspace body 按主视图渲染。保留 `data-route` 作为兼容状态字段但默认/有效值为 `workspace`。更新 story 的 picker variant 为 workspace + empty session。

- [ ] **Step 2: 让 route-picker 直接打开 popover**

在 `input-methods.ts` 将：

```ts
if (action === "route-picker") this.route("picker");
```

改为：

```ts
if (action === "route-picker") void this.openFolderPopover("workspace", event);
```

folder-open 同样把 click event 传给 `openFolderPopover`。`route-picker`、`folder-open` 的按钮应加入 outside-click 豁免，避免同一次 click 被 window handler 立即关闭。

- [ ] **Step 3: 移除普通 route 的 picker 切换**

在 `layout-methods.ts`：

```ts
route(route) {
  this.dataset.route = route === "picker" ? "workspace" : route;
  this.querySelector('[data-view="workspace"]')?.removeAttribute("hidden");
  if (route === "workspace") this.scrollTerm();
}
```

删除对 `[data-view="picker"]`、picker trigger focus 的访问。Escape 继续优先关闭可见 popover，再关闭其他 modal。

在 `workspace-bootstrap-methods.ts` 的 forbidden recovery 使用 `resetFolderPopover()` 后调用 `route("workspace")` 和 `showEmptyMain()`。无 active workspace/session 的 bootstrap 也调用 `route("workspace")` 与 `showEmptyMain()`。

- [ ] **Step 4: 让 allowed-root 不依赖 picker section**

在 `app-config-methods.ts` 的 add button listener 把 click event 传给 `browseFolder("allowed-root", event)`。在 `workspace-folder-methods.ts` 中 allowed-root browse 只隐藏 settings modal、记住返回 settings，不切换到 picker route；完成/取消调用 `restoreAllowedRootPicker()`，重新显示 settings、聚焦 `[data-allowed-root-add]`。普通 workspace 关闭时恢复保存的触发按钮焦点。

- [ ] **Step 5: 更新测试 fixture 与定向测试**

将 `test-helper.ts` 的 popover 放在 workspace/empty 主视图共同父级，sidebar `+ open` 仍为 `data-action="route-picker"`。运行 Task 1 定向命令，Expected：所有行为测试通过。

- [ ] **Step 6: Commit implementation**

```bash
git add src/App.astro src/App.stories.ts src/pi-app/input/input-methods.ts src/pi-app/index.ts src/pi-app/status/layout-methods.ts src/pi-app/workspace/app-config-methods.ts src/pi-app/workspace/workspace-bootstrap-methods.ts src/pi-app/workspace/workspace-folder-methods.ts src/pi-app/test-helper.ts
git commit -m "feat: open workspace picker inline" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 3: 改造坐标定位、popover 样式与全量回归

**Files:**
- Modify: `src/pi-app/workspace/workspace-folder-methods.ts`
- Modify: `src/styles/parts/workspace-picker.css`
- Modify: `src/pi-app/workspace/workspace-methods-coverage.test.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-coverage.test.ts`
- Modify: `src/pi-app/workspace/app-config-methods.test.ts`
- Modify: `src/pi-app/workspace/workspace-navigation.test.ts`

**Interfaces:**
- Consumes: Task 2 的根层 popover、`openFolderPopover(purpose, event?)`、allowed-root return flow。
- Produces: 视口内 fixed popover、可滚动目录 browser、无 picker route 残留的测试结果。

- [ ] **Step 1: 实现 click 坐标和 trigger rect fallback**

在 `workspace-folder-methods.ts` 增加：

```ts
positionFolderPopover(event, trigger) {
  const popover = this.querySelector("[data-folder-popover]");
  if (!popover) return;
  const rect = trigger?.getBoundingClientRect?.();
  const x = Number(event?.clientX) > 0 ? event.clientX : rect?.left || 16;
  const y = Number(event?.clientY) > 0 ? event.clientY : rect?.bottom || 16;
  const width = popover.offsetWidth || 420;
  const height = popover.offsetHeight || 360;
  const left = Math.max(16, Math.min(x, window.innerWidth - width - 16));
  const top = Math.max(16, Math.min(y + 8, window.innerHeight - height - 16));
  popover.style.left = `${left}px`;
  popover.style.top = `${top}px`;
},
```

`openFolderPopover(purpose, event)` 保存 `this.folderPopoverTrigger = event?.target?.closest("button") || sidebar trigger`，显示 popover 后调用定位；`closeFolderPopover()` 从该属性恢复焦点并清除引用。定位使用 `Math.max/Math.min`，不把用户路径放入 style。

- [ ] **Step 2: 完成 CSS fixed anchored layout**

先运行 `rg -n '\.picker-shell|\.picker-inner|\.picker-tag|\.picker-card|\.recent-row' src`，确认这些选择器仅由已删除的 picker markup 使用；随后删除其全屏 picker 专属规则。保留仍被其他视图使用的通用 row 样式。popover 使用：

```css
.folder-popover {
  position: fixed;
  z-index: 60;
  width: min(420px, calc(100vw - 32px));
  max-height: min(480px, calc(100vh - 32px));
  overflow: hidden;
  border: 1px solid var(--border-hi);
  background: var(--bg-2);
  box-shadow: 0 12px 30px rgba(0, 0, 0, .28);
}
.folder-list { max-height: 300px; overflow: auto; }
```

小屏保持 `left/top` 由 JS 设置，CSS 只约束 `max-width/max-height`；不能用固定 `top: 16px` 覆盖鼠标坐标。Popover 层级必须高于主窗口内容但不遮挡 settings focus ring，focus-visible 样式保留。

- [ ] **Step 3: 更新 allowed-root 和 empty state 回归**

在 `app-config-methods.test.ts` 验证设置添加目录不会出现 picker section 或改变 route，完成/取消显示 settings 并恢复 add button focus。在 bootstrap coverage 验证无 workspace 自动 `showEmptyMain()`，403 后 route 为 workspace 且 empty visible。

- [ ] **Step 4: 运行前端全量验证**

```bash
bun run test
bun run typecheck
```

Expected：所有测试通过，且测试覆盖坐标边界、键盘 fallback、主窗口触发、allowed-root、empty state、403、Escape/outside、stale open 和安全文本。

- [ ] **Step 5: Commit styles and regression**

```bash
git add src/pi-app/workspace/workspace-folder-methods.ts src/styles/parts/workspace-picker.css src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/workspace/app-config-methods.test.ts src/pi-app/workspace/workspace-navigation.test.ts
git commit -m "style: anchor workspace picker to main window" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 4: 后端隔离验证、构建和提交边界检查

**Files:**
- Read: `docs/superpowers/specs/2026-09-01-inline-workspace-picker-design.md`
- No source changes expected.

**Interfaces:**
- Consumes: Tasks 1–3 的实现。
- Produces: 可复现验证结果；static 仍不进入 staging。

- [ ] **Step 1: 验证残留和工作树**

```bash
rg -n 'data-view="picker"|route\("picker"|route\x27picker|picker-shell|pickerVisible|pickerHidden' src || true
git diff --check -- . ':(exclude)static'
git status --short --branch
```

Expected：普通 source 不再依赖全屏 picker；只允许既有 `static/` dirty，非 static diff check 通过。

- [ ] **Step 2: 运行完整验证**

```bash
PI_CODING_AGENT_SESSION_DIR="$(mktemp -d)/sessions" bun run backend:test
bun run test
bun run typecheck
bun run build
```

Expected：隔离后端测试、前端测试、typecheck、build 均退出 0。coverage 全局阈值若仍因仓库既有缺口失败，记录实际数值，不修改无关模块；build 产生的 static 变化不得 staging。

- [ ] **Step 3: 检查最终提交边界**

```bash
git diff --check -- . ':(exclude)static'
git diff --cached --name-status
git status --short --branch
```

确认 `git diff --cached --name-status` 不包含 `static/`，分支保持未合并、未推送。
