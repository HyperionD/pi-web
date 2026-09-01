# pi-web 内联工作区选择器设计

- 日期：2026-09-01
- 状态：待用户审阅
- 范围：取消全屏 picker 页面，让所有工作区选择都在主窗口内显示 anchored popover

## 1. 背景与目标

当前工作区选择仍保留 `data-view="picker"` 全屏页面。用户已确认希望侧栏 `+ 打开` 直接在当前主窗口弹出目录选择器，不再离开会话布局或进入全屏 picker。

目标：

- 删除全屏 picker 页面及普通 picker route 的视觉切换。
- 侧栏 `+ 打开` 直接显示共享目录 popover。
- popover 以点击坐标为锚点，在视口内自动避让，保持合适尺寸和独立滚动。
- 没有工作区时显示现有主窗口空状态，不自动弹出 popover；用户点击 `+ 打开` 后选择目录。
- 设置中的 allowed-root 选择继续使用同一 popover，但保留 `purpose=allowed-root` 和返回设置焦点。
- 403 recovery 回到主窗口空状态并清理 popover 状态。

## 2. 交互与布局

### 2.1 共享 popover

将 `[data-folder-popover]` 从 picker card 提升到 `pi-app` 根层，使其不受 picker/工作区视图隐藏影响。popover 使用 `position: fixed`，打开时接收触发事件的 `clientX/clientY`；键盘触发或坐标不可用时使用触发按钮的 bounding rect。位置计算把菜单限制在视口内，避免右侧或底部溢出。

popover 保留标题、当前路径、上一级、目录列表、加载/错误/空状态和“选择此文件夹”。目录数据使用安全文本节点渲染。popover 外点击和 Escape 关闭，并恢复原触发按钮或设置添加按钮焦点。

### 2.2 触发入口

- 侧栏 `[data-action="route-picker"]` 改为直接调用 workspace popover，不再 `route("picker")`。
- popover 自身的“选择文件夹”按钮继续打开同一 workspace popover。
- tweaks 中的“工作区选择器”使用相同直接弹出行为。
- 最近工作区按钮继续直接打开已有 workspace。
- 没有工作区时保留 `data-main="empty"` 主窗口；`+ 打开` 仍可用，不自动弹出目录选择器。

### 2.3 Route 与状态

保留 `route("workspace")` 作为唯一主视图 route。删除或停用 `data-view="picker"` 的全屏切换逻辑；route picker action 不再改变 route。普通 bootstrap 不因无 workspace 自动 browse 或自动打开 popover。

当历史 workspace 因 403 不可访问时，清空 active workspace/session、关闭 popover并显示主窗口 empty 状态；不再把用户送入全屏 picker。设置 allowed-root 的临时浏览仍可使用共享 popover，完成/取消后恢复设置 modal。

## 3. 数据流与安全

普通 workspace 触发器调用 `openFolderPopover("workspace", event)`，首次请求 `listFolders("~", "workspace")`。选择当前目录沿用 `openWorkspacePath` 与 opening token；旧请求失效时不得激活错误 workspace。

allowed-root 触发器调用 `browseFolder("allowed-root")`，服务端仍是最终授权边界。前端只负责展示服务端返回的允许根目录和子目录；open、workspace/session API 的后端 policy 不变。

popover 位置只影响 UI，不影响提交给后端的路径。路径、目录名、错误和状态必须通过 `textContent` 或等价 DOM API 写入，不使用服务端值拼接 HTML。

## 4. 实现边界

需要修改：

- `src/App.astro`：移除全屏 picker section，将共享 popover 放到 `pi-app` 根层；保留主窗口 empty state。
- `src/pi-app/input/input-methods.ts`：route-picker 直接打开 popover，并传递点击事件。
- `src/pi-app/workspace/workspace-folder-methods.ts`：增加坐标定位、视口避让和共享 popover trigger 管理。
- `src/pi-app/status/layout-methods.ts`：route 只处理 workspace 主视图，Escape/关闭不再依赖 picker page。
- `src/pi-app/workspace/workspace-bootstrap-methods.ts`：403 recovery 使用主窗口 empty state，不切换全屏 picker。
- `src/pi-app/workspace/app-config-methods.ts`：allowed-root popover 返回设置时不依赖 picker section。
- `src/styles/parts/workspace-picker.css`：固定定位、尺寸、滚动和窄屏样式。
- 相关 Vitest fixture 与 workspace/input/bootstrap/core tests。

不修改：

- Go folder/workspace policy、API contract、数据库和用户配置。
- 后端 clone API；前端 clone/path 入口仍保持删除。
- `static/` 构建产物。

## 5. 测试策略

前端测试覆盖：

- 侧栏 `+ 打开` 不改变 `data-route`，直接显示 popover 并请求 `purpose=workspace`。
- popover 以事件坐标打开，并在视口边界内修正位置；键盘触发使用按钮 rect。
- 无 workspace 时显示主窗口 empty state，点击 `+ 打开` 后可选目录，且不自动弹出。
- picker section 不再存在或不再参与普通 route；route picker action 不触发全屏切换。
- allowed-root 添加、完成、取消后恢复 settings modal 和焦点。
- Escape、外部点击、403 recovery、重复打开和 stale open 不残留 hidden/aria 状态。
- 文本安全渲染和移动端滚动保持有效。

验证命令：

```bash
bun run test
bun run typecheck
PI_CODING_AGENT_SESSION_DIR="$(mktemp -d)/sessions" bun run backend:test
bun run build
```

build 生成的 `static/` 变化不得加入提交。

## 6. 回滚

恢复 `data-view="picker"` section、`route("picker")` 的显示逻辑和旧触发器处理即可回滚；无数据迁移和后端接口变更。
