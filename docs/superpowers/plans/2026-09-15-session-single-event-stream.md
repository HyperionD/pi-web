# Session Single Event Stream Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将会话域的所有可见状态统一到 SQLite `session_events`，最终只保留 `/state` + `/events-v2` 和 `TranscriptState` reducer。

**Architecture:** 以 `SessionCommandService` 为唯一会话命令入口，以 SQLite event store 为唯一写入真源和 projection 来源。迁移期间旧 `/events` 只能作为从新事件流派生的只读兼容层，最终删除 Broker 会话流、legacy adapter 和 durable/legacy 分支。

**Tech Stack:** Go、SQLite、TypeScript、SSE、Vitest、Go `testing`、Playwright。

**Spec:** `docs/superpowers/specs/2026-09-15-session-single-event-stream-design.md`

## Global Constraints

- 只修改会话域；不改 workspace 文件树、编辑器、Git、账号、OAuth、布局、语言和独立 shell 终端。
- `session_events` 是唯一会话状态真源；JSONL 只可用于首次导入或显式 rebase。
- 所有命令使用 `clientRequestId + requestHash` 幂等；重复命令必须返回原 receipt，不能重复产生日志事件。
- REST 命令响应只表示 accepted/receipt；UI 终态只能由事件 reducer 产生。
- 迁移期间 Broker 只能作为从 durable event 派生的兼容读适配器，业务代码不得直接 `Broker.Publish`。
- 不把当前工作区已有的 `static/` 生成物变更混入实现提交。
- 每个任务独立运行相关 Go/Vitest/Playwright 检查，并用 Conventional Commit 提交。

---

## 文件地图

| 文件/目录 | 责任 |
| --- | --- |
| `internal/piweb/session_event_types.go` | 会话事件常量、payload/stream 类型 |
| `internal/piweb/session_projection.go` | 事件到 metadata/transcript/run projection 的归约 |
| `internal/piweb/session_event_append.go` | 事务 append、seq、projection、幂等 receipt |
| `internal/piweb/session_command_service.go` | 会话命令入口与异步 runtime 启动 |
| `internal/piweb/server_session_handlers.go` | 会话 HTTP command/read handlers |
| `internal/piweb/compaction.go` | compact runtime 控制；迁移后不得直接写 Broker |
| `internal/piweb/runner.go`、`session_durable_runner.go` | steer/cancel/run terminal 事件写入 |
| `internal/piweb/session_sse.go` | snapshot、唯一 durable SSE、content range |
| `internal/piweb/*_test.go` | event store、projection、command、SSE、handler 集成验证 |
| `src/pi-app/workspace/workspace-bootstrap-methods.ts` | 会话加载、snapshot、历史分页、流启动 |
| `src/pi-app/index.ts` | 当前旧流入口、状态转换和事件处理 |
| `src/pi-app/input/input-methods.ts` | prompt/steer/cancel 的客户端 command 行为 |
| `src/pi-app/status/compaction-methods.ts` | compact command 和客户端等待逻辑 |
| `src/pi-app/status/toast-methods.ts` | 后台会话通知流 |
| `src/pi-app/sessions/transcript-reducer.ts` | 唯一 transcript/run reducer |
| `src/pi-app/sessions/session-stream-client.ts` | durable SSE、cursor、reset、重连 |
| `src/pi-app/sessions/legacy-session-event-adapter.ts` | 迁移期兼容层，最终删除 |
| `e2e/session-stream-recovery.spec.ts` | 现有 durable 恢复 E2E；需扩展为真实 session command 覆盖 |
| `scripts/check-session-single-stream.mjs` | 静态禁止旧会话流/写入旁路 |

---

### Task 1: 建立事件序列基线和静态不变量

**Files:**
- Create: `internal/piweb/session_domain_integration_test.go`
- Create: `src/pi-app/sessions/session-domain-events.test.ts`
- Create: `scripts/check-session-single-stream.mjs`
- Modify: `package.json: scripts`
- Modify: `internal/piweb/repository_contract_test.go`

