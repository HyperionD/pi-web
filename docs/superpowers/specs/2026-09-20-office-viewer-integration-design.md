# File Viewer Office 预览接入设计

## 目标

在保持现有 pi-web 文件预览行为不变的前提下，按需接入 `flyfish-dev/file-viewer`，为 DOCX/XLSX/PPTX 及其兼容 Office 路径提供浏览器内预览，并以 Chrome 107、离线部署和 Go 单二进制为交付约束。

## 范围

### 本次实现

- 使用 `@file-viewer/web`，不使用 `@file-viewer/web-full`。
- 使用 `@file-viewer/preset-office` 作为 Office renderer 集合。
- 仅在 Office 文件命中时懒加载 Office viewer；普通文本、Markdown、图片、音视频、HTML、PDF 继续使用现有实现。
- 复用现有 `/files/raw` 接口，浏览器端以同源 URL 或带凭据的自定义 fetch 提供文件源。
- 运行时资源完全自托管并进入 `static/`，通过现有 Go embed 交付。
- 明确的加载失败、资源缺失、文件过大和不兼容提示，并保留原文件下载入口。

### 不在本次实现

- 不接入 `@file-viewer/web-full`、CAD、3D、压缩包、Adobe、iWork 等非 Office 能力。
- 不替换当前 PDF renderer。
- 不实现 Office 编辑、转换、服务端转码或在线第三方预览。
- 不承诺 Chrome 107 下所有历史 DOC/XLS/PPT 样本的高保真；首轮以 DOCX/XLSX/PPTX 为验收重点。

## 架构

文件预览仍由当前 `file-preview-methods.ts` 统一调度。新增 `office-preview.ts` 负责文件类型判断、viewer 动态导入、controller 创建/销毁和错误归一化；`file-preview-methods.ts` 只负责在 Office 分支委托它，不直接耦合第三方 renderer。

Office viewer 接收同源 raw URL。由于 File Viewer 的默认 source loader 会读取完整文件，Office 文件设置独立大小上限；跨源 API 场景使用 `credentials: 'include'` 的 `fetchFile` 适配器，并将响应转换为 `File`。现有 Go raw 接口继续负责路径安全、MIME、Range、Content-Disposition 和访问控制。

资源目录只包含 Office preset 所需的 Worker、WASM、字体和 vendor 文件。构建后执行资源一致性检查，防止 Worker/WASM 缺失或被 SPA fallback 返回 HTML。

## Chrome 107 兼容策略

- 发布代码以 `@file-viewer/web` 的 ES2019/ES2020 构建产物为主。
- 禁止依赖 Chrome 107 之后的浏览器 API；不使用 File System Access API、URLPattern、`Array.prototype.toSorted` 等 API。
- 在真实 Chrome 107 中验证 DOCX/XLSX/PPTX、中文字体、Worker、WASM、CSP、子路径和离线加载。
- Office 解析失败必须回退到下载操作，不得阻塞现有预览或文件树。
- 资源必须使用同源静态路径，Worker 返回 JavaScript MIME，WASM 返回 `application/wasm`。

## 安全与资源

- Office 文件 URL 必须由现有 workspace raw API 生成，不能拼接未经校验的用户 URL。
- 不允许 renderer 通过外部 CDN 获取脚本、字体、Worker 或 WASM。
- viewer controller 在文件切换、workspace 切换和关闭预览时销毁；异步加载必须校验 workspace/path 请求身份。
- Office 文件读取大小上限与当前后端 raw/read 限制分开配置，并在 UI 中显示可操作错误。
- 继续保留下载、Git 状态、脏编辑确认和当前非 Office 预览行为。

## 验收标准

1. DOCX/XLSX/PPTX 在 Chrome 107 中可从 workspace raw URL 加载并显示中文内容。
2. 普通文本、Markdown、图片、音视频、HTML、PDF 的现有测试和交互不回归。
3. Office viewer 不请求公网资源，缺少静态资源时显示明确错误并可下载原文件。
4. 快速切换两个文件不会让旧 Office controller 或 Worker 回写当前面板。
5. `bun run lint`、`bun run typecheck`、前端测试、Go 测试、构建和 embedded-assets 检查通过。
