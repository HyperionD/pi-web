# Skill Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在左下角增加技能管理入口和弹出面板，通过 Pi RPC 展示当前工作区 skills，并安全切换 `disable-model-invocation`。

**Architecture:** Go 后端调用 `pi --mode rpc --no-session --no-extensions` 的 `get_commands`，筛选 skill 并读取其 frontmatter 状态；PATCH 前重新发现并 canonicalize 路径后原子修改 `SKILL.md`。Astro 输出独立 SkillManager 面板，Custom Element 负责按 workspace 请求、分组、详情、开关和焦点管理。

**Tech Stack:** Go `net/http`、`os/exec`、原子文件写入；Astro、原生 TypeScript Custom Element、现有 CSS tokens、Vitest、Go `testing`。

**Spec:** `docs/superpowers/specs/2026-09-18-skill-management-design.md`

## Global Constraints

- 只实现本地发现、查看和启用/禁用；不实现在线搜索、安装、删除、更新和版本检查。
- 保持 Go 单二进制，不引入 Node 服务或 Pi SDK 运行时依赖。
- Pi RPC 不得使用 `--approve`；skill 请求使用 `--no-extensions`，不得因打开面板自动信任项目或执行 project extension。
- PATCH 必须重新发现 skill，并要求 canonical path 与发现结果匹配；不能把客户端 path 直接作为写入目标。
- 只修改 `disable-model-invocation` frontmatter，保留 BOM、换行、正文和其它字段。
- 全局和自定义 path skill 仅管理员可修改；项目 skill 允许有 workspace 权限的用户修改；单用户模式不额外限制。
- 前端继续使用 `target: "chrome107"`，不得使用 `color-mix()`、`100dvh`、`backdrop-filter` 或 Chrome 107 不支持的 API。
- 不新增 SQLite 表；skill 状态以 `SKILL.md` 为唯一事实来源。
- 每个任务完成后运行该任务列出的测试，并使用 Conventional Commit。

---

### Task 1: 建立 Pi skill RPC 发现层

**Files:**
- Create: `internal/piweb/pi_rpc.go`
- Create: `internal/piweb/skills.go`
- Create: `internal/piweb/skills_test.go`
- Modify: `internal/piweb/commands.go`
- Modify: `internal/piweb/commands_test.go`（仅在共享 RPC runner 需要补回归测试时）

**Interfaces:**
- Produces `runPiRPC(ctx context.Context, cwd string, extraArgs []string, request RPCCommand) (RPCResponse, error)`。
- Produces `ListPiSkills(ctx context.Context, cwd string) ([]PiSkillCommand, error)`。
- Produces `parsePiSkillCommands(response RPCResponse) ([]PiSkillCommand, error)`。
- `PiSkillCommand` 字段为 `Name string`、`Description string`、`Path string`、`Scope string`。
- `ListPiCommands` 改用共享 runner，但保持现有返回值和行为不变。

- [ ] **Step 1: 添加 RPC fixture 解析失败测试**

```go
func TestParsePiSkillCommandsFiltersNonSkills(t *testing.T) {
    response := RPCResponse{
        Type: "response", Command: "get_commands", Success: true,
        Data: json.RawMessage(`{"commands":[
          {"name":"skill:review","description":"Review code","source":"skill","sourceInfo":{"path":"/tmp/review/SKILL.md","scope":"project"}},
          {"name":"format","description":"Format","source":"extension","sourceInfo":{"path":"/tmp/format.ts","scope":"project"}},
          {"name":"skill:global","description":"Global","source":"skill","sourceInfo":{"path":"/home/u/.pi/agent/skills/global/SKILL.md","scope":"user"}}
        ]}`),
    }
    skills, err := parsePiSkillCommands(response)
    if err != nil { t.Fatal(err) }
    if len(skills) != 2 { t.Fatalf("got %d skills, want 2", len(skills)) }
    if skills[0].Scope != "project" || skills[1].Scope != "global" {
        t.Fatalf("unexpected scopes: %#v", skills)
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/piweb -run 'TestParsePiSkillCommandsFiltersNonSkills' -count=1`
Expected: FAIL because `parsePiSkillCommands` and `PiSkillCommand` do not exist。

