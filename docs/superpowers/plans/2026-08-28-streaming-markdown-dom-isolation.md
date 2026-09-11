# Streaming Markdown DOM Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将流式 assistant/thinking/loading 节点移出虚拟 transcript 列表，降低 Markdown 重排与虚拟滚动布局竞争。

**Architecture:** `.term-inner` 保留历史 transcript 和 `virtual-scroller`；新增同级 `.streaming-tail` 承载所有临时流式节点。流式 Markdown 继续使用现有 `markdown-it` 和 rAF 批处理，最终消息提交时才进入历史 transcript。

**Tech Stack:** Astro、原生 Custom Element、TypeScript、`markdown-it`、Vitest、jsdom、`virtual-scroller`。

**Spec:** `docs/superpowers/specs/2026-08-28-streaming-markdown-dom-design.md`

## Global Constraints

- 保留现有 `renderPiBody()`、`renderUserBody()`、AG-UI 协议和 fallback choice 行为。
- 不引入新的 Markdown 引擎、React 渲染链或完整 transcript 分页重构。
- 流式节点不得加入 `transcriptItems`，不得调用 `notifyTranscriptNodeHeightDidChange()`；assistant、thinking 和 loading 的 tail 更新都不触发虚拟列表高度通知。
- 所有 streaming render frame、scroll frame 和 tail 节点必须在 reset、session render、销毁时清理。
- 每个流式类型最多保留一个 DOM 节点；所有输入必须继续经过现有转义路径。

---

### Task 1: 为 streaming tail 生命周期建立失败回归测试

**Files:**
- Modify: `src/pi-app/messages/messages.test.ts`
- Modify: `src/pi-app/transcript/transcript-window.test.ts`
- Test fixture: `src/App.astro` 已提供 session DOM，测试通过 `connectPiApp()` 使用真实模板

**Interfaces:**
- Consumes: 当前 `appendDelta()`, `appendLoadingMessage()`, `finalizePiStream()`, `renderMessages()` 行为。
- Produces: 对 `.streaming-tail` 的稳定断言，供后续实现任务使用。

- [ ] **Step 1: 写失败测试**

在 `messages.test.ts` 增加以下行为断言：

```ts
it("keeps streaming markdown outside the virtual transcript items", async () => {
  const app = await connectPiApp();
  app.renderMessages([{ kind: "pi", text: "history" }]);

  app.appendDelta({ kind: "pi", delta: "**live**" });
  app.flushStreamingRender();

  expect(app.transcriptItems).toHaveLength(1);
  expect(app.querySelectorAll(".term-inner .msg.streaming")).toHaveLength(0);
  expect(app.querySelectorAll(".streaming-tail .msg.streaming")).toHaveLength(1);
  expect(app.querySelector(".streaming-tail .body").innerHTML).toContain("<strong>live</strong>");
});

it("moves the final assistant message from streaming tail into history once", async () => {
  const app = await connectPiApp();
  app.renderMessages([]);
  app.appendDelta({ kind: "pi", delta: "partial" });

  app.finalizePiStream("complete");

  expect(app.querySelectorAll(".streaming-tail .msg.streaming")).toHaveLength(0);
  expect(app.querySelectorAll(".term-inner .msg[data-kind='pi']")).toHaveLength(1);
  expect(app.transcriptItems).toHaveLength(1);
  expect(app.querySelector(".term-inner .msg[data-kind='pi']").textContent).toContain("complete");
});
```

在 `transcript-window.test.ts` 增加 reset 清理断言：

```ts
it("clears streaming tail and pending render on transcript reset", async () => {
  const frames = [];
  vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
    frames.push(callback);
    return frames.length;
  });
  const app = await connectPiApp();
  app.appendDelta({ kind: "pi", delta: "pending" });

  app.resetTranscriptWindow();

  expect(app.querySelector(".streaming-tail").childElementCount).toBe(0);
  expect(app.pendingStreamingRow).toBeUndefined();
  expect(app.streamingRenderFrame).toBeUndefined();
  expect(frames.length).toBeGreaterThan(0);
});
```

并将旧测试中直接查找 `.term-inner .msg.streaming` 的断言更新为 `.streaming-tail .msg.streaming`，但保留历史消息仍位于 `.term-inner` 的断言。

- [ ] **Step 2: 运行测试确认失败**

Run: `bun test src/pi-app/messages/messages.test.ts src/pi-app/transcript/transcript-window.test.ts`

Expected: FAIL，失败原因应为 `.streaming-tail` 尚未存在或流式节点仍被加入 `transcriptItems`，不是测试语法错误。

