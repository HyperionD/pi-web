# P0 TranscriptState 单一真源与移除 AG-UI 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 移除 AG-UI 传输层，并将所有会话渲染统一为 `TranscriptState → ViewModel → DOM`，彻底修复 P0 的虚拟滚动丢输出、工具失败状态错误和流恢复竞态。

**Architecture:** 命令只通过 HTTP POST 提交；所有运行时展示事件只通过 durable session event stream（生产路径）进入 `transcriptReducer`。`TranscriptState` 是唯一业务真源，`TranscriptViewModel` 是只读投影，DOM 节点是可随时销毁和重建的临时视图，不保存正文、工具输出、状态或去重信息。

**Tech Stack:** TypeScript、Astro、Vitest、Go、SQLite durable event store、SSE、`bun`、`go test`。

**Spec:** `docs/upstream-parity-audit.md` 中的 P0 审查结论，以及已批准的“删除 AG-UI + 统一 TranscriptState → ViewModel → DOM”方案。

## Global Constraints

- 外部会话行为保持兼容：`postPrompt`、附件、取消、steer、刷新恢复和历史分页继续可用。
- 删除 `/api/sessions/{sessionID}/ag-ui`，不保留兼容分支或 AG-UI custom cursor。
- 所有实时会话展示必须先经过 reducer；不得在事件回调中直接调用 DOM 流式写入方法。
- `dataset.streamText`、`__toolOutputText`、`latestToolCards` 和永久 `item.nodes` 不得保存业务状态。
- 每个行为修复先写失败测试，再写最小实现；每个任务完成后单独提交。
- 不修改生成的 `static/` 资产；最终统一运行 lint、typecheck、Vitest、Go tests 和 build。
- 每个提交信息必须包含：`Co-authored-by: JuunAI <juunai.ai@gmail.com>`。

---

## 文件与边界

### 新增

- `src/pi-app/transcript/transcript-view-model.ts`：从 `TranscriptState` 生成渲染消息和视图元数据；不操作 DOM。
- `src/pi-app/transcript/transcript-view-model.test.ts`：ViewModel 的状态、工具、错误和顺序测试。
- `src/pi-app/sessions/legacy-session-event-adapter.ts`：仅在后端暂未提供 durable stream 的旧部署中，把 legacy envelope 转成 reducer 可消费的 durable 事件；不直接渲染。
- `src/pi-app/sessions/legacy-session-event-adapter.test.ts`：legacy 事件映射测试。

### 修改

- `src/pi-app/sessions/transcript-state.ts`：补充渲染所需的稳定类型和事件字段约束。
- `src/pi-app/sessions/transcript-reducer.ts`：保证工具终态、失败、取消和稀疏事件的状态完整性。
- `src/pi-app/transcript/transcript-window-methods.ts`：将 transcript item 改为稳定业务 key + message + height，不保存 DOM 节点。
- `src/pi-app/transcript/transcript-virtual-scroller.ts`：卸载时只释放节点和 observer，重新挂载时始终从 message 投影生成节点。
- `src/pi-app/messages/message-methods.ts`：删除 DOM-first 的 assistant streaming 状态，改为提交 reducer 状态后刷新 ViewModel。
- `src/pi-app/messages/tool-message-methods.ts`：删除 DOM-first 工具输出缓存，工具内容和状态改由 ViewModel 投影。
- `src/pi-app/workspace/workspace-bootstrap-methods.ts`：统一 snapshot、SSE、resync、历史分页的 state-to-view 流程。
- `src/pi-app/index.ts`：删除 `aguiSubscriber` 和直接 DOM 事件分支，所有 legacy 事件交给 adapter/reducer。
- `src/pi-app/input/input-methods.ts`：用 `postPrompt` 替代 `runAguiSessionPrompt`，依赖 durable stream 展示运行过程。
- `src/lib/api.ts`：删除 `HttpAgent` import 和 `runAguiSessionPrompt`。
- `src/pi-app/sessions/session-stream-client.ts`：保留 generation guard，并明确只消费 durable SSE。
- `internal/piweb/server.go`：删除 AG-UI 路由注册。
- `internal/piweb/agui.go`、`internal/piweb/agui_event_adapter.go`：删除 AG-UI 实现。
- `internal/piweb/repository_contract_test.go`、`internal/piweb/server_workspace_policy_test.go`：删除 AG-UI 路由契约，补 durable prompt/stream 契约。
- `internal/piweb/go.mod` / `go.sum`：移除 AG-UI SDK 间接依赖后执行 `go mod tidy`。
- `docs/upstream-parity-audit.md`：将 P0 状态、统一渲染架构和 AG-UI 删除决策写回审查文档。

