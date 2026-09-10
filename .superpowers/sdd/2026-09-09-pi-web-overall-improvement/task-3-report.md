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

## 修复轮次 2（独立复审阻断项）

状态：已完成，待独立提交。

- `independentShutdownContext` 始终从 `context.Background()` 创建新的 5 秒 deadline；父 context 已超时仍可完成 runtime/broker/store cleanup，并新增真实 cleanup 完成回归测试。
- Cancel 在 abort 后必须等事件消费者 settled；超时只异步等待并保持 run claim，不再 `finishRun` 制造重叠。Compact 取消会先 abort、关闭旧 runtime、完成 waiter 隔离后才释放 session。
- 取消后的旧事件流在新 prompt 前关闭，确保旧 agent_end/delta/tool 不进入新 runtime；回归测试覆盖新 runtime 的真实 `agent_start` 与 delta 事件。
- RuntimeManager tombstone 改为 generation token；批量删除失败按 token rollback，成功删除 finalize 并清理 tombstone；增加重叠请求回归测试。
- stdout 普通 EOF 立即 poison 并唤醒 waiter；runtime entry 背压不再静默丢非终止事件，而是 poison/close；closed entry 可由 idle eviction 回收。

本轮 TDD：先加入独立 context、EOF、entry 背压、Cancel/Compact、tombstone generation、批量删除 rollback 回归测试并确认缺失契约/行为失败，再实现修复。

最终验证：

- `go test ./...`：PASS。
- `go test -race ./internal/piweb -run 'Test(RuntimeManager|Runner)' -count=1`：PASS。
- `go test -race ./internal/piweb -count=1`：PASS，无 race detector 报告。
- `git diff --check`：PASS。

Concerns：取消流程对 session runtime 采用关闭并在下一代 prompt 重建，以提供强事件隔离；正常完成 prompt 仍复用原 runtime。未修改 `static/` 或前端。

## 修复轮次 2（最终复审）

状态：已完成并通过独立并发回归验证。

- `Server.Shutdown` 先切换 admission gate、关闭 broker 使 SSE 退出，再等待 active handlers，最后完成 runtime/store cleanup；manager shutdown 同时收集 `sessions` 与 `closing` promises。
- Prompt/Compact 在 claim 后再 Ensure，并以 runtime-ready 协调 closing runtime；Cancel 使用独立 abort/settle deadline，取消 command context，关闭并 retire runtime，缺失 `agent_end` 不再永久 busy。
- Compact 纳入 settled/cancel lifecycle；commandMu 使 Steer 的检查与发送、Cancel/Abort 串行，旧 compact 不再发布取消后的 error/idle 状态。
- runtime/process 与 entry 背压不驱逐旧事件；以 poison、waiter 唤醒和 channel close 显式终止，保留 protocol EOF/error 语义。补充真实 SSE shutdown、Cancel timeout/new prompt、Compact Cancel、closing shutdown、entry waiter/channel、Steer/Cancel 并发测试；generation 测试改为 channel 重叠，不使用固定 Sleep。

验证：

- `go test -race ./internal/piweb -run 'Test(RuntimeManager|Runner)' -count=1`：PASS。
- `go test ./...`：PASS。
- `go test -race ./internal/piweb`：PASS，无 race detector 报告。
- `git diff --check`：PASS。

Concerns：`Store.Shutdown` 仍是明确 no-op（Store 每次操作独立打开 SQLite）；取消 deadline 常量为 abort 5 秒、settle 1 秒，真实 process close 仍有 5 秒强制终止兜底。未修改 HTTP API/ACL、Task2 Broker 协议、Linux single binary、fallback/mock 或 `static/`。

## 修复轮次 3

状态：已完成，针对最新 review 阻断项补强并通过定向并发验证。

