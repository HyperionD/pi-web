# Task 2 报告：移除 picker 页面并接入共享根层 popover

## 文件

实现提交 `7a7ef3f`（`feat: open workspace picker inline`），修改：

- `src/App.astro`：移除 picker section、recent workspace card 和相关 route props；将 folder popover 提升为 `pi-app` 根层；workspace body 始终渲染。
- `src/App.stories.ts`：WorkspacePicker 改为 workspace + empty session。
- `src/pi-app/input/input-methods.ts`：route-picker/folder-open 直接打开或关闭 popover，并传递有效 click event。
- `src/pi-app/index.ts`：outside-click 豁免 sidebar route-picker trigger。
- `src/pi-app/status/layout-methods.ts`：picker route 归一为 workspace，不再访问 picker section 或 focus picker trigger。
- `src/pi-app/workspace/app-config-methods.ts`：allowed-root browse 传递 click event；恢复逻辑不再依赖 picker section。
- `src/pi-app/workspace/workspace-bootstrap-methods.ts`：403 和无 active session/workspace 时恢复 workspace + empty main。
- `src/pi-app/workspace/workspace-folder-methods.ts`：加入 trigger 定位、viewport 内 popover 定位、共享 trigger focus，以及 allowed-root 根层流程。

`src/pi-app/test-helper.ts` 已由 Task 1 提供 root-level popover、sidebar trigger、empty main，本次无需产生差异。后端和用户配置逻辑未修改；`static` 现有工作区改动未暂存、未修改。

## 验证实际输出

- `bunx vitest run src/pi-app/core-events.test.ts src/pi-app/input/input-actions-coverage.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/workspace/workspace-methods-coverage.test.ts`
  - `Test Files 5 passed (5)`
  - `Tests 72 passed (72)`
- `bun run typecheck`
  - `$ tsc --noEmit`，退出码 0。
- `bun run build`
  - `1 page(s) built`、`Complete!`，退出码 0。
  - 仅有既存的 chunk size warning（chunks larger than 500 kB）。
- `bun run test`
  - `Test Files 35 passed | 2 failed (37)`
  - `Tests 350 passed | 2 failed (352)`。

定向测试覆盖并通过 allowed-root 返回 settings/focus、outside click 保留 trigger、stale folder loading/opening 和 forbidden recovery。

## Concerns

完整前端测试中的 2 个失败位于未修改的 session 测试：Task 1 fixture 新增 empty main 后，旧测试再追加一个同类 main，而 session display 方法使用首个匹配节点，导致 `session-methods-display.test.ts` 与 `sessions.test.ts` 的 empty main hidden 断言失败。该问题在本次定向 Task 1 测试中不存在，本次未扩展范围修复。

## Reviewer findings follow-up

- `bunx vitest run src/pi-app/input/input-actions-coverage.test.ts src/pi-app/core-events.test.ts src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/input/controls.test.ts`
  - `Test Files 6 passed (6)`
  - `Tests 92 passed (92)`
- `bun run test`
  - `Test Files 37 passed (37)`
  - `Tests 353 passed (353)`
- `bun run typecheck`
  - `$ tsc --noEmit`，退出码 0。

修复了旧 session 测试的重复 empty main、`(0,0)` click event 传递、键盘/零坐标 trigger fallback 与真实 sidebar trigger 的 ARIA 属性；未修改 `static/` 或用户配置。
