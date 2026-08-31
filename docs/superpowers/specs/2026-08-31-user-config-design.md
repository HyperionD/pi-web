# pi-web 用户级应用配置设计

- 日期：2026-08-31
- 状态：待用户审阅
- 范围：首个配置项为左上角 brand；建立可扩展的 pi-web 用户级配置边界

## 1. 背景与目标

pi-web 需要保存不应写入 pi 原生配置、项目目录或浏览器会话的应用级设置。首个需求是配置左上角 brand，后续可能增加工作区目录选择范围等安全相关设置。

目标：

- 配置按当前操作系统用户持久化，同一用户启动的 pi-web 进程共享。
- 不复用、不修改 pi 的 `settings.json`、`auth.json` 等原生配置文件，也不写入项目 `.pi` 配置。
- 配置可被网页读取和修改，也可被用户手动查看或修复。
- API 不依赖已打开的 workspace，使 brand 在 picker 和 workspace 页面都能加载。
- 后端保留最终校验边界，为后续工作区目录限制提供统一入口。

非目标：

- 本次不实现工作区允许目录限制，只预留配置扩展方式。
- 不把应用配置写入 `localStorage`、`sessionStorage` 或 URL。
- 不把应用配置混入现有 pi settings API。

## 2. 配置存储

配置文件固定使用：

```text
<pi session 目录的父目录>/pi-web-config.json
```

默认情况下：

```text
~/.pi/agent/pi-web-config.json
```

路径由现有 `DefaultPiSessionDir()` 推导，因此设置 `PI_CODING_AGENT_SESSION_DIR` 后，配置文件仍与 `pi-web.db` 位于同一目录。文件名使用 `pi-web-config.json`，避免与 pi 原生的 `settings.json`、`auth.json` 等文件重名。

首版文件内容：

```json
{
  "version": 1,
  "brand": "pi.dev / web"
}
```

约束：

- 文件和父目录分别使用 `0600`、`0700` 权限。
- 文件不存在时返回默认配置，不在启动时强制创建空文件。
- 写入采用同目录临时文件、写完同步并替换的方式，避免进程中断造成半个 JSON 文件。
- 已知字段进行类型和范围校验；未识别字段保留，保证未来版本和人工编辑不会被无意抹除。
- `brand` 去除首尾空白，允许 Unicode 文本，拒绝空值以外的控制字符，最大长度为 120 个 Unicode 字符。提交 `null` 或空白值表示恢复默认 brand 并移除显式字段。

## 3. 后端 API

新增独立应用配置 API：

```text
GET   /api/app-config
PATCH /api/app-config
```

读取响应：

```json
{
  "config": {
    "version": 1,
    "brand": "pi.dev / web"
  },
  "path": "~/.pi/agent/pi-web-config.json"
}
```

更新请求：

```json
{
  "brand": "JuunAI / web"
}
```

更新接口使用 patch 语义：只修改请求中出现的字段；`brand: null` 删除显式值并恢复默认值。响应返回写入后的完整配置，便于前端以服务端结果刷新状态。

读取或保存配置失败时返回明确的 HTTP 错误和可读错误信息，不静默吞错。非法 brand 返回 `400`；文件系统错误返回 `500`。API 不需要 workspace ID。

配置读写放在独立的应用配置模块中。现有 SQLite 继续只管理最近打开的 workspace，不新增配置表。

## 4. 前端数据流与界面

### 启动与切换

1. `PiApp` 启动时优先调用 `GET /api/app-config`，成功后用 `textContent` 更新 `[data-brand]`。
2. brand 加载失败不阻塞 workspace 列表；页面继续显示默认 brand，并将错误交给连接/设置状态处理。
3. workspace 切换不重新读取用户级 brand；保存成功后所有页面区域立即使用服务端返回值。
4. brand 始终作为纯文本渲染，不能通过 `innerHTML` 注入标记或脚本。

### 设置界面

现有 pi settings 弹窗增加独立的“pi-web 应用设置”区域：

- 文本输入：品牌（brand）。
- 明确显示作用域为“用户级”，不受 project/global 的 pi settings scope 下拉框影响。
- 显示配置文件路径。
- 与现有 pi settings 字段分开加载、校验和保存。
- 即使没有 workspace，应用配置仍可读取和保存；pi settings 在没有 workspace 时保持现有不可用行为。
- 保存时分别提交应用配置和 pi settings；任一失败都显示错误，不把失败误报为成功。应用配置成功后立即更新左上角 brand。

默认 brand 的视觉表现保持当前布局；自定义值整体显示在左上角，并保留光标块装饰。brand 输入只承担文本，不支持 HTML、Markdown、图片 URL 或链接。

## 5. 后续工作区限制扩展点

未来配置可扩展为：

```json
{
  "version": 1,
  "brand": "JuunAI / web",
  "workspace": {
    "allowedRoots": ["~/code", "/workspaces"]
  }
}
```

实现工作区限制时，后端必须在以下入口统一校验规范化后的绝对路径：

- 打开已有 workspace
- Git clone 目标目录
- 任何新增 workspace 的 API

前端 picker 只用于改善提示和减少无效请求，不能作为安全边界。路径比较需要处理 `~`、清理路径、目录边界和符号链接策略，并为拒绝请求返回明确原因。本次不实现该规则。

## 6. 测试策略

后端：

- 默认路径与 `PI_CODING_AGENT_SESSION_DIR` 自定义路径推导测试。
- 不存在配置文件时返回默认 brand。
- 合法 brand 的读取、保存、删除恢复默认测试。
- 非法类型、空白/控制字符、超长 brand 返回校验错误。
- 文件权限、目录创建和原子写入失败路径测试。
- API 在无 workspace 上下文时仍可 GET/PATCH，并验证错误状态码。
- 验证应用配置不会修改 pi 原生 settings 文件或 SQLite workspace 记录。

前端：

- API 请求地址不包含 workspace ID，PATCH body 仅包含应用配置字段。
- 启动后将服务端 brand 作为纯文本写入顶部 brand 节点。
- 保存后即时更新 brand，空值恢复默认值。
- API 失败时保留默认值并展示错误状态。
- brand 中包含 HTML 字符时只显示文本。
- 设置弹窗中应用配置 scope 与 pi settings scope 相互独立。

验证命令：`bun run typecheck`、`bun run test`、`bun run backend:test`、`bun run build`。

## 7. 兼容与回滚

- 现有 `~/.pi/agent/pi-web.db` 不迁移、不改变 schema。
- 现有 pi 原生配置文件不读写 brand，也不做自动迁移。
- 删除 `pi-web-config.json` 即可恢复全部应用配置默认值。
- 如果新配置文件损坏，API 返回错误并继续使用内存中的默认配置；设置界面提供错误反馈，不覆盖原文件。