- [ ] **Step 3: 实现共享 RPC runner**

将现有 `ListPiCommands` 中的子进程生命周期、8 秒 timeout、stderr 收集、`PI_SKIP_VERSION_CHECK=1`、进程组清理和 response 行扫描提取到 `runPiRPC`。固定参数为：

```go
[]string{"--mode", "rpc", "--no-session"}
```

`extraArgs` 追加 `--no-extensions`；请求使用 `RPCCommand{ID: "skills", Type: "get_commands"}`。`ListPiCommands` 使用相同 runner 但不追加 `--no-extensions`，并继续调用已有 `parseCommandsRPCResponse`。

- [ ] **Step 4: 实现 skill command 映射**

从 `get_commands` response 中只保留 `source == "skill"` 且 `sourceInfo.path` 非空的条目；把 scope 映射为：`user -> global`、`project -> project`、其它非空值 -> `path`。名称和描述 trim 后为空的条目不返回。按 `Scope`、`Name`、`Path` 排序。

- [ ] **Step 5: 添加 Pi RPC 集成 fixture 测试**

在测试中创建临时 `pi` 可执行文件，让它读取 stdin 后输出固定 response，并把临时目录加入 `PATH`：

```go
func TestListPiSkillsUsesNoExtensions(t *testing.T) {
    binDir := t.TempDir()
    script := filepath.Join(binDir, "pi")
    if err := os.WriteFile(script, []byte(`#!/bin/sh
printf '%s\\n' '{"id":"skills","type":"response","command":"get_commands","success":true,"data":{"commands":[]}}'
`), 0o755); err != nil { t.Fatal(err) }
    if err := os.Chmod(script, 0o755); err != nil { t.Fatal(err) }
    t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

    _, err := ListPiSkills(context.Background(), t.TempDir())
    if err != nil { t.Fatal(err) }
}
```

测试同时记录 argv，断言包含 `--mode`、`rpc`、`--no-session`、`--no-extensions`，不包含 `--approve`。

- [ ] **Step 6: 运行 Task 1 测试**

Run: `go test ./internal/piweb -run 'Test(ParsePiSkillCommands|ListPiSkills|ListPiCommands)' -count=1`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/piweb/pi_rpc.go internal/piweb/skills.go internal/piweb/skills_test.go internal/piweb/commands.go internal/piweb/commands_test.go
git commit -m "feat: add pi skill discovery"
```

---

### Task 2: 实现 frontmatter 状态读取与原子编辑

**Files:**
- Create: `internal/piweb/skills_frontmatter.go`
- Create: `internal/piweb/skills_frontmatter_test.go`
- Modify: `internal/piweb/skills.go`

**Interfaces:**
- Produces `skillInvocationEnabled(content []byte) (bool, error)`。
- Produces `setSkillInvocation(content []byte, enabled bool) ([]byte, error)`。
- Produces `readSkillInvocation(path string) (bool, error)`。
- Produces `writeSkillFileAtomic(path string, content []byte) error`。

- [ ] **Step 1: 写状态和格式保留测试**

```go
func TestSetSkillInvocationPreservesFrontmatterAndBody(t *testing.T) {
    original := []byte("\ufeff---\r\nname: demo\r\ndescription: Demo\r\ndisable-model-invocation: false\r\n---\r\n\r\nBody\r\n")
    updated, err := setSkillInvocation(original, false)
    if err != nil { t.Fatal(err) }
    if string(updated) != string(original) { t.Fatalf("false update changed content:\n%s", updated) }

    updated, err = setSkillInvocation(original, true)
    if err != nil { t.Fatal(err) }
    want := "\ufeff---\r\nname: demo\r\ndescription: Demo\r\ndisable-model-invocation: true\r\n---\r\n\r\nBody\r\n"
    if string(updated) != want { t.Fatalf("got %q, want %q", updated, want) }
}