### 删除

- AG-UI 专用后端测试：`internal/piweb/agui_test.go`、`internal/piweb/agui_event_adapter_test.go`。
- AG-UI 专用前端测试：`src/lib/agui-api.test.ts`。

---

## Task 1: 建立稳定的 Transcript ViewModel

**目标：** 让渲染层只接收可序列化、无 DOM 引用的消息模型。

**Files:**
- Create: `src/pi-app/transcript/transcript-view-model.ts`
- Create: `src/pi-app/transcript/transcript-view-model.test.ts`
- Modify: `src/pi-app/sessions/transcript-state.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts:419-437`

**Interfaces:**

```ts
export type TranscriptViewMessage = {
  key: string;
  kind: "user" | "pi" | "think" | "tool" | "error" | "annotation" | "banner";
  text: string;
  tool?: string;
  args?: string;
  body?: string;
  status?: string;
  resultMeta?: string;
  durationMs?: number;
  attachments?: unknown[];
};

export type TranscriptViewModel = {
  sessionId: string;
  status: TranscriptStatus;
  runId?: string;
  items: TranscriptViewMessage[];
};

export function selectTranscriptView(state: TranscriptState): TranscriptViewModel;
export function selectTranscriptViewMessage(item: TranscriptItem): TranscriptViewMessage;
```

- [ ] **Step 1: Write failing selector tests.** 覆盖 user/pi/think/tool/error、工具 `bodyPreview`、`status: err`、稳定 key、itemOrder 顺序，以及空 state。
- [ ] **Step 2: Run the focused tests.**

```bash
bunx vitest run src/pi-app/transcript/transcript-view-model.test.ts
```

Expected: FAIL because the selector module and exports do not exist.
- [ ] **Step 3: Implement pure selectors.** 将现有 `durableMessage()` 的映射逻辑移入 `selectTranscriptViewMessage()`；不得读取 DOM，不得产生随机 ID，不得修改 state。
- [ ] **Step 4: Replace `durableMessage()` callers.** `renderDurableTranscript()` 只接收 `selectTranscriptView(state)` 的 `items`，删除 workspace 中重复的消息映射函数。
- [ ] **Step 5: Run focused tests and reducer tests.**

```bash
bunx vitest run \
  src/pi-app/transcript/transcript-view-model.test.ts \
  src/pi-app/sessions/transcript-reducer.test.ts
```

Expected: all tests pass.
- [ ] **Step 6: Commit.**

```bash
git add src/pi-app/transcript/transcript-view-model.ts \
  src/pi-app/transcript/transcript-view-model.test.ts \
  src/pi-app/sessions/transcript-state.ts \
  src/pi-app/workspace/workspace-bootstrap-methods.ts
git commit -m "refactor(ui): add canonical transcript view model"
```

---

## Task 2: 让虚拟滚动只缓存稳定数据，不缓存 DOM

**目标：** 节点滚出窗口后，输出和终态仍由 state 保留，重新挂载必然读取最新 ViewModel。

