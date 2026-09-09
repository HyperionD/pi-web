# Pi Web 整体能力与架构改进实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在保留当前项目 Go + Astro + SQLite + 单二进制 + 多用户 ACL 定位的前提下，补齐 Pi 原生运行时、实时可靠性、安全边界、Agent 工作流和工程质量能力。

**Architecture:** 不迁移到 Next.js；在 Go 后端增加按 session 管理的持久 Pi RPC runtime、可靠事件订阅和命令路由。前端继续使用 Astro + `PiApp` Custom Element，逐步把运行状态、输入状态和 API 类型从隐式 DOM 状态抽离为可测试模块。

**Tech Stack:** Go 1.26, `net/http`, SQLite, external `pi --mode rpc`, Astro, React islands, TypeScript, Vitest, Go `testing`, Playwright, Vite `chrome107`；PTY 首期仅支持 Linux（优先麒麟 V10 SP3）。

**Spec:** `docs/superpowers/plans/2026-09-09-pi-web-overall-improvement.md`（本文同时作为设计基线）

## Global Constraints

- 保留 Go 后端、Astro 前端、SQLite、单二进制发布和现有多用户 workspace/session ACL。
- PTY 首期仅支持 Linux amd64/arm64，重点验证麒麟 V10 SP3；不实现 Windows/macOS PTY 适配。
- 前端生产构建目标固定为 `chrome107`；不得引入 Chrome 107 不支持的 authored CSS/JS API。
- 后端始终是文件、Shell、workspace、session 和资源执行的最终安全边界。
- 生产代码必须先有失败测试；每个任务都要执行定向测试、全量测试和独立提交。
- 不提交 `static/` 生成产物，除非任务明确要求更新嵌入资源；现有未提交 `static/` 变更不得被覆盖。
- 不把用户输入拼接为 Shell 命令；所有会话命令使用结构化 JSON RPC 和白名单校验。
- 所有异步 workspace/session 请求必须带 generation/token 校验，旧请求不得覆盖新状态。
- 保留当前安全文本渲染约束：外部路径、命令描述、错误和文件名使用 `textContent` 或 React escaping。
- 单次任务修改的源文件控制在 300 行以内；需要更大改动时先按职责拆分文件。
- 每个 commit message 必须包含 `Co-authored-by: JuunAI <juunai.ai.i@gmail.com>`。

---

## 目标状态

```text
Browser
  ├── Astro + PiApp
  ├── typed API client
  ├── explicit input/runtime state
  └── SSE cursor + resync
          │
          ▼
Go HTTP API
  ├── account/workspace/session ACL
  ├── command whitelist and project trust
  ├── reliable event broker
  ├── TerminalManager
  └── RuntimeManager
          │
          ▼
Per-session PiRuntime
  ├── one long-lived pi --mode rpc process
  ├── request ID → response waiter
  ├── event sequence and lifecycle state
  ├── idle eviction
  └── graceful shutdown
```

本计划是总体路线图，不是一个不可拆分的大版本。每个阶段都应产出可独立验证的提交；P0 阶段完成前不得以新增 UI 功能替代可靠性和安全修复。

## 文件映射

### P0 基础与可靠性

- Modify: `internal/piweb/broker.go` — 原子订阅、事件游标、断档通知和安全取消。
- Create: `internal/piweb/broker_types.go` — 订阅和事件同步类型。
- Modify: `internal/piweb/runner.go` — 适配 RuntimeManager，保留现有 Prompt/Steer/Compact API。
- Create: `internal/piweb/runtime_manager.go` — session runtime registry、启动锁、idle eviction、shutdown。
- Create: `internal/piweb/runtime_process.go` — Pi RPC 子进程 stdin/stdout/stderr 生命周期。
- Modify: `internal/piweb/server.go` — 注入 runtime manager、增加 shutdown、命令和信任路由。
- Modify: `server.go` — HTTP shutdown 时关闭内部 runtime、broker 和 store。
- Modify: `internal/piweb/*_test.go` — 并发、恢复、退出和错误契约。

### P1 Agent 输入与会话工作流

- Create: `src/pi-app/input/slash-command-types.ts` — 内置/动态命令类型和纯过滤函数。
- Modify: `src/pi-app/input/input-methods.ts` — 接入命令执行、流式状态和输入清理。
- Modify: `src/pi-app/index.ts` — 键盘、Escape、外部点击和 IME 行为。
- Modify: `src/App.astro` — 命令面板 loading、分组、计数和状态节点。
- Modify: `src/styles/parts/empty-shell.css` — Chrome 107 兼容的命令面板样式。
- Modify: `src/lib/api.ts` — typed session command、branch、search、export API。
- Create: `internal/piweb/session_commands.go` — 六个内置命令的白名单执行。
- Modify: `internal/piweb/server_session_handlers.go` — session command、stats、fork 响应。
- Modify: `internal/piweb/pi_sessions.go` — leaf/context/fork 数据辅助。
- Create: `internal/piweb/session_search.go` — 会话搜索索引/查询。
- Create: `internal/piweb/session_export.go` — 安全 HTML 导出。

### P1 安全与交互式工具

- Create: `internal/piweb/project_trust.go` — account/workspace trust 持久化和校验。
- Modify: `internal/piweb/account_middleware.go` — trust route 和执行前校验。
- Modify: `internal/piweb/commands.go` — 资源发现与资源执行分离。
- Modify: `cli.go`、`README.md` — localhost 默认值和远程访问警告。
- Create: `internal/piweb/terminal_manager.go` — Linux PTY registry、写入、resize、退出和 offset。
- Create: `internal/piweb/terminal_process_linux.go` — Linux Unix98 PTY adapter。
- Modify: `go.mod`、`go.sum` — 添加 `github.com/creack/pty` 直接依赖。
- Modify: `internal/piweb/server.go` — terminal API。
- Create: `src/components/TerminalPanel.tsx` — 交互式终端 Tab。
- Modify: `src/lib/api.ts` — terminal client 和 SSE cursor。