**Interfaces:**
- Consumes: 现有 `NewServerWithEventsAndRuntimeManager`、`session_events` store、`sessionEventsV2`、`createSessionStreamClient`。
- Produces: 可复用的 session command/event fixture，以及 `bun run check:session-stream` 静态检查。

Task 1 在 `internal/piweb/session_domain_integration_test.go` 中定义以下测试辅助接口，后续任务复用：

```go
func newDurableIntegrationServer(t *testing.T) (*httptest.Server, SessionEventStore, string)
func postPromptAndDecodeReceipt(t *testing.T, server *httptest.Server, sessionID, requestID, text string) RunReceipt
func postJSON(t *testing.T, server *httptest.Server, path string, body any) *http.Response
func getState(t *testing.T, server *httptest.Server, sessionID string) SessionSnapshot
func getSession(t *testing.T, server *httptest.Server, sessionID string) SessionResponse
func mustReadEvents(t *testing.T, events SessionEventStore, sessionID string) []StoredSessionEvent
func eventually(t *testing.T, timeout time.Duration, condition func() bool)
func requireTypes(t *testing.T, events []StoredSessionEvent, types ...string)
func hasEventTypes(events []StoredSessionEvent, types ...string) bool
```

- [ ] **Step 1: 写 Go 失败测试，固定 command → event → projection → SSE 链路。**

```go
func TestSessionDomainPromptProducesOneDurableSequence(t *testing.T) {
    server, events, sessionID := newDurableIntegrationServer(t)
    receipt := postPromptAndDecodeReceipt(t, server, sessionID, "req-1", "hello")

    eventually(t, 2*time.Second, func() bool {
        snapshot, err := events.Snapshot(context.Background(), sessionID, SessionPageRequest{Limit: 50})
        return err == nil && snapshot.Status == SessionStatusIdle
    })

    rows := mustReadEvents(t, events, sessionID)
    requireTypes(t, rows,
        EventUserMessageCreated,
        EventRunStarted,
        EventAssistantSegmentStarted,
        EventAssistantSegmentCommit,
        EventRunFinished,
    )
    if receipt.AcceptedSeq != rows[1].Seq {
        t.Fatalf("accepted seq = %d, run.started seq = %d", receipt.AcceptedSeq, rows[1].Seq)
    }
}
```

- [ ] **Step 2: 运行该测试确认当前实现暴露缺口。**

Run: `go test ./internal/piweb -run TestSessionDomainPromptProducesOneDurableSequence -count=1`

Expected: 当前 mock/runtime fixture 尚未覆盖完整事件序列时失败；不得通过放宽断言解决。

- [ ] **Step 3: 写前端 reducer 契约测试，确认命令 receipt 不直接改变事实状态。**

```ts
it("keeps state unchanged until user.message.created arrives", () => {
  const before = emptyTranscriptState("s1", "view-1");
  const afterReceipt = before;
  expect(afterReceipt.itemsById.size).toBe(0);
  const afterEvent = reduceSessionEvent(afterReceipt, {
    seq: "1", sessionId: "s1", sourceId: "req-1:user",
    type: "user.message.created", payload: { text: "hello" },
  });
  expect(afterEvent.itemOrder).toHaveLength(1);
});
```

- [ ] **Step 4: 添加静态检查，扫描会话生产代码中的旧写入和旧订阅。**

`check-session-single-stream.mjs` 必须扫描 `internal/piweb`、`src/pi-app`，并对以下匹配返回非零：`Broker.Publish(`、`appendMessageAndPublish(`、`sessionEvents(`、`/api/sessions/` + `events`（排除 `events-v2`）、`legacy-session-event-adapter`。迁移阶段用 `--compat` 允许旧只读适配器文件，但仍禁止业务写入。

- [ ] **Step 5: 将脚本加入 package scripts，并补路由契约断言。**

```json
"check:session-stream": "node scripts/check-session-single-stream.mjs"
```

- [ ] **Step 6: 运行基线检查并提交。**

