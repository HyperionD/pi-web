# pi-web 工作区允许目录限制设计

- 日期：2026-09-01
- 状态：待用户审阅
- 范围：为 pi-web 增加用户级 `workspace.allowedRoots` 限制，工作区只能选择配置根目录及其子目录

## 1. 背景与目标

pi-web 已有用户级 `pi-web-config.json` 和独立 `/api/app-config` API。本功能在该配置中增加工作区允许目录列表，使工作区选择范围由当前操作系统用户配置，并由后端作为最终安全边界执行。

目标：

- 支持多个允许根目录；每个根目录及其后代目录均可作为工作区。
- 前端目录选择器只改善体验，不能绕过后端限制。
- 目录限制同时适用于目录浏览、打开工作区、Git clone 和已有工作区/会话访问。
- 旧配置缺少 `workspace.allowedRoots` 或列表为空时保持不限制，避免升级后锁死已有用户。
- 通过真实路径比较处理 `~`、路径清理、目录边界和符号链接越界。

非目标：

- 不实现操作系统级沙箱；shell 命令仍由现有 pi 执行模型负责。
- 不改变 SQLite schema 或 pi 原生 `settings.json`、`auth.json`。
- 不把限制逻辑放在浏览器端作为唯一安全边界。

## 2. 配置格式与语义

配置示例：

```json
{
  "version": 1,
  "brand": "JuunAI / web",
  "workspace": {
    "allowedRoots": ["~/code", "/workspaces"]
  }
}
```

规则：

- `workspace.allowedRoots` 是字符串数组。
- 缺少 `workspace`、缺少 `allowedRoots` 或 `allowedRoots: []` 均表示未启用限制，保持当前无限制行为。
- 非空数组启用限制。每个路径必须非空、可展开、存在且为目录；无效项使整个 PATCH 失败，旧配置保持不变。
- 保存时对路径执行：去除首尾空白、展开 `~`、转为绝对路径、`Clean`，再解析符号链接为真实目录；真实路径去重后保存。
- 配置读取到无效的非空列表时不得降级为无限制；工作区相关请求拒绝并返回可读错误，应用配置 API 仍报告配置错误。
- 未识别的顶层配置字段继续保留。更新工作区设置不得清除 `brand` 或未来字段。
- 删除全部根目录会保存空数组，并恢复无限制行为。

## 3. 真实路径与符号链接策略

后端新增独立的 workspace policy 模块，所有判断使用规范化绝对真实路径：

1. 展开 `~`，清理路径并转换为绝对路径。
2. 对已存在的路径调用 `EvalSymlinks`。
3. 使用目录边界比较：候选路径必须等于根目录，或以 `root + separator` 开头；不能仅使用字符串前缀。
4. 根目录本身必须是目录。
5. `open` 目标必须存在且解析后的真实目录位于某个允许根目录下。
6. `clone` 目标可能尚不存在：先解析并校验父目录，目标名称必须是单个安全目录名，最终目标必须位于允许根目录下；若目标已存在，再解析其真实路径校验。
7. 配置根目录自身可以是符号链接，但保存和比较使用其解析后的真实目录；候选符号链接若解析后越界，一律拒绝。

配置目录选择器需要能在限制已启用时添加新的根目录，因此复用 folder API 的明确查询参数 `purpose=allowed-root`。`GET /api/system/folders?purpose=allowed-root` 只返回可供配置的目录项，不创建或打开工作区；普通请求（缺少该参数或 `purpose=workspace`）仍严格受 `allowedRoots` 过滤。配置根目录浏览不得被任何 workspace open/clone 请求复用。

## 4. 后端入口与授权

新增/扩展的策略检查必须集中在后端，不依赖前端传入的信任状态。

### 4.1 目录和工作区入口

- `GET /api/system/folders`：普通 workspace picker（无 `purpose` 或 `purpose=workspace`）只返回允许根目录及其后代；查询越界路径返回 `403`。无限制时保持现有行为。
- `GET /api/system/folders?purpose=allowed-root`：仅供设置界面选择配置根目录，可浏览用户可访问目录，但不能直接打开工作区，也不改变 open/clone 的授权规则。
- `POST /api/workspaces/open`：校验目标真实目录，越界返回 `403`。
- `POST /api/workspaces/clone`：校验 clone 最终目标目录，越界返回 `403`。
- `GET /api/workspaces`：隐藏所有真实路径越界的历史工作区。

