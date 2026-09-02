# Inline Workspace Picker bounded 修复报告

## 修复范围

- `openWorkspacePath` 优先使用 open API 返回的目标 workspace；不再错误回退到列表第一项。
- 为 workspace bootstrap 增加独立 generation 守卫；所有 load meta/session 的异步边界校验 generation 与 active workspace/session，移除 `loadSession()` 对共享 generation 的覆盖。
- refresh 使用独立 request token；乱序旧结果不会覆盖列表或触发 cleanup。返回空、过滤 active workspace 或 active session 不再属于返回 workspace 时，清理 active workspace/session、持久化 session、事件流、watchers、tree，并显示 empty view。
- 403 recovery 清理 active workspace/session 文本，避免 empty view 仍显示旧上下文；清理顺序不会由旧 active rows 重建 background watchers。
- folder browser 改为 flex column；目录列表可收缩滚动，确认按钮保持可见。
- allowed-root 添加按钮补齐 dialog ARIA，并在打开/关闭时同步 `aria-expanded`。
- 未修改 backend、`static/` 或 user config。

## TDD 证据

先添加并运行回归测试，旧实现失败于目标 workspace、stale bootstrap、refresh cleanup、403 文本、allowed-root ARIA 和 folder layout；随后实现最小修复并验证通过。

## 最终验证

- 定向测试：`4` 个文件、`36/36` 通过。
- `bun run test`：`37` 个文件、`367/367` 通过。
- `bun run typecheck`：通过。
- `PI_CODING_AGENT_SESSION_DIR="$(mktemp -d)/sessions" bun run backend:test`：2 个 Go package 通过。
- `bun run build`：通过；保留既有 chunk >500 kB warning。
- `git diff --check -- . ':(exclude)static'`：通过；`static/` 未 staging。
