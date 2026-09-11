# 离线产品完善 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 保留 Go 单二进制 + Astro/React + SQLite/JSONL 架构，补齐质量基线、会话生产力、Git worktree、PTY 终端、Pi 资源管理与离线体验，并形成可供用户手动测试的预发行版本。

**Architecture:** Go 服务继续作为部署边界，JSONL 继续保存 Pi 会话事实，SQLite 保存索引和控制面数据；不切换到 Next.js，也不把 Pi SDK 嵌入 Node。先稳定迁移、API 契约、runtime capability 和自动化测试，再按“会话 → worktree → terminal → Pi 资源 → 预览/PWA”增加纵向功能；每项新能力均通过 Go API 暴露，由前端 typed client 消费。

**Tech Stack:** Go 1.26、`net/http`、SQLite (`modernc.org/sqlite`)、Astro 6、React 19、TypeScript 6、Vitest 4、Playwright、Pi RPC、SSE；PTY 使用 `github.com/creack/pty`，双向终端连接使用 `github.com/coder/websocket`，PWA 使用 `@vite-pwa/astro`，PDF/DOCX 使用本地打包的 `pdfjs-dist`/`mammoth`。

**Spec:** `.pi/tasks/offline-product-improvement-roadmap.md#范围与非目标`

## Global Constraints

- 面向离线或隔离网络部署；运行时不得依赖 CDN、远程字体、npm registry、在线 skill/plugin 市场或第三方 Web 服务。
- 不在本计划中安排监听地址、TLS、认证加固、命令沙箱、ACL 加固等安全工作。
- 保留单 Go 可执行文件分发；目标机不要求安装 Node、Bun 或前端依赖。
- 保留 Pi JSONL 会话兼容性；不得把会话正文迁入 SQLite，也不得重写原始历史文件来实现搜索。
- SQLite schema 只允许 forward-only、事务化、可重复执行的迁移；每次迁移必须覆盖旧库升级测试。
- 新增和改动源文件要求 statement/branch/function/line 100% 覆盖；前端单元测试使用 `bun:test`/Vitest 逻辑测试，浏览器流程使用 Playwright。
- 外部依赖在构建期锁定到 lockfile/go.sum；测试不得访问真实 provider、互联网或用户真实目录。
- 保持当前中文默认 UI 和现有多语言机制，不另建第二套 i18n。
- 每个任务独立提交；commit message 使用 Conventional Commits，并且只包含一次 `Co-authored-by: JuunAI <juunai.ai.i@gmail.com>`。
- 不提交其他任务遗留的 `static/` 生成资产；仅在发行任务统一重建和提交/打包。

---

## 范围与非目标

### 纳入范围

1. 工程质量：可重复基线、SQLite migrations、API contract、runtime capabilities、TypeScript strict、coverage CI、Playwright E2E。
2. 会话能力：全文搜索、导出、会话树、从历史消息 fork；仅当 Pi RPC capability 探测成功时提供当前会话 leaf 导航。
3. Git 能力：worktree 创建、切换、移除、同仓库会话分组。
4. 开发体验：真正的 PTY Web Terminal、重连回放、尺寸同步、退出状态。
5. Pi 控制面：模型测试、会话工具预设、本地 Plugins/Skills 管理、Subagent 配置。
6. 离线体验：丰富文件预览、PWA app shell、诊断导出、版本化预发行流程。

### 不纳入范围

- 任何安全整改或远程公网部署方案。
- 在线插件/skill 搜索、在线安装市场或云同步。
- 将后端改写为 Node/Next.js，或把整个 `internal/piweb` 一次性拆成多个 Go package。
- 把 PWA 描述成“后端离线仍可工作”；PWA 只缓存应用壳，动态数据仍以 Go 服务为准。
- 在测试版前完成所有参考项目功能；测试版只承诺本计划“Alpha 发行门禁”中的范围。

## 现状与目标差距

| 领域 | 当前状态 | 本计划目标 |
| --- | --- | --- |
| 部署 | Astro 静态资源嵌入 Go binary | 保持；增加可重复预发行、校验和、诊断信息 |
| 数据 | `web_db.go` 启动时混合建表和 `ALTER TABLE` | `schema_migrations` + 顺序事务迁移 + 升级 fixture |
| API | `src/lib/api.ts` 多处隐式 `any`，路由靠源码测试 | OpenAPI 文档 + Go/TS contract tests + typed request |
| Runtime | `PiRuntime` seam 已存在 | capability negotiation，按 Pi 版本降级 UI |
| 类型 | `tsconfig.json` 设置 `strict:false` | `strict:true`，核心全局状态有明确类型 |
| 测试 | Go coverage CI；前端 CI 不跑 coverage；无 E2E | 前后端 coverage、race、Playwright 均为门禁 |
| 会话 | 创建/分页/重命名/删除/compact/steer/cancel | 搜索、导出、会话树、fork；capability 可用时增加 leaf 导航 |
| Git | status/history/commit/clone | worktree 生命周期和同仓库分组 |
| Shell | 一次性 `sh -lc` | 保留 shell quick command，并增加独立 PTY terminal |
| Pi 资源 | OAuth/API key、models/settings、quota | model test、tool preset、本地 plugin/skill、subagent 配置 |
| 离线 UI | 静态 UI，可离线部署 | app-shell PWA、PDF/DOCX/媒体预览、诊断包 |

## 目标模块与文件地图

```text
internal/piweb/
  db_migrations.go             顺序迁移执行器与 schema version
  api_contract.go              公共 DTO，不包含 handler 逻辑
  capabilities.go              应用与 Pi runtime 能力快照
  session_search.go            JSONL/metadata 搜索和 mtime cache
  session_export.go            HTML/Markdown/JSONL 导出
  session_branch.go            fork/navigate RPC 编排
  worktrees.go                 git worktree 服务
  terminal.go                  terminal manager 与 backlog
  terminal_pty_unix.go         Unix PTY adapter
  terminal_pty_unsupported.go  非支持平台显式能力降级
  pi_resources.go              plugin/skill/model/subagent 读模型
  pi_resource_mutations.go     本地安装、启停与原子配置写入
  diagnostics.go               版本/capability/迁移/runtime 摘要
src/lib/
  api.ts                       仅保留通用 request 和 re-export
  api-types.ts                 API DTO 唯一 TS 定义
  api-sessions.ts              会话 API
  api-workspaces.ts            workspace/git/worktree API
  api-runtime.ts               runtime/model/resource API
  terminal-client.ts           WebSocket terminal client
src/pi-app/
  sessions/session-search-methods.ts
  sessions/session-branch-methods.ts
  workspace/worktree-methods.ts
  workspace/resource-methods.ts
  terminal/terminal-methods.ts
  terminal/terminal-view.ts
src/components/
  SessionSearchDialog.astro
  SessionBranchDialog.astro
  WorktreePanel.astro
  TerminalPanel.astro
  ResourceManagerDialog.astro
docs/api/openapi.yaml          HTTP/SSE/WebSocket contract
e2e/                           Playwright fixtures/specs
```

这些文件按功能纵切，暂时仍属于 `package piweb`；只有当两个以上模块出现稳定、无环依赖接口后，再单独提出 Go 子包拆分计划。

## 依赖顺序与发行节点

```text
Task 1 基线
  ├─ Task 2 DB migration
  └─ Task 3 API contract + capabilities
          └─ Task 4 strict + coverage
                  └─ Task 5 Playwright harness
                          ├─ Task 6 搜索/导出
                          ├─ Task 7 fork/branch
                          └─ Task 8 worktree
                                  └─ Task 9 Alpha 发行

Task 9 Alpha
  ├─ Task 10 PTY
  ├─ Task 11 资源只读面
  └─ Task 12 本地资源修改
          └─ Task 13 subagent/tool preset/model test
                  └─ Task 14 preview/PWA/diagnostics
                          └─ Task 15 Beta/RC 收口
```