func TestSetSkillInvocationAddsAndRemovesKey(t *testing.T) {
    source := []byte("---\nname: demo\ndescription: Demo\n---\nBody\n")
    disabled, err := setSkillInvocation(source, false)
    if err != nil { t.Fatal(err) }
    enabled, err := setSkillInvocation(disabled, true)
    if err != nil { t.Fatal(err) }
    if string(enabled) != string(source) { t.Fatalf("round trip changed content: %q", enabled) }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/piweb -run 'TestSetSkillInvocation' -count=1`
Expected: FAIL because the frontmatter helpers do not exist。

- [ ] **Step 3: 实现 frontmatter 定点编辑**

实现逻辑必须：

1. 识别文件开头的 BOM 和 `---` opening fence。
2. 找到首个完整 closing fence；找不到时返回错误。
3. 只在 frontmatter 区间识别普通、单引号和双引号 key。
4. `enabled=false` 时写入唯一的 `disable-model-invocation: true`。
5. `enabled=true` 时删除现有 key 行；没有 key 时保持原文。
6. 拒绝 inline/object frontmatter、重复 key 和无法定位的格式，不能静默重写。
7. 保持原有 newline 风格、BOM、其它字段和正文。

- [ ] **Step 4: 添加边界测试**

覆盖 LF、CRLF、CR、BOM、缩进 key、引号 key、无 frontmatter、缺 closing fence、重复 key、正文中出现同名文本，以及 key 在 frontmatter 最后一行无 newline 的情况。测试 `skillInvocationEnabled` 对缺失/false/true 分别返回 true/true/false。

- [ ] **Step 5: 实现安全读取和原子写入**

`readSkillInvocation` 使用 `os.Open`、最大 1 MiB reader 和 `skillInvocationEnabled`。`writeSkillFileAtomic`：

```go
func writeSkillFileAtomic(path string, content []byte) error {
    info, err := os.Stat(path)
    if err != nil { return err }
    temporary, err := os.CreateTemp(filepath.Dir(path), ".pi-web-skill-*.tmp")
    if err != nil { return err }
    temporaryPath := temporary.Name()
    removeTemporary := true
    defer func() {
        if removeTemporary { _ = os.Remove(temporaryPath) }
    }()
    if err := temporary.Chmod(info.Mode().Perm()); err != nil {
        _ = temporary.Close()
        return err
    }
    if _, err := temporary.Write(content); err != nil {
        _ = temporary.Close()
        return err
    }
    if err := temporary.Sync(); err != nil {
        _ = temporary.Close()
        return err
    }
    if err := temporary.Close(); err != nil { return err }
    if err := os.Rename(temporaryPath, path); err != nil { return err }
    removeTemporary = false
    return nil
}
```

保留原文件权限；不得跟随客户端提供的未校验路径调用该函数。

- [ ] **Step 6: 运行 Task 2 测试**

Run: `go test ./internal/piweb -run 'Test(SkillInvocation|SetSkill|ReadSkill|WriteSkill)' -count=1`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/piweb/skills.go internal/piweb/skills_frontmatter.go internal/piweb/skills_frontmatter_test.go
git commit -m "feat: safely edit skill invocation state"
```

---

### Task 3: 增加 workspace skill API 和账号安全

**Files:**
- Create: `internal/piweb/server_workspace_skill_handlers.go`
- Create: `internal/piweb/server_workspace_skill_handlers_test.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/skills.go`

**Interfaces:**
- Adds `GET /api/workspaces/{workspaceID}/skills`。
- Adds `PATCH /api/workspaces/{workspaceID}/skills`。
- Adds `func (s *Server) workspaceSkills(w http.ResponseWriter, r *http.Request)`。
- Adds `func (s *Server) patchWorkspaceSkill(w http.ResponseWriter, r *http.Request)`。
- Adds `SkillDiscoveryResponse` and `SkillPatchRequest` JSON types from the spec。

- [ ] **Step 1: 写 handler 路由和成功响应测试**

测试通过现有 `NewServer`/`httptest` fixtures 建立 workspace，并在临时 PATH 中提供固定 Pi fixture。先验证：

```go
request := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/skills", nil)
response := httptest.NewRecorder()
server.Handler().ServeHTTP(response, request)
if response.Code != http.StatusOK { t.Fatalf("status=%d", response.Code) }
var body SkillDiscoveryResponse
if err := json.NewDecoder(response.Body).Decode(&body); err != nil { t.Fatal(err) }
if len(body.Skills) != 1 || body.Skills[0].Name != "demo" { t.Fatalf("unexpected body: %#v", body) }
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/piweb -run 'TestWorkspaceSkills' -count=1`
Expected: FAIL because routes and handlers do not exist。

- [ ] **Step 3: 注册路由并实现 GET**

在 `Server.routes` 中注册：

```go
s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/skills", s.requireWorkspaceAccess(s.workspaceSkills))
s.mux.HandleFunc("PATCH /api/workspaces/{workspaceID}/skills", s.requireWorkspaceAccess(s.patchWorkspaceSkill))
```

GET handler 使用 `r.PathValue("workspaceID")` 解析 workspace 根目录；`EnablePiExecution=false` 时返回 `200`、`available:false`、空列表和错误文本。Pi 发现成功时读取每个命令的文件状态，形成稳定的 `Skill` 列表。

- [ ] **Step 4: 实现 PATCH 的重新发现和权限判断**

PATCH handler 解码 `SkillPatchRequest`，拒绝空 path、缺少 enabled 和未知 JSON 结构。重新调用 `ListPiSkills`，用 `filepath.EvalSymlinks` 对请求 path 和发现 path 做 canonical 比较。根据发现结果的 `Scope` 判断权限：

```go
if actor := requestActor(r); actor != nil && actor.Role != "admin" && skill.Scope != "project" {
    writeError(w, http.StatusForbidden, errAccountDenied)
    return
}
```

通过 server 级互斥串行执行“重发现 → 读取 → 修改 → 重发现”，修改后返回 `{ "skill": ... }`。

- [ ] **Step 5: 添加错误和安全测试**

覆盖：workspace 不存在、普通用户访问未授权 workspace、普通用户修改 global/path 返回 403、普通用户修改 project 成功、未发现 path 返回 404、软链接 canonical match、任意 workspace 外路径拒绝、Pi timeout 返回 504、`EnablePiExecution=false` 返回 `available:false`、PATCH 缺字段返回 400、文件被删除返回 409。

- [ ] **Step 6: 运行 Task 3 测试**

Run: `go test ./internal/piweb -run 'Test(WorkspaceSkills|PatchWorkspaceSkill|SkillAccess|SkillAPI)' -count=1`
Expected: PASS。

- [ ] **Step 7: 运行完整后端回归**

Run: `go test ./internal/piweb ./...`
Expected: PASS。

- [ ] **Step 8: 提交**

```bash
git add internal/piweb/server.go internal/piweb/skills.go internal/piweb/server_workspace_skill_handlers.go internal/piweb/server_workspace_skill_handlers_test.go
git commit -m "feat: expose workspace skill management API"
```

---

### Task 4: 增加前端 API、入口和面板结构

**Files:**
- Create: `src/components/SkillManager.astro`
- Create: `src/pi-app/workspace/skill-methods.test.ts`
- Modify: `src/lib/api.ts`
- Modify: `src/App.astro`
- Modify: `src/pi-app/test-helper.ts`
- Modify: `src/pi-app/core-events.test.ts`

**Interfaces:**
- Produces `getWorkspaceSkills(workspaceId)`。
- Produces `setWorkspaceSkillEnabled(workspaceId, path, enabled)`。
- Produces DOM hooks `[data-skill-manager]`, `[data-skill-open]`, `[data-skill-close]`, `[data-skill-refresh]`, `[data-skill-list]`, `[data-skill-detail]`, `[data-skill-toggle]`。
- `SkillManager.astro` 不包含 `<form>`，避免嵌套现有 settings form。

- [ ] **Step 1: 添加 API client 测试**

在 `src/pi-app/workspace/skill-methods.test.ts` 中先写：

```ts
it("loads skills for the active workspace", async () => {
  globalThis.fetch = vi.fn(async (url) => {
    expect(String(url)).toContain("/api/workspaces/ws-1/skills");
    return new Response(JSON.stringify({ skills: [], available: true }), { status: 200 });
  }) as typeof fetch;
  await getWorkspaceSkills("ws-1");
  expect(fetch).toHaveBeenCalledTimes(1);
});
```

在同一测试文件中从 `../test-helper` 导入 `installPiAppFixture`；面板行为测试使用 `installPiAppFixture()` 返回的 `<pi-app>`，再设置 `app.dataset.activeWorkspaceId = "ws-1"`，不引入未定义的 `createTestApp`。

- [ ] **Step 2: 运行测试确认失败**

Run: `bunx vitest run src/pi-app/workspace/skill-methods.test.ts`
Expected: FAIL because the API functions and test file do not exist。

- [ ] **Step 3: 实现 API client 类型和请求函数**

在 `src/lib/api.ts` 中添加：

```ts
export type WorkspaceSkill = {
  name: string;
  description: string;
  path: string;
  scope: "global" | "project" | "path";
  enabled: boolean;
};

export type WorkspaceSkillsResponse = {
  skills: WorkspaceSkill[];
  available: boolean;
  error?: string;
  notice?: string;
};

export function getWorkspaceSkills(workspaceId: string): Promise<WorkspaceSkillsResponse> {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/skills`);
}

