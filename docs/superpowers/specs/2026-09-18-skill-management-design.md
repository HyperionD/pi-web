# Skill 管理功能技术设计

## 目标

在 pi-web 中增加基础 skill 管理能力：查看当前工作区可用的本地 skills、按来源范围展示、查看元数据，并启用或禁用 skill 的模型自动调用；不包含在线搜索、安装、删除、更新和版本检查。

## 已确认范围

### 包含

- 全局、项目和自定义路径 skill 的发现与分组展示。
- skill 名称、描述、来源范围和文件路径展示。
- 修改 `disable-model-invocation` frontmatter 字段。
- 在左下角人员管理图标旁增加技能管理图标。
- 弹出独立的技能管理面板。
- 单用户和多账号模式下的权限控制。
- Chrome 107 兼容验证。

### 不包含

- skills.sh 或其它远程目录搜索。
- 从网络、本地压缩包或目录安装 skill。
- 删除或移动 skill 文件。
- skill 版本检查和更新。
- 新增 SQLite 表或把 skill 状态复制到 pi-web 配置。
- 自动信任项目或自动执行项目 extension。

## 设计原则

1. **运行时一致性**：通过 Pi RPC 的资源发现结果建立列表，不复制完整的 Pi 资源加载逻辑。
2. **单二进制**：后端能力使用现有 Go 服务和本机 `pi` 命令，不引入 Node 服务或 Pi SDK 运行时依赖。
3. **文件即事实**：启用状态继续保存在 `SKILL.md` 的 `disable-model-invocation` 字段中，使 CLI、Web 和其它运行时共享状态。
4. **最小写权限**：客户端不能凭任意路径直接要求后端写文件；后端必须重新发现并校验目标 skill。
5. **不扩大信任边界**：发现请求不使用 `--approve`，不因为打开 Web 面板而自动信任项目或加载项目 extension。
6. **Chrome 107 基线**：遵循现有 `vite.build.target = "chrome107"`，不引入高于该基线的浏览器 API 或 CSS 特性。

## 架构

```text
Browser
  │
  ├─ GET /api/workspaces/{workspaceID}/skills
  │       │
  │       ├─ workspace access + policy
  │       ├─ pi --mode rpc --no-session --no-extensions
  │       ├─ get_commands
  │       ├─ filter source == skill
  │       └─ read disable-model-invocation from each SKILL.md
  │
  └─ PATCH /api/workspaces/{workspaceID}/skills
          │
          ├─ workspace access + policy
          ├─ account role check
          ├─ repeat skill discovery
          ├─ canonical path match
          ├─ surgical frontmatter edit
          └─ rediscover and return updated skill
```

Pi RPC 使用以下约束：

- 工作目录设置为目标 workspace 根目录。
- 使用 `--mode rpc --no-session --no-extensions`。
- 设置 `PI_SKIP_VERSION_CHECK=1`。
- 不使用 `--approve`，保留 Pi 当前项目 trust 行为。
- 请求 `get_commands` 超时上限为 8 秒。
- RPC 输出只作为发现信息使用，不执行 skill 内容。

`--no-extensions` 用于避免 skill 列表请求加载和执行项目 extension；skill 资源仍由 Pi 负责发现。若当前 Pi 版本不能在该模式下返回 skill command，后端将返回明确的不可用诊断，而不会自行扩大加载范围。

## 后端设计

### 文件职责

```text
internal/piweb/skills.go
  - Skill、SkillDiscoveryResponse、SkillPatchRequest 等领域类型
  - 调用 Pi RPC 并筛选 skill command
  - 读取 skill 文件中的启用状态
  - 将 Pi 的 sourceInfo.scope 转换为 global/project/path

internal/piweb/skills_frontmatter.go
  - 解析和修改 disable-model-invocation
  - 保留其它 frontmatter、正文、BOM 和换行风格
  - 拒绝无法安全定位 frontmatter 的文件

internal/piweb/server_workspace_skill_handlers.go
  - GET /api/workspaces/{workspaceID}/skills
  - PATCH /api/workspaces/{workspaceID}/skills
  - 工作区和账号权限处理

internal/piweb/skills_test.go
  - RPC 响应解析
  - scope 映射
  - frontmatter 状态读取
  - 路径 canonicalization

internal/piweb/server_workspace_skill_handlers_test.go
  - API 成功和错误路径
  - workspace policy
  - 多账号权限
  - Pi 不可用和超时
```