**最早可供用户手动测试的版本：完成 Task 1–8，并通过 Task 9 的全部发行门禁后，发行 `v1.10.0-alpha.1`。**

该 Alpha 明确包含：现有主链路、稳定迁移/契约/capability、严格类型和自动化门禁、会话搜索/导出/会话树/fork、Git worktree；`navigate_tree` 仅在 Pi RPC capability 可用时启用，不作为 Alpha 阻塞项；明确不包含 PTY、资源安装管理、PWA 和高级预览。

---

### Task 1: 冻结基线与统一验证入口

**Files:**
- Modify: `package.json`
- Modify: `.github/workflows/frontend.yml`
- Modify: `.github/workflows/backend.yml`
- Create: `scripts/verify-release.sh`
- Create: `e2e/not-configured.mjs`
- Create: `docs/testing.md`
- Test: `coverage_test.go`

**Interfaces:**
- Consumes: 当前 `lint`、`typecheck`、`test`、`backend:test:coverage`、`build` scripts。
- Produces: `bun run verify:release`，后续所有任务和发行 workflow 使用同一验证入口。

- [ ] **Step 1: 写失败的仓库契约测试**

在 `coverage_test.go` 增加断言：`package.json` 必须包含 `test:e2e`、`test:coverage`、`verify:release`；两个 CI workflow 必须调用对应 coverage/race 命令；`scripts/verify-release.sh` 必须存在且可执行。

- [ ] **Step 2: 验证测试先失败**

Run: `go test . -run TestReleaseVerificationContract -count=1`
Expected: FAIL，指出缺失 `verify:release` 或 `scripts/verify-release.sh`。

- [ ] **Step 3: 建立唯一验证脚本**

`scripts/verify-release.sh` 固定执行：

```bash
#!/usr/bin/env bash
set -euo pipefail
bun install --frozen-lockfile
bun run lint
bun run typecheck
bun run test:coverage
go test ./...
go test -race ./...
bun run backend:test:coverage
bun run build
bun run test:e2e
```

`package.json` 增加：

```json
{
  "scripts": {
    "test:e2e": "playwright test",
    "verify:release": "bash scripts/verify-release.sh"
  }
}
```

Task 5 加入 Playwright 前，创建 `e2e/not-configured.mjs`，输出“Playwright harness is not configured; complete Task 5”并以状态码 1 退出；`test:e2e` 暂时执行该文件，保证 Task 1–4 不会被误判为可发行。

- [ ] **Step 4: CI 改用同一组质量命令**

前端 CI 执行 lint、typecheck、`test:coverage`、build；后端 CI 执行 `go test ./...`、`go test -race ./...`、`backend:test:coverage`。`docs/testing.md` 记录本地命令、临时目录要求和 mock-only 原则。

- [ ] **Step 5: 验证基线并记录真实失败项**

Run: `bun run lint && bun run typecheck && bun run test && go test ./... && bun run build`
Expected: 全部 exit 0；若 `test:coverage` 失败，只把精确覆盖率记入 Task 4，不降低阈值。

- [ ] **Step 6: Commit**

```bash
git add package.json .github/workflows/frontend.yml .github/workflows/backend.yml scripts/verify-release.sh e2e/not-configured.mjs docs/testing.md coverage_test.go
git commit -m "chore: establish release verification baseline" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 2: SQLite 顺序迁移与升级恢复

**Files:**
- Create: `internal/piweb/db_migrations.go`
- Create: `internal/piweb/db_migrations_test.go`
- Modify: `internal/piweb/web_db.go`
- Create: `internal/piweb/testdata/db/v1.sql`
- Create: `internal/piweb/testdata/db/v2.sql`
- Modify: `docs/testing.md`

**Interfaces:**
- Consumes: `openWebDB(path string) (*sql.DB, error)`。
- Produces: `runMigrations(db *sql.DB) error`、`currentSchemaVersion(db *sql.DB) (int, error)`；后续 worktree/resource 表只能通过 migration 添加。

- [ ] **Step 1: 写迁移失败测试**

覆盖五种情形：空库升级到 latest、v1/v2 fixture 升级、重复执行无变化、故障 migration 整体回滚、`authorizationModel` 缺失时调用 `PrepareUserManagementDB`。最后一种必须断言现有 users、workspaces、user_workspaces、session_owners、app_settings 和 user_model_settings 全部保留。

- [ ] **Step 2: 验证测试先失败**

Run: `go test ./internal/piweb -run 'TestRunMigrations|TestOpenWebDBUpgrade' -count=1`
Expected: FAIL with `undefined: runMigrations`。

- [ ] **Step 3: 实现迁移执行器**

```go
type dbMigration struct {
    Version int
    Name    string
    Up      func(*sql.Tx) error
}