- Broker：`SubscribeFrom` 在 closed 状态返回已关闭哨兵 channel，不注册 subscriber；覆盖 shutdown 后迟到订阅与 active SSE shutdown 完成。
- Runner：取消 claim 按 session + run pointer 原子确认；Compact 取消也保留旧 generation 直到 owner 完成 retire/release。新增真正交叠的 Prompt/Compact complete 与 Cancel 测试，确认新 run 不收到 abort。
- Cancel：以 cancellable command gate 替代不可取消 `commandMu.Lock`；非 cooperative Send 会跳过阻塞 abort、停止事件消费并 retire runtime，随后新 prompt 可重建；新增 non-cooperative fake。
- RuntimeProcess：保存并关闭 stdout/stderr pipe；强杀后 bounded 等待，超时只返回错误、不伪造 `done` 已清理，reaper 继续负责 waiter/channel cleanup。新增真实进程 inherited-pipe、backpressure、EOF/channel-close 测试。
- 清理未使用的旧 compaction helper、stale event drainer 与 active-run `done`。

TDD：先加入测试并确认因新 hooks/契约缺失而 RED，再实现后定向测试 GREEN。

验证：

- `go test -race ./internal/piweb -run 'Test(SubscribeFromAfterShutdown|Runner(CancelDoesNotAbortNewPromptAfterCompletionRace|CompactDoesNotAbortNewPromptAfterCompletionRace|CancelReleasesNonCooperativeSend)|ProcessRuntime(RealProcessBackpressure|ClosesEventChannelAfterEOF|ForceCloseDoesNotWaitForeverForInheritedPipes))' -count=10`：PASS。
- `go test -race ./internal/piweb -run 'Test(RuntimeManager|Runner|ProcessRuntime|ServerShutdown|SubscribeFrom)' -count=1`：PASS。
- `go test ./...`：PASS。
- `go test -race ./internal/piweb`：PASS，无 race detector 报告。
- `go build ./...`、`git diff --check`：PASS。

Concerns：`Cancel` 对不响应 context 的第三方 runtime 只提供 bounded retire，不保证其实现内部 goroutine 自行退出；旧 runtime 与新 generation 已通过 manager identity 隔离。未修改 HTTP/ACL、Task2 Broker FIFO/gap/close、fallback/mock、Linux single binary 或 `static/`。

## 修复轮次 4

状态：已完成，针对三个 Important、启动失败清理和 request ID 复用阻断项补强。

- Runner：每次 claim 分配单调 run generation；Steer 在同一 command gate 内再次确认 `running[sessionID] == run` 后才发送，完成/取消与发送协调，迟到 Steer 不会写入 successor generation。
- RuntimeManager：新增 generation-bound ownership lease；Prompt/Compact 的 Ensure、attach、Retire 均携带 lease，交叠 startup/旧 owner retire 不会关闭新 owner runtime。
- RuntimeManager.Close：超时且旧进程仍运行时保留 closing promise；Ensure 不提前 rebuild，待 `IsRunning` 确认退出后才允许新 runtime，close error 保留并可观察。
- RuntimeProcess：启动失败关闭 stdin/stdout/stderr descriptor；使用独立 os pipes 避免 Wait 提前关闭未消费 response；取消 waiter 进入 request-ID quarantine，迟到 response 不会满足新 waiter。
- 新增 Steer generation、overlapping startup ownership、bounded close/rebuild、request-ID quarantine 回归测试；不使用固定 sleep 或遗留后台进程。

TDD：先运行新增测试确认 hooks/ownership API 缺失导致 RED，再实现并验证 GREEN。

验证：

- `go test -race ./internal/piweb -run 'Test(RunnerSteerCannotSendAfterRunGenerationChanges|RuntimeManagerOverlappingStartupOwnership|RuntimeOwnershipBlocksStaleRetire|RuntimeManagerRetainsTimedOutClosingRuntime|ProcessRuntimeQuarantinesCanceledRequestID|RunnerCancelReleasesNonCooperativeSend|RunnerCompactDoesNotAbortNewPromptAfterCompletionRace)' -count=10`：PASS。
- `go test ./...`：PASS。
- `go test -race ./internal/piweb -count=1`：PASS，无 race detector 报告。
- `go build ./...`：PASS。
- `git diff --check`：PASS。

Concerns：Cancel API 仍保持既有 bool 契约；超时 close error 通过 retained closing 状态阻止静默 rebuild，并在后续 Ensure 返回。未修改 static、HTTP/ACL、Task2 Broker、fallback/mock 或 Linux single binary。