### P2 工作区、资源和质量

- Create: `internal/piweb/worktrees.go`、`internal/piweb/server_worktree_handlers.go` — worktree 生命周期。
- Create: `src/components/BranchNavigator.tsx` — 会话分支导航。
- Create: `src/components/SessionSearch.tsx` — 会话搜索。
- Create: `src/components/FileViewer.tsx` — 富文件预览。
- Create: `src/components/ModelsConfig.tsx`、`src/components/SkillsConfig.tsx`、`src/components/PluginsConfig.tsx` — Pi 资源控制面。
- Modify: `tsconfig.json` — 分阶段开启 strict。
- Modify: `src/lib/api.ts`、`src/pi-app/input/input-methods.ts`、`src/pi-app/sessions/session-methods.ts`、`src/pi-app/workspace/workspace-bootstrap-methods.ts` — 为核心边界补充显式类型。
- Modify: `.github/workflows/frontend.yml` — lint、typecheck、unit、build、E2E 分层。
- Create: `e2e/run.mjs`、`e2e/slash-commands.mjs`、`e2e/stream-recovery.mjs`、`e2e/account-isolation.mjs`、`e2e/file-workspace.mjs` — Playwright 浏览器链路测试。

---

## Phase 0：基线、契约与发布安全

### Task 1: 固化当前基线并建立质量门槛

**Files:**
- Modify: `.github/workflows/frontend.yml`
- Modify: `.github/workflows/backend.yml`
- Modify: `tsconfig.json`
- Test: `src/browser-compatibility.test.ts`
- Create: `internal/piweb/repository_contract_test.go`

**Interfaces:**
- Produces: CI 顺序 `lint → typecheck → frontend unit → backend unit → build`。
- Produces: `repository_contract_test.go` 验证默认 host、API 路由、Chrome 107 target 和生成资源提交策略。

- [ ] **Step 1: 写 CI 契约失败测试**

在 `src/browser-compatibility.test.ts` 增加断言：`astroConfig.vite.build.target === "chrome107"`，并断言所有 authored CSS 不含 `color-mix(`、`100dvh`。在 `internal/piweb/repository_contract_test.go` 增加 HTTP route 表断言，确保所有受保护 route 继续经过 `requireWorkspaceAccess` 或 `requireSessionAccess`。

- [ ] **Step 2: 运行测试确认基线**

Run:

```bash
bunx vitest run src/browser-compatibility.test.ts
 go test ./internal/piweb -run 'TestRepositoryContract|TestRoutes' -count=1
```

Expected: 当前已存在的 Chrome 107 与权限测试通过；若新增契约发现不一致，先记录实际行为再修改断言。

- [ ] **Step 3: 补 CI 检查命令**

在 frontend workflow 中按以下顺序执行：

```yaml
- run: bun run lint
- run: bun run typecheck
- run: bun run test:coverage
- run: bun run build
```

backend workflow 保留 `go test ./...` 和 Go coverage 门槛。

- [ ] **Step 4: 提交基线**

```bash
git add .github/workflows/frontend.yml .github/workflows/backend.yml tsconfig.json src/browser-compatibility.test.ts internal/piweb/repository_contract_test.go
git commit -m "chore: establish repository quality gates" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

## Phase 1：实时可靠性和进程生命周期

### Task 2: 修复 Broker 的订阅竞态和静默丢事件

**Files:**
- Create: `internal/piweb/broker_types.go`
- Modify: `internal/piweb/broker.go`
- Modify: `internal/piweb/broker_test.go`

**Interfaces:**

```go
type EventSubscription struct {
    Events <-chan Event
    Replay []Event
    Close  func()
}

func (b *Broker) SubscribeFrom(sessionID string, after uint64) EventSubscription
```

- `SubscribeFrom` 在同一把锁内注册订阅并复制 `after` 之后的历史，消除 Replay/Subscribe 间隙。
- `Close` 删除订阅但不关闭被 `Publish` 持有的 channel；连接通过 request context 退出，避免并发 `send on closed channel`。
- channel 满时发布 `broker_gap`，携带 `sessionID`、`after` 和最新事件 ID；不能静默丢失。
- 关键事件 `session.message`、`session.status`、`tool.started`、`tool.finished`、`error` 不得在正常订阅中被静默丢弃。

- [ ] **Step 1: 写失败测试**

增加以下测试：

```go
func TestSubscribeFromDoesNotMissEventBetweenReplayAndLive(t *testing.T)
func TestBrokerGapIsObservableWhenSubscriberIsSlow(t *testing.T)
func TestClosingSubscriptionConcurrentlyWithPublishDoesNotPanic(t *testing.T)
```

测试使用 `go test -race`，并断言第一项收到连续事件 ID，第二项收到 `broker_gap`，第三项完成 100 次并发 publish/close 不 panic。

- [ ] **Step 2: 确认测试按预期失败**

```bash
go test -race ./internal/piweb -run 'TestSubscribeFrom|TestBrokerGap|TestClosingSubscription' -count=1
```

Expected: 当前无 `SubscribeFrom` 或 gap 事件，测试失败。

- [ ] **Step 3: 实现原子订阅和 gap 事件**

`ServeSession` 改为先调用 `SubscribeFrom`，再写 `subscription.Replay`，最后监听 `subscription.Events`。`Publish` 保持全局事件 ID，并在缓冲区满时产生可观测 gap；不得通过关闭 channel 通知客户端。

- [ ] **Step 4: 运行定向和竞态测试**

```bash
go test -race ./internal/piweb -run 'Test(Broker|SubscribeFrom|ClosingSubscription)' -count=1
```

Expected: PASS，无 data race、panic 或静默失败。

- [ ] **Step 5: 提交**

```bash
git add internal/piweb/broker.go internal/piweb/broker_types.go internal/piweb/broker_test.go
git commit -m "fix: make session event delivery loss-aware" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 3: 引入按 session 的持久 Pi runtime

