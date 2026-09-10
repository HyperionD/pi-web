# Task 4 报告：增加 SSE 状态对账和断档恢复

## 状态

已完成并准备提交独立 commit。严格保留 Chrome 107、ACL、动态文本 escaping、mock/fallback 行为；未修改 `static/`。

## TDD 红绿证据

### RED

先写入后端 `broker_recovery_test.go`、前端 `session-recovery.test.ts` 和 `core-events.test.ts` 回归测试，再运行 brief 指定命令：

- `bunx vitest run src/pi-app/sessions/session-recovery.test.ts`
  - 失败：`./session-recovery` 尚不存在。
- `go test ./internal/piweb -run 'TestBrokerRecovery' -count=1`
  - 失败：`broker.LatestEventID undefined`。

并发缺陷测试 `TestRuntimeEntryPoisonWaitsForSteerExecutionGate` 在实现前验证为缺少 gate 协调的红测场景。

### GREEN

- `bunx vitest run src/pi-app/sessions/session-recovery.test.ts src/pi-app/core-events.test.ts`
  - PASS：20 tests。
- `go test ./internal/piweb -run 'Test(BrokerRecovery|SessionSnapshot|ServeSessionRecovers|RuntimeEntryPoison)' -count=1 -v`
  - PASS：4 tests。

## 实现

- `runtime_entry.poison()` 使用同一 `steerGate` 与 runner steer execution gate 协调，并保持 gate 到 runtime close 完成，避免 poison/Steer Send 交叠；新增 race/regression test。
- Broker 增加 session 最新事件 ID、`after` 查询恢复、历史截断 gap 事件；snapshot 返回 `latestEventId`；`Last-Event-ID` 仍优先于 `after`。
- 前端新增 `SessionEventEnvelope`、`shouldResyncAfterGap`、per-session event cursor；SSE 保留 broker event ID 并支持 `after`；gap、初始连接和 status 变化重新加载 snapshot；snapshot 替换 transcript/transient state；现有 session load token 防旧请求覆盖。
- 动态文本继续使用既有 `textContent`/escaping 路径，未新增 HTML 注入路径。

## 验证

- `bun run lint`：PASS。
- `bun run typecheck`：PASS。
- `bunx vitest run src/pi-app/sessions/session-recovery.test.ts`：PASS，3 tests。
- `bunx vitest run src/pi-app/sessions/session-recovery.test.ts src/pi-app/core-events.test.ts`：PASS，20 tests。
- `bunx vitest run`：PASS，43 files / 491 tests。
- `go test ./internal/piweb -run 'TestBrokerRecovery' -count=1`：PASS。
- `go test -race ./internal/piweb -run 'Test(Broker|Session)' -count=1`：PASS。
- `go test ./...`：PASS。
- `bun run build`：PASS；仅有既有 chunk size warning；未修改 `static/`。
- `git diff --check`：PASS。

## 文件

- 修改：`internal/piweb/broker.go`
- 修改：`internal/piweb/runtime_entry.go`
- 修改：`internal/piweb/server_session_handlers.go`
- 新增：`internal/piweb/broker_recovery_test.go`
- 修改：`src/lib/api.ts`
- 修改：`src/pi-app/index.ts`
- 修改：`src/pi-app/core-events.test.ts`
- 新增：`src/pi-app/sessions/session-recovery.ts`
- 新增：`src/pi-app/sessions/session-recovery.test.ts`
- 修改：`src/pi-app/workspace/workspace-bootstrap-methods.ts`（snapshot cursor/generation 复用）

## Concerns

- Astro build 保留既有大 chunk warning；不影响 exit 0。
- `broker_gap` 是 synthetic event（ID 0），前端不会推进 cursor；收到后以 snapshot 的最新事件 ID 重建 SSE。

## Review 修复轮次 1

- Store session mutation boundary 统一 AppendMessage+Publish；snapshot 与 latestEventId 在同一 boundary 内读取，新增真实并发 barrier 测试。
- 明确 `replay=false` 永不回放历史；`after`/Last-Event-ID 非法值返回 400；SSE event ID、snapshot/gap cursor 使用字符串，前端按十进制字符串比较，避免 uint64 精度损失。
- 增加真实 AG-UI 完成后 SSE 重连回归：只接收 live event，不 replay 已渲染事件；gap envelope 测试真实解析 event/id/data。
- loadSession 绑定 session view generation；gap/status/open resync single-flight，旧 callback/load 不得覆盖新 prompt 状态。
- runtime_entry 所有 Send 与 Close/poison 共用 gate；取消路径保留普通非协作 Send 的非阻塞行为，并补普通 Send/Close race。

