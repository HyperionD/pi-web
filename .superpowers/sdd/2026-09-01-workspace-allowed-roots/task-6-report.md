# Task 6 报告

## 修改文件

- `src/pi-app/workspace/workspace-folder-methods.ts`：普通 picker 使用 `purpose=workspace`；支持 `allowed-root` purpose；展示后端文件夹错误；工作区列表应用允许根过滤。
- `src/pi-app/workspace/workspace-methods.ts`：增加错误文本提取与 forbidden 判断辅助函数。
- `src/pi-app/workspace/workspace-bootstrap-methods.ts`：过滤越界工作区；处理工作区/会话 403，清空 active workspace/session、停止相关加载并返回 picker；保留服务端错误 toast。
- `src/pi-app/workspace/workspace-methods-coverage.test.ts`：新增 purpose、403 错误可见、允许根过滤测试。
- `src/pi-app/workspace/workspace-navigation.test.ts`：新增工作区 forbidden recovery 测试。
- `src/pi-app/workspace/workspace-bootstrap-coverage.test.ts`：新增会话 forbidden recovery 测试。

未修改 `static`、后端、Task 5 app-config methods/markup；`src/pi-app/index.ts` 与 `src/pi-app/input/input-actions-coverage.test.ts` 无需变更。

## TDD 与验证

- 红灯：
  - `bunx vitest run src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/input/input-actions-coverage.test.ts -t 'allowed|forbidden|purpose|picker'`
  - 结果：新增行为测试 6 项失败，原因分别为缺少 purpose、错误未显示、越界工作区仍渲染、403 未返回 picker。
- 绿灯 focused：同上命令；结果 `3 passed, 1 skipped`，`8 passed, 21 skipped`。
- 完整 focused frontend：
  - `bunx vitest run src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/input/input-actions-coverage.test.ts`
  - 结果：`4 passed`，`29 passed`。
- TypeScript：
  - `bun run typecheck`
  - 结果：`tsc --noEmit` 通过。
- 目标文件 `git diff --check`：通过。

## Commit

`c178011 feat(ui): recover from restricted workspaces`

## Concerns

- 工作区当前已有未触碰的 `static` 构建产物改动；它们未纳入本次 commit。
- 对全部工作区执行 `git diff --check` 会报告既有 `static/index.html` 尾随空格，本次目标文件无该问题。

## Fix Round 1

### 修复

- settings 的“添加目录”真实调用 `browseFolder("allowed-root")`；复用 picker 浏览器，选择当前目录后写回 allowed-root 行并返回 settings。
- 新增 `renderFilteredWorkspaces`，统一 bootstrap、open、clone、delete、refresh 的过滤后渲染路径。
- clone 仅从已过滤 workspace 列表中解析响应，不直接信任响应对象。
- workspace settings 403 与 settings modal 的 settings 请求复用 forbidden recovery；清空 active workspace/session、关闭 settings、回 picker，并清理持久化 `pi.activeSession`，不删除 SQLite 记录。

### 测试与验证

- 先运行新增回归测试确认红灯：3 个测试失败，分别覆盖真实 root picker、clone 越界响应、settings 403 recovery。
- Task 6 focused：`4 passed`，`33 passed`。
- app-config/session-storage 回归：`2 passed`，`28 passed`。
- TypeScript：`bun run typecheck` 通过。
- 目标文件 `git diff --check`：通过。

### Commit

待提交新 fix commit。

## Fix Round 2

### 修复

- `loadWorkspaceSettingsState` 为每次请求生成 workspace settings token，并同时校验请求 workspace ID 与当前 active workspace；过期或切换后的旧请求不再应用结果，也不触发 forbidden recovery、状态清理或返回 picker。
- 未修改 `static`、后端或 Task 5 app-config 文件。

### TDD 与验证

- 红灯：新增 deferred 旧 workspace 403 回归测试；当前实现收到延迟错误后将 active workspace 清空，测试失败。
- 绿灯回归：`bunx vitest run src/pi-app/workspace/workspace-bootstrap-coverage.test.ts -t 'deferred forbidden settings response'`；结果 `1 passed`。
- Task 6 focused：`bunx vitest run src/pi-app/workspace/workspace-methods-coverage.test.ts src/pi-app/workspace/workspace-navigation.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/input/input-actions-coverage.test.ts`；结果 `4 passed`，`34 passed`。
- TypeScript：`bun run typecheck`；`tsc --noEmit` 通过。

### Commit

本次新 fix commit。