**Files:**
- Create: `internal/piweb/runtime_manager.go`
- Create: `internal/piweb/runtime_process.go`
- Modify: `internal/piweb/runner.go`
- Modify: `internal/piweb/server.go`
- Create: `internal/piweb/runtime_manager_test.go`
- Modify: `internal/piweb/runner_test.go`

**Interfaces:**

```go
type RPCCommand struct {
    ID      string
    Type    string
    Payload json.RawMessage
}

type RPCResponse struct {
    ID      string
    Type    string
    Command string
    Success bool
    Data    json.RawMessage
    Error   string
}

type PiEvent struct {
    Type string
    Raw  json.RawMessage
}

type PiRuntime interface {
    Send(ctx context.Context, command RPCCommand) (RPCResponse, error)
    Events() <-chan PiEvent
    IsRunning() bool
    Close(ctx context.Context) error
}

type RuntimeManager struct {
    // registry is keyed by session ID and guarded internally.
}

func (m *RuntimeManager) Ensure(ctx context.Context, sessionID string) (PiRuntime, error)
func (m *RuntimeManager) CloseSession(ctx context.Context, sessionID string) error
func (m *RuntimeManager) Shutdown(ctx context.Context) error
```

- 同一 session 的并发 `Ensure` 共享一个启动 Promise/lock。
- runtime 使用现有 `pi --session <file> --mode rpc --append-system-prompt ...` 参数。
- `Runner.StartPiPrompt`, `Steer`, `Compact` 通过 runtime 发送结构化 RPC，不再每次 prompt 启动新进程。
- runtime 空闲 10 分钟回收；有活动事件、正在执行工具或未完成命令时不得回收。
- `Server.Shutdown` 必须先停止接收新命令，再关闭 runtime、broker 和 store。

- [ ] **Step 1: 写失败测试**

增加：

```go
func TestRuntimeManagerReusesProcessForSameSession(t *testing.T)
func TestRuntimeManagerSharesConcurrentStartup(t *testing.T)
func TestRuntimeManagerClosesAllSessions(t *testing.T)
func TestRunnerSendsSecondPromptToExistingRuntime(t *testing.T)
```

使用 fake `PiRuntime` 或可注入 process factory；断言同一 session 只创建一次 process，不同 session 互不共享。

- [ ] **Step 2: 确认失败**

```bash
go test ./internal/piweb -run 'TestRuntimeManager|TestRunnerSendsSecondPrompt' -count=1
```

Expected: 当前 Runner 没有 runtime registry，测试失败。

- [ ] **Step 3: 实现最小 runtime registry**

先实现 process factory、session map、启动锁和 `Close`；再把 Runner 的 Prompt/Steer/Compact 改为调用 `Ensure`。保留现有 HTTP API，避免一次性修改前端。

- [ ] **Step 4: 实现 idle eviction 和 shutdown**

每个 runtime 保存 `lastUsedAt`、`activeRequests` 和 `running`。定时清理只回收 `activeRequests == 0` 且达到 idle timeout 的 runtime。`Shutdown(ctx)` 逐个关闭 stdin、等待子进程、超时后终止 process group。

- [ ] **Step 5: 运行测试**

```bash
go test -race ./internal/piweb -run 'Test(RuntimeManager|Runner)' -count=1
go test ./...
```

- [ ] **Step 6: 提交**

```bash
git add internal/piweb/runtime_manager.go internal/piweb/runtime_process.go internal/piweb/runner.go internal/piweb/server.go internal/piweb/runtime_manager_test.go internal/piweb/runner_test.go
git commit -m "feat: add persistent per-session pi runtimes" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

### Task 4: 增加 SSE 状态对账和断档恢复

**Files:**
- Modify: `internal/piweb/broker.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Modify: `src/lib/api.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `src/pi-app/core-events.test.ts`
- Create: `src/pi-app/sessions/session-recovery.ts`
- Create: `internal/piweb/broker_recovery_test.go`
- Create: `src/pi-app/sessions/session-recovery.test.ts`

**Interfaces:**

```ts
type SessionEventEnvelope = {
  id: number;
  type: string;
  sessionId: string;
  payload: unknown;
};

export function shouldResyncAfterGap(event: SessionEventEnvelope): boolean;
```

- `broker_gap`、连接初始状态和 session status 变化触发 `GET /api/sessions/{id}` 重新加载。
- 重新加载后从最新事件 ID 建立 SSE，避免重复渲染用户消息和工具卡片。
- 页面刷新时先获取 session snapshot，再连接 `Last-Event-ID`。

- [ ] **Step 1: 写前后端失败测试**

后端断言 gap 后能返回完整 session snapshot；前端断言收到 `broker_gap` 后调用 session reload，而不是把 gap 文本显示给用户。

- [ ] **Step 2: 运行失败测试**

```bash
bunx vitest run src/pi-app/sessions/session-recovery.test.ts
go test ./internal/piweb -run 'TestBrokerRecovery' -count=1
```

- [ ] **Step 3: 实现重同步协议**

在 SSE payload 中保留事件 ID；前端维护每 session 的 `lastEventID`，对 gap 执行 `loadSession`，并以 snapshot 替换 transient streaming state。

- [ ] **Step 4: 验证断线场景**

```bash
bunx vitest run src/pi-app/sessions/session-recovery.test.ts src/pi-app/core-events.test.ts
go test -race ./internal/piweb -run 'Test(Broker|Session)' -count=1
```

- [ ] **Step 5: 提交**

```bash
git add internal/piweb/broker.go internal/piweb/server_session_handlers.go internal/piweb/broker_recovery_test.go src/lib/api.ts src/pi-app/index.ts src/pi-app/core-events.test.ts src/pi-app/sessions/session-recovery.ts src/pi-app/sessions/session-recovery.test.ts
git commit -m "feat: reconcile sessions after event gaps" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