export function setWorkspaceSkillEnabled(workspaceId: string, path: string, enabled: boolean) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/skills`, {
    method: "PATCH",
    body: JSON.stringify({ path, enabled }),
  });
}
```

- [ ] **Step 4: 添加面板静态结构**

`SkillManager.astro` 提供：

- modal root `hidden`、`role="dialog"`、`aria-modal="true"`、标题和关闭按钮；
- 左侧空列表/loading/error/list 容器；
- 右侧 detail empty/name/description/path/scope/toggle 容器；
- footer count、refresh、status；
- 遮罩 `[data-skill-overlay]`。

不要在 Astro 中预渲染动态 skill，动态内容全部由 `skill-methods.ts` 使用 `textContent` 写入，避免把 skill 描述当 HTML 插入。

- [ ] **Step 5: 接入左下角入口**

在 `src/App.astro`：

```astro
import { ..., Sparkles, ... } from "@lucide/astro";
import SkillManager from "./components/SkillManager.astro";
```

将 footer actions 改为人员管理旁新增：

```astro
<button class="sb-skill-manager" type="button" data-action="open-skill-manager"
  title="技能管理" aria-label="打开技能管理">
  <Sparkles size={14} strokeWidth={1.75} aria-hidden="true" />
</button>
```

在 `</pi-app>` 前挂载 `<SkillManager />`。保留人员管理链接的管理员可见性规则。

