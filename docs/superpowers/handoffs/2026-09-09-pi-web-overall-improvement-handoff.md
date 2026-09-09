# Pi Web 整体改进交接文档

更新时间：2026-09-09 16:51（Asia/Shanghai）

## 续接位置

- 执行分支：`feat/pi-web-overall-improvement`
- 隔离工作区：`/home/hyperion/projects/pi-web-offline/.worktrees/pi-web-overall-improvement`
- 实施计划：`docs/superpowers/plans/2026-09-09-pi-web-overall-improvement.md`
- SDD ledger：`.superpowers/sdd/2026-09-09-pi-web-overall-improvement/progress.md`
- 当前 HEAD：`2bc259d7 docs: record task 3 runtime verification`

不要在主工作区覆盖或提交已有 `static/` 变更。主工作区当前仍有 `static/` 删除、修改和新增文件；本执行 worktree 未修改 `static/`。

## 已完成

### Task 1：质量基线

已完成并审查通过，主要提交：

- `13a74b16` 建立 repository quality gates
- `dfbcb18e` 修复 lint/CI/CSS/repository contract
- `cd9a9ca8` 收紧 CSS 与 workflow contract
- `56e37e47` 收紧 `100vh`/`100dvh` 检查
- `a269dfd9` 完成兼容性扫描修复
- `63832fa7` 记录 staged frontend coverage gate

现状：lint、typecheck、unit、Go tests、build 可执行；Task 1 暂用 `bun run test` 作为 frontend unit gate，前端 100% coverage 需 Task 12/13 收敛后启用。构建仍有既有 chunk size warning。

### Task 2：Broker 可靠性

已完成并在第 5 轮审查后闭环：

- `312eca82` 原子 `SubscribeFrom`、gap 事件
- `cdc8a333` 关键事件 backlog 与 AG-UI gap 终止
- `8fddf1e9` 单 FIFO、bounded backlog、ID 顺序
- `74e97b19` gap snapshot immutable、可取消 forwarder
- `1504e68f` 自动 overflow 清理
- `59451492` 自动 overflow drain deadline 与 gap 可观察性

Task 2 已知 parked/ruling 见 ledger：`gapInFlight` 后续状态语义、取消与 ready send 边界；当时新测试的失败判断经实际 `go test -count=1` 复核为不可复现。Task 3 已修复此前 commands/pi_rpc_status 的全量 race，后续要重新确认 Broker 全包 race。

### Task 3：持久 Pi runtime

实现已提交，审查尚未完成：

- `d987a64f` 按 session 持久 runtime 实现
- `2bc259d7` Task 3 验证报告

实现涉及：

- `internal/piweb/runtime_manager.go`：runtime registry、并发 startup promise、idle eviction、shutdown
- `internal/piweb/runtime_entry.go`：请求/事件转发及活跃度
- `internal/piweb/runtime_process.go`：Pi RPC stdin/stdout/stderr、request ID waiter
- `internal/piweb/runner.go`：Prompt/Steer 改走持久 runtime
- `internal/piweb/compaction.go`：Compact 改走持久 runtime
- `internal/piweb/server.go`、根 `server.go`：runtime 注入与 shutdown
- `internal/piweb/server_session_handlers.go`：删除 session 时关闭 runtime
- `internal/piweb/commands.go`、`internal/piweb/pi_rpc_status.go`：修复 stderr buffer race

代理报告的验证：定向 race、`go test ./...`、`go test -race ./internal/piweb -count=1` 均通过。控制器尚未独立重跑；报告中 Store 没有持久数据库句柄，因此 shutdown 无额外 Store handle 可释放。

Task 3 review package：

`.superpowers/sdd/2026-09-09-pi-web-overall-improvement/review-59451492..2bc259d7.diff`

注意：reviewer 子代理通常看不到 gitignored 的 `.superpowers/sdd` 临时 artifact；若路径不存在，直接用 `git diff 59451492..2bc259d7` 审查。

## 当前未完成任务

- Task 3：等待/完成独立 code review；若有 Important/Critical，按 review loop 修复。
- Task 4：SSE 状态对账与断档恢复。
- Task 5：六个内置 slash 命令后端白名单路由。
- Task 6：slash palette 输入交互。
- Task 7：Project Trust、远程暴露和 Shell 限制。
- Task 8：session branch/fork/search/export。
- Task 9：Linux PTY，优先麒麟 V10 SP3。
- Task 10：Git worktree 与富文件 viewer。
- Task 11：Models/Skills/Plugins/Subagents 控制面。
- Task 12：TypeScript strict 和 coverage 收敛。
- Task 13：Playwright、CI E2E、Chrome 107 回归。

## 明天第一步

```bash
cd /home/hyperion/projects/pi-web-offline/.worktrees/pi-web-overall-improvement
git status --short --branch
git log --oneline -5

# 重新生成 Task 3 review package（若临时 package 不可见）
/home/hyperion/.pi/agent/git/github.com/obra/superpowers/skills/subagent-driven-development/scripts/review-package \
  docs/superpowers/plans/2026-09-09-pi-web-overall-improvement.md \
  59451492f789cb4523bdd30b7d89ddfb686a2bc8 HEAD
```

然后用 reviewer 审查 Task 3，重点检查：同 session 单实例、并发 Ensure、RPC response waiter、EOF/error、idle eviction、shutdown 顺序、进程组清理、Runner Prompt/Steer/Compact 兼容和 ACL；review clean 后在 ledger 登记 Task 3 complete，再生成 Task 4 brief。

## 重要约束

- PTY 首期只做 Linux amd64/arm64，重点麒麟 V10 SP3；不做 Windows/macOS PTY adapter。
- Chrome 107 是硬基线；不引入 `color-mix`、`100dvh` authored API 风险。
- 不提交当前已有 `static/` 变更，除非未来任务明确授权生成嵌入资源。
- 每个 commit 必须带：`Co-authored-by: JuunAI <juunai.ai@gmail.com>`。
- 生产代码先测试后实现；每个 Task 需定向验证、独立 review。
- reviewer 若报告“绝对路径不存在”，通常是其隔离工作区看不到 gitignored `.superpowers/sdd` artifact，不代表路径拼写错误；使用 Git diff 重建 review package。