## Phase 2：输入框、内置命令和 Pi 命令一致性

### Task 5: 实现六个内置斜杠命令的后端白名单路由

**Files:**
- Create: `internal/piweb/session_commands.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Modify: `internal/piweb/runner.go`
- Modify: `src/lib/api.ts`
- Create: `internal/piweb/session_commands_test.go`
- Create: `src/lib/session-commands.test.ts`

**Interfaces:**

```go
type SessionCommandRequest struct {
    Name string `json:"name"`
    Args string `json:"args,omitempty"`
}

type SessionStats struct {
    MessageCount int     `json:"messageCount"`
    TokenCount   int     `json:"tokenCount,omitempty"`
    Cost         float64 `json:"cost,omitempty"`
}

type SessionCommandResult struct {
    Handled   bool   `json:"handled"`
    Message   string `json:"message,omitempty"`
    Error     string `json:"error,omitempty"`
    NewSessionID string `json:"newSessionId,omitempty"`
    AssistantText string `json:"assistantText,omitempty"`
    Stats     *SessionStats `json:"stats,omitempty"`
}

func (s *Server) sessionCommand(w http.ResponseWriter, r *http.Request)
```

允许命令只有：`compact`、`reload`、`name`、`session`、`copy`、`clone`。未知命令返回 `handled:false`；禁止把任意 JSON 字段直接转发为 RPC。

- `compact`: 调用已有 compact runtime，支持 custom instructions。
- `reload`: reload resources、tools、commands 和 models。
- `name`: 要求非空且长度不超过 120 个 Unicode 字符。
- `session`: 返回当前 session stats。
- `copy`: 返回最后一条 assistant text，由浏览器写入 clipboard。
- `clone`: 仅在未运行且 session 有可保存内容时调用 Pi clone，返回新 session ID。

- [ ] **Step 1: 写失败测试**

测试未知命令、空 name、运行中 clone、compact 参数、copy 空结果和成功 clone；所有非 session owner 请求必须返回 404/403 的既有权限语义。

- [ ] **Step 2: 确认失败**

```bash
go test ./internal/piweb -run 'TestSessionCommand' -count=1
bunx vitest run src/lib/session-commands.test.ts
```

- [ ] **Step 3: 实现白名单 handler 和 API client**

增加 `POST /api/sessions/{sessionID}/command`，handler 使用 `requireSessionAccess`。前端增加：

```ts
export function runSessionCommand(sessionId: string, name: string, args = "") {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/command`, {
    method: "POST",
    body: JSON.stringify({ name, args }),
  });
}
```

- [ ] **Step 4: 运行测试和权限测试**

```bash
go test ./internal/piweb -run 'Test(SessionCommand|AccountIsolation)' -count=1
bunx vitest run src/lib/session-commands.test.ts src/pi-app/status/context-usage.test.ts
```

- [ ] **Step 5: 提交**

```bash
git add internal/piweb/session_commands.go internal/piweb/server.go internal/piweb/server_session_handlers.go internal/piweb/runner.go internal/piweb/session_commands_test.go src/lib/api.ts src/lib/session-commands.test.ts
git commit -m "feat: expose validated session commands" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

### Task 6: 重做 slash palette 交互并保持 Chrome 107

**Files:**
- Create: `src/pi-app/input/slash-command-types.ts`
- Modify: `src/pi-app/input/input-methods.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `src/App.astro`
- Modify: `src/styles/parts/empty-shell.css`
- Modify: `src/pi-app/input/controls.test.ts`
- Modify: `src/pi-app/core-events.test.ts`
- Create: `src/pi-app/input/slash-command-types.test.ts`

**Interfaces:**

```ts
export type BuiltinSlashCommand = {
  name: "compact" | "reload" | "name" | "session" | "copy" | "clone";
  description: string;
  source: "builtin";
  availableWhileStreaming?: boolean;
};

export function extractSlashQuery(value: string): string | null;
export function filterSlashCommands(commands: SlashCommand[], query: string): SlashCommand[];
export function isExactBuiltinCommand(value: string, command: SlashCommand): boolean;
```

- 只有 `/` 后尚未出现空白字符时打开补全；输入空格后关闭。
- 支持精确匹配、前缀匹配、描述匹配和稳定排序。
- 支持 builtin/extension/prompt/skill 分组、计数、loading、空状态。
- 支持 ↑↓←→、Tab、Enter、Escape、鼠标 hover/click。
- Enter 对精确内置命令直接执行；其他命令填入 `/name `。
- 流式中仅允许 `/session`、`/copy` 直接执行；其他 slash prompt 使用现有 steer/follow-up 语义。
- 不使用 `ResizeObserver`、`AbortSignal.timeout`、`Array.prototype.at` 等不必要的 Chrome 107 风险 API；样式不使用 `color-mix`、`100dvh`。

- [ ] **Step 1: 写失败测试**

覆盖：

```ts
it("closes the slash menu after a command argument starts", ...)
it("filters exact names before descriptions", ...)
it("uses Tab to apply the selected command", ...)
it("uses Escape to close without clearing input", ...)
it("executes exact builtin commands on Enter", ...)
it("does not intercept IME Enter or arrow keys", ...)
```

- [ ] **Step 2: 确认失败**

```bash
bunx vitest run src/pi-app/input/slash-command-types.test.ts src/pi-app/input/controls.test.ts src/pi-app/core-events.test.ts
```

Expected: 当前不支持 Tab/Escape/四方向/内置命令直达，测试失败。

- [ ] **Step 3: 实现纯查询函数**

将 query 提取、过滤、排序和 exact match 放在 `slash-command-types.ts`，避免把业务逻辑继续堆进 `input-methods.ts`。

- [ ] **Step 4: 接入 DOM 事件和命令执行**

`inputMethods` 调用 `runSessionCommand`；成功后清理输入并显示 toast，`copy` 使用 `navigator.clipboard.writeText`，失败保留输入并显示错误。动态命令仍使用普通 prompt 提交。

- [ ] **Step 5: 更新命令面板和 CSS**

Astro 只提供稳定容器；运行时使用 `createElement` + `textContent` 渲染动态命令。使用 `max-height: calc(100vh - ...)` 和 `scrollIntoView({ block: "nearest" })`，不使用 Chrome 107 不支持的 authored CSS。

- [ ] **Step 6: 运行兼容性和全量前端测试**

```bash
bunx vitest run src/pi-app/input/slash-command-types.test.ts src/pi-app/input/controls.test.ts src/pi-app/core-events.test.ts src/browser-compatibility.test.ts
bun run typecheck
bun run build
```

- [ ] **Step 7: 提交**

```bash
git add src/pi-app/input/slash-command-types.ts src/pi-app/input/slash-command-types.test.ts src/pi-app/input/input-methods.ts src/pi-app/index.ts src/App.astro src/styles/parts/empty-shell.css src/pi-app/input/controls.test.ts src/pi-app/core-events.test.ts
git commit -m "feat: add reliable slash command palette" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

## Phase 3：安全边界和运行环境

### Task 7: 增加 Project Trust 和远程暴露保护

**Files:**
- Create: `internal/piweb/project_trust.go`
- Create: `internal/piweb/project_trust_test.go`
- Modify: `internal/piweb/web_db.go`
- Modify: `internal/piweb/account_middleware.go`
- Modify: `internal/piweb/commands.go`
- Modify: `internal/piweb/runner.go`
- Modify: `cli.go`
- Modify: `README.md`
- Create: `server_host_test.go`

**Interfaces:**

```go
func (s *Store) IsWorkspaceTrusted(accountID, workspaceID string) (bool, error)
func (s *Store) SetWorkspaceTrust(accountID, workspaceID string, trusted bool) error
func (s *Server) workspaceTrust(w http.ResponseWriter, r *http.Request)
```

- trust 以 account + workspace 为键，默认 false。
- 命令发现可以返回资源元数据，但未信任 workspace 时不得加载项目 extension/skill 执行资源。
- 现有 workspace/session ACL 继续先于 trust 检查。
- CLI 默认 host 改为 `127.0.0.1`；显式绑定非 loopback 时日志提示必须使用 HTTPS reverse proxy 和强密码。
- Shell 继续由服务进程执行，但增加请求超时、最大输出字节和可审计的 actor/session/workspace 字段。

- [ ] **Step 1: 写失败测试**

测试默认不信任、授权后允许、撤销后拒绝、跨账号互不共享 trust、远程 host 警告和 Shell 超时/输出限制。

- [ ] **Step 2: 确认失败**

```bash
go test ./internal/piweb -run 'Test(ProjectTrust|ShellLimit)' -count=1
go test . -run 'TestServerDefaultHost' -count=1
```

- [ ] **Step 3: 实现 SQLite trust 表和 API**

使用启动迁移创建 `workspace_trust(account_id, workspace_id, trusted, updated_at, PRIMARY KEY(account_id, workspace_id))`；所有 SQL 使用参数绑定。

- [ ] **Step 4: 接入 Pi 资源加载和 Shell 限制**

Runner 在建立 runtime 前检查 trust；Shell 使用 `context.WithTimeout`、有限读取器和明确的超限错误，不改变现有路径 ACL。

- [ ] **Step 5: 验证并提交**

```bash
go test -race ./internal/piweb -run 'Test(ProjectTrust|AccountIsolation|Shell)' -count=1
go test ./...
git add internal/piweb/project_trust.go internal/piweb/project_trust_test.go internal/piweb/web_db.go internal/piweb/account_middleware.go internal/piweb/commands.go internal/piweb/runner.go cli.go README.md
git commit -m "feat: add project trust and remote execution guards" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

## Phase 4：会话和工作区高级能力

### Task 8: 增加会话 branch、fork、搜索和导出

**Files:**
- Modify: `internal/piweb/pi_sessions.go`
- Create: `internal/piweb/session_search.go`
- Create: `internal/piweb/session_export.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Create: `src/components/BranchNavigator.tsx`
- Create: `src/components/SessionSearch.tsx`
- Modify: `src/pi-app/sessions/session-methods.ts`
- Modify: `src/lib/api.ts`
- Create: `internal/piweb/session_branch_test.go`
- Create: `src/components/BranchNavigator.test.tsx`

**Interfaces:**

```go
type SessionContextPage struct {
    Messages []Message `json:"messages"`
    Cursor   string    `json:"cursor,omitempty"`
    HasMore  bool      `json:"hasMore"`
    LeafID   string    `json:"leafId,omitempty"`
}

type SessionSearchResult struct {
    Session Session `json:"session"`
    Snippet string  `json:"snippet,omitempty"`
}

func (s *Store) SessionContext(sessionID, leafID string, limit int) (SessionContextPage, error)
func (s *Store) ForkSession(sessionID, entryID string) (Session, error)
func (m *WorkspaceAccess) ForkSession(actor *Account, sessionID, entryID string) (Session, error)
func (s *Store) SearchSessions(actor *Account, query string, limit int) ([]SessionSearchResult, error)
func (s *Store) ExportSession(sessionID string, leafID string) ([]byte, error)
```

- `leafID` 只影响当前 session 内 branch，不改变原始 JSONL 历史。
- fork 创建独立 session 文件，并通过 `WorkspaceAccess` 记录新 session owner。
- 搜索必须经过 actor ACL，不得通过全文索引泄露其他用户 session。
- 导出 HTML 只使用安全渲染器，不能把原始 Markdown/HTML 直接拼进模板。

- [ ] **Step 1: 写 Store/API 失败测试**

覆盖 root leaf、分支 leaf、compaction 边界、fork 后 owner、跨用户搜索和导出中的 HTML escaping。

- [ ] **Step 2: 确认失败**

```bash
go test ./internal/piweb -run 'Test(SessionContext|ForkSession|SearchSessions|ExportSession)' -count=1
```

- [ ] **Step 3: 实现后端 context/fork/search/export**

先复用现有 JSONL parser 和 session pagination，再增加 leaf chain；所有新 route 复用 `requireSessionAccess` 或 actor-aware search。

- [ ] **Step 4: 实现前端导航和搜索**

BranchNavigator 只消费 typed context 数据；切换 leaf 时保留滚动位置，旧请求通过 generation 丢弃。SessionSearch 只展示当前用户可见 session。

- [ ] **Step 5: 运行测试并提交**

```bash
go test ./internal/piweb -run 'Test(Session|AccountIsolation)' -count=1
bunx vitest run src/components/BranchNavigator.test.tsx src/pi-app/sessions
bun run build
git add internal/piweb src/components src/pi-app/sessions src/lib/api.ts
git commit -m "feat: add session branches search and export" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

### Task 9: 增加持久 PTY 终端

**Files:**
- Create: `internal/piweb/terminal_manager.go`
- Create: `internal/piweb/terminal_process_linux.go`
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/piweb/server.go`
- Create: `internal/piweb/terminal_manager_test.go`
- Create: `src/components/TerminalPanel.tsx`
- Modify: `src/lib/api.ts`
- Create: `src/components/TerminalPanel.test.tsx`

**Interfaces:**

```go
type Terminal struct {
    ID          string `json:"id"`
    ActorID     string `json:"actorId"`
    WorkspaceID string `json:"workspaceId"`
    CWD         string `json:"cwd"`
}

type TerminalEvent struct {
    ID   uint64 `json:"id"`
    Type string `json:"type"`
    Data []byte `json:"data"`
}

type TerminalManager interface {
    Start(ctx context.Context, actorID, workspaceID string) (Terminal, error)
    Write(id string, data []byte) error
    Resize(id string, cols, rows uint16) error
    Events(id string, after uint64) (<-chan TerminalEvent, func(), error)
    Stop(id string) error
    Shutdown(ctx context.Context) error
}
```

- 每个 terminal 绑定 actor、workspace 和 cwd。
- API 支持 start、write、resize、events、stop；所有 route 校验 actor ACL。
- 事件带 offset；慢客户端收到 gap 后请求当前 terminal snapshot。
- 终端进程退出、HTTP shutdown、session logout 都必须释放资源。
- Linux adapter 使用 Unix98 PTY、`setsid`、controlling terminal 和 `TIOCSWINSZ`；不在公共文件中调用平台专属 syscall。
- 首期不增加 Windows/macOS PTY 文件；非 Linux 构建保留现有一次性 Shell 能力或明确返回 unsupported。

- [ ] **Step 1: 写失败测试**

测试 terminal ACL、连续写入、resize 参数边界、退出事件、断线 offset 恢复和 shutdown 清理。

- [ ] **Step 2: 确认失败**

```bash
go test ./internal/piweb -run 'TestTerminalManager' -count=1
bunx vitest run src/components/TerminalPanel.test.tsx
```

- [ ] **Step 3: 实现 manager 和 API**

先完成 Linux adapter 和 fake adapter 测试；使用现有 SSE 风格 API，不引入新的 websocket 依赖。Linux adapter 使用 `github.com/creack/pty` 的 Unix98 实现完成，依赖编译进二进制，不要求目标机安装额外库。

- [ ] **Step 4: 验证并提交**

```bash
go test -race ./internal/piweb -run 'TestTerminalManager' -count=1
go test ./...
bun run typecheck
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/pi-web-linux-amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/pi-web-linux-arm64 .
git add go.mod go.sum internal/piweb src/components/TerminalPanel.tsx src/components/TerminalPanel.test.tsx src/lib/api.ts
git commit -m "feat: add reconnectable linux terminals" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

### Task 10: 增加 Git worktree 和富文件工作台

**Files:**
- Create: `internal/piweb/worktrees.go`
- Create: `internal/piweb/server_worktree_handlers.go`
- Create: `internal/piweb/worktrees_test.go`
- Modify: `internal/piweb/server.go`
- Modify: `src/components/WorkspaceFileTree.tsx`
- Create: `src/components/FileViewer.tsx`
- Create: `src/components/FileViewer.test.tsx`
- Modify: `src/pi-app/editor/file-preview-methods.ts`
- Modify: `src/lib/api.ts`

**Interfaces:**

```go
type Worktree struct {
    ID          string `json:"id"`
    Path        string `json:"path"`
    Branch      string `json:"branch"`
    ProjectRoot string `json:"projectRoot"`
    Dirty       bool   `json:"dirty"`
}

type CreateWorktreeRequest struct {
    WorkspaceID string `json:"workspaceId"`
    Branch      string `json:"branch"`
}

func (m *WorkspaceAccess) ListWorktrees(actor *Account, workspaceID string) ([]Worktree, error)
func (m *WorkspaceAccess) CreateWorktree(actor *Account, req CreateWorktreeRequest) (Worktree, error)
func (m *WorkspaceAccess) RemoveWorktree(actor *Account, id string, force bool) error
```

- worktree 创建、删除和切换必须复用当前 workspace path security 和 ACL。
- dirty worktree 删除首次返回 409 `{dirty:true}`，只有显式 `force=true` 才能继续。
- 文件 viewer 先补 PDF/DOCX/媒体的安全预览，再补自动刷新；二进制文件不得误走文本编辑器。
- 保留当前上传大小、符号链接、路径 traversal 和 overwrite 测试。

- [ ] **Step 1: 写后端失败测试**

测试同仓库 worktree identity、重复 branch、dirty 删除 409、跨用户拒绝、worktree 路径越界和文件类型选择。

- [ ] **Step 2: 确认失败**

```bash
go test ./internal/piweb -run 'Test(Worktree|File)' -count=1
bunx vitest run src/components/FileViewer.test.tsx
```

- [ ] **Step 3: 实现 API 和前端工作流**

先完成后端 path/ACL 边界，再接入 sidebar/workspace picker；FileViewer 通过 typed file metadata 选择 markdown/text/image/document/media renderer。

- [ ] **Step 4: 验证并提交**

```bash
go test -race ./internal/piweb -run 'Test(Worktree|File|AccountIsolation)' -count=1
bun run typecheck
bun run build
git add internal/piweb src/components src/pi-app/editor src/lib/api.ts
git commit -m "feat: add worktrees and richer file workspace" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

## Phase 5：Pi 资源控制面

### Task 11: 补齐 Models、Skills、Plugins 和 Subagents 控制面

**Files:**
- Create: `internal/piweb/resource_handlers.go`
- Create: `internal/piweb/resource_handlers_test.go`
- Modify: `internal/piweb/commands.go`
- Modify: `internal/piweb/server.go`
- Create: `src/components/ModelsConfig.tsx`
- Create: `src/components/SkillsConfig.tsx`
- Create: `src/components/PluginsConfig.tsx`
- Create: `src/components/AgentsConfig.tsx`
- Modify: `src/components/SettingsDialog.astro`
- Modify: `src/lib/api.ts`

**Interfaces:**

```go
func (s *Server) resources(w http.ResponseWriter, r *http.Request)
func (s *Server) updateResource(w http.ResponseWriter, r *http.Request)
func (s *Server) testModel(w http.ResponseWriter, r *http.Request)
```

- 所有资源读取和执行都经过 Project Trust、workspace ACL 和 actor policy。
- 模型凭据只返回 provider 状态，不返回原始 key。
- Skill/Plugin 安装必须使用固定参数数组，不接受任意 Shell 字符串。
- reload 后必须刷新 tools、commands、models 和 UI 状态。
- 前端保留简体中文文案，并为英文/繁中保留 key。

- [ ] **Step 1: 写失败测试**

测试 provider key 脱敏、project skill trust、插件包名校验、reload 资源刷新和跨账号拒绝。

- [ ] **Step 2: 确认失败**

```bash
go test ./internal/piweb -run 'Test(Resource|Model|Skill|Plugin)' -count=1
```

- [ ] **Step 3: 实现 typed resource API**

先实现只读列表和状态，再实现 enable/disable/test；安装和更新必须使用 `exec.CommandContext` 的参数数组、超时和输出上限。

- [ ] **Step 4: 实现设置 UI**

组件只消费 typed API response；loading、error、success 状态必须显式渲染，不能通过静默失败改变 UI。

- [ ] **Step 5: 验证并提交**

```bash
go test ./...
bun run typecheck
bunx vitest run src/components
bun run build
git add internal/piweb src/components src/lib/api.ts src/components/SettingsDialog.astro
git commit -m "feat: add pi resource control panels" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

## Phase 6：前端状态、浏览器和端到端质量

### Task 12: 分阶段恢复 TypeScript strict

**Files:**
- Modify: `tsconfig.json`
- Create: `src/pi-app/types.ts`
- Modify: `src/lib/api.ts`
- Modify: `src/pi-app/input/input-methods.ts`
- Modify: `src/pi-app/input/slash-command-types.ts`
- Modify: `src/pi-app/sessions/session-methods.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts`
- Test: `src/lib/api.test.ts`、`src/pi-app/input/controls.test.ts`、`src/pi-app/core-events.test.ts`、`src/pi-app/sessions/sessions.test.ts`、`src/pi-app/workspace/workspace-bootstrap-coverage.test.ts`

**Interfaces:**

```ts
export interface PiAppState {
  activeWorkspaceId: string;
  activeSessionId: string;
  runtime: "idle" | "running" | "compacting" | "error";
  eventCursor: number;
}
```

- [ ] **Step 1: 为 `src/lib` 打开 strict 子边界**

将 API response、request error 和 session command 类型显式化；先修复 `src/lib` 类型错误，不修改行为。

- [ ] **Step 2: 为 slash/input/session 状态增加类型**

去除输入相关 `@ts-nocheck`，为 `this` 依赖的 mixin 方法声明接口，保留运行时行为不变。

- [ ] **Step 3: 每个边界执行 typecheck**

```bash
bun run typecheck
bunx vitest run src/pi-app/input src/pi-app/sessions src/pi-app/workspace
```

- [ ] **Step 4: 提交**

```bash
git add tsconfig.json src/pi-app/types.ts src/lib src/pi-app/input src/pi-app/sessions src/pi-app/workspace
git commit -m "refactor: strengthen frontend state types" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

### Task 13: 增加 Playwright 浏览器链路和 Chrome 107 回归

**Files:**
- Create: `e2e/README.md`
- Create: `e2e/slash-commands.mjs`
- Create: `e2e/stream-recovery.mjs`
- Create: `e2e/account-isolation.mjs`
- Create: `e2e/file-workspace.mjs`
- Modify: `package.json`
- Modify: `.github/workflows/frontend.yml`
- Modify: `src/browser-compatibility.test.ts`

**Interfaces:**

```json
{
  "scripts": {
    "test:e2e": "node e2e/run.mjs"
  }
}
```

- [ ] **Step 1: 写失败 E2E**

覆盖：输入 `/`、Tab 选择、Escape 关闭、内置命令成功/失败、SSE 断线重同步、普通用户越权、文件上传下载、移动 viewport 和中文 UI。

- [ ] **Step 2: 确认测试基础设施失败**

```bash
bunx playwright install chromium
bun run test:e2e
```

Expected: 在实现 `e2e/run.mjs` 前，命令因测试入口缺失而失败；失败原因必须是测试基础设施缺失，不得是浏览器兼容性或 provider 连接错误。

- [ ] **Step 3: 安装 Playwright 并实现 deterministic mock server**

执行 `bun add --dev playwright` 更新 `package.json` 和 `bun.lock`。E2E 不依赖真实 provider；使用固定 session JSONL、固定 Pi RPC fixture 和显式 API base URL。所有选择器优先使用可见 role/label/text，只有无语义元素才使用 test id。

- [ ] **Step 4: 加入 CI**

CI 顺序固定为 install、lint、typecheck、unit、build、E2E；E2E 失败上传 Playwright trace 和 screenshot。Chrome 107 仍由 Vite build target 和 authored CSS compatibility test 双重约束。

- [ ] **Step 5: 验证并提交**

```bash
bun run lint
bun run typecheck
bun run test:coverage
bun run build
bun run test:e2e
git add e2e package.json .github/workflows/frontend.yml src/browser-compatibility.test.ts
git commit -m "test: add browser workflow coverage" -m "Co-authored-by: JuunAI <juunai.ai@gmail.com>"
```

---

## 发布验收标准

### P0 发布阻断项

- `go test -race ./...` 通过。
- 同一 session 的连续 Prompt 复用一个 Pi runtime。
- runtime、Broker、SQLite 在 HTTP shutdown 后无残留 goroutine/process。
- SSE Replay/Subscribe 不丢事件；慢客户端能收到 gap 并完成 resync。
- 所有 workspace/session/file/Shell route 继续经过 ACL。
- 未信任项目资源不能执行。
- 默认服务只监听 localhost，远程暴露有明确警告和强密码要求。

### P1 发布阻断项

- 六个内置 slash 命令有成功、失败、运行中和权限测试。
- slash palette 支持 `/` 查询、空格关闭、Tab、Enter、Escape、四方向和 IME。
- Chrome 107 compatibility tests、typecheck、lint、unit、build 全部通过。
- session branch/fork/search/export 至少有后端和浏览器链路测试。
- 持久终端的 write、resize、断线恢复和 stop 有测试。

### P2 发布条件

- Linux PTY 在麒麟 V10 SP3 实机完成 start/write/resize/断线恢复/stop 验收；`/dev/ptmx` 和 `/dev/pts` 不可用时返回明确错误。
- worktree、富文件 viewer、Models/Skills/Plugins/Subagents 控制面完成对应 API 和 UI 测试。
- Playwright 覆盖账户隔离、流式恢复、文件工作区和输入框。
- 覆盖率报告明确列出排除文件；不再把“排除核心文件后的 100%”作为全项目覆盖率表述。

## 执行顺序和提交规则

1. Task 1 → Task 2 → Task 3 → Task 4：这是可靠性基础，必须顺序执行。
2. Task 5 与 Task 6：Task 5 后端命令契约先完成，Task 6 再接入输入框。
3. Task 7 必须在持久 runtime 对外开放前完成；未完成时禁止开放远程高权限执行。
4. Task 8、9、10 可在 P0/P1 稳定后并行，但每项使用独立分支或独立工作树。
5. Task 11 依赖 Task 7 的 trust boundary 和 Task 3 的 runtime reload。
6. Task 12、13 在功能阶段稳定后执行；CI 通过前不允许宣称完成。
7. 每个 Task 单独 commit；合并前执行 `git status --short`，确认没有提交既有 `static/` 变更。

## 麒麟 V10 SP3 PTY 验收

在麒麟 V10 SP3 amd64/arm64 实机执行：

```bash
cat /etc/os-release
uname -m
command -v sh
test -c /dev/ptmx
test -d /dev/pts
grep -q ' devpts ' /proc/mounts
CGO_ENABLED=0 go build -o /tmp/pi-web .
if ldd /tmp/pi-web 2>&1 | grep -q 'not a dynamic executable'; then :; else
  echo "pi-web must not require dynamic runtime libraries"
  exit 1
fi
```

PTY 功能必须验证：交互输入、`stty size`、resize、Ctrl-C、长输出、进程退出、SSE 重连和服务 shutdown。目标机不安装 Node.js、npm、Bun、gcc 或额外 PTY 库。

## 最终验收命令

```bash
bun run lint
bun run typecheck
bun run test:coverage
bun run build
go test -race ./...
bun run test:e2e
```

所有命令退出码必须为 0；若环境没有 provider 或真实 Pi，E2E 必须使用固定 fixture，不得把外部服务可用性作为测试前提。
