# 文件预览能力增强设计

## 目标

在不实现 Microsoft Office 文件在线预览的前提下，完善 pi-web 的工作区文件查看体验，并保持 Chrome 107、Go 单二进制、离线部署和现有工作区安全边界兼容。

## 范围

### 包含

- 图片、音频、视频和 PDF 文件预览。
- 图片及媒体使用流式资源 URL，支持 HTTP Range。
- 文本文件分块读取和继续加载。
- 文件大小、MIME、语言、行数等元信息展示。
- 已打开文件的外部变更自动刷新。
- Markdown 工作区内文件链接和 PDF 页码链接。
- HTML 沙箱预览。
- Source / Preview / Diff 文件查看模式。
- Git 新增、修改、删除文件的 Diff 查看。
- 文件树目录按需加载。
- 预览模式、滚动位置和换行设置恢复。
- Chrome 107 兼容性测试和降级提示。

### 不包含

- DOCX、XLSX、PPTX、DOC、XLS、PPT 在线预览。
- Office 转换服务、WASM Office 引擎或外部在线 Office 服务。
- 与文件预览无关的重构。

## 现状问题

当前 `ReadWorkspaceFile` 固定读取 256 KiB，并将小图片编码成 JSON Base64；`previewKindForMIME` 只把文本和图片视为可预览类型。前端预览状态只覆盖文本、Markdown 和图片，没有媒体、PDF、HTML、分块读取、文件 watch 或 Diff 模式。文件树由后端一次性返回且固定展开深度。

## 设计

### 1. 文件 API

保留现有 `/api/workspaces/{workspaceID}/files/read` 作为文本和元信息 API，扩展响应字段：

- `content`: 当前文本分块。
- `offset`: 当前分块起点。
- `nextOffset`: 下一分块起点；无更多内容时省略或为文件大小。
- `size`: 文件总字节数。
- `truncated`: 是否仍有未读取内容。
- `mime`、`language`、`previewKind`。

新增或扩展资源接口，用于图片、音频、视频、PDF 和下载：

- 正确设置 `Content-Type`。
- 预览使用 `inline`，下载使用 `attachment`。
- 支持 `Range`、`Accept-Ranges`、`Content-Length`。
- 图片设置明确大小上限；媒体和 PDF 不通过 JSON Base64 返回。
- 延续 `SafeJoin`、工作区访问检查和符号链接防逃逸策略。

新增文件元信息接口或等价查询，用于前端刷新后只比较版本信息，避免无变化时重复加载内容。

### 2. 文件变更通知

服务端提供按文件订阅的 SSE 接口。服务端使用现有 Go 文件访问边界，并通过文件系统事件或受控轮询判断文件的 mtime、size、inode 变化；只有确认内容版本变化时发送事件。客户端在切换文件、关闭预览或组件销毁时关闭 `EventSource`。

刷新规则：

- 查看模式：自动重新加载当前文件。
- Markdown/HTML/媒体：刷新资源 URL，避免浏览器缓存旧内容。
- 编辑模式且无脏数据：自动刷新。
- 编辑模式且有脏数据：不覆盖用户内容，显示外部修改提示，并提供重新加载动作。
- 保存完成后更新当前版本基线。

### 3. 前端预览模式

将文件预览状态扩展为明确的显示模式：

- `source`: CodeMirror 文本查看/编辑。
- `preview`: Markdown 或 HTML 渲染。
- `diff`: Git 差异查看。
- 媒体/PDF 使用专用只读查看器。

文本文件仍保留当前 CodeMirror 编辑、搜索、保存、脏状态、离开确认和 Git gutter 行标记。大文件在未加载完整前只读，不允许保存。

Markdown：

- 相对图片继续通过受保护的工作区资源 URL加载。
- 相对文件链接解析到工作区内，并通过应用内打开。
- PDF 链接支持 `#page=N`。
- 外部链接保持新窗口行为。
- 渲染 HTML 时继续禁用不必要的原始 HTML；HTML 文件本身使用 sandbox iframe 预览。

Diff：

- 使用后端 Git diff 数据。
- 支持新增、修改、删除文件。
- 变更附近显示上下文，远处未变更区域可折叠。
- 没有 Diff 时不显示 Diff 模式。

### 4. 文件树

根目录只返回当前层级。展开目录时按需请求子目录；刷新时只刷新已展开目录。保留现有 Git 状态、搜索、上传、下载和选中项行为。搜索结果仍可构造成临时树，不要求预先加载全部目录。

### 5. 状态恢复和操作

预览状态按工作区和文件路径保存：

- 显示模式。
- 是否换行。
- `scrollTop` / `scrollLeft`。

预览头部显示文件路径、类型和大小，并提供下载动作。资源加载失败时显示具体错误和下载回退。

### 6. Chrome 107 约束

实现只依赖 Chrome 107 已支持的能力：Fetch/XHR、EventSource、Range、Blob/ArrayBuffer、iframe sandbox、HTML media element、ResizeObserver 和 CodeMirror 所需 DOM API。不得依赖 File System Access API、URLPattern 或更新版本专属 API。

PDF iframe 不使用会阻止 Chrome 内置 PDF Viewer 的严格 sandbox；HTML 和生成的 Markdown 内容必须保持隔离，不允许把不可信 HTML 直接插入主应用。

## 错误处理

- 非法路径、目录、越界 offset、无权限和符号链接逃逸返回明确 HTTP 错误。
- 不支持的 MIME 显示“不支持预览”，同时提供下载。
- 超过文本预览限制时提供继续加载，不静默截断。
- 媒体不受浏览器支持时显示播放失败和下载按钮。
- SSE 断开时显示静态状态，不阻塞文件查看；浏览器重连后重新同步元信息。
- 预览请求通过请求序列号或 AbortController 丢弃过期响应。

## 测试验收

### Go

- 文本分块读取、offset 边界、UTF-8 边界和大文件。
- 图片/音频/视频/PDF MIME、Range、Content-Disposition 和大小限制。
- SSE 连接、变化事件、关闭和无效路径。
- Git Diff 新增、修改、删除文件。
- 所有访问控制、路径遍历和符号链接测试继续通过。

### 前端

- 各类文件模式选择和回退。
- 加载更多、过期响应和自动刷新。
- 脏编辑状态下外部变更不覆盖内容。
- Markdown 内部链接、图片和 PDF 页码。
- Diff 模式及无 Diff 情况。
- 文件树懒加载、搜索、刷新和选中状态。
- 预览状态恢复。

### 验证命令

```bash
bun run lint
bun run typecheck
bun run test
bun run backend:test
git diff --check
```

另使用 Chrome 107 运行至少一组媒体、PDF、Markdown 内部链接、HTML sandbox 和 SSE 刷新 E2E 验证。