func runMigrations(db *sql.DB) error
func currentSchemaVersion(db *sql.DB) (int, error)
```

执行器先创建 `schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`，按 `Version` 升序逐个事务执行；迁移成功后在同一事务记录版本。把 `web_db.go` 现有建表、workspace ID、users column 迁移拆入版本 1–3；把 authorization model 升级作为版本 4，只写入版本标记并保留全部现有数据。

- [ ] **Step 4: 将 openWebDB 收敛为连接配置 + migrations**

`openWebDB` 只负责目录、连接、PRAGMA 和 `runMigrations`；删除散落的“忽略 duplicate column”分支。删除 `PrepareUserManagementDB` 中清空五张表的路径，把它改为调用 `openWebDB`/`runMigrations` 的兼容 wrapper；fixture 必须证明旧 user/workspace/grant/session owner/app settings/model settings 数据和数量均未丢失。

- [ ] **Step 5: 运行迁移和后端全量测试**

Run: `go test ./internal/piweb -run 'TestRunMigrations|TestOpenWebDBUpgrade' -count=1 && go test ./...`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/piweb/db_migrations.go internal/piweb/db_migrations_test.go internal/piweb/web_db.go internal/piweb/testdata/db docs/testing.md
git commit -m "refactor: add versioned database migrations" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 3: API 契约、Typed Client 与 Runtime Capabilities

**Files:**
- Create: `docs/api/openapi.yaml`
- Create: `internal/piweb/api_contract.go`
- Create: `internal/piweb/api_contract_test.go`
- Create: `internal/piweb/capabilities.go`
- Create: `internal/piweb/capabilities_test.go`
- Modify: `internal/piweb/runtime_process.go`
- Modify: `internal/piweb/server.go`
- Create: `src/lib/api-types.ts`
- Create: `src/lib/api-types.test.ts`
- Modify: `src/lib/api.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts`

**Interfaces:**
- Consumes: `PiRuntime.Send`、`GET /api/health`、当前 route set。
- Produces: `GET /api/capabilities` 与 `AppCapabilities`；后续 UI 只能根据 capability 显示 fork/worktree/terminal/resources。

- [ ] **Step 1: 写 capability 和 contract 失败测试**

Go 测试断言 route 存在、mock/真实 runtime 能力响应稳定、OpenAPI operationId 不重复；使用 fake runtime 分别返回成功、`unknown command`、超时和不可解析版本。TS 测试断言 capability 缺字段时应用默认 `false`，未知字段被忽略。

- [ ] **Step 2: 验证测试先失败**

Run: `go test ./internal/piweb -run 'TestCapabilities|TestOpenAPIContract' -count=1 && bunx vitest run src/lib/api-types.test.ts`
Expected: FAIL，指出 endpoint/type 不存在。

- [ ] **Step 3: 定义公共契约**

```go
type RuntimeCapabilities struct {
    Prompt, Steer, Compact, Models, Thinking, Fork, Navigate bool
}
type AppCapabilities struct {
    SchemaVersion int                 `json:"schemaVersion"`
    Runtime       RuntimeCapabilities `json:"runtime"`
    SessionSearch bool                `json:"sessionSearch"`
    SessionExport bool                `json:"sessionExport"`
    Worktrees     bool                `json:"worktrees"`
    Terminal      bool                `json:"terminal"`
    Resources     bool                `json:"resources"`
    PWA           bool                `json:"pwa"`
}
```

```ts
export type AppCapabilities = {
  schemaVersion: number;
  runtime: Record<"prompt" | "steer" | "compact" | "models" | "thinking" | "fork" | "navigate", boolean>;
  sessionSearch: boolean;
  sessionExport: boolean;
  worktrees: boolean;
  terminal: boolean;
  resources: boolean;
  pwa: boolean;
};
```

初始未实现能力必须返回 `false`，不能根据前端版本猜测。`RuntimeProcessFactory` 启动前执行一次 `pi --version` 并缓存 `PiRuntimeInfo { Version, Protocol:"jsonl-rpc" }`；真实 session 建立后依次发送非破坏性 `get_state`、`get_available_models`、`get_commands`、`get_tree`、`get_fork_messages`。`get_tree` 返回的当前 `leafId` 非空时，发送幂等的 `navigate_tree { targetId: leafId }` 探测导航；命令返回 `unknown command`、超时或异常结构时仅将对应能力置 false。fork 能力要求 `get_fork_messages` 成功，navigate 能力要求幂等探测成功；mock runtime 显式声明能力，不执行版本猜测。当前集成基线固定记录 `pi 0.84.4`，未知/不可解析版本只保留实际探测成功的能力。

- [ ] **Step 4: 拆分 typed request 边界**

把 `request<T>(path, options): Promise<T>` 设为唯一 fetch 入口；新增 DTO 时禁止裸 `any`。OpenAPI 记录现有 health/workspace/session/file/git/runtime 路由、SSE envelope 和统一 error body；contract test 校验 route method/path 与文档一致。

- [ ] **Step 5: 前端按 capability 降级**

bootstrap 首次加载 capability；请求失败时只保留当前稳定功能，不显示未来入口。测试 mock 老后端（404）和新后端两条路径。

- [ ] **Step 6: 验证并提交**

Run: `go test ./internal/piweb -run 'TestCapabilities|TestOpenAPIContract' -count=1 && bunx vitest run src/lib/api-types.test.ts src/lib/api.test.ts && bun run typecheck`
Expected: PASS。

```bash
git add docs/api/openapi.yaml internal/piweb/api_contract.go internal/piweb/api_contract_test.go internal/piweb/capabilities.go internal/piweb/capabilities_test.go internal/piweb/runtime_process.go internal/piweb/server.go src/lib/api-types.ts src/lib/api-types.test.ts src/lib/api.ts src/pi-app/workspace/workspace-bootstrap-methods.ts
git commit -m "feat: publish typed application capabilities" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 4: TypeScript Strict 与 100% Coverage 门禁

**Files:**
- Modify: `tsconfig.json`
- Modify: `src/types.d.ts`
- Modify: `src/lib/api.ts`
- Create: `src/lib/api-sessions.ts`
- Create: `src/lib/api-workspaces.ts`
- Create: `src/lib/api-runtime.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `src/pi-app/**/*.ts`
- Modify: `vitest.config.ts`
- Modify: `.github/workflows/frontend.yml`
- Test: `src/**/*.test.ts`
- Test: `src/**/*.test.tsx`

**Interfaces:**
- Consumes: Task 3 的 DTO 和 `request<T>`。
- Produces: `strict:true`；按领域拆分的 typed client；前端 coverage gate 真实执行。

- [ ] **Step 1: 打开 strict 并收集完整错误清单**

把 `tsconfig.json` 改为 `"strict": true`，运行 `bun run typecheck`，将错误按 `api → accounts → pi-app sessions → workspace → editor/input/status` 顺序处理；不得通过新增 `any`、`@ts-ignore` 或关闭子选项消错。

- [ ] **Step 2: 为 PiApp 状态定义类型**

在 `src/types.d.ts` 定义 `PiAppElement`、`SessionSummary`、`WorkspaceSummary`、`RuntimeStatus`、`SessionEventEnvelope`；DOM query 使用泛型并处理 `null`。catch 参数保持 `unknown`，使用 `errorMessage(error: unknown): string`。

- [ ] **Step 3: 拆分 typed API modules**

`src/lib/api.ts` 只保留 request、apiBase 和 re-export；sessions/workspaces/runtime 文件中的每个函数有输入和 Promise 返回类型。每拆一个模块先移动对应测试，再移动实现，避免大爆炸改动。

- [ ] **Step 4: 逐项移除无依据 coverage exclude**

先运行 `bun run test:coverage -- --reporter=verbose`；为当前被排除且包含行为的文件补逻辑测试，再从 exclude 删除。仅允许保留声明文件、测试 helper、纯常量聚合入口；每个保留项在 `vitest.config.ts` 同行注释说明不可执行原因。

- [ ] **Step 5: 验证 strict 与 coverage**

Run: `bun run lint && bun run typecheck && bun run test:coverage`
Expected: exit 0；statements/branches/functions/lines 均为 100%。

- [ ] **Step 6: Commit**

```bash
git add tsconfig.json src/types.d.ts src/lib src/pi-app vitest.config.ts .github/workflows/frontend.yml
git commit -m "refactor: enable strict frontend contracts" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 5: Playwright 离线 E2E Harness

**Files:**
- Modify: `package.json`
- Modify: `bun.lock`
- Create: `playwright.config.ts`
- Delete: `e2e/not-configured.mjs`
- Create: `e2e/fixtures/server.mjs`
- Create: `e2e/fixtures/account.mjs`
- Create: `e2e/fixtures/workspace.mjs`
- Create: `e2e/smoke.spec.ts`
- Create: `e2e/session-recovery.spec.ts`
- Create: `e2e/file-edit.spec.ts`
- Modify: `.github/workflows/frontend.yml`
- Modify: `scripts/verify-release.sh`
- Modify: `docs/testing.md`

**Interfaces:**
- Consumes: `go run . --mock`、`bun run dev`、Task 3 capability endpoint。
- Produces: 可复用 `testAccount`/`testWorkspace` fixture、随机本地端口的 Go mock backend 与 Astro dev server、`bun run test:e2e`；E2E 始终测试当前前端源码，不读取仓库中可能陈旧的 `static/`。

- [ ] **Step 1: 安装并锁定 Playwright**

Run: `bun add -d @playwright/test`
Expected: `package.json` 和 `bun.lock` 更新；不添加运行时 CDN。

- [ ] **Step 2: 写首个失败 smoke test**

```ts
test("initializes an account and receives a mock response", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("密码").fill(testAccount.password);
  await page.getByLabel("确认管理员密码").fill(testAccount.password);
  await page.getByRole("button", { name: "设置并登录" }).click();
  await page.getByRole("button", { name: /打开/ }).click();
  await page.getByLabel(/目录/).fill(testWorkspace.path);
  await page.getByRole("button", { name: /确认|打开/ }).click();
  await page.getByRole("textbox", { name: /消息/ }).fill("hello offline");
  await page.getByRole("button", { name: /发送/ }).click();
  await expect(page.getByText(/hello offline/)).toBeVisible();
});
```