**Files:**
- Modify: `src/pi-app/transcript/transcript-window-methods.ts`
- Modify: `src/pi-app/transcript/transcript-virtual-scroller.ts`
- Modify: `src/pi-app/transcript/transcript-window.test.ts`
- Modify: `src/pi-app/transcript/transcript-virtualization.test.ts`

**Interfaces:**

```ts
type TranscriptRenderItem = {
  key: string;
  message: TranscriptViewMessage;
  height: number;
};

function renderVirtualTranscriptItem(owner: PiApp, item: TranscriptRenderItem): HTMLElement;
```

- [ ] **Step 1: Add failing virtualization tests.** 测试以下流程：创建 running tool → 让它离开可见窗口 → reducer 更新 output/status → 重新进入窗口；断言正文、错误状态正确且 `.tool-card` 只有一张。
- [ ] **Step 2: Run the new focused tests.**

```bash
bunx vitest run \
  src/pi-app/transcript/transcript-window.test.ts \
  src/pi-app/transcript/transcript-virtualization.test.ts
```

Expected: new regression tests FAIL because detached DOM data is still authoritative.
- [ ] **Step 3: Replace numeric-only render items.** 使用 domain `key` 对应 `TranscriptItem.id`，将 `message` 作为唯一渲染输入；`height` 是唯一允许保留的视图测量数据。
- [ ] **Step 4: Remove cached node ownership.** 删除 `item.nodes`、`transcriptItemNodes()` 中对旧节点的复用；`renderVirtualTranscriptItem()` 每次从 `item.message` 创建新节点。
- [ ] **Step 5: Make patching key-based.** 当前可见 wrapper 按 `data-transcript-item` 查找；消息变更时替换 wrapper 内容，不从旧卡片读取正文、args、status 或 output。
- [ ] **Step 6: Remove DOM business caches.** 删除 `latestToolCards` 注册和通过它查找卡片的逻辑；保留 DOM 查询只能用于找到当前可见 wrapper，不得作为状态索引。
- [ ] **Step 7: Run focused tests.**

```bash
bunx vitest run \
  src/pi-app/transcript/transcript-window.test.ts \
  src/pi-app/transcript/transcript-virtualization.test.ts \
  src/pi-app/messages/messages.test.ts
```

Expected: all relevant tests pass, including the detached-tool regression.
- [ ] **Step 8: Commit.**

```bash
git add src/pi-app/transcript/transcript-window-methods.ts \
  src/pi-app/transcript/transcript-virtual-scroller.ts \
  src/pi-app/transcript/transcript-window.test.ts \
  src/pi-app/transcript/transcript-virtualization.test.ts
 git commit -m "refactor(ui): make transcript virtualization state driven"
```

---

## Task 3: 将 assistant/tool streaming 收敛到 reducer

**目标：** 删除 `appendDelta()`、`appendToolOutput()` 作为业务状态入口；事件只能更新 `TranscriptState`，渲染器只投影 state。

**Files:**
- Modify: `src/pi-app/messages/message-methods.ts`
- Modify: `src/pi-app/messages/tool-message-methods.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts`
- Modify: `src/pi-app/sessions/transcript-reducer.ts`
- Modify: `src/pi-app/messages/messages.test.ts`
- Modify: `src/pi-app/sessions/session-durable-transport.test.ts`

**Interfaces:**

```ts
function applyTranscriptEvent(event: DurableSessionEvent): void;
function renderTranscriptView(options?: { stickToBottom?: boolean; preservePrepend?: boolean }): void;
```

- [ ] **Step 1: Add failing state-driven streaming tests.** 覆盖 assistant delta 合并、think delta、tool args delta、tool output append/replace、tool finished，以及同一事件重复到达时不重复 item。
- [ ] **Step 2: Run focused tests and confirm old path fails.**

```bash
bunx vitest run \
  src/pi-app/messages/messages.test.ts \
  src/pi-app/sessions/session-durable-transport.test.ts \
  src/pi-app/sessions/transcript-reducer.test.ts
```

