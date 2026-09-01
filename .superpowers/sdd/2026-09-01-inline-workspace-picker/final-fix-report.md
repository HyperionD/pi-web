# Inline Workspace Picker bounded 修复报告

## 修复范围

- `openWorkspacePath` 优先使用 open API 返回的目标 workspace；不再错误回退到列表第一项。
- 为 workspace bootstrap 增加 generation 守卫；旧 bootstrap、旧 app-config 成功结果不会覆盖新状态。会话切换也会使未完成 bootstrap 失效。
- refresh 返回空列表或 allowed-root 过滤掉当前 workspace 时，清理 active workspace/session、持久化 session、事件流、workspace tree，并显示 empty view。
- 403 recovery 清理 active workspace/session 文本，避免 empty view 仍显示旧上下文。
- folder browser 改为 flex column；目录列表可收缩滚动，确认按钮保持可见。
- allowed-root 添加按钮补齐 dialog ARIA，并在打开/关闭时同步 `aria-expanded`。
- 未修改 backend、`static/` 或 user config。

## TDD 证据

先添加并运行回归测试，旧实现失败于目标 workspace、stale bootstrap、refresh cleanup、403 文本、allowed-root ARIA 和 folder layout；随后实现最小修复并验证通过。

## 最终验证

- 定向测试：`7` 个文件、`126/126` 通过。
- `bun run test`：`37` 个文件、`362/362` 通过。
- `bun run typecheck`：通过。
- `git diff --check -- . ':(exclude)static'`：通过。