- [ ] **Step 3: 提交测试**

```bash
git add src/pi-app/messages/messages.test.ts src/pi-app/transcript/transcript-window.test.ts
git commit -m "test: define isolated streaming markdown lifecycle" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 2: 创建 streaming tail 并接入 transcript 生命周期

**Files:**
- Modify: `src/App.astro:252-270`
- Modify: `src/pi-app/index.ts:45-75, 80-100`
- Modify: `src/pi-app/transcript/transcript-window-methods.ts:15-155`
- Test: `src/pi-app/messages/messages.test.ts`, `src/pi-app/transcript/transcript-window.test.ts`

**Interfaces:**
- Consumes: Task 1 的 `.streaming-tail` 测试契约。
- Produces: `this.streamingTail` DOM 引用、`clearStreamingTail()` 方法，以及 reset/render/destroy 时可重复调用的清理行为。

- [ ] **Step 1: 在模板中增加固定 tail 容器**

在 `.term-inner` 闭合标签后、`.term` 闭合标签前增加：

```astro
<div class="streaming-tail" data-streaming-tail></div>
```

它必须与 `.term-inner` 同为 `.term` 的直接子元素，不能放入 `.term-inner`。

- [ ] **Step 2: 增加初始化和清理接口**

在 `connectedCallback()` 保存：

```ts
this.streamingTail = this.querySelector("[data-streaming-tail]");
```

在 `transcript-window-methods.ts` 增加：

```ts
clearStreamingTail() {
  this.streamingTail?.replaceChildren();
  this.streamingRows = {};
  this.pendingStreamingRow = undefined;
  if (this.streamingRenderFrame) {
    window.cancelAnimationFrame?.(this.streamingRenderFrame);
    this.streamingRenderFrame = undefined;
  }
},
```

`resetTranscriptWindow()` 和 `renderMessages()` 必须调用 `clearStreamingTail()`；若 tail 缺失，方法安全返回。

- [ ] **Step 3: 让组件销毁调用统一清理接口**

在 `disconnectedCallback()` 中用 `this.clearStreamingTail?.()` 替代仅取消 `streamingRenderFrame` 的局部清理，同时保留现有 `scrollFrame` 清理。

- [ ] **Step 4: 运行生命周期测试**

Run: `bun test src/pi-app/messages/messages.test.ts src/pi-app/transcript/transcript-window.test.ts`

Expected: Task 1 的 reset/tail 容器测试通过；流式节点位置测试仍失败，因为 `appendDelta()` 尚未迁移。

- [ ] **Step 5: 提交容器生命周期改动**

```bash
git add src/App.astro src/pi-app/index.ts src/pi-app/transcript/transcript-window-methods.ts
git commit -m "refactor: add isolated streaming transcript tail" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 3: 将 streaming assistant/thinking/loading 从 virtual-scroller 迁移到 tail

**Files:**
- Modify: `src/pi-app/messages/message-methods.ts:1-240`
- Modify: `src/pi-app/transcript/transcript-window-methods.ts:150-230`
- Test: `src/pi-app/messages/messages.test.ts`

**Interfaces:**
- Consumes: `streamingTail`, `clearStreamingTail()`, `appendTranscriptNode()` 和现有 Markdown renderer。
- Produces: `appendStreamingNode(node)`, `removeStreamingNode(node)`, `streamingRowsForKind(kind)` 只覆盖 tail 节点；最终消息仍通过 `appendTranscriptNode()` 进入历史列表。

- [ ] **Step 1: 为 tail 增加节点操作方法**

在 `transcript-window-methods.ts` 增加：

```ts
appendStreamingNode(node) {
  const nodes = this.transcriptElementNodes(node);
  if (!nodes.length || !this.streamingTail) return;
  this.streamingTail.append(...nodes);
},

removeStreamingNode(node) {
  if (!node) return;
  node.remove?.();
  if (this.streamingRows) {
    for (const [kind, row] of Object.entries(this.streamingRows)) {
      if (row === node) this.streamingRows = { ...this.streamingRows, [kind]: undefined };
    }
  }
},
```

- [ ] **Step 2: 修改 `appendDelta()`**

保留 choice 过滤、文本累积和 Markdown rAF 调度；将新 row 的：

```ts
this.appendTranscriptNode(messageRow, { stickToBottom: true });
```

替换为：

```ts
this.appendStreamingNode(messageRow);
```

删除“virtual scroller unmounted streaming row”的重建分支，因为 tail 不再由虚拟列表卸载。delta 更新后只触发已有 `scrollTerm()` 跟随，不调用高度 notification。