- [ ] **Step 6: 更新 test helper 和静态入口测试**

将最小测试 DOM 加入 SkillManager root、入口按钮和 workspace id；增加断言验证 App.astro 同时包含 `data-action="open-skill-manager"`、`Sparkles` 和 `<SkillManager />`。

- [ ] **Step 7: 运行 Task 4 测试**

Run: `bunx vitest run src/pi-app/workspace/skill-methods.test.ts src/pi-app/core-events.test.ts`
Expected: API client 和静态入口测试 PASS。

- [ ] **Step 8: 提交**

```bash
git add src/lib/api.ts src/components/SkillManager.astro src/App.astro src/pi-app/test-helper.ts src/pi-app/core-events.test.ts src/pi-app/workspace/skill-methods.test.ts
git commit -m "feat: add skill manager panel structure"
```

---

### Task 5: 实现面板状态、交互和 Chrome 107 样式

**Files:**
- Create: `src/pi-app/workspace/skill-methods.ts`
- Create: `src/styles/parts/skill-manager.css`
- Modify: `src/pi-app/index.ts`
- Modify: `src/styles.css`
- Modify: `src/browser-compatibility.test.ts`
- Modify: `src/pi-app/workspace/skill-methods.test.ts`

**Interfaces:**
- Produces `skillMethods`，由 `Object.assign(PiApp.prototype, ..., skillMethods, ...)` 挂载。
- Provides `openSkillManager()`、`closeSkillManager()`、`loadWorkspaceSkills()`、`renderSkillList()`、`renderSelectedSkill()`。
- `skillMethods` 使用 `this.dataset.activeWorkspaceId` 和 `this.apiConnected`，不启动额外全局轮询。