Expected: tests expose direct DOM mutation and duplicate live/history entries.
- [ ] **Step 3: Make `applyDurableEvent()` the only event-to-view entry.** reducer 返回新 state 后调用 `renderTranscriptView()`；只在生命周期事件时同步运行模式，不能由 DOM 内容推断运行状态。
- [ ] **Step 4: Remove streaming business methods.** 删除 `appendDelta()`、`appendToolOutput()`、`finishTool()` 作为事件入口的职责；事件只允许从 `applyDurableEvent()` 或 legacy adapter 进入 reducer。保留的 DOM helper 只能接收 `TranscriptViewMessage` 并生成节点，不得写 `dataset.streamText`、`__toolOutputText` 或其他业务缓存。
- [ ] **Step 5: Separate presentation-only HTML.** `messageNode()` 仍可生成 Markdown/工具 HTML，但输入必须是 ViewModel message；工具展开、复制、显示完整输出等交互状态单独保留在 DOM/UI 层，不得覆盖 transcript 内容。
- [ ] **Step 6: Add atomic render scheduling.** 在已有 `transcript-render-scheduler.ts` 上按 item key 合并刷新，保证 burst delta 每帧最多一次投影；flush 前 state 已包含全部事件。
- [ ] **Step 7: Run focused tests.**

```bash
bunx vitest run \
  src/pi-app/messages/messages.test.ts \
  src/pi-app/sessions/session-durable-transport.test.ts \
  src/pi-app/sessions/transcript-reducer.test.ts \
  src/pi-app/transcript/transcript-render-scheduler.test.ts
```

Expected: all pass; no test should require detached DOM text as input.
- [ ] **Step 8: Commit.**

```bash
git add src/pi-app/messages/message-methods.ts \
  src/pi-app/messages/tool-message-methods.ts \
  src/pi-app/index.ts \
  src/pi-app/workspace/workspace-bootstrap-methods.ts \
  src/pi-app/sessions/transcript-reducer.ts \
  src/pi-app/messages/messages.test.ts \
  src/pi-app/sessions/session-durable-transport.test.ts
git commit -m "refactor(ui): route streaming through transcript reducer"
```

---

## Task 4: 兼容旧事件流，但禁止旧事件直接操作 DOM

**目标：** durable 后端不可用的旧部署仍可工作，但 legacy event stream 也必须进入同一 reducer；不恢复 DOM-first 分支。

**Files:**
- Create: `src/pi-app/sessions/legacy-session-event-adapter.ts`
- Create: `src/pi-app/sessions/legacy-session-event-adapter.test.ts`
- Modify: `src/pi-app/index.ts:211-408`
- Modify: `src/pi-app/sessions/transcript-state.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts`

**Interfaces:**

```ts
export function adaptLegacySessionEvent(
  event: SessionEventEnvelope,
  context: { sessionId: string; runId?: string; activeToolIds: Map<string, string> },
): DurableSessionEvent[];
```

Mapping contract:

- `session.delta(kind=pi)` → `assistant.segment.delta`。
- `session.delta(kind=think)` → `assistant.segment.delta` with `kind=think`。
- `session.message(kind=pi|think)` → `assistant.segment.committed`。
- `tool.started` → `tool.started`，稳定使用 payload `id/toolCallId`，缺失时按 session/run/sequence 生成一次 ID。
- `tool.output` → `tool.output`，保留 `append`/`replace`。
- `tool.finished` → `tool.finished`，完整保留 `body/status/resultMeta/durationMs`。
- `session.status` → `run.started`/`run.finished`/`run.cancelled`；`error` → `run.failed`。

- [ ] **Step 1: Write adapter tests first.** 覆盖事件顺序、缺失 ID、append/replace、失败工具和 terminal status。
- [ ] **Step 2: Run adapter tests.**