- [ ] **Step 3: 修改 loading/thinking 生命周期**

`appendLoadingMessage()` 使用 `appendStreamingNode(row)`；`removeLoadingMessage()` 直接删除 tail 中的 loading 节点，不过滤 `transcriptItems`，并清除 loading 引用。thinking 和 assistant 的 tail 更新及 flush 也不调用 `notifyTranscriptNodeHeightDidChange()`。

`hasActiveTranscriptItem()` 改为同时检查 `.streaming-tail` 中的 streaming assistant 和 fallback choice 状态，避免 streaming 存在时重复显示 loading。

- [ ] **Step 4: 修改 finalize 和 clear 逻辑**

`finalizePiStream(text)` 执行顺序固定为：

```text
flushStreamingRender()
检查最终消息是否重复
删除 tail 中 pi streaming row
appendTranscriptNode(messageNode(message), { stickToBottom: true })
清理 pi streaming state
scrollTerm()
```

不再调用 `finalizeStreamingRow()`，不再把 tail row 替换成历史 item。`clearStreamingState(kind)` 只 flush 并清除对应 tail row 引用；清理节点由 finalize/reset 明确完成。

`finalizeStreamingMessages()` 必须清除 tail 中的 streaming rows，但不能修改历史消息节点的 `streaming` 状态以外的内容。

- [ ] **Step 5: 运行消息回归测试确认通过**

Run: `bun test src/pi-app/messages/messages.test.ts src/pi-app/transcript/transcript-window.test.ts`

Expected: 新增 tail/finalize/reset 测试以及现有 Markdown、loading、thinking、choice、滚动测试全部通过；旧的“重建被卸载 row”测试应被删除或改为断言 tail 永不卸载。

- [ ] **Step 6: 提交流式生命周期改动**

```bash
git add src/pi-app/messages/message-methods.ts src/pi-app/transcript/transcript-window-methods.ts src/pi-app/messages/messages.test.ts
git commit -m "fix: isolate streaming markdown from virtual transcript" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

### Task 4: 补齐 tail 样式并进行完整验证

**Files:**
- Modify: `src/styles/parts/terminal-messages.css:101-132`
- Test: `src/pi-app/transcript/transcript-style.test.ts`
- Verify: all project test/build commands

**Interfaces:**
- Consumes: Task 3 的 `.streaming-tail` DOM 结构。
- Produces: 与 `.term-inner` 相同内容宽度和 stacking context 的 tail，不新增滚动容器。

- [ ] **Step 1: 写样式失败测试**

在 `transcript-style.test.ts` 增加源码断言：

```ts
it("keeps streaming tail in the same non-anchored content layer", () => {
  expect(terminalStyles).toContain(".streaming-tail");
  expect(terminalStyles).toMatch(/\.streaming-tail[^}]*max-width:\s*960px/);
  expect(terminalStyles).toMatch(/\.streaming-tail[^}]*overflow-anchor:\s*none/);
  expect(terminalStyles).not.toMatch(/\.streaming-tail[^}]*overflow-y:\s*auto/);
});
```

- [ ] **Step 2: 运行样式测试确认失败**

Run: `bun test src/pi-app/transcript/transcript-style.test.ts`

Expected: FAIL，原因是 `.streaming-tail` 尚未有专门规则。

- [ ] **Step 3: 增加最小 CSS**

在 `.term-inner` 规则后增加：

```css
.streaming-tail {
  position: relative;
  z-index: 2;
  max-width: 960px;
  min-height: 0;
  overflow-anchor: none;
}
```

不设置 `overflow`，确保滚动容器仍只有 `.term`。

- [ ] **Step 4: 运行样式与消息测试**

Run: `bun test src/pi-app/transcript/transcript-style.test.ts src/pi-app/messages/messages.test.ts src/pi-app/transcript/transcript-window.test.ts`

Expected: PASS。

- [ ] **Step 5: 运行完整检查**

Run: `bun run check`

Expected: typecheck、前端测试、Go 测试和 Astro build 全部退出码为 0。

- [ ] **Step 6: 检查生成物和工作区**

Run: `git status --short --branch && git diff --check`

Expected: 只有本次源码、测试和计划/设计文档的已提交改动；无 whitespace error。若 build 生成 `static/assets` hash 变化，只恢复本次构建生成物，不修改源码文件。

- [ ] **Step 7: 提交样式与最终验证**

```bash
git add src/styles/parts/terminal-messages.css src/pi-app/transcript/transcript-style.test.ts
git commit -m "fix: style streaming transcript tail" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```