Run: `go test ./internal/piweb -run 'TestSessionDomainPromptProducesOneDurableSequence|TestRepositoryContract' -count=1`

Run: `bun run test -- src/pi-app/sessions/session-domain-events.test.ts`

Run: `bun run check:session-stream -- --compat`

Commit: `test: establish session event stream invariants`

---

### Task 2: 扩展事件模型和 projection，覆盖会话元数据与控制状态

**Files:**
- Modify: `internal/piweb/session_event_types.go`
- Modify: `internal/piweb/session_projection.go`
- Modify: `internal/piweb/session_event_schema.go`
- Create: `internal/piweb/session_metadata_projection.go`
- Create: `internal/piweb/session_metadata_projection_test.go`
- Modify: `internal/piweb/session_event_append_test.go`
- Modify: `internal/piweb/session_projection_test.go`

**Interfaces:**
- Consumes: `StoredSessionEvent`, `ProjectionView`, `ProjectionChange`, `SQLiteSessionEventStore.Append`。
- Produces: `session.created`, `session.renamed`, `session.deleted`, `session.model.changed`, `run.cancel.requested`, `compaction.*`, `context.usage.updated` 的常量、payload 校验和 projection 更新。

- [ ] **Step 1: 先写事件 reducer 失败测试。**

```go
func TestReduceSessionEventRenamesSessionProjection(t *testing.T) {
    view := emptyProjectionView("s1")
    event := storedEvent(1, "s1", "", EventSessionRenamed, map[string]any{"title": "New title"})
    change, err := ReduceSessionEvent(view, event)
    if err != nil { t.Fatal(err) }
    if change.Metadata == nil || change.Metadata.Title != "New title" {
        t.Fatalf("metadata title = %#v", change.Metadata)
    }
}
```

为每个新增事件分别添加：合法 payload、空字段、错误 session/run、重复事件、终态后拒绝非法变更测试。

- [ ] **Step 2: 定义明确 payload 结构和 projection change 字段。**

在 `session_event_types.go` 中增加具名结构，而不是在 reducer 中使用无类型 `map[string]any`：

```go
type SessionMetadataProjection struct {
    SessionID string
    WorkspaceID string
    Title string
    Provider string
    Model string
    Deleted bool
    UpdatedSeq int64
}

type SessionMetadataChange struct {
    Metadata *SessionMetadataProjection
}
```

将 `ProjectionChange` 扩展为 `Metadata *SessionMetadataProjection`，并让 `persistProjectionChange` 在同一事务写入 metadata projection。

- [ ] **Step 3: 将新增事件纳入校验和归约。**

`ReduceSessionEvent` 必须覆盖 `EventSessionCreated`、`EventSessionRenamed`、`EventSessionDeleted`、`EventSessionModelChanged`、`EventRunCancelRequested`、`EventCompactionStarted`、`EventCompactionCommitted`、`EventCompactionFailed`、`EventContextUsageUpdated`，未知事件继续返回 `ErrUnknownSessionEvent`。

- [ ] **Step 4: 运行 projection 测试并修复事务写入。**

Run: `go test ./internal/piweb -run 'TestReduceSessionEvent|TestSQLiteSessionEventStore' -count=1`

Expected: 新事件均可重放，projection seq 与 event seq 相同，事务失败时 event 和 projection 同时回滚。

- [ ] **Step 5: 提交。**

Commit: `feat: add session metadata and control events`

---

### Task 3: 将后端所有会话命令接入 durable event store