```bash
bunx vitest run src/pi-app/sessions/legacy-session-event-adapter.test.ts
```

Expected: FAIL before adapter exists.
- [ ] **Step 3: Implement deterministic mapping.** adapter 只返回事件，不访问 `document`、`termInner` 或 message methods。
- [ ] **Step 4: Route `connectEvents()` through adapter + reducer.** 保留旧 SSE 的重连、cursor、gap/resync 逻辑；删除 `applyEvent()` 中对 `appendMessage()`、`appendDelta()`、`appendToolOutput()`、`finishTool()` 的直接调用。
- [ ] **Step 5: Verify both transports render identical state.** 给同一组 legacy 和 durable fixtures，断言 `selectTranscriptView()` 输出深相等。
- [ ] **Step 6: Run focused tests.**

```bash
bunx vitest run \
  src/pi-app/sessions/legacy-session-event-adapter.test.ts \
  src/pi-app/sessions/session-stream-client.test.ts \
  src/pi-app/sessions/session-recovery.test.ts \
  src/pi-app/messages/messages.test.ts
```

- [ ] **Step 7: Commit.**

```bash
git add src/pi-app/sessions/legacy-session-event-adapter.ts \
  src/pi-app/sessions/legacy-session-event-adapter.test.ts \
  src/pi-app/index.ts \
  src/pi-app/sessions/transcript-state.ts \
  src/pi-app/workspace/workspace-bootstrap-methods.ts
git commit -m "refactor(ui): adapt legacy events into transcript state"
```

---

## Task 5: 删除 AG-UI，命令改走 durable stream

**目标：** 消除第二条实时展示通道，避免 AG-UI 终态、cursor、DOM 状态和重复 prompt 问题。

**Files:**
- Modify: `src/pi-app/input/input-methods.ts:1-100,360-415`
- Modify: `src/lib/api.ts:1,299-374`
- Modify: `src/pi-app/index.ts:200-378`
- Modify: `internal/piweb/server.go:247`
- Delete: `internal/piweb/agui.go`
- Delete: `internal/piweb/agui_event_adapter.go`
- Delete: `src/lib/agui-api.test.ts`
- Delete: `internal/piweb/agui_test.go`
- Delete: `internal/piweb/agui_event_adapter_test.go`
- Modify: `internal/piweb/repository_contract_test.go`
- Modify: `internal/piweb/server_workspace_policy_test.go`
- Modify: `go.mod` / `go.sum`

**Interfaces:**

```ts
export function postPrompt(
  sessionId: string,
  text: string,
  attachments?: PromptAttachment[],
  clientRequestId?: string,
): Promise<PromptResponse>;
```

- [ ] **Step 1: Add failing prompt-path tests.** 断言发送一次 prompt 后，UI 不调用 AG-UI，不创建第二条流；运行、tool、终态、错误均由 session stream 事件驱动。
- [ ] **Step 2: Run the focused tests.**

```bash
bunx vitest run \
  src/pi-app/input/input-methods-coverage.test.ts \
  src/pi-app/input/input.test.ts \
  src/pi-app/sessions/session-durable-transport.test.ts
```

Expected: tests fail while input still calls `runAguiSessionPrompt`.
- [ ] **Step 3: Change input submission.** 在发送前确保当前 durable/legacy event stream 已连接；调用 `postPrompt()` 一次，使用 `clientRequestId` 保持重试幂等；删除 AG-UI subscriber、run token 和 fallback retry。
- [ ] **Step 4: Make mode/error state reducer-driven.** `run.started`、`run.failed`、`run.finished`、`run.cancelled` 更新 `TranscriptState.status`；输入层只负责禁用/恢复 composer，不负责渲染回答。
- [ ] **Step 5: Remove AG-UI client code.** 删除 `HttpAgent` import、函数、subscriber 方法和 `aguiRunActive` 分支；保留普通 session stream 的 generation guard。
- [ ] **Step 6: Remove server route and SDK.** 删除路由、实现、AG-UI tests，更新 route contract tests，执行 `go mod tidy`。
- [ ] **Step 7: Run frontend and backend focused tests.**