### 4.2 已有 workspace 和 session

限制启用后，任何位于允许目录外的历史 workspace 均隐藏并拒绝访问。所有按 workspace ID 的 API 在执行原业务逻辑前检查其真实路径，包括：

- workspace 删除、session 列表/创建/删除；
- 文件树、文件搜索、文件读写创建重命名删除上传；
- commands、models、runtime status/model/quota；
- workspace settings；
- git status/history/commit；
- shell command。

所有按 session ID 的 API 先解析 session 所属 workspace，再执行同一授权检查，包括 session 读取、消息、AG-UI、重命名、删除、压缩、steer、cancel 和事件流。越界统一返回明确的 `403`，不存在资源仍保持现有 `404` 语义。

### 4.3 配置 API

继续使用 workspace-independent 的 `/api/app-config`：

```text
PATCH /api/app-config
{
  "workspace": {
    "allowedRoots": ["/home/user/code", "/workspaces"]
  }
}
```

PATCH 只更新出现的字段并返回完整配置。非法列表返回 `400`；配置文件读写失败返回 `500`。保存配置后策略立即使用新值。若当前前端工作区因此变为越界，后续请求收到 `403`，前端清空当前工作区并回到 picker。

## 5. 前端设置与工作区选择

- 在现有“pi-web 应用设置”区域增加允许目录列表。
- 每行展示一个规范化根目录，支持“添加目录”和删除；添加使用配置根目录 picker，避免手写路径。
- 保存时发送独立 app-config PATCH，不附带 workspace ID，也不改变 project/global pi settings scope。
- 配置读取失败或保存失败显示现有错误状态，不显示成功；品牌与允许目录互不覆盖。
- 普通 workspace picker 收到 `403` 时显示后端可读原因；越界历史工作区不出现在列表。
- 配置未启用时显示“未限制”状态；空列表保存后恢复该状态。
- 所有路径和状态文本使用安全的文本节点更新，不使用 `innerHTML`。

## 6. 错误处理与兼容

- 允许目录配置错误不得静默降级为无限制，防止错误配置意外放宽访问范围。
- 新增错误应包含失败路径和原因，但不得泄露超出当前 API 已有范围的敏感信息。
- 旧用户无 `workspace.allowedRoots` 时所有现有工作区继续可用。
- 限制配置修改后，SQLite 中的历史记录不删除；越界项目只是不再返回或访问。
- 删除 `pi-web-config.json` 恢复 brand 默认值和工作区无限制行为。

## 7. 测试策略

后端单元测试：

- 缺失/空列表无限制；多个根目录匹配自身和子目录，不匹配相邻目录。
- `~` 展开、绝对化、重复根目录去重、非目录和无效路径拒绝。
- 根目录符号链接允许；候选符号链接指向根外拒绝。
- open 合法/越界、clone 合法/越界及目标目录边界。
- folder list 过滤和越界拒绝；workspace 列表过滤历史越界项目。
- workspace ID 和 session ID API 对越界资源返回 `403`，不存在资源仍返回 `404`。
- 配置 PATCH 失败时旧配置不变，brand/未知字段不丢失。

前端测试：

- 设置界面添加、删除、保存多个允许根目录，且与 pi settings scope 独立。
- 普通 picker 处理 `403`；越界当前工作区清空并返回 picker。
- 无限制、限制启用和配置错误状态的显示。
- 路径文本安全渲染，不产生 HTML 元素。

验证命令：

```bash
bun run typecheck
bun run test
go test ./...
bun run backend:test
bun run build
```

## 8. 回滚

删除 `workspace.allowedRoots` 或保存空数组即可恢复无限制；删除整个 `pi-web-config.json` 可恢复所有应用配置默认值。回滚不修改 SQLite 或 pi 原生配置文件。