**Files:**
- Modify: `internal/piweb/session_command_service.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Modify: `internal/piweb/runner.go`
- Modify: `internal/piweb/session_durable_runner.go`
- Modify: `internal/piweb/compaction.go`
- Modify: `internal/piweb/server.go`
- Create: `internal/piweb/session_command_handlers_test.go`
- Modify: `internal/piweb/server_durable_prompt_test.go`
- Modify: `internal/piweb/compaction_test.go`
- Modify: `internal/piweb/runtime_regression_test.go`

**Interfaces:**
- Consumes: Task 2 的事件常量和 metadata projection。
- Produces: `StartPromptWith` 之外的统一命令入口；所有命令返回 receipt，并只通过 event store 改变会话状态。

- [ ] **Step 1: 为 compact、rename、steer、cancel、model、delete 写失败 handler 测试。**

每个测试都必须读取同一 session 的 `EventsAfter`，而不是只检查 HTTP 200：

```go
func TestCompactAppendsCompactionEvents(t *testing.T) {
    server, events, sessionID := newDurableIntegrationServer(t)
    response := postJSON(t, server, "/api/sessions/"+sessionID+"/compact", nil)
    requireStatus(t, response, http.StatusAccepted)
    eventually(t, time.Second, func() bool {
        rows := mustReadEvents(t, events, sessionID)
        return hasEventTypes(rows, EventCompactionStarted, EventCompactionCommitted)
    })
}
```

对 rename/model/delete 验证 metadata projection；steer 验证新的 `user.message.created` 或 `steer.accepted`；cancel 验证 `run.cancel.requested` 后出现 `run.cancelled` 或 `run.interrupted`。

- [ ] **Step 2: 将 compact 改为 command + event adapter。**

`Runner.Compact` 不得直接调用 `broker.Publish` 或 `appendMessageAndPublish`。新增 durable compact starter，按以下顺序写事件：

```text
compaction.started
→ pi RPC compact
→ compaction.committed + optional transcript marker
→ run/status projection returns idle
```

失败必须写 `compaction.failed`，并由 projection 产生可恢复的错误状态。

- [ ] **Step 3: 将 rename/model/delete 改为 event store command。**

`renameSession`、自动命名、`setSessionModel`、`deleteSession` 都先经过 `SessionCommandService`。删除采用 tombstone：先写 `session.deleted`，再清理非事件缓存；读接口对 tombstone 返回一致的 not found，不直接从 Store 判断当前状态。

- [ ] **Step 4: 将 steer/cancel 的接受和终态统一写入 event store。**

`activePiRun.durableSteer` 继续调用 adapter，但用户消息的 source ID 必须使用 command receipt；`Runner.Cancel` 在 durable run 上只通过 `completeDurableTerminal(EventRunCancelled)` 完成，不在 handler 中 publish Broker status。

- [ ] **Step 5: 为 event store 不可用路径设置 fail-closed。**

`POST /prompt|steer|cancel|compact|rename|model|delete` 在 `s.events == nil` 时返回 `503 event store unavailable`，不得调用旧 runner/Broker 分支。mock 模式单独保留为测试 fixture，不得被生产 durable handler 使用。

- [ ] **Step 6: 运行后端全量验证并提交。**

Run: `go test ./internal/piweb -run 'Test(ServerDurablePrompt|Compact|Runtime|SessionCommand)' -count=1`

Run: `go test ./...`

Commit: `feat: route all session commands through durable events`

---

### Task 4: 收敛后端读取、快照和迁移期兼容层

**Files:**
- Modify: `internal/piweb/session_sse.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Modify: `internal/piweb/store_sessions.go`
- Modify: `internal/piweb/server.go`
- Create: `internal/piweb/session_legacy_compat.go`
- Create: `internal/piweb/session_legacy_compat_test.go`
- Modify: `internal/piweb/session_sse_test.go`
- Modify: `internal/piweb/repository_contract_test.go`

**Interfaces:**
- Consumes: Task 2 的 metadata/transcript projection 和 Task 3 的 durable command events。
- Produces: `GET /state`、`/events-v2` 为唯一真源；迁移期 `/events` 只从 durable events 转换，不向 Broker 写回。

- [ ] **Step 1: 写读取一致性失败测试。**

```go
func TestLegacySessionReadAndDurableStateShareProjection(t *testing.T) {
    server, events, sessionID := newDurableIntegrationServer(t)
    postPromptAndWait(t, server, sessionID, "req-1", "hello")

    state := getState(t, server, sessionID)
    legacy := getSession(t, server, sessionID)
    if legacy.LatestEventID != state.Seq {
        t.Fatalf("legacy cursor %s != durable seq %s", legacy.LatestEventID, state.Seq)
    }
    assertSameTranscript(t, legacy.Messages, state.Items)
    _ = events
}
```

