# Task 1 报告：事件序列基线和静态不变量

## 改动文件

- `internal/piweb/session_domain_integration_test.go`：新增 durable integration server、prompt receipt、state/session/event 读取、最终一致性等待及事件类型断言辅助接口；新增 command → event → projection 基线测试和确定性 runtime 事件夹具。
- `src/pi-app/sessions/session-domain-events.test.ts`：新增 reducer 契约测试，确认 receipt 不直接改变 transcript，只有 `user.message.created` 才产生事实状态。
- `scripts/check-session-single-stream.mjs`：新增生产代码静态扫描；默认/`--compat` 模式检查旧 Broker 写入、旧 append、旧订阅、旧 events 路由和 legacy adapter 引用；兼容模式仅跳过 adapter 文件本身。
- `package.json`：新增 `check:session-stream` script。
- `internal/piweb/repository_contract_test.go`：补充静态检查 script 和 `events-v2` 受保护路由契约断言。

未修改 `static/` 生成物。

## 测试命令及完整结果

### `go test ./internal/piweb -run 'TestSessionDomainPromptProducesOneDurableSequence|TestRepositoryContract' -count=1`

退出码：`1`

```text
2026/09/15 11:05:36 INFO request method=POST path=/api/sessions/8e7c-44ff/prompt duration=598.085µs
--- FAIL: TestSessionDomainPromptProducesOneDurableSequence (0.02s)
    session_domain_integration_test.go:218: accepted seq = 0, run.started seq = 2
FAIL
FAIL	github.com/Epsilondelta-ai/pi-web/internal/piweb	0.032s
FAIL
```

`TestRepositoryContract` 单独通过；组合命令因基线测试失败而退出码为 1。

### `bun run test -- src/pi-app/sessions/session-domain-events.test.ts`

退出码：`0`

```text
$ vitest run src/pi-app/sessions/session-domain-events.test.ts

 RUN  v4.1.6 /home/hyperion/projects/pi-web-offline/.worktrees/session-single-event-stream

 Test Files  1 passed (1)
      Tests  1 passed (1)
   Start at  11:05:36
   Duration  736ms (transform 54ms, setup 65ms, import 49ms, tests 4ms, environment 508ms)
```

### `bun run check:session-stream -- --compat`

退出码：`1`

