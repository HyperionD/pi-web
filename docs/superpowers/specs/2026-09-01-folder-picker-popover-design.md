# pi-web 打开文件夹 Popover 设计

- 日期：2026-09-01
- 状态：待用户审阅
- 范围：将工作区打开入口改为锚定目录选择 popover，移除手动路径和 Git clone UI

## 1. 背景与目标

当前 picker 同时展示手动路径输入、目录浏览器和 Git clone 表单，入口分散且在受限目录模式下不符合“直接选择文件夹”的交互目标。本变更将“打开文件夹”收敛为一个锚定 popover，让用户从允许根目录开始逐级选择并打开工作区。

目标：

- 点击“打开文件夹”按钮后展示锚定 popover。
- popover 首次展示允许的根目录；无限制时展示现有目录列表。
- 点击目录进入下一层，点击“选择此文件夹”打开当前目录。
- 支持“上一级”、Escape 和点击外部关闭。
- 移除手动路径输入和 Git clone 的前端入口。
- 保留最近工作区列表。
- 继续由后端 folder/open API 执行最终路径和 workspace policy 校验。

非目标：

- 不改变 `workspace.allowedRoots` 语义、后端授权逻辑或目录 API。
- 不删除后端 Git clone API，以兼容已有 API 调用；clone 不再由当前 UI 暴露。
- 不引入浏览器原生目录选择器、SQLite 或浏览器存储。

## 2. 交互设计

### 2.1 Picker 入口

现有“打开文件夹”卡片保留标题，但移除路径 form 和 clone form。卡片内使用一个按钮触发 popover；按钮文案为“打开文件夹”，并带有目录图标或现有一致的操作图标。

最近工作区卡片保持现有行为，可继续直接打开历史工作区。

### 2.2 Popover 内容

Popover 锚定在触发按钮附近，使用现有 workspace picker 的视觉 token，不遮挡整个页面。内容包括：

1. 标题“选择工作区”。
2. 当前目录显示文本；路径只作为文本渲染。
3. “上一级”按钮；不可再上级时禁用或隐藏。
4. 目录列表，每项是可聚焦 button，显示名称和规范化显示路径。
5. “选择此文件夹”确认按钮。
6. 加载中、空目录和错误状态。

开启限制时，`path=~&purpose=workspace` 返回的允许根目录作为首屏入口；点击根目录后只浏览该根目录及子目录。无限制模式保持现有 `~` 浏览能力。Popover 不提供手工输入路径。

### 2.3 关闭与键盘

- 点击触发按钮切换 popover。
- Escape 关闭 popover，并将焦点还给触发按钮。
- 点击 popover 外部关闭。
- 打开后将焦点放到目录列表首项；目录项支持 Enter/Space；确认按钮支持 Enter。
- 打开或导航期间禁用重复请求，错误时恢复可操作状态。

## 3. 前端组件与数据流

沿用现有 `listFolders(path, purpose)` 和 `openWorkspace(path)` API：

1. `openFolderPopover()` 设置 `folderBrowsePurpose = "workspace"`，显示 popover。
2. 首次打开调用 `loadFolder("~", "workspace")`。
3. `renderFolderListing()` 只更新 popover 内的文本节点和目录 button。
4. 目录 button 调用 `loadFolder(folder.path, "workspace")`。
5. 确认按钮调用 `openWorkspacePath(currentFolder)`；成功后关闭 popover并进入工作区，失败显示现有错误并执行现有 forbidden recovery。
6. 取消、Escape 或外部点击只关闭 popover，不改变当前 workspace。

删除前端 `data-path-form`、`data-clone-form` 标记及其事件绑定、submit 方法和无 UI 用途的 clone import。后端 clone API 与后端测试保留。

## 4. 后端与安全边界

不新增后端接口。普通 picker 始终使用 `purpose=workspace`。后端继续负责：

- 受限模式下返回允许根目录入口和范围内子目录。
- 拒绝越界、相邻目录和 symlink 越界路径。
- 校验 open 目标真实路径；前端 popover 不能绕过授权。

路径、目录名、错误和状态使用纯文本节点渲染，不将服务端路径插入 HTML。

## 5. 测试策略

前端测试覆盖：

- 打开/关闭 popover、初始加载 `~`、目录进入和选择当前目录。
- Escape、外部点击、加载中和错误状态。
- 手动路径与 clone 表单不再出现在 picker，且不再注册对应 submit 事件。
- 选择成功进入 workspace，403 继续恢复到 picker。

后端不新增行为测试；保留现有 folder policy、workspace open 和 clone 授权测试，确保 UI 重构不改变安全边界。

验证命令：

```bash
bun run typecheck
bun run test
go test ./...
bun run backend:test
bun run build
```

## 6. 回滚

恢复 picker 的旧 HTML、事件绑定和 folder methods 即可回滚 UI；后端 API 和 workspace policy 无迁移，因此无需数据回滚。