- [ ] **Step 2: 重写 `sessionSnapshot` 和 `sessionStatus`。**

当 event store 可用时，`GET /api/sessions/:id` 必须由 durable snapshot + metadata projection 生成；禁止 `broker.WithSessionSnapshot`、`runner.IsRunning` 和 `broker.IsRunning` 参与会话事实状态。

- [ ] **Step 3: 实现只读旧格式适配器。**

`session_legacy_compat.go` 读取 `EventsAfter`，把 durable event 转成旧 SSE envelope；适配器不得调用 `Broker.Publish`。旧事件 ID 必须映射自 durable `seq`，并在 `broker_gap` 等旧语义无法表达时返回明确 reset。

- [ ] **Step 4: 让旧 `/events` 在兼容期只读 durable stream。**

保留路由用于迁移期旧客户端，但 handler 不再调用 `s.broker.ServeSession`。增加 `Deprecation: true` 响应头和日志字段 `session_stream=legacy_compat`。

- [ ] **Step 5: 运行路由、cursor、reset、重启测试并提交。**

Run: `go test ./internal/piweb -run 'Test(SessionSSE|LegacySession|RepositoryContract|Broker)' -count=1`

Commit: `refactor: derive legacy session reads from durable events`

---

### Task 5: 前端主会话切换到唯一 durable stream

**Files:**
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts`
- Modify: `src/pi-app/sessions/session-stream-client.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `src/pi-app/sessions/transcript-reducer.ts`
- Delete: `src/pi-app/sessions/legacy-session-event-adapter.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-coverage.test.ts`
- Modify: `src/pi-app/sessions/session-stream-client.test.ts`
- Modify: `src/pi-app/sessions/transcript-reducer.test.ts`
- Modify: `src/pi-app/core-events-coverage.test.ts`

**Interfaces:**
- Consumes: 后端 `/state` 和 `/events-v2`；使用现有 `SessionStreamClient`、`installSessionSnapshot`、`reduceSessionEvent`。
- Produces: `loadSession()`、历史分页、恢复和会话切换均只操作 `TranscriptState`，不再暴露 legacy cursor/event adapter。

- [ ] **Step 1: 写失败测试，验证 durable unavailable 不 fallback。**

```ts
it("does not fall back to legacy session REST/events when durable state fails", async () => {
  app.durableEvents = true;
  getSessionStateMock.mockRejectedValue(new Error("event store unavailable"));
  await app.loadSession("s1");
  expect(getSessionMock).not.toHaveBeenCalled();
  expect(sessionEventsMock).not.toHaveBeenCalled();
  expect(app.connectionState).toBe("err");
});
```

- [ ] **Step 2: 删除 `loadSession` 的 legacy 分支。**

`loadSession()` 只调用 `loadDurableSessionState()`；`loadOlderSessionMessages()` 只调用 `getSessionState(before=cursor)`；snapshot cursor、projection generation 和 seq 继续由 reducer 校验。

- [ ] **Step 3: 删除旧 `connectEvents/applyEvent` 生产路径。**

`index.ts` 不再 import/use `sessionEvents`、`adaptLegacySessionEvent`、`legacyEventContext` 或旧 cursor。会话流统一调用 `createSessionStreamClient({ onEvent: applyDurableEvent, onReset: requestSessionResync })`。

- [ ] **Step 4: 运行前端核心测试并提交。**

Run: `bun run test -- src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/sessions/session-stream-client.test.ts src/pi-app/sessions/transcript-reducer.test.ts`

Run: `bun run typecheck`

Commit: `refactor: make durable stream the only session client path`

---

### Task 6: 删除客户端 optimistic 状态和 REST 后置状态旁路