### 数据结构

Go 内部结构与 JSON 字段保持稳定：

```go
type Skill struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Path        string `json:"path"`
    Scope       string `json:"scope"`
    Enabled     bool   `json:"enabled"`
}

type SkillDiscoveryResponse struct {
    Skills    []Skill `json:"skills"`
    Available bool    `json:"available"`
    Error     string  `json:"error,omitempty"`
    Notice    string  `json:"notice,omitempty"`
}

type SkillPatchRequest struct {
    Path    string `json:"path"`
    Enabled *bool  `json:"enabled"`
}
```

`Enabled` 是 `disable-model-invocation` 的反值：字段缺失或值为 false 时为 true，字段为 true 时为 false。

### 发现流程

1. 检查目标 workspace 已通过 `requireWorkspaceAccess` 和 `WorkspacePolicy`。
2. 若 `EnablePiExecution` 为 false，返回 `200`、`available:false` 和可读错误，不启动 Pi。
3. 启动短生命周期 Pi RPC 子进程并发送固定的 `get_commands` 请求。
4. 忽略 `source != "skill"` 的 command。
5. 对每个 skill command 要求存在非空路径，并将路径转换为 clean、absolute、canonical 形式。
6. 读取对应文件，限制单个文件读取大小为 1 MiB，仅解析 frontmatter 状态，不把正文返回给浏览器。
7. 文件不存在、路径不是普通文件或状态无法安全解析的条目不进入可修改列表，并在响应 `Error` 或 `Notice` 中说明。
8. 将 `sourceInfo.scope` 映射为：`user -> global`、`project -> project`、其它非空 scope -> `path`。
9. 按 scope、名称和 canonical path 稳定排序，避免 RPC 返回顺序变化造成 UI 抖动。

项目未被 Pi trust 时，项目本地 skill 不会因为 Web 请求自动出现；响应可附带提示，说明需要先由 Pi 信任该项目。

### 修改流程

PATCH 请求只接受非空 `path` 和布尔 `enabled`。处理顺序：

1. 解析 JSON；缺少字段返回 400。
2. 通过 workspace access 和 policy 校验 workspace。
3. 重新执行一次 skill 发现，禁止信任客户端上一次 GET 的结果。
4. 将客户端路径 canonicalize，并要求与发现结果中的 skill path 完全匹配。
5. 多账号模式下：
   - `global` 和 `path` skill 仅管理员可修改；
   - `project` skill 允许有该 workspace 权限的用户修改；
   - 单用户模式不额外限制。
6. 使用 frontmatter 编辑器只修改 `disable-model-invocation`：
   - 有字段时只替换该字段的值；
   - 启用 skill 且没有字段时删除整行；
   - 禁用 skill 且没有字段时在 frontmatter 开始处插入；
   - 识别普通、单引号和双引号 key；
   - 支持 LF、CRLF、CR 和 UTF-8 BOM；
   - frontmatter 为不支持的 inline/object 格式时拒绝修改；
   - 不修改正文和其它字段；
   - 使用临时文件、`fsync`、权限复制和 rename 完成原子写入。
7. 写入后重新发现目标 skill，返回更新后的 skill；若文件在过程中被删除或发生不一致，返回 409 并要求前端刷新。
8. 对同一 canonical path 使用进程内互斥，避免同一服务并发 toggle 互相覆盖。

### API

#### 获取 skills

```http
GET /api/workspaces/{workspaceID}/skills
```

成功响应：

```json
{
  "skills": [
    {
      "name": "code-review",
      "description": "代码审查工作流",
      "path": "/home/user/.pi/agent/skills/code-review/SKILL.md",
      "scope": "global",
      "enabled": true
    }
  ],
  "available": true
}
```

