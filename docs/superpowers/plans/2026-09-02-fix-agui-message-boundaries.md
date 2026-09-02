# AG-UI Assistant Message Boundaries Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让实时 AG-UI transcript 按 Pi RPC 的 assistant `message_end` 分段，消除最终总结拼接本轮历史说明的问题。

**Architecture:** Pi RPC 的 delta 继续用于实时预览，`message_end` 转换出的消息作为 canonical event 同时写入 Store 和发布到 Broker。AG-UI 适配器复用现有 `session.message` 分支关闭当前文本流；前端复用现有 streaming-row 替换逻辑，不新增字符串级去重。

**Tech Stack:** Go、Pi RPC JSONL、AG-UI Go SDK、`@ag-ui/client`、TypeScript、Vitest。

**Spec:** `docs/superpowers/specs/2026-09-02-agui-message-boundary-design.md`

## Global Constraints

- 不修改 `static/` 现有未提交生成物。
- 不修改 compaction、分页和虚拟 transcript 架构。
- delta 只用于预览；Store 与 Broker 必须接收同一条 canonical 最终消息。
- 保留 fallback choice、thinking、tool output mode、取消和错误处理。
- 修复提交必须包含且只包含：`Co-authored-by: JuunAI <juunai.ai.i@gmail.com>`。

---

### Task 1: 用跨层回归测试锁定 Pi RPC → Broker → AG-UI 消息边界

**Files:**
- Modify: `internal/piweb/agui_test.go`
- Modify: `internal/piweb/runner_test.go`

**Interfaces:**
- Consumes: `handlePiJSONEvent(line, broker, store, sessionID, state) bool`、`aguiStreamState.consume(event, stream) bool`。
- Produces: “每个 `message_end` 都发布 canonical message”及“tool 前结束当前文本消息”的测试契约。

- [ ] **Step 1: 增加 runner canonical-message 失败测试**

在 `internal/piweb/runner_test.go` 增加：

```go
func TestHandlePiJSONEventPublishesCanonicalMessageAfterTextDelta(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}

	handlePiJSONEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"draft"}}`, broker, store, "session", state)
	handlePiJSONEvent(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"draft"}]}}`, broker, store, "session", state)

	events := broker.Replay("session", 0)
	if len(events) != 2 || events[0].Type != "session.delta" || events[1].Type != "session.message" {
		t.Fatalf("expected delta followed by canonical message: %#v", events)
	}
	if got := events[1].Payload.(Message).Text; got != "draft" {
		t.Fatalf("unexpected canonical text: %q", got)
	}
}
```

- [ ] **Step 2: 增加完整 AG-UI 边界失败测试**

在 `internal/piweb/agui_test.go` 增加一个跨层测试：先通过 `handlePiJSONEvent` 生成“工具前说明 → tool → 最终总结”，再把 `broker.Replay()` 逐条交给 `aguiStreamState.consume()`，最后发布 idle。核心断言：

```go
if strings.Count(got, `"type":"TEXT_MESSAGE_START"`) != 2 ||
	strings.Count(got, `"type":"TEXT_MESSAGE_END"`) != 2 {
	t.Fatalf("expected two assistant text lifecycles:\n%s", got)
}
firstEnd := strings.Index(got, `"type":"TEXT_MESSAGE_END"`)
toolStart := strings.Index(got, `"type":"TOOL_CALL_START"`)
secondStart := strings.LastIndex(got, `"type":"TEXT_MESSAGE_START"`)
if firstEnd < 0 || toolStart < 0 || secondStart < 0 || !(firstEnd < toolStart && toolStart < secondStart) {
	t.Fatalf("assistant boundary must close before tool start:\n%s", got)
}
```

测试输入必须包含两个不同文本：`工具前说明`、`最终总结`，并断言二者各出现一次。

- [ ] **Step 3: 运行测试确认根因**

Run: `go test ./internal/piweb -run 'TestHandlePiJSONEventPublishesCanonicalMessageAfterTextDelta|TestAgui.*MessageBoundar' -count=1 -v`

Expected: FAIL；runner 只有 `session.delta`，AG-UI 只有一组 `TEXT_MESSAGE_START/END`，且同一 `messageId` 横跨 tool call。

---

### Task 2: 发布 canonical assistant 消息并删除跨 run 抑制状态

**Files:**
- Modify: `internal/piweb/runner_events.go:10-18, 60-90`
- Test: `internal/piweb/runner_test.go`
- Test: `internal/piweb/agui_test.go`