**Files:**
- Modify: `src/pi-app/input/input-methods.ts`
- Modify: `src/pi-app/status/compaction-methods.ts`
- Modify: `src/pi-app/status/toast-methods.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `src/pi-app/messages/message-methods.ts`
- Modify: `src/pi-app/workspace/settings-methods.ts`
- Modify: `src/pi-app/input/input-methods-coverage.test.ts`
- Modify: `src/pi-app/core-events-coverage.test.ts`
- Create: `src/pi-app/sessions/session-command-state.test.ts`

**Interfaces:**
- Consumes: Task 5 的唯一 `TranscriptState` stream。
- Produces: command receipt UI；所有会话事实状态由 event reducer 产生，后台通知复用同一 stream client。

- [ ] **Step 1: 写失败测试，固定“REST accepted 不等于状态完成”。**

```ts
it("does not append a user item after steer REST succeeds", async () => {
  app.durableEvents = true;
  steerSessionMock.mockResolvedValue({ accepted: true });
  await app.submitSteeringPrompt("follow up");
  expect(app.transcriptState.itemOrder).toHaveLength(0);
});
```

另测 `cancelActiveSession()`、`compactActiveSession()`：HTTP 成功后状态仍保持事件前状态，收到 `run.cancelled` 或 `compaction.committed` 后才变化。

- [ ] **Step 2: 移除 prompt/steer 的 optimistic transcript。**

`submitPrompt()` 保留输入清理、command receipt 和临时 pending ViewModel，不调用 `appendMessage({ kind: "user" })`；用户消息只能由 `user.message.created` 事件产生。`submitSteeringPrompt()` 删除 REST 成功后的 `appendMessage()`。

- [ ] **Step 3: 将 setMode/finishTool 改为事件派生。**

`setMode()` 只更新连接/按钮 pending 表现，不写 `running`、`compacting` 的事实字段；`run.finished|failed|cancelled|interrupted` 在 reducer 更新状态后触发 UI sync。删除 `finishRunningTools()` 作为终态修复入口。

- [ ] **Step 4: compact 和后台通知只等待事件。**

`compactActiveSession()` 在 accepted 后等待 stream 事件；`toast-methods.ts` 所有后台会话都使用 `createSessionStreamClient`，禁止 `sessionEvents()` 分支。通知由 `run.finished|run.failed|run.cancelled|run.interrupted` 派生。

- [ ] **Step 5: 将 session model/context usage 的响应改为 projection refresh。**

REST command 只显示 pending；收到 `session.model.changed` 或 `context.usage.updated` 后更新 session projection。若仍保留只读 context endpoint，响应必须与 snapshot projection 的 seq 关联，不能覆盖 reducer 状态。

- [ ] **Step 6: 运行前端全量测试并提交。**

Run: `bun run test`

Run: `bun run typecheck && bun run lint`

Commit: `refactor: remove session optimistic state paths`

---

### Task 7: 增加真实后端 E2E 和故障恢复覆盖

**Files:**
- Create: `e2e/session-domain-events.spec.ts`
- Modify: `e2e/fixtures/session-stream-backend.ts`
- Modify: `playwright.config.ts`
- Modify: `internal/piweb/session_domain_integration_test.go`
- Modify: `src/pi-app/sessions/session-stream-client.test.ts`

**Interfaces:**
- Consumes: Task 3–6 的 durable command endpoints 和唯一 stream。
- Produces: 不依赖 `MockEventSource` 替换所有 API 的真实 session domain acceptance suite。

- [ ] **Step 1: 先增加真实 server fixture，禁止 API 全 mock。**

fixture 必须启动测试 Go server、临时 SQLite、确定性 runtime fixture，并只 mock 外部 `pi --mode rpc` 输入；浏览器请求必须真实经过 `/prompt`、`/state`、`/events-v2`。

- [ ] **Step 2: 添加完整事件序列 E2E。**

```ts
test("rebuilds the same session after prompt, steer, compact and reload", async ({ page }) => {
  await page.getByRole("textbox", { name: /prompt/i }).fill("hello");
  await page.getByRole("button", { name: /send/i }).click();
  await expect(page.getByText("hello")).toBeVisible();
  await expect(page.getByText(/context summarized/i)).toBeVisible();
  await page.reload();
  await expect(page.getByText("hello")).toBeVisible();
});
```

覆盖 prompt、tool output、steer、cancel、compact、rename、model、delete、后台通知和历史分页。

- [ ] **Step 3: 添加断线/重启/重复命令场景。**

验证 SSE 断开后按 applied seq 重连、server restart 后 snapshot + replay 一致、同一 `clientRequestId` 不重复 user/run 事件、多标签页不产生重复 transcript item。

- [ ] **Step 4: 运行验收套件并提交。**

Run: `go test ./...`

Run: `bun run test:e2e`

Commit: `test: cover session command and recovery end to end`

---

### Task 8: 删除旧会话流并完成架构验收

**Files:**
- Delete: `src/pi-app/sessions/legacy-session-event-adapter.ts`
- Delete or reduce: `internal/piweb/session_legacy_compat.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Modify: `internal/piweb/broker.go`
- Modify: `src/lib/api.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `docs/upstream-parity-audit.md`
- Modify: `docs/repository-audit.md`
- Modify: `scripts/check-session-single-stream.mjs`
- Modify: `internal/piweb/repository_contract_test.go`

**Interfaces:**
- Consumes: 已通过 Task 7 的真实 durable session flow。
- Produces: 生产代码只存在 event store + `/state` + `/events-v2` + reducer，旧 `/events` 和 Broker session history 不再存在。

- [ ] **Step 1: 先把静态检查切换为严格模式。**

`check-session-single-stream.mjs` 不再接受 `--compat`，以下命中必须失败：

```text
/api/sessions/:id/events
sessionEvents(
Broker.Publish(
appendMessageAndPublish(
legacy-session-event-adapter
```

- [ ] **Step 2: 删除旧路由和旧客户端 API。**

从 `internal/piweb/server.go` 删除 `GET /api/sessions/{sessionID}/events`；从 `src/lib/api.ts` 删除 `sessionEvents()`；从 `repository_contract_test.go` 删除旧路由契约并增加 `/events-v2` 唯一性断言。

- [ ] **Step 3: 删除 Broker 会话事实状态。**

删除 Broker 的 session history/cursor/running 状态及 `ServeSession`；保留非会话用途前先确认 `rg -n "Broker|ServeSession|Publish" internal/piweb` 只剩允许的非会话代码，否则先迁移调用点，不直接删除。

- [ ] **Step 4: 删除 legacy frontend state。**

移除 `connectEvents`、`applyEvent`、`legacyEventContext`、旧 session cursor 和 `durableEvents` 分支；所有 session load/stream/recovery 只调用 durable API。

- [ ] **Step 5: 更新文档并运行最终门禁。**

文档将结论改为：会话域单事件流已完成，工作区/全局域不在本次范围。执行：

```bash
bun run typecheck
bun run lint
bun run test
bun run backend:test
go test ./...
bun run build
bun run check:embedded-assets
bun run check:session-stream
bun run test:e2e
git diff --check
```

- [ ] **Step 6: 只提交会话域变更。**

确认 `git status --short` 中已有 `static/` 生成物没有被本任务提交；提交：

`refactor: remove legacy session event stream`

---

## 实施顺序和检查点

1. Task 1：基线和测试护栏；确认当前缺口可复现。
2. Task 2：事件/投影模型；确认新事件可重放。
3. Task 3：后端写路径；确认所有命令只写 durable event store。
4. Task 4：后端读路径和兼容层；确认旧读接口不再是第二真源。
5. Task 5：前端主流；确认无 legacy fallback。
6. Task 6：删除 optimistic/REST 状态旁路；确认 reducer 是唯一事实状态。
7. Task 7：真实 E2E；确认刷新、重连、重启和多客户端一致。
8. Task 8：删除旧实现；严格静态门禁和全量验收。

每个检查点都必须满足“测试通过 + 事件序列可观察 + 没有新增旧写入”，否则停止后续迁移，不做跨阶段补丁。