Pi 执行被禁用或 Pi 不可用时保持 HTTP 200，返回 `available:false`、空 `skills` 和 `error`，让面板能够显示可解释状态。网络/API 请求本身失败仍使用对应 HTTP 错误码。

#### 更新状态

```http
PATCH /api/workspaces/{workspaceID}/skills
Content-Type: application/json

{
  "path": "/home/user/.pi/agent/skills/code-review/SKILL.md",
  "enabled": false
}
```

成功响应：

```json
{
  "skill": {
    "name": "code-review",
    "description": "代码审查工作流",
    "path": "/home/user/.pi/agent/skills/code-review/SKILL.md",
    "scope": "global",
    "enabled": false
  }
}
```

错误码约定：

| 场景 | 状态 |
| --- | ---: |
| JSON 或字段无效 | 400 |
| frontmatter 无法安全修改 | 400 |
| 未登录或无 workspace 权限 | 403 |
| 全局/path skill 非管理员修改 | 403 |
| skill 不存在或不属于发现结果 | 404 |
| workspace 不存在 | 404 |
| 文件在读写期间发生变化 | 409 |
| Pi RPC 超时 | 504 |
| 其它 Pi/文件系统错误 | 502 或 500 |

## 前端设计

### 文件职责

```text
src/components/SkillManager.astro
  - 独立弹出面板的静态结构
  - 列表、详情、状态、关闭和刷新控件

src/pi-app/workspace/skill-methods.ts
  - 打开/关闭面板
  - GET/PATCH 请求和请求 token
  - skill 列表渲染、分组、详情和 toggle 状态
  - focus restore、Escape、遮罩关闭

src/lib/api.ts
  - getWorkspaceSkills(workspaceID)
  - setWorkspaceSkillEnabled(workspaceID, path, enabled)

src/styles/parts/skill-manager.css
  - 面板布局、列表、详情、移动端回退和错误状态

src/App.astro
  - 引入 Sparkles 图标
  - 在人员管理旁增加技能管理按钮
  - 挂载 SkillManager 面板

src/pi-app/index.ts
  - 绑定技能管理按钮、面板关闭和技能面板方法
```

### 入口与交互

左下角排列：

```text
[人员管理] [技能管理] [设置]
```

- `Sparkles` SVG 图标作为技能管理入口。
- 人员管理仍只对管理员显示；技能管理按钮对可访问 workspace 的用户显示。
- 没有当前 workspace 时技能管理按钮 disabled。
- 点击按钮后才发起 GET 请求，不加入启动 bootstrap 请求。
- 面板为独立 modal，不嵌套在现有 settings form，避免与 settings submit、scope 切换和 focus trap 相互影响。
- 面板关闭后恢复打开按钮焦点；Escape 和遮罩点击关闭。
- 当前 workspace 变化时使旧请求失效，防止旧 workspace 数据覆盖新 workspace。

### 面板布局

桌面端使用左右两栏：

- 左栏：按 global/project/path 分组的 skill 列表；启用状态显示状态点。
- 右栏：名称、描述、scope、路径和启用开关。
- 底部：数量、刷新按钮和全局错误状态。

移动端使用单栏：选择列表项后展示详情，面板宽度不超过视口，使用 `100vh` 作为高度回退。样式使用现有 CSS 变量、普通 border/background/flex/grid，不使用 backdrop-filter、color-mix 或动态 viewport 单位。

### 状态与错误

- 初始：显示加载状态。
- 空列表：显示“当前工作区没有可用 skill”。
- `available:false`：显示 Pi 不可用提示和刷新按钮，不显示可操作开关。
- 普通用户查看 global/path：显示 skill 详情，但开关 disabled 并注明需管理员修改。
- toggle 请求中：当前开关和同一 skill 的重复操作 disabled。
- PATCH 403/409/404：保留当前详情，显示错误并刷新列表。
- PATCH 成功：使用服务端返回对象更新列表和详情。

## 权限与安全