fixture 为每个 test 建临时 HOME、Pi session dir、workspace 和 DB，启动随机端口的 Go `--mock` backend 与 Astro dev server；`PI_WEB_API_BASE`/Astro proxy 指向该 backend。账号 fixture 要么走上述首次设置 UI，要么通过同一 browser context 的公开 account API 建立 cookie，不允许绕过认证中间件。测试后依次终止两个进程并删除目录。

- [ ] **Step 3: 验证测试先失败**

Run: `bun run test:e2e -- e2e/smoke.spec.ts`
Expected: FAIL，直到 fixture 和可访问 label 完成。

- [ ] **Step 4: 完成最小 E2E 集**

覆盖：首次设置/登录、打开 workspace、创建 session、mock 流式响应、刷新恢复、SSE cursor 重连、文件读写、中文默认 UI。只使用 role/label/text；test id 仅用于无语义的 terminal canvas。

- [ ] **Step 5: CI 安装 Chromium 并运行 E2E**

CI 使用 `bunx playwright install --with-deps chromium`，再运行 `bun run test:e2e`；失败上传 trace，不上传用户目录。

- [ ] **Step 6: 验证并提交**

Run: `bun run test:e2e && bun run verify:release`
Expected: PASS，测试加载的是 Astro 当前源码且测试期间无外网请求。嵌入 `static/` 的最终二进制由 Task 9 archive smoke 单独验证。

```bash
git add package.json bun.lock playwright.config.ts e2e .github/workflows/frontend.yml scripts/verify-release.sh docs/testing.md
git commit -m "test: add offline browser acceptance suite" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 6: 会话全文搜索与导出

**Files:**
- Create: `internal/piweb/session_search.go`
- Create: `internal/piweb/session_search_test.go`
- Create: `internal/piweb/session_export.go`
- Create: `internal/piweb/session_export_test.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Modify: `docs/api/openapi.yaml`
- Modify: `src/lib/api-sessions.ts`
- Create: `src/components/SessionSearchDialog.astro`
- Create: `src/pi-app/sessions/session-search-methods.ts`
- Create: `src/pi-app/sessions/session-search-methods.test.ts`
- Modify: `src/App.astro`
- Modify: `src/pi-app/index.ts`
- Create: `e2e/session-search-export.spec.ts`

**Interfaces:**
- Produces:
  - `GET /api/sessions/search?q=<query>&workspaceID=<id>&limit=50&cursor=<cursor>`。
  - `GET /api/sessions/{sessionID}/export?format=html|markdown|jsonl`。
  - `SessionSearchResult { sessionID, workspaceID, title, snippet, matchedAt, entryID }`。
  - `SessionSearchPage { results: SessionSearchResult[], nextCursor: string | null }`；page cursor 只存在于 envelope，不放入单条结果。

- [ ] **Step 1: 写搜索/导出失败测试**

fixture 包含多 workspace、中文、tool output、compaction 和损坏 JSONL 行；断言只返回可解析匹配、snippet 高亮位置稳定、cursor 无重复、HTML 为自包含文件、JSONL 与原始字节一致。

- [ ] **Step 2: 验证测试先失败**

Run: `go test ./internal/piweb -run 'TestSearchSessions|TestExportSession' -count=1`
Expected: FAIL with undefined search/export functions。

- [ ] **Step 3: 实现 mtime 缓存搜索**

JSONL 仍为事实源；内存 cache key 使用 `path + size + modTime`，缓存 title、纯文本消息和 entry ID。搜索按 `matchedAt DESC, sessionID ASC` 排序并返回 opaque cursor；文件变化时只重建该 session cache。

- [ ] **Step 4: 实现三种导出**

HTML 使用现有 escape/render helper 生成内联 CSS 的只读 transcript；Markdown 保留角色、时间和 fenced tool output；JSONL 直接下载原始文件。导出错误使用统一 API error body。

- [ ] **Step 5: 完成搜索 UI 和 E2E**

侧栏增加“搜索会话”；支持 workspace filter、键盘上下选择、打开结果和加载更多。会话菜单增加三种导出。E2E 验证中文查询、打开结果、下载文件名和内容。

- [ ] **Step 6: 验证并提交**

Run: `go test ./internal/piweb -run 'TestSearchSessions|TestExportSession' -count=1 && bunx vitest run src/pi-app/sessions/session-search-methods.test.ts && bun run test:e2e -- e2e/session-search-export.spec.ts`
Expected: PASS。

```bash
git add internal/piweb/session_search.go internal/piweb/session_search_test.go internal/piweb/session_export.go internal/piweb/session_export_test.go internal/piweb/server.go internal/piweb/server_session_handlers.go docs/api/openapi.yaml src/lib/api-sessions.ts src/components/SessionSearchDialog.astro src/pi-app/sessions/session-search-methods.ts src/pi-app/sessions/session-search-methods.test.ts src/App.astro src/pi-app/index.ts e2e/session-search-export.spec.ts
git commit -m "feat: add session search and export" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 7: 会话树、Fork 与 Capability-gated Leaf 导航

**Files:**
- Create: `internal/piweb/session_branch.go`
- Create: `internal/piweb/session_branch_test.go`
- Modify: `internal/piweb/pi_sessions.go`
- Modify: `internal/piweb/runtime_manager.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Modify: `internal/piweb/capabilities.go`
- Modify: `docs/api/openapi.yaml`
- Modify: `src/lib/api-sessions.ts`
- Create: `src/components/SessionBranchDialog.astro`
- Create: `src/pi-app/sessions/session-branch-methods.ts`
- Create: `src/pi-app/sessions/session-branch-methods.test.ts`
- Modify: `src/pi-app/messages/message-methods.ts`
- Modify: `src/pi-app/sessions/session-methods.ts`
- Create: `e2e/session-branch.spec.ts`

**Interfaces:**
- Produces:
  - `GET /api/sessions/{id}/tree` → entries/leaves/currentLeaf。
  - `POST /api/sessions/{id}/fork` body `{ entryID: string }` → `{ session, workspaceID }`。
  - `POST /api/sessions/{id}/navigate` body `{ leafID: string }` → `{ currentLeaf, messages }`。
- Runtime capability `fork`/`navigate` 为 false 时，handler 返回 `501 capability unavailable`，UI 不展示入口。

- [ ] **Step 1: 写 JSONL 树和 runtime 编排失败测试**

覆盖线性会话、两个 sibling leaf、compaction 后 ancestry、fork 后 `parentSession`、原 session 不变、活跃 runtime fork 后 retire、navigate 后 transcript 与 leaf 一致。

- [ ] **Step 2: 验证测试先失败**

Run: `go test ./internal/piweb -run 'TestSessionTree|TestForkSession|TestNavigateSession' -count=1`
Expected: FAIL。

- [ ] **Step 3: 实现树模型与 capability probe**

```go
type SessionTreeEntry struct {
    ID, ParentID, Kind, Preview, Timestamp string
    Children []string
}
type SessionTree struct {
    Entries []SessionTreeEntry
    Leaves []string
    CurrentLeaf string
}
```

树优先使用 Pi RPC `get_tree`，fork 使用文档化 `fork { entryId }`；navigate 只在 Task 3 对当前 leaf 的幂等 `navigate_tree { targetId }` 探测成功时启用。任何 RPC 成功后 retire 旧 runtime、重新读取文件并刷新 Store cache；不手写活跃 session 历史。Pi 0.84.4 的公开 RPC 未承诺 `navigate_tree`，因此该版本可以交付树查看与 fork，但 UI 必须隐藏同 session leaf 切换并在 Alpha release notes 标记 capability unavailable。