```text
$ node scripts/check-session-single-stream.mjs --compat
Session single-stream check failed:
- internal/piweb/broker.go:745: legacy message append: _ = appendMessageAndPublish(store, b, sessionID, user)
- internal/piweb/broker.go:774: legacy message append: _ = appendMessageAndPublish(store, b, sessionID, msg)
- internal/piweb/broker.go:777: broker publish: b.Publish(sessionID, step.typeName, step.payload)
- internal/piweb/compaction.go:60: broker publish: broker.Publish(sessionID, "session.status", map[string]string{"status": "compacting"})
- internal/piweb/compaction.go:83: broker publish: broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
- internal/piweb/compaction.go:84: broker publish: broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})
- internal/piweb/compaction.go:93: broker publish: broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
- internal/piweb/compaction.go:94: broker publish: broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})
- internal/piweb/compaction.go:104: legacy message append: if err := appendMessageAndPublish(store, broker, sessionID, message); err != nil {
- internal/piweb/compaction.go:108: broker publish: broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
- internal/piweb/compaction.go:109: broker publish: broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})
- internal/piweb/compaction.go:115: broker publish: broker.Publish(sessionID, "session.status", map[string]string{
- internal/piweb/runner.go:159: legacy message append: if err := appendMessageAndPublish(store, broker, sessionID, user); err != nil {
- internal/piweb/runner.go:165: broker publish: broker.Publish(sessionID, "session.status", map[string]string{"status": "running"})
- internal/piweb/runner.go:200: broker publish: broker.Publish(sessionID, "tool.output", map[string]string{"tool": "pi", "chunk": line})
- internal/piweb/runner.go:240: broker publish: broker.Publish(sessionID, "error", map[string]string{"error": runErr.Error()})
- internal/piweb/runner.go:242: broker publish: broker.Publish(sessionID, "session.status", map[string]string{
- internal/piweb/runner_events.go:47: broker publish: broker.Publish(sessionID, "error", map[string]string{"error": event.Error, "command": event.Command})
- internal/piweb/runner_events.go:60: broker publish: broker.Publish(sessionID, "session.delta", map[string]string{"kind": "think", "delta": delta})
- internal/piweb/runner_events.go:62: broker publish: broker.Publish(sessionID, "session.delta", map[string]string{"kind": "pi", "delta": delta})
- internal/piweb/runner_events.go:79: legacy message append: if err := appendMessageAndPublish(store, broker, sessionID, msg); err != nil {
- internal/piweb/runner_events.go:80: broker publish: broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
- internal/piweb/runner_events.go:86: broker publish: broker.Publish(sessionID, "tool.started", Message{ID: event.ToolCallID, Kind: "tool", Tool: event.ToolName, Args: string(event.Args), Status: "running", CollapsedByDefault: true})
- internal/piweb/runner_events.go:89: broker publish: broker.Publish(sessionID, "tool.output", map[string]any{
- internal/piweb/runner_events.go:102: legacy message append: if err := appendMessageAndPublish(store, broker, sessionID, msg); err != nil {
- internal/piweb/runner_events.go:103: broker publish: broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
- internal/piweb/runner_events.go:151: broker publish: broker.Publish(sessionID, "error", map[string]string{"error": message})
- internal/piweb/runner_tail.go:23: legacy message append: _ = appendMessageAndPublish(store, broker, sessionID, msg)
- internal/piweb/runner_tail.go:37: legacy message append: _ = appendMessageAndPublish(store, broker, sessionID, msg)
- internal/piweb/server.go:250: legacy session events route: s.mux.HandleFunc("GET /api/sessions/{sessionID}/events", s.requireSessionAccess(s.sessionEvents))
- internal/piweb/server_session_handlers.go:110: broker publish: s.broker.Publish(sessionID, "session.renamed", session)
- internal/piweb/server_session_handlers.go:210: legacy message append: _ = appendMessageAndPublish(s.store, s.broker, sessionID, user)
- internal/piweb/server_session_handlers.go:217: broker publish: s.broker.Publish(r.PathValue("sessionID"), "session.status", map[string]string{"status": "cancelled"})
- internal/piweb/server_session_handlers.go:221: legacy session subscription: func (s *Server) sessionEvents(w http.ResponseWriter, r *http.Request) {
- internal/piweb/store_sessions.go:371: legacy message append: func appendMessageAndPublish(store *Store, broker *Broker, sessionID string, msg Message) error {
- internal/piweb/store_sessions.go:377: broker publish: broker.Publish(sessionID, eventTypeForMessage(msg), msg)
- src/pi-app/index.ts:14: legacy session event adapter: } from "./sessions/legacy-session-event-adapter";
- src/pi-app/index.ts:223: legacy session subscription: this.eventSource = sessionEvents(sessionId, {
- src/pi-app/messages/message-methods.ts:8: legacy session event adapter: import { createLegacyTranscriptState } from "../sessions/legacy-session-event-adapter";
- src/pi-app/status/toast-methods.ts:331: legacy session subscription: watch.source = sessionEvents(sessionId, {
- src/pi-app/workspace/workspace-bootstrap-methods.ts:12: legacy session event adapter: import { createLegacyTranscriptState } from "../sessions/legacy-session-event-adapter";
error: script "check:session-stream" exited with code 1
```

### `git diff --check`

退出码：`0`，无 whitespace 错误；提交后工作树干净。

## 风险 / 疑问

1. 当前 prompt HTTP receipt 没有返回 `acceptedSeq`，因此基线测试实际得到 `accepted seq = 0`、`run.started seq = 2`；后续命令 receipt/API 任务需补齐该契约，不能放宽断言。
2. `--compat` 静态检查仍按要求阻止现有业务 Broker/legacy 写入、旧订阅及 `/events` 路由；这是后续迁移任务必须消除的基线缺口。

## 提交 hash

`e6553ae5dde79e5ee97e6b7ef7d78972aff18768`

---

# Task 1 Fix Round 1 报告

## 修复内容

