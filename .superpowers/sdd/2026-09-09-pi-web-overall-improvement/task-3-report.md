# Task 3 报告：按 session 的持久 Pi runtime

## 状态

已完成并提交实现。代码提交：`d987a64fd5bf5360f686fda36f1e5eb24b99e39e`。

## TDD 红绿证据

- RED：先运行
  `go test ./internal/piweb -run 'TestRuntimeManager|TestRunnerSendsSecondPrompt' -count=1`。
  失败原因是目标契约尚不存在：`RPCCommand`、`PiRuntime`、`NewRuntimeManagerWithFactory`、`NewRunnerWithRuntimeManager` 等均为 `undefined`，不是测试误报。
- GREEN：补齐 fake runtime、session registry、并发启动 promise、持久 process 与 Runner 接入后，四个指定测试及新增 idle/activity 测试通过。
- 后续全包 race 首次暴露既有 `commands.go` 与 `pi_rpc_status.go` 的 stderr `bytes.Buffer` 读写竞争；增加 reader 完成同步后复跑通过。

## 测试命令与摘要

- `go test -race ./internal/piweb -run 'Test(RuntimeManager|Runner)' -count=1`
  - PASS：`ok github.com/Epsilondelta-ai/pi-web/internal/piweb 1.044s`
- `go test ./...`
  - PASS：根包、`internal/piweb`、flatted 包均通过。
- `go test -race ./internal/piweb -count=1`
  - PASS：全包 race，无 race detector 报告。
- `git diff --check`
  - PASS：无 whitespace 错误。

## 修改文件

- `internal/piweb/runtime_manager.go`：RPC/PiRuntime 契约、按 session registry、共享 startup promise、idle eviction、shutdown。
- `internal/piweb/runtime_entry.go`：runtime 代理、请求/事件活跃度跟踪、事件转发。
- `internal/piweb/runtime_process.go`：Linux 目标 Pi RPC 子进程、request ID waiter、stdout/stderr 生命周期。
- `internal/piweb/runner.go`：Prompt/Steer 改走持久 runtime，并沿用现有 Broker 事件发布。
- `internal/piweb/compaction.go`：Compact 改走持久 runtime。
- `internal/piweb/server.go`：runtime manager 注入、shutdown guard、Server.Shutdown 生命周期。
- `server.go`：HTTP shutdown 前先停止内部 runtime/broker。
- `internal/piweb/broker.go`：broker 可取消 shutdown。
- `internal/piweb/process_unix.go`、`internal/piweb/process_windows.go`：进程组强制终止辅助函数。
- `internal/piweb/runtime_manager_test.go`：reuse/concurrent startup/close-all/idle/activity 测试与 fake runtime。
- `internal/piweb/runner_test.go`：第二次 prompt 复用既有 runtime 测试。
- `internal/piweb/commands.go`、`internal/piweb/pi_rpc_status.go`：stderr buffer 完成同步，修复 race。
- `internal/piweb/server_session_handlers.go`：删除 session 前关闭 runtime。

未触碰 `static/` 或前端。

## Concerns

- `Store` 当前没有持久打开的数据库句柄；其 SQLite 操作均为临时打开/关闭，因此 Server shutdown 无额外 store handle 可释放。
- runtime 默认 idle timeout 为 10 分钟；idle close 使用 5 秒强制进程组退出兜底。
- 进程 runtime 的事件通道保持单一 Broker 发布路径，未增加浏览器直连通道。