- [ ] **Step 4: 实现两个清晰 UI 动作**

“从这里新建会话”调用 fork，创建独立 session；“编辑/继续此分支”调用 navigate，保持同一 session。Branch dialog 展示 ancestry 和 sibling，不把 Git branch 混入该组件。

- [ ] **Step 5: 加入回归和 E2E**

E2E 使用 mock runtime fixture 暴露 fork/navigate；验证 fork 后原会话消息不变、分支切换不重复消息、刷新后 current leaf 一致。

- [ ] **Step 6: 验证并提交**

Run: `go test ./internal/piweb -run 'TestSessionTree|TestForkSession|TestNavigateSession' -count=1 && bunx vitest run src/pi-app/sessions/session-branch-methods.test.ts && bun run test:e2e -- e2e/session-branch.spec.ts`
Expected: PASS。

```bash
git add internal/piweb/session_branch.go internal/piweb/session_branch_test.go internal/piweb/pi_sessions.go internal/piweb/runtime_manager.go internal/piweb/server.go internal/piweb/server_session_handlers.go internal/piweb/capabilities.go docs/api/openapi.yaml src/lib/api-sessions.ts src/components/SessionBranchDialog.astro src/pi-app/sessions/session-branch-methods.ts src/pi-app/sessions/session-branch-methods.test.ts src/pi-app/messages/message-methods.ts src/pi-app/sessions/session-methods.ts e2e/session-branch.spec.ts
git commit -m "feat: add session branching and forks" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 8: Git Worktree 生命周期与会话分组

**Files:**
- Create: `internal/piweb/worktrees.go`
- Create: `internal/piweb/worktrees_test.go`
- Modify: `internal/piweb/db_migrations.go`
- Modify: `internal/piweb/db_migrations_test.go`
- Modify: `internal/piweb/store_workspace.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/capabilities.go`
- Modify: `docs/api/openapi.yaml`
- Modify: `src/lib/api-workspaces.ts`
- Create: `src/components/WorktreePanel.astro`
- Create: `src/pi-app/workspace/worktree-methods.ts`
- Create: `src/pi-app/workspace/worktree-methods.test.ts`
- Modify: `src/pi-app/workspace/workspace-render-methods.ts`
- Create: `e2e/worktree.spec.ts`

**Interfaces:**
- Produces:
  - DB table `workspace_git_links(worktree_id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL UNIQUE, repository_id TEXT NOT NULL, main_workspace_id TEXT NOT NULL, git_common_dir TEXT NOT NULL, branch TEXT NOT NULL, created_at TEXT NOT NULL)`；`worktree_id` 固定等于已存在的 stable `workspace_id`。
  - `GET /api/workspaces/{id}/worktrees`。
  - `POST /api/workspaces/{id}/worktrees` body `{ branch: string, createBranch: boolean }`。
  - `DELETE /api/workspaces/{id}/worktrees/{worktreeID}?force=false`。
  - `Worktree { id, workspaceID, path, branch, head, main, dirty }`。

- [ ] **Step 1: 写 Git fixture 和迁移失败测试**

临时创建 bare origin、main checkout、已有/新 branch、dirty worktree；覆盖同仓库识别、branch 含 `/`、detached HEAD、移除 dirty 返回 `409 { dirty:true }`、删除后 session 回归 main group。

- [ ] **Step 2: 验证测试先失败**

Run: `go test ./internal/piweb -run 'TestWorktree|TestRepositoryGrouping' -count=1`
Expected: FAIL。

- [ ] **Step 3: 实现 repository identity 与 worktree service**

通过 `git rev-parse --git-common-dir` 和规范化绝对路径计算 `repository_id`；新 worktree 放在 `<repoRoot>-worktrees/<sanitized-branch>`。新 branch 使用 `git worktree add -b`，已有 branch 使用 `git worktree add`；命令输出进入结构化错误。

- [ ] **Step 4: 数据与 UI 分组**

migration 为每个可识别 Git workspace 写入 `workspace_git_links`；main checkout 的 `main_workspace_id=workspace_id`，linked checkout 保存 main workspace ID。sidebar 以 `repository_id` 分组，worktree 是组内可切换项。创建成功后先注册 workspace，再以其 stable workspace ID 写 link row；DELETE 用 path parameter 的 `worktree_id` 唯一映射 workspace/path。移除后删除 link row并隐藏 workspace，不删除 JSONL session；已移除路径 session 显示到 main repository 历史组。

- [ ] **Step 5: E2E 验收**

覆盖创建新 branch worktree、切换后创建 session、回 main、dirty 移除确认、强制移除、刷新后分组不丢失。

- [ ] **Step 6: 验证并提交**

Run: `go test ./internal/piweb -run 'TestWorktree|TestRepositoryGrouping' -count=1 && bunx vitest run src/pi-app/workspace/worktree-methods.test.ts && bun run test:e2e -- e2e/worktree.spec.ts`
Expected: PASS。

```bash
git add internal/piweb/worktrees.go internal/piweb/worktrees_test.go internal/piweb/db_migrations.go internal/piweb/db_migrations_test.go internal/piweb/store_workspace.go internal/piweb/server.go internal/piweb/capabilities.go docs/api/openapi.yaml src/lib/api-workspaces.ts src/components/WorktreePanel.astro src/pi-app/workspace/worktree-methods.ts src/pi-app/workspace/worktree-methods.test.ts src/pi-app/workspace/workspace-render-methods.ts e2e/worktree.spec.ts
git commit -m "feat: add git worktree workflows" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 9: 发行 `v1.10.0-alpha.1` 手动测试版

**Files:**
- Modify: `.github/workflows/release.yml`
- Create: `docs/releases/v1.10.0-alpha.1-test-plan.md`
- Create: `docs/releases/test-feedback-template.md`
- Modify: `README.md`
- Modify: `package.json`
- Modify: `bun.lock`

**Interfaces:**
- Consumes: Task 1–8 的全部验证命令和功能。
- Produces: GitHub prerelease、四个平台架构压缩包、SHA256SUMS、人工测试清单与反馈格式。

- [ ] **Step 1: 增加预发行 workflow 契约测试**

断言带 `-alpha.`/`-beta.`/`-rc.` 的 tag 生成 prerelease；四个 matrix build 只上传 workflow artifact；独立 aggregate job 下载全部 archive 后生成单一 `SHA256SUMS` 并创建 release。workflow 的 verify job 必须先执行 `verify:release`，build jobs 通过 `needs: verify` 等待，不能只编译。

- [ ] **Step 2: 编写人工测试清单**

`docs/releases/v1.10.0-alpha.1-test-plan.md` 必须逐项记录：环境、binary checksum、空目录首次启动、旧 `pi-web.db` 升级、登录、workspace、文件读写、mock/真实 Pi prompt、刷新/SSE 恢复、会话 search/export/tree/fork、capability 可用时的 leaf navigate、worktree 创建/切换/移除、重启恢复、日志和实际结果。

- [ ] **Step 3: 完成 prerelease workflow**

release matrix 保持 `linux/{amd64,arm64}`、`darwin/{amd64,arm64}`；版本号来自 tag。每个 matrix job 用 `actions/upload-artifact` 上传一个 archive；`aggregate-release` job 通过 `needs: build` 下载四个 archive、执行 `sha256sum pi-web_* > SHA256SUMS`，再用一次 `softprops/action-gh-release` 上传全部文件。tag 含连字符时设置 `prerelease: true` 并生成统一 release notes。

- [ ] **Step 4: 执行 Alpha 门禁**