1. `src/pi-app/sessions/session-domain-events.test.ts` 改为通过 `dispatchCommandReceipt` 调用真实 `reduceSessionEvent` seam，使用 command receipt payload 验证 receipt dispatch 不改变 state；随后仍通过真实 `user.message.created` reducer 路径断言 transcript 产生变化，并修正 `viewGeneration` fixture 的 TypeScript 类型。
2. `internal/piweb/session_domain_integration_test.go` 在 prompt 前建立真实 `/events-v2` SSE 连接并消费 JSON event frames，断言 SSE 与 durable rows 的完整顺序、seq 对齐和连续性；随后通过 `/state` 断言 projection 的 status、seq、user/assistant items 及 committed 状态。`acceptedSeq` 断言保留并显式标注为 Task 3 API receipt 缺口。
3. `requireTypes`/`hasEventTypes` 改为 exact-length、exact-order 类型校验，另校验 seq 必须从 1 开始唯一连续，额外旧事件或 seq gap/duplicate 都会失败。SSE 与事件 store 共用同一个 wake hub，避免 fixture 因通知隔离而漏收 live events；session JSONL fixture 使用 `t.TempDir()` 隔离。
4. `scripts/check-session-single-stream.test.mjs` 新增 Node automated tests，覆盖违规代码、合法 durable code、`--compat` 仅 adapter 放行、adapter 内 Broker 写入仍拒绝，以及 `static/` 排除。脚本导出可测试扫描函数；`--compat` 不再整体跳过 adapter，而只放行 adapter 内 legacy 规则，Broker 写入仍扫描；旧 events route 改用不会误判 `events-v2` 的边界匹配。
5. `package.json` 增加 `test:session-stream-check`，便于自动运行静态检查测试。

未修改生产实现，未触碰 `static/`。

## 验证结果

### 通过

- `bun run typecheck`：退出码 0。
- `bun run lint`：退出码 0。
- `bun run test`：50 files / 533 tests passed。
- `bun run test -- src/pi-app/sessions/session-domain-events.test.ts`：1 file / 1 test passed。
- `bun run test:session-stream-check`：4 tests passed。
- `go test ./internal/piweb -run 'TestRepositoryContract|TestSessionEventsV2' -count=1`：通过。
- `git diff --check`：通过。

### 按要求保留的红测 / 基线缺口

- `go test ./internal/piweb -run TestSessionDomainPromptProducesOneDurableSequence -count=1`：失败于 `accepted seq = 0, run.started seq = 2 (API receipt must expose acceptedSeq)`；失败前已消费真实 SSE，并完成 durable event exact sequence、SSE 对齐、projection items/status 断言。该失败是当前 prompt HTTP response 未返回 `acceptedSeq`，应由后续 Task 3 修复，未放宽断言。
- `go test -race ./internal/piweb -run 'TestSessionDomainPromptProducesOneDurableSequence|TestRepositoryContract|TestSessionEventsV2' -count=1`：同一 `acceptedSeq` 红测，未见额外 race failure。
- `go test ./internal/piweb -count=1`：仅报告上述 Task 1 acceptedSeq 红测，其他 package tests 未报告失败。
- `bun run test:coverage`：50 files / 533 tests passed，但命令按现有全局 100% coverage threshold 非零退出；当前全仓 coverage 为 statements 83.67%、branches 86.11%、functions 86.38%、lines 86%。这不是本轮改动引入的失败，未修改 threshold 或补无关覆盖。
- `bun run test:coverage -- src/pi-app/sessions/session-domain-events.test.ts`：目标测试通过，但同样因全局 threshold 非零；本轮使用普通 targeted test 与全套 `bun run test` 验证功能。
- `bun run check:session-stream -- --compat`：按设计非零；现有 `internal/piweb` 仍含 legacy Broker/append/subscription/route 写入，现有 `src/pi-app` 仍有 legacy adapter 使用。这些是后续迁移基线缺口，未通过放宽静态检查掩盖。
- `bun run check:session-stream`：同样按设计非零，且不会因 `--compat` 隐藏非 adapter 违规。

## Commit

已提交 fix commit；commit message 使用 Conventional Commits，并包含 `Co-authored-by: JuunAI <juunai.ai@gmail.com>`。