```bash
bunx vitest run \
  src/pi-app/input/input-methods-coverage.test.ts \
  src/pi-app/input/input.test.ts \
  src/pi-app/sessions/session-durable-transport.test.ts

go test ./internal/piweb -run 'Test(Server|Repository|Session|Broker|Prompt)'
```

- [ ] **Step 8: Commit.**

```bash
git add src/pi-app/input/input-methods.ts \
  src/lib/api.ts \
  src/pi-app/index.ts \
  internal/piweb/server.go \
  internal/piweb/repository_contract_test.go \
  internal/piweb/server_workspace_policy_test.go \
  go.mod go.sum
git rm src/lib/agui-api.test.ts \
  internal/piweb/agui.go \
  internal/piweb/agui_event_adapter.go \
  internal/piweb/agui_test.go \
  internal/piweb/agui_event_adapter_test.go
git commit -m "refactor(stream): remove AG-UI session transport"
```

---

## Task 6: 完成工具失败状态与 durable cursor 验收

**目标：** 对剩余 P0-3/P0-4 建立不可回归的端到端边界。

**Files:**
- Modify: `src/pi-app/sessions/transcript-reducer.test.ts`
- Modify: `src/pi-app/sessions/session-recovery.test.ts`
- Modify: `src/pi-app/sessions/session-durable-transport.test.ts`
- Modify: `src/pi-app/transcript/transcript-virtualization.test.ts`
- Modify: `internal/piweb/session_event_projection_test.go`
- Modify: `internal/piweb/broker_recovery_test.go`
- Modify: `internal/piweb/session_sse_test.go`

- [ ] **Step 1: Add reducer failure tests.** `tool.finished(status=err, body=boom)` 后 ViewModel 必须同时显示 `boom` 和 `err`；`run.failed` 必须保留工具终态并产生 error item；cancelled/interrupted 不得被归为 ok。
- [ ] **Step 2: Add cursor ordering tests.** 模拟事件 `tool.finished`、断线、重连，断言客户端只从最后一个已应用 `seq` 恢复；payload 未应用时不得推进 cursor。
- [ ] **Step 3: Add stale EventSource tests.** 保留并扩展现有 replacement-source 测试，覆盖旧 source 的 `error`、malformed payload、reset、heartbeat 全部不能关闭新 source。
- [ ] **Step 4: Fix broker snapshot/subscribe race.** 以当前失败的 `TestSnapshotCursorSafelyBridgesPublishBeforeSubscribe` 为回归入口，保证 snapshot cursor 与 subscribe barrier 在同一锁/屏障语义下，不丢 snapshot 后发布的事件。
- [ ] **Step 5: Run focused P0 suite.**

```bash
bunx vitest run \
  src/pi-app/sessions/transcript-reducer.test.ts \
  src/pi-app/sessions/session-recovery.test.ts \
  src/pi-app/sessions/session-stream-client.test.ts \
  src/pi-app/sessions/session-durable-transport.test.ts \
  src/pi-app/transcript/transcript-virtualization.test.ts

go test ./internal/piweb -run 'Test(Broker|SessionEvent|Snapshot|.*Cursor|.*Recovery)'
```

- [ ] **Step 6: Commit.**

```bash
git add src/pi-app/sessions/transcript-reducer.test.ts \
  src/pi-app/sessions/session-recovery.test.ts \
  src/pi-app/sessions/session-durable-transport.test.ts \
  src/pi-app/transcript/transcript-virtualization.test.ts \
  internal/piweb/session_event_projection_test.go \
  internal/piweb/broker_recovery_test.go \
  internal/piweb/session_sse_test.go
git commit -m "test(stream): lock down p0 transcript recovery semantics"
```