Run:

```bash
bun run verify:release
git status --short
git diff --check
git log -1 --format='%h %s%n%b'
```

Expected: 所有验证 exit 0；除计划内版本/文档/构建产物外无未提交文件；最新 commit 只有一次要求的 co-author trailer。

- [ ] **Step 5: 安装包跨架构 smoke test**

从 release workflow 同样的命令生成 Linux amd64/arm64 archive，解压到空目录；amd64 在 native runner、arm64 在 native runner 或 QEMU runner 设置临时 HOME 后启动 binary，访问 `/api/health` 和 `/api/capabilities` 并执行 archive Playwright smoke。当前开发机 archive 额外执行一次同样流程；禁止直接测试仓库中的开发服务器代替 archive。

- [ ] **Step 6: 执行内部 Alpha 人工清单**

在至少一台 Linux amd64 或 arm64 真实设备上完整执行 `docs/releases/v1.10.0-alpha.1-test-plan.md`，每项填写 pass/fail/evidence；所有 fail 必须形成 issue，并且阻塞级问题修复后重跑受影响用例。未填写结果的清单不满足发行条件。

- [ ] **Step 7: 提交、打 tag、发布 prerelease**

```bash
git add .github/workflows/release.yml docs/releases README.md package.json bun.lock
git commit -m "chore: prepare v1.10.0 alpha testing" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
git tag -a v1.10.0-alpha.1 -m "v1.10.0-alpha.1"
git push origin HEAD
git push origin v1.10.0-alpha.1
gh release view v1.10.0-alpha.1 --json isPrerelease,assets,url
```

Expected: `isPrerelease=true`，包含 4 个 archives 和 `SHA256SUMS`。

#### Alpha 人工测试准入标准

必须同时满足：

- Task 1–8 已各自独立提交并通过任务内测试。
- `bun run verify:release` 在 clean checkout 通过。
- Linux amd64 与 Linux arm64 archive 至少各完成一次空 HOME smoke test。
- 旧数据库 fixture 升级后账号、workspace、session owner 和设置数量一致。
- mock 模式完整 E2E 通过；真实 Pi prompt 作为人工测试项，不作为 CI 的互联网依赖。
- 已知问题写入 release notes；PTY、资源管理、PWA/高级预览明确标记为后续版本。

**结论：只有完成 Task 1–8 且 Task 9 Step 1–6 全部通过后，才可以发行第一个用户手动测试版。**

### Task 10: PTY Web Terminal 与断线回放

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/piweb/terminal.go`
- Create: `internal/piweb/terminal_test.go`
- Create: `internal/piweb/terminal_pty_unix.go`
- Create: `internal/piweb/terminal_pty_unsupported.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/capabilities.go`
- Modify: `docs/api/openapi.yaml`
- Create: `src/lib/terminal-client.ts`
- Create: `src/lib/terminal-client.test.ts`
- Create: `src/components/TerminalPanel.astro`
- Create: `src/pi-app/terminal/terminal-methods.ts`
- Create: `src/pi-app/terminal/terminal-view.ts`
- Create: `src/pi-app/terminal/terminal-methods.test.ts`
- Create: `e2e/terminal.spec.ts`

**Interfaces:**
- Produces:
  - `POST /api/workspaces/{id}/terminals` → terminal metadata。
  - `GET /api/terminals/{id}/connect` WebSocket frames：`input`、`output`、`resize`、`snapshot`、`exit`。
  - `DELETE /api/terminals/{id}`。
  - 每 terminal 保存 128 KiB ring backlog，最后 client 断开 120 秒后清理。

- [ ] **Step 1: 添加依赖并写 manager 失败测试**

Run: `go get github.com/creack/pty github.com/coder/websocket`

测试 terminal 创建、唯一 ID、backlog offset、UTF-8 split、resize、exit code、重连 snapshot、120 秒租约清理、server shutdown 收割。

- [ ] **Step 2: 验证测试先失败**

Run: `go test ./internal/piweb -run 'TestTerminal' -count=1`
Expected: FAIL。

- [ ] **Step 3: 实现平台 adapter 和 manager**

Unix adapter 使用 PTY 启动用户 shell并设置 workspace cwd；unsupported adapter 返回 typed unsupported error，使 capability 为 false。Manager 不复用一次性 shell API；每个 terminal 独立生命周期和 ring buffer。

- [ ] **Step 4: 实现 WebSocket client/UI**

client 维护 `terminalID + offset`，重连先请求 snapshot 再接 live output；UI 支持 tabs、键盘输入、复制、清屏、resize observer、退出状态。终端静态资源全部嵌入 binary。

- [ ] **Step 5: E2E 和全量验证**

E2E 运行 `printf`、交互输入、resize、浏览器刷新后回放、exit；不依赖用户 shell profile 输出。

Run: `go test ./... && bun run test:coverage && bun run test:e2e -- e2e/terminal.spec.ts`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/piweb/terminal* internal/piweb/server.go internal/piweb/capabilities.go docs/api/openapi.yaml src/lib/terminal-client* src/components/TerminalPanel.astro src/pi-app/terminal e2e/terminal.spec.ts
git commit -m "feat: add reconnectable web terminal" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 11: Pi 资源只读目录

**Files:**
- Create: `internal/piweb/pi_resources.go`
- Create: `internal/piweb/pi_resources_test.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/capabilities.go`
- Modify: `docs/api/openapi.yaml`
- Modify: `src/lib/api-runtime.ts`
- Create: `src/components/ResourceManagerDialog.astro`
- Create: `src/pi-app/workspace/resource-methods.ts`
- Create: `src/pi-app/workspace/resource-methods.test.ts`

**Interfaces:**
- Produces:
  - `GET /api/workspaces/{id}/resources?kind=plugin|skill|agent|model`。
  - `PiResource { id, kind, name, scope, source, enabled, loaded, version, diagnostics[] }`。
- 资源来源仅限全局 Pi 目录、workspace `.pi`/`.agents` 和已配置本地 package；不调用在线搜索。

- [ ] **Step 1: 写资源发现失败测试**

固定 fixture 覆盖 global/project 同名覆盖、disabled package、损坏 frontmatter、缺失 path、local package version、subagent profile、model provider；断言顺序和 diagnostics 稳定。

- [ ] **Step 2: 验证测试先失败**

Run: `go test ./internal/piweb -run 'TestDiscoverPiResources' -count=1`
Expected: FAIL。

- [ ] **Step 3: 实现与 Pi runtime 一致的只读发现**

先从 runtime 请求可用资源；老 Pi 不支持时再解析 settings 和目录，并在 response 标记 `source:"fallback"`。不得因单个损坏资源导致整个列表失败。

- [ ] **Step 4: 实现只读 UI**

按 kind/scope/filter 展示名称、来源、版本、启用/加载状态和诊断；本 Task 不显示安装、删除、启停按钮。

- [ ] **Step 5: 验证并提交**

Run: `go test ./internal/piweb -run 'TestDiscoverPiResources' -count=1 && bunx vitest run src/pi-app/workspace/resource-methods.test.ts && bun run typecheck`
Expected: PASS。

```bash
git add internal/piweb/pi_resources.go internal/piweb/pi_resources_test.go internal/piweb/server.go internal/piweb/capabilities.go docs/api/openapi.yaml src/lib/api-runtime.ts src/components/ResourceManagerDialog.astro src/pi-app/workspace/resource-methods.ts src/pi-app/workspace/resource-methods.test.ts
git commit -m "feat: add offline pi resource catalog" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 12: Plugins/Skills 本地安装、启停与更新