### Review 轮次验证

- `go test ./...`：PASS
- `go test -race ./internal/piweb -run 'Test(Broker|Session|RuntimeEntry|RunnerCancel)' -count=1`：PASS
- `bunx vitest run`：PASS，43 files / 493 tests
- `bun run lint`：PASS
- `bun run typecheck`：PASS
- `bun run build`：PASS；仅既有 chunk size warning
- `git diff --check`：PASS

## 修复复审轮次 2

### 根因与修复

- Snapshot 读取现在持有 Broker `publishBarrier` 写锁，并与 Store `sessionMutationMu` 共同覆盖 durable session read 与 latest event cursor；普通 `Broker.Publish` 不能在 snapshot 内容之后抢先推进 cursor。
- Snapshot 后的首次/resync SSE 使用 `after=latestEventId` 且 `replay=true`；SubscribeFrom 的原子注册与重叠 replay 边界覆盖 snapshot 返回到 Subscribe 前发生的事件，不再使用 `replay=false` 丢事件。
- AG-UI 每消费一个 Broker event，后端发送 `piweb.broker-cursor` custom event，前端以十进制 string 更新 `sessionEventCursors`；完成、fallback、断线重连统一 replay 当前 cursor 之后的事件。
- 前端优先使用真实 SSE `lastEventId` 覆盖 envelope cursor；cursor 接受范围限制为 `0..18446744073709551615`，不将大于 `2^53` 的值转换为 number。
- 完善真实并发 snapshot→subscribe、普通 Publish、AG-UI cursor、真实 SSE envelope、旧 generation callback 与 uint64 边界回归测试；补齐 shutdown 后监听 goroutine join，消除全量 race 中暴露的依赖测试竞争。

### TDD 红绿证据

- RED：`TestSessionSnapshotDoesNotAdvanceCursorAcrossOrdinaryPublish` 首先确认普通 Publish 可在 snapshot hook 期间抢先完成；AG-UI cursor、snapshot replay、SSE `lastEventId`、旧 AG-UI generation 与 uint64 边界测试先因契约/行为缺失失败。
- GREEN：加入 publish barrier、snapshot-after replay、AG-UI cursor custom event、SSE ID 解析与 generation-safe cursor callback 后，定向 Go/Vitest 全部通过。

### 文件变更

- `internal/piweb/broker.go`、`server_session_handlers.go`：snapshot publish barrier 与 cursor 原子读取。
- `internal/piweb/agui.go`、`agui_test.go`：AG-UI Broker cursor custom event。
- `internal/piweb/broker_recovery_test.go`：snapshot/subscribe 并发和普通 Publish 并发恢复测试。
- `src/lib/api.ts`、`src/lib/api.test.ts`、`src/lib/agui-api.test.ts`：AG-UI cursor 与真实 SSE ID 解析。
- `src/pi-app/index.ts`、`src/pi-app/input/input-methods.ts`、`src/pi-app/workspace/workspace-bootstrap-methods.ts`：cursor 同步及安全 replay reconnect。
- `src/pi-app/sessions/session-recovery.ts`、`session-recovery.test.ts`：uint64 cursor 校验、generation 与恢复测试。
- `server.go`：shutdown 等待 listen goroutine 完成，避免测试/生命周期 race。

### 验证

- `go test ./...`：PASS
- `go test -race ./...`：PASS
- `go test -race ./internal/piweb -run 'Test(BrokerRecovery|SessionSnapshot|ServeSession|AguiStream)' -count=10`：PASS
- `bunx vitest run`：PASS，43 files / 498 tests
- `bunx vitest run src/pi-app/sessions/session-recovery.test.ts src/pi-app/input/input-methods-coverage.test.ts src/lib/agui-api.test.ts src/lib/api.test.ts`：PASS，4 files / 48 tests
- `bun run lint`：PASS
- `bun run typecheck`：PASS
- `bun run build`：PASS；仅既有 chunk size warning
- `git diff --check`：PASS

### Concerns

- Astro build 仍有既有 chunk size warning；不影响 exit 0。
- Broker snapshot barrier 是全局 publish barrier，会让 snapshot read 短暂串行化所有 session publish；这是为保证 cursor/content 一致性的最小安全边界。
- `replay=false` 仍保留给明确的 live-only background watcher；snapshot、resync、AG-UI reconnect 均不再使用它。