- [ ] **Step 1: 写面板打开/加载/关闭测试**

```ts
it("loads only the active workspace and closes with restored focus", async () => {
  const app = installPiAppFixture();
  app.dataset.activeWorkspaceId = "ws-1";
  const button = app.querySelector("[data-action='open-skill-manager']") as HTMLButtonElement;
  button.focus();
  await app.openSkillManager();
  expect(app.querySelector("[data-skill-manager]")?.hidden).toBe(false);
  expect(globalThis.fetch).toHaveBeenCalledWith(
    expect.stringContaining("/api/workspaces/ws-1/skills"),
    expect.anything(),
  );
  app.closeSkillManager();
  expect(app.querySelector("[data-skill-manager]")?.hidden).toBe(true);
  expect(document.activeElement).toBe(button);
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `bunx vitest run src/pi-app/workspace/skill-methods.test.ts`
Expected: FAIL because `skillMethods` and panel behavior do not exist。

- [ ] **Step 3: 实现面板状态机**

维护以下实例状态：

```ts
type SkillPanelState = {
  skills: WorkspaceSkill[];
  selectedPath: string;
  loading: boolean;
  savingPath: string;
  error: string;
  notice: string;
  loadToken: symbol | undefined;
};
```

`openSkillManager` 保存当前 focus、显示面板并调用 `loadWorkspaceSkills`。`loadWorkspaceSkills` 在请求开始时生成 token，响应只在 token、workspace id 和当前面板仍一致时应用。`closeSkillManager` 清理状态、隐藏 root 并恢复 focus。

- [ ] **Step 4: 实现分组、详情和安全文本渲染**

按 `global`、`project`、`path` 顺序渲染分组；列表项使用 `button`，包含名称、enabled status dot 和 scope label。所有 skill name、description、path 使用 `textContent`，不能使用 `innerHTML`。没有 selected skill 时显示空详情。

- [ ] **Step 5: 实现 toggle 和错误处理**

点击开关时：

1. 拒绝 `available:false` 和无 selected skill。
2. 设置 `savingPath`，禁用当前开关。
3. 调用 `setWorkspaceSkillEnabled`。
4. 成功时用响应的 `skill` 替换列表对应项。
5. 403/404/409/504 时显示错误并重新 GET 列表；其它错误显示 `requestErrorMessage` 或字符串。
6. finally 清除 `savingPath`。

对 global/path skill 根据当前账号角色将 toggle disabled，并显示“需要管理员权限”。

- [ ] **Step 6: 绑定事件和入口**

在 `src/pi-app/index.ts`：

```ts
import { skillMethods } from "./workspace/skill-methods";

this.querySelector("[data-action='open-skill-manager']")?.addEventListener(
  "click", () => void this.openSkillManager(),
);