**Files:**
- Create: `internal/piweb/pi_resource_mutations.go`
- Create: `internal/piweb/pi_resource_mutations_test.go`
- Modify: `internal/piweb/settings.go`
- Modify: `internal/piweb/server.go`
- Modify: `docs/api/openapi.yaml`
- Modify: `src/lib/api-runtime.ts`
- Modify: `src/components/ResourceManagerDialog.astro`
- Modify: `src/pi-app/workspace/resource-methods.ts`
- Modify: `src/pi-app/workspace/resource-methods.test.ts`
- Create: `e2e/resources.spec.ts`

**Interfaces:**
- Produces:
  - `POST /api/workspaces/{id}/resources/install` JSON body `{ kind, scope, source: { type:"serverPath", path } }`，复用服务端 folder picker 选择本机目录/`.tgz`/package cache。
  - `POST /api/workspaces/{id}/resources/install-upload?kind=<kind>&scope=<scope>` 接收浏览器 multipart `.tgz` 上传。
  - `PATCH /api/workspaces/{id}/resources/{resourceID}` body `{ enabled }`。
  - `POST /api/workspaces/{id}/resources/{resourceID}/update` 从该资源持久化的原始 server path 或本地缓存 archive 重新 staging；源不存在时返回明确错误且不改当前版本。
  - `DELETE /api/workspaces/{id}/resources/{resourceID}`。
  - 不接受 marketplace query，不执行在线 package resolution。

- [ ] **Step 1: 写本地 package fixture 和失败测试**

fixture 包含 plugin package、standalone skill、冲突版本、无效 manifest；覆盖 global/project scope、原子 settings 写入、runtime reload、删除后恢复默认资源集合。

- [ ] **Step 2: 验证测试先失败**

Run: `go test ./internal/piweb -run 'TestInstallLocalResource|TestToggleResource|TestRemoveResource' -count=1`
Expected: FAIL。

- [ ] **Step 3: 实现两阶段 mutation**

先验证并 staging 到目标目录，再原子更新 settings；任一步失败都删除 staging 并保留原配置。成功后 retire 受影响 workspace 的空闲 runtime；活跃 runtime 标记 `reloadRequired:true`，不在运行中热替换。

- [ ] **Step 4: 完成 UI/E2E**

UI 提供两种明确入口：服务端路径选择器安装目录/`.tgz`，或浏览器上传 `.tgz`；同时提供 scope、启停、从原始本地源更新、移除和 reload 状态。E2E 分别用 server path 与 multipart fixture 完成安装、启用、替换 fixture 版本后更新、重启服务后仍存在、删除。

- [ ] **Step 5: 验证并提交**

Run: `go test ./internal/piweb -run 'TestInstallLocalResource|TestToggleResource|TestRemoveResource' -count=1 && bun run test:e2e -- e2e/resources.spec.ts`
Expected: PASS。

```bash
git add internal/piweb/pi_resource_mutations.go internal/piweb/pi_resource_mutations_test.go internal/piweb/settings.go internal/piweb/server.go docs/api/openapi.yaml src/lib/api-runtime.ts src/components/ResourceManagerDialog.astro src/pi-app/workspace/resource-methods.ts src/pi-app/workspace/resource-methods.test.ts e2e/resources.spec.ts
git commit -m "feat: manage local pi resources" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 13: Subagents、Tool Presets 与 Model Test

**Files:**
- Create: `internal/piweb/subagent_settings.go`
- Create: `internal/piweb/subagent_settings_test.go`
- Create: `internal/piweb/model_test_run.go`
- Create: `internal/piweb/model_test_run_test.go`
- Modify: `internal/piweb/runtime_model_settings.go`
- Modify: `internal/piweb/server.go`
- Modify: `docs/api/openapi.yaml`
- Modify: `src/lib/api-runtime.ts`
- Modify: `src/lib/api-sessions.ts`
- Modify: `src/components/ResourceManagerDialog.astro`
- Modify: `src/pi-app/workspace/resource-methods.ts`
- Modify: `src/pi-app/sessions/session-methods.ts`
- Create: `e2e/model-tools-subagent.spec.ts`

**Interfaces:**
- Produces:
  - `GET/PUT /api/workspaces/{id}/subagents/settings`。
  - `POST /api/workspaces/{id}/models/test` body `{ provider, model, mode:"discovery"|"live" }`。
  - `POST /api/workspaces/{id}/sessions` 扩展 body `{ provider?, model?, thinkingLevel?, toolPreset? }`。
  - Tool preset：`default`、`coding`、`read-only`、`chat-only`、自定义 tool names。

- [ ] **Step 1: 写设置优先级和 model test 失败测试**

覆盖 global/project/user preset 合并、空 tools 表示 chat-only、旧 session 无 preset 保持默认、subagent profile 解析、discovery 不发 prompt、live 使用固定 `Respond with OK` 并返回 latency/model/error category。

- [ ] **Step 2: 验证测试先失败**

Run: `go test ./internal/piweb -run 'TestSubagentSettings|TestToolPreset|TestModelTest' -count=1`
Expected: FAIL。

- [ ] **Step 3: 实现 session startup options**

```go
type SessionStartOptions struct {
    Provider string `json:"provider"`
    Model string `json:"model"`
    ThinkingLevel string `json:"thinkingLevel"`
    ToolPreset string `json:"toolPreset"`
    ToolNames []string `json:"toolNames"`
}
```

创建 session 时原子保存 custom metadata，再启动 runtime；若 capability 不支持 tool selection，保留 session 但返回 warning，不伪装成功应用 preset。

- [ ] **Step 4: 实现 subagent/resource UI**

新会话 dialog 可选模型、thinking、preset；资源管理器可启停 built-in subagent、编辑 profile 显示名/模型/tools/skills。Model test 明确区分本地 discovery 和实际请求 live。

- [ ] **Step 5: E2E 和验证**

mock runtime 记录 start options；E2E 断言 chat-only 无 tool cards、coding preset 有预期 tools、subagent setting 重启后保留、model test 呈现 latency。

Run: `go test ./internal/piweb -run 'TestSubagentSettings|TestToolPreset|TestModelTest' -count=1 && bun run test:e2e -- e2e/model-tools-subagent.spec.ts`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/piweb/subagent_settings.go internal/piweb/subagent_settings_test.go internal/piweb/model_test_run.go internal/piweb/model_test_run_test.go internal/piweb/runtime_model_settings.go internal/piweb/server.go docs/api/openapi.yaml src/lib/api-runtime.ts src/lib/api-sessions.ts src/components/ResourceManagerDialog.astro src/pi-app/workspace/resource-methods.ts src/pi-app/sessions/session-methods.ts e2e/model-tools-subagent.spec.ts
git commit -m "feat: add agent tools and model controls" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 14: 高级预览、PWA App Shell 与诊断包

**Files:**
- Modify: `package.json`
- Modify: `bun.lock`
- Modify: `astro.config.ts`
- Create: `src/manifest.ts`
- Create: `public/icons/icon-192.png`
- Create: `public/icons/icon-512.png`
- Create: `src/pi-app/editor/pdf-preview.ts`
- Create: `src/pi-app/editor/docx-preview.ts`
- Modify: `src/pi-app/editor/file-preview-methods.ts`
- Modify: `internal/piweb/file_preview.go`
- Create: `internal/piweb/diagnostics.go`
- Create: `internal/piweb/diagnostics_test.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/capabilities.go`
- Create: `src/lib/pwa.test.ts`
- Create: `e2e/offline-shell-preview.spec.ts`

**Interfaces:**
- Produces:
  - app-shell cache 只含 hashed JS/CSS/icons/index/login/users。
  - PDF 在本地 pdf.js viewer 渲染；DOCX 使用 mammoth 转为受控 DOM；音视频使用原生 media controls。
  - `GET /api/diagnostics/export` 下载 JSON：版本、OS/arch、schema version、capabilities、workspace/session/runtime 计数和最近结构化错误摘要。

- [ ] **Step 1: 安装本地打包依赖并写失败测试**

Run: `bun add @vite-pwa/astro pdfjs-dist mammoth`

测试 service worker precache 不包含 `/api/`、SSE、WebSocket 或用户文件；preview MIME mapping；diagnostics key 稳定且不依赖互联网。

- [ ] **Step 2: 验证测试先失败**

Run: `bunx vitest run src/lib/pwa.test.ts src/pi-app/editor/file-preview.test.ts && go test ./internal/piweb -run TestDiagnostics -count=1`
Expected: FAIL。

- [ ] **Step 3: 实现 PWA app shell**

使用 `@vite-pwa/astro` generateSW；navigation fallback 仅应用页面。API 使用 NetworkOnly；离线打开已缓存 app shell 时显示“服务未连接”，不显示陈旧 workspace/session 数据。

- [ ] **Step 4: 实现 preview 与 diagnostics**

PDF worker、mammoth 和字体资源都由构建产物提供；DOCX 输出只映射 heading/paragraph/table/list/image，不直接 innerHTML 未处理输出。Diagnostics 使用显式 struct，不打包 DB/JSONL 正文。

- [ ] **Step 5: E2E 和产物检查**

E2E 在线首次加载后关闭 Go server，验证 app shell 和“服务未连接”；重启后恢复。fixture 验证 PDF 页数、DOCX 文本、audio/video controls。检查 `dist` 无远程 URL。

Run: `bun run test:coverage && go test ./... && bun run build && ! rg -n 'https?://' dist --glob '!*.map' && bun run test:e2e -- e2e/offline-shell-preview.spec.ts`
Expected: PASS；`dist` 不引用 CDN/远程静态资源。

- [ ] **Step 6: Commit**

```bash
git add package.json bun.lock astro.config.ts src/manifest.ts public/icons src/pi-app/editor internal/piweb/file_preview.go internal/piweb/diagnostics.go internal/piweb/diagnostics_test.go internal/piweb/server.go internal/piweb/capabilities.go src/lib/pwa.test.ts e2e/offline-shell-preview.spec.ts
git commit -m "feat: improve offline previews and diagnostics" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

