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

## 修复轮次 1（Critical/Important）

状态：已完成并独立提交：`f2b3e84cf248d4bd39db469da7e6da94d9010fa4`。

- `runtime_process.go`：所有 RPC（Prompt/Steer/Compact/Abort）按真实 response 等待；严格匹配 request ID，传播 response failure、EOF、scanner/非法 stdout 错误，并清理全部 waiter。
- stdout 事件发送改为非阻塞且保留终止/error 事件；stderr reader 纳入 join；Runner 取消会发送 `abort`，等待取消消费退出后才允许新 Prompt，失败时关闭 runtime。
- RuntimeManager 增加 session tombstone 与 startup 等待，阻止 CloseSession/startup 竞态创建第二个进程；活动改为 request + agent/turn/tool 计数，idle eviction 不依赖接口外可选方法。
- session 单删、account/workspace 批量删均先调用 `CloseSession`；Server shutdown guard 持有读锁覆盖请求，shutdown 明确执行 runtime、broker、store；主程序即使 runtime shutdown 报错仍继续 HTTP shutdown。
- 新增 process/RPC、EOF/非法输出、背压、stderr、Cancel、startup tombstone、活动计数、批量删除与 shutdown guard 回归测试；更新 fake Pi 响应携带真实 request ID。

验证：`go test -race ./internal/piweb -run 'Test(RuntimeManager|Runner)' -count=1`、`go test ./...`、`go test -race ./internal/piweb -count=1`、`git diff --check` 均通过。

Concerns：Store 当前仍无持久 handle，`Store.Shutdown` 为明确的 no-op 契约；Cancel 使用 Pi RPC `abort`，若 abort 失败会关闭该 session runtime 后由下一次 Ensure 重建。

## 修复轮次 1：本次补充执行记录

状态：已完成代码补强，保持 Linux single binary、ACL、Task2 Broker、mock/fallback API；未修改 `static/` 或前端。

- RuntimeManager 使用 per-session `closing` registry；eviction、CloseSession、Shutdown 共用单一 close promise，Ensure 等待关闭完成后才允许新实例；删除失败通过 `RollbackSessionClose` 清除 tombstone。
- Server 生命周期改为 accepting/closing/closed gate，活动请求计数替代跨 handler 的长持有 `RLock`；Shutdown 先拒绝新请求，再使用独立 runtime/broker/store contexts。
- process runtime 的 stdin write 支持 context 取消；事件通道背压会显式 poison runtime；Cancel 等待 agent settled，再隔离并清理 stale/queued 事件。
- 新增 closing eviction、tombstone rollback、可取消 stdin、背压 poison、settled/stale Cancel、shutdown gate、独立 cleanup context、删除失败回归测试。

TDD：新增测试先运行并因缺失 `RollbackSessionClose` 契约失败；实现后定向测试转绿。最终验证：

- `go test -race ./internal/piweb -run 'Test(RuntimeManager|Runner)' -count=1`：PASS。
- `go test ./...`：PASS。
- `go test -race ./internal/piweb -count=1`：PASS，无 race detector 报告。
- `git diff --check`：PASS。

补充 concerns：stale event 丢弃窗口以 Pi 的下一次 `agent_start` 作为新 run 边界；若未来 Pi 协议省略该事件，需要改用协议级 run ID。可取消写入对遵循 `io.WriteCloser.Close` 语义的 OS pipe 生效；不遵循 Close 解阻塞语义的第三方 writer 可能留下写 goroutine，但不会继续污染 runtime 状态。