---

## Task 7: 清理旧 DOM-first API 与文档

**目标：** 让源码结构无法重新引入 P0 的双状态源。

**Files:**
- Modify: `src/pi-app/messages/message-methods.ts`
- Modify: `src/pi-app/messages/tool-message-methods.ts`
- Modify: `src/pi-app/transcript/transcript-window-methods.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `docs/upstream-parity-audit.md`
- Create: `src/pi-app/transcript/transcript-state-boundary.test.ts`

- [ ] **Step 1: Add static boundary tests.** 读取相关源码并断言不存在 `dataset.streamText`、`__toolOutputText`、`latestToolCards`、`item.nodes` 业务写入，以及不存在 `runAguiSessionPrompt`、`aguiSubscriber`、`/ag-ui`。
- [ ] **Step 2: Remove dead methods and imports.** 删除只服务 AG-UI/DOM-first 的函数；保留纯展示函数和明确的 interaction-only DOM state。
- [ ] **Step 3: Update the audit.** 将四项 P0 标记为“已解决”，说明：统一 reducer/ViewModel、AG-UI 已移除、cursor 只随 durable event 应用推进、工具失败状态保留。
- [ ] **Step 4: Run boundary and lint tests.**

```bash
bunx vitest run src/pi-app/transcript/transcript-state-boundary.test.ts
bun run lint
bun run typecheck
```

- [ ] **Step 5: Commit.**

```bash
git add src/pi-app/messages/message-methods.ts \
  src/pi-app/messages/tool-message-methods.ts \
  src/pi-app/transcript/transcript-window-methods.ts \
  src/pi-app/index.ts \
  src/pi-app/transcript/transcript-state-boundary.test.ts \
  docs/upstream-parity-audit.md
git commit -m "docs: close p0 transcript parity audit"
```

---

## Task 8: 全量验证与发布前检查

- [ ] **Step 1: Run Vitest through the project configuration.** 不使用裸 `bun test`，使用项目当前的 Vitest 配置与 jsdom 环境：

```bash
bunx vitest run
```

Expected: 0 failed tests and 0 unhandled errors。
- [ ] **Step 2: Run Go tests.**

```bash
go test ./...
```

Expected: exit code 0。
- [ ] **Step 3: Run production checks.**

```bash
bun run lint
bun run typecheck
bun run build
```

Expected: all commands exit 0；不得执行 `embed:assets`，避免修改 `static/`。
- [ ] **Step 4: Verify no AG-UI references remain.**

```bash
rg -n "AG-UI|agui|/ag-ui|HttpAgent|runAguiSessionPrompt|aguiSubscriber" \
  src internal go.mod go.sum package.json
```

Expected: 无生产代码引用；仅允许迁移说明中的历史文字。
- [ ] **Step 5: Verify Git diff.**

```bash
git status --short
git diff --check
```

Expected: 无生成资产变更、无 whitespace error、所有源代码变更已提交。
- [ ] **Step 6: Final commit if verification-only documentation changed.**

```bash
git status --short
```

只有在前述步骤产生计划外文档修正时才创建一个 `chore: verify p0 transcript migration` 提交，并包含 Co-authored-by trailer。

## Acceptance Checklist

- [ ] 工具滚出虚拟窗口后继续输出，重新挂载显示完整最新内容。
- [ ] 工具失败显示 `err`，不会被终态回调改成 `ok`。
- [ ] snapshot/live append 不产生第二张工具卡。
- [ ] 旧 EventSource 回调不能关闭或影响新连接。
- [ ] cursor 只在对应事件已被 reducer 应用后推进。
- [ ] prompt 只发一次，运行过程完全由 session SSE 驱动。
- [ ] `/api/sessions/{id}/ag-ui` 已删除，AG-UI 依赖和代码引用已清零。
- [ ] full Vitest、Go test、lint、typecheck、build 全部通过。