### Task 15: Beta、人工反馈闭环与 RC 门禁

**Files:**
- Modify: `docs/releases/test-feedback-template.md`
- Create: `docs/releases/v1.10.0-beta.1-test-plan.md`
- Create: `docs/releases/v1.10.0-rc.1-checklist.md`
- Modify: `README.md`
- Modify: `docs/testing.md`
- Modify: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: Task 10–14 的 terminal/resources/PWA/preview/diagnostics。
- Produces: `v1.10.0-beta.1` 广泛手测版本；反馈闭环后产生 `v1.10.0-rc.1` 候选版本。

- [ ] **Step 1: 定义 Beta 手测矩阵**

至少覆盖 Linux amd64、Linux arm64、macOS arm64；浏览器覆盖目标 Chrome 107 和当前 Chromium。每个平台执行 Alpha 全量用例，并增加 PTY 重连、local plugin/skill、subagent/tool preset、model test、PDF/DOCX/媒体、PWA app shell 和 diagnostics 下载。

- [ ] **Step 2: 定义反馈字段和严重级别**

模板固定字段：版本/校验和、OS/arch、浏览器、Pi version、schema version、capabilities、复现步骤、预期/实际、diagnostics、P0–P3、是否可稳定复现；不接受只有截图而无步骤的 P0/P1。

- [ ] **Step 3: 执行 Beta 自动门禁**

Run: `bun run verify:release && git diff --check`
Expected: PASS。另运行 archive smoke、升级 fixture、重启恢复和无网络静态资源扫描。

- [ ] **Step 4: 发行 Beta 并收集至少一轮反馈**

发布 `v1.10.0-beta.1` prerelease；P0/P1 必须修复并有自动化回归测试，P2 必须明确纳入 RC 或写入已知问题，P3 可进入后续版本。

- [ ] **Step 5: RC 冻结规则**

RC 前连续两次 clean checkout `verify:release` 通过；所有支持平台 archive smoke 通过；无未关闭 P0/P1；OpenAPI、capability schema、DB migration 和用户文档冻结，只接受回归修复。

- [ ] **Step 6: Commit 与 RC tag**

```bash
git add docs/releases README.md docs/testing.md .github/workflows/release.yml
git commit -m "chore: prepare v1.10.0 release candidate" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
git tag -a v1.10.0-rc.1 -m "v1.10.0-rc.1"
```

## 完成定义

### `v1.10.0-alpha.1`（首个用户手测版本）

- 完成 Task 1–8。
- 完成 Task 9 Step 1–6，自动门禁、跨架构 archive smoke 与内部人工清单均通过。
- 可测试：会话搜索/导出/tree/fork、worktree，以及现有 workspace/session/file/Git/runtime 主链路；leaf navigate 仅在 capability 可用时纳入。

### `v1.10.0-beta.1`（完整功能手测版本）

- 在 Alpha 基础上完成 Task 10–14。
- 可测试：PTY、离线资源管理、subagent/tool preset/model test、高级预览、PWA app shell、diagnostics。

### `v1.10.0-rc.1`（候选发布版本）

- 完成 Task 15。
- P0/P1 人工反馈清零；所有自动门禁、升级测试、支持平台 archive smoke 连续通过。

## 全局验证矩阵

| 检查 | 每个任务 | Alpha | Beta/RC |
| --- | --- | --- | --- |
| `bun run lint` | 是 | 是 | 是 |
| `bun run typecheck` | 是 | 是 | 是 |
| `bun run test:coverage` | 涉及前端时 | 是 | 是 |
| `go test ./...` | 涉及 Go 时 | 是 | 是 |
| `go test -race ./...` | 并发/DB/runtime 时 | 是 | 是 |
| `backend:test:coverage` | 涉及 Go 时 | 是 | 是 |
| `bun run test:e2e` | 用户流程时 | 是 | 是 |
| `bun run build` | 涉及 UI/依赖时 | 是 | 是 |
| archive smoke | 否 | Linux amd64/arm64 | 所有支持平台 |
| 旧 DB 升级 | migration 任务 | 是 | 是 |
| 真实 Pi 人工测试 | 否 | 至少一种本地配置 | 支持模型矩阵 |
| 无互联网运行 | 新外部依赖时 | 是 | 是 |

## 实施注意事项

- Task 6–8 可以在 Task 5 后由不同 worktree 并行实现，但 `server.go`、`src/lib/api-*`、OpenAPI 和 capability 由集成者统一合并，避免多写入者冲突。
- Task 10、11 可以在 Alpha 后并行；Task 12 依赖 Task 11，Task 13 依赖 Task 11/12 的资源模型。
- 每次新增 capability 时，先后端返回 false 和前端隐藏，再随功能提交切换为 true；避免新前端连旧后端时出现死入口。
- 所有时间、随机 ID、HOME、session dir、workspace 和 provider 都由测试注入；禁止 sleep 驱动的并发测试。
- 遇到 Pi RPC 版本差异时通过 Task 3 capability 降级，不复制或直接修改 Pi JSONL 活跃历史。
- 参考实现固定到 `agegr/pi-web@def1478dedb45fa2720c452f5652b283351244d7`；只借鉴功能语义，不复制 Next.js 进程模型。