**Interfaces:**
- Consumes: Task 1 的失败契约。
- Produces: 每个有效 `message_end` 都按 `eventTypeForMessage(msg)` 发布；AG-UI 由已有 `session.message` 分支生成准确文本边界。

- [ ] **Step 1: 删除全 run 的 streamed flags**

将状态收敛为：

```go
type jsonStreamState struct {
	errorReported     bool
	finishedToolIDs   map[string]struct{}
	finishedToolNames map[string]int
}
```

并删除 `message_update` 中的：

```go
state.streamedThinking = true
state.streamedText = true
```

- [ ] **Step 2: 让 `message_end` 总是发布已持久化消息**

保留 user、running tool placeholder 和已完成 tool-result 的过滤；在 `_ = store.AppendMessage(sessionID, msg)` 后直接执行：

```go
broker.Publish(sessionID, eventTypeForMessage(msg), msg)
```

删除以下抑制分支：

```go
if msg.Kind == "pi" && state.streamedText && !containsFallbackChoice(msg.Text) {
	continue
}
if msg.Kind == "think" && state.streamedThinking {
	continue
}
```

若 `containsFallbackChoice()` 已无引用，同时删除该函数及 `strings` import；先用 `rg -n "containsFallbackChoice" internal/piweb` 确认。

- [ ] **Step 3: 运行定向测试确认变绿**

Run: `go test ./internal/piweb -run 'TestHandlePiJSONEvent|TestAgui' -count=1 -v`

Expected: PASS；新边界测试得到两组文本生命周期，既有 fallback choice、tool ID/output mode 和错误测试继续通过。

- [ ] **Step 4: 提交根因修复**

```bash
git add internal/piweb/runner_events.go internal/piweb/runner_test.go internal/piweb/agui_test.go
git commit -m "fix: preserve assistant message boundaries in AG-UI" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 3: 锁定前端 canonical-message 替换行为并完成验证

**Files:**
- Modify: `src/pi-app/messages/messages.test.ts`
- Verify: `src/pi-app/messages/message-methods.ts`（预期无需修改）
- Verify: `src/pi-app/index.ts`（预期无需修改）

**Interfaces:**
- Consumes: `applyEvent()`、`appendDelta()`、`appendMessage()` 的现有行为。
- Produces: canonical `session.message` 到达后移除 streaming row、只保留一条最终消息的回归保护。

- [ ] **Step 1: 增加实时与刷新一致性测试**

在 `src/pi-app/messages/messages.test.ts` 增加：

```ts
it("replaces each streamed segment with its canonical message around tools", async () => {
  const app = await connectPiApp();
  app.renderMessages([]);
  app.setMode("running");

  app.applyEvent({ type: "session.delta", payload: { kind: "pi", delta: "工具前说明" } });
  app.applyEvent({ type: "session.message", payload: { kind: "pi", text: "工具前说明" } });
  app.applyEvent({ type: "tool.started", payload: { id: "call-1", kind: "tool", tool: "bash", status: "running" } });
  app.applyEvent({ type: "tool.finished", payload: { id: "call-1", kind: "tool", tool: "bash", status: "ok" } });
  app.applyEvent({ type: "session.delta", payload: { kind: "pi", delta: "最终总结" } });
  app.applyEvent({ type: "session.message", payload: { kind: "pi", text: "最终总结" } });
  app.applyEvent({ type: "session.status", payload: { status: "idle" } });

  expect([...app.querySelectorAll(".term-inner .msg[data-kind='pi']")]
    .map((row) => row.dataset.rawText)).toEqual(["工具前说明", "最终总结"]);
  expect(app.querySelector(".streaming-tail .msg.streaming")).toBeNull();
});
```

- [ ] **Step 2: 运行前端定向测试**

Run: `bun run test -- src/pi-app/messages/messages.test.ts src/pi-app/core-events-coverage.test.ts`

Expected: PASS，无需生产前端改动；若失败，只修 canonical message 替换生命周期，不增加文本截断或 substring 去重。

- [ ] **Step 3: 运行完整验证**

Run: `bun run typecheck && bun run test && go test ./... && bun run build`

Expected: 全部退出码为 0；前端测试总数较当前 398 至少增加 1，Go 测试至少增加 2。

- [ ] **Step 4: 检查工作区并提交测试**

Run: `git diff --check && git status --short --branch`

Expected: `static/` 仅保留任务开始前已有生成物差异；无临时 `agui_boundary_debug_test.go`。

```bash
git add src/pi-app/messages/messages.test.ts
git commit -m "test: cover streamed assistant segment finalization" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```