Object.assign(
  PiApp.prototype,
  workspaceMethods,
  skillMethods,
  sessionMethods,
  // existing method groups...
);
```

同时绑定 close、overlay、refresh、Escape；在 `clearActiveWorkspaceView` 后确保面板关闭或显示无 workspace 状态。

- [ ] **Step 7: 添加 CSS**

`skill-manager.css` 使用现有变量，包含：

- modal overlay 和 panel；
- 桌面端左右两栏；
- group heading、selected row、status dot、detail fields、error/status；
- `@media (max-width: 720px)` 下单栏布局；
- 高度使用 `max-height: calc(100vh - var(--space-8))`，不使用 `100dvh`；
- 不使用 `color-mix()`、`backdrop-filter` 或 CSS nesting。

在 `src/styles.css` 增加：

```css
@import "./styles/parts/skill-manager.css";
```

- [ ] **Step 8: 添加 Chrome 107 兼容断言**

在 `src/browser-compatibility.test.ts` 增加：

```ts
it("keeps the skill manager within the Chrome 107 CSS baseline", () => {
  const styles = readFileSync(new URL("./styles/parts/skill-manager.css", import.meta.url), "utf8");
  expect(styles).not.toContain("color-mix(");
  expect(styles).not.toContain("100dvh");
  expect(styles).toContain("100vh");
});
```

- [ ] **Step 9: 运行 Task 5 测试**

Run: `bunx vitest run src/pi-app/workspace/skill-methods.test.ts src/browser-compatibility.test.ts src/pi-app/core-events.test.ts`
Expected: PASS。

- [ ] **Step 10: 提交**

```bash
git add src/pi-app/workspace/skill-methods.ts src/pi-app/workspace/skill-methods.test.ts src/pi-app/index.ts src/styles/parts/skill-manager.css src/styles.css src/browser-compatibility.test.ts
git commit -m "feat: implement skill manager interactions"
```

---

### Task 6: 完整回归、构建和嵌入资源同步

**Files:**
- Modify: `static/*`（仅由构建命令生成与源码相关的嵌入资源）
- Inspect: `git diff`、`git status`

**Interfaces:**
- Consumes all backend and frontend interfaces from Tasks 1–5。
- Produces a verified Astro build and synchronized Go embedded assets。

- [ ] **Step 1: 运行格式化和静态检查**

```bash
gofmt -w internal/piweb/pi_rpc.go internal/piweb/skills.go internal/piweb/skills_frontmatter.go internal/piweb/server_workspace_skill_handlers.go
git diff --check
bun run lint
bun run typecheck
```

Expected: all commands exit 0，且 `git diff --check` 无输出。

- [ ] **Step 2: 运行前后端测试**

```bash
bun run test
bun run backend:test
```

Expected: Vitest 和 Go tests 全部通过；失败时只修复当前 skill 管理变更，不调整无关测试。

- [ ] **Step 3: 构建前端并验证嵌入资源**

```bash
bun run build
bun run check:embedded-assets
```

Expected: Astro build 成功，skill manager 的 CSS/JS 出现在构建输出中，嵌入资源检查通过。

- [ ] **Step 4: 构建单二进制**

```bash
bun run build:single
```

Expected: `static/` 与当前源码构建结果同步，`dist/pi-web` 生成成功；不提交依赖目录、缓存或测试产物。

- [ ] **Step 5: 复核 diff 和工作区状态**

```bash
git status --short
git diff --stat HEAD~1
rg -n "100dvh|color-mix\(|backdrop-filter" src/styles/parts/skill-manager.css src/App.astro src/components/SkillManager.astro || true
```

Expected: diff 只包含 skill 管理功能、相关测试和构建嵌入资源；兼容性搜索无匹配。

- [ ] **Step 6: 提交构建同步结果**

```bash
git add static dist
if git diff --cached --quiet; then
  echo "no generated asset changes"
else
  git commit -m "chore: sync embedded skill manager assets"
fi
```

Expected: 仅在构建确实更新嵌入资源时提交构建产物；最终工作区干净。

- [ ] **Step 7: 最终验证**

```bash
bun run check
git status --short --branch
git log --oneline --decorate -8
```

Expected: `bun run check` 通过，分支为 `feat/skill-management`，工作区无未提交文件。