- 所有 endpoint 使用现有 `requireWorkspaceAccess`，因此受 workspace catalog、账号归属和 `allowedRoots` 限制。
- 服务端不接受“只要 path 在客户端提交就可以写”的逻辑；每次 PATCH 都重新发现并验证。
- canonical path 校验覆盖软链接，避免通过软链接逃逸到未授权目录。
- 不向浏览器返回 SKILL.md 正文，降低 prompt injection 内容暴露面。
- 发现过程使用 `--no-extensions` 和不带 `--approve` 的 Pi 进程，不执行项目 extension，不自动改变 trust 状态。
- 错误信息不得包含 secret、API key 或完整文件内容；路径和 Pi stderr 仅使用现有 `RedactSecrets` 后的诊断。

## 测试设计

### Go 单元测试

覆盖：

1. 正确筛选 RPC 中 `source=skill` 的条目，并忽略 extension command。
2. 正确解析 `sourceInfo.scope` 为 global/project/path。
3. 缺少路径、空名称和 malformed RPC 响应被拒绝。
4. frontmatter 缺失字段时读取为 enabled。
5. `disable-model-invocation: true/false` 读取正确。
6. 单引号 key、双引号 key、缩进 key、LF/CRLF/CR 和 BOM 保持格式。
7. 启用时删除字段，禁用时插入或替换字段，且正文和其它字段不变。
8. inline frontmatter、重复 key 和缺失结束 fence 不会被静默改写。
9. canonical path、软链接和 workspace policy 校验有效。
10. PATCH 不能写入未被本次发现的任意文件。
11. 普通用户不能修改 global/path skill，能够按规则修改 project skill。
12. Pi 超时、Pi 不可用、文件删除和并发修改返回预期错误。

测试使用临时目录和固定 RPC fixture，不依赖网络、真实用户 home 或在线服务。

### 前端测试

覆盖：

1. 左下角同时渲染人员管理和技能管理入口。
2. 没有 active workspace 时技能按钮禁用。
3. 点击按钮只请求当前 workspace 的 skills endpoint。
4. global/project/path 分组和 enabled 状态正确渲染。
5. 点击 skill 后详情正确显示。
6. toggle 成功后更新状态，失败后保留旧状态并显示错误。
7. 旧 workspace 请求返回时不会覆盖当前 workspace。
8. `available:false`、空列表、403、404、409 和 504 显示可理解提示。
9. Escape、遮罩和关闭按钮可关闭面板并恢复焦点。
10. Chrome 107 兼容测试确认新增 CSS 不含 `color-mix()`、`100dvh` 和其它超出基线的特性。

### 验证命令

实现阶段至少运行：

```bash
bun run lint
bun run typecheck
bun run test
bun run backend:test
bun run build
bun run check:embedded-assets
```

如果更新了 `static/` 嵌入资源，必须确认 `git diff` 只包含本功能相关的构建产物，并运行 `bun run build:single` 验证单二进制嵌入结果。

## 实施顺序

1. 先实现并测试 Go RPC skill 发现和 frontmatter 读写纯逻辑。
2. 增加 workspace skill API 和账号/路径安全测试。
3. 增加前端 API client、面板静态结构和渲染/交互测试。
4. 将入口接入左下角人员管理旁边，并补充 focus、移动端和错误状态。
5. 运行完整前端、后端、构建和嵌入资源验证。
6. 检查 diff、工作区状态和生成的静态资源后再提交实现。

## 验收标准

- 在已信任工作区中可以看到 Pi 实际发现的 skills，并能区分 global/project/path。
- 启用/禁用后，`SKILL.md` 只有目标 frontmatter 字段发生变化。
- Web 刷新、Pi CLI 和新的 Pi 会话看到相同的 skill 状态。
- 未信任项目不会因为打开面板而被自动信任。
- 非管理员不能修改全局 skill。
- Pi 不可用时界面不会白屏，并给出明确原因。
- Chrome 107 下列表、详情、开关、弹窗关闭和移动端布局可用。
- 完整验证命令通过，且 release 静态资源与源码同步。
