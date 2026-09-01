# Task 4 完成报告

## 修改文件

- `internal/piweb/server_workspace_policy.go`
  - 新增 `requireWorkspaceAccess` 与 `requireSessionAccess`。
  - 统一处理资源不存在（404）、越界 workspace（403）和 policy/filesystem 错误（500）。
- `internal/piweb/server.go`
  - 将全部现有 workspace/session API route 注册到对应授权 wrapper。
  - 保留 `GET /api/workspaces` 的 Task 3 filtering 逻辑。
- `internal/piweb/store_sessions.go`
  - 新增 `SessionWorkspacePath`，解析 session 所属 workspace，并保留不存在 session 的 404 语义。
- `internal/piweb/server_workspace_policy_test.go`
  - 新增 workspace/session 越界、允许资源和缺失资源的 route 授权测试。

brief 列出的其他后端文件已执行 `gofmt`，无额外语义修改；static、前端及无关代码未修改。

## Commit

- `6c5a77a feat(api): authorize existing workspace access`
- `0e0acb0 test(api): harden session route authorization coverage`

## 测试命令与结果

- `gofmt -w internal/piweb/server_workspace_policy.go internal/piweb/server.go internal/piweb/store_workspace.go internal/piweb/store_sessions.go internal/piweb/server_workspace_handlers.go internal/piweb/server_session_handlers.go internal/piweb/server_session_status_handlers.go internal/piweb/server_workspace_policy_test.go` — PASS
- `go test ./internal/piweb -run 'TestWorkspaceRoutesRejectForbiddenWorkspace|TestSessionRoutesRejectForbiddenWorkspace'` — PASS
- `go test ./internal/piweb` — PASS
- `go test -race ./internal/piweb -run 'TestWorkspaceRoutesRejectForbiddenWorkspace|TestSessionRoutesRejectForbiddenWorkspace'` — PASS
- `go test -race ./internal/piweb` — FAIL：发现既有 `internal/piweb/commands.go` 与 `internal/piweb/pi_rpc_status.go` 中 `bytes.Buffer` 并发读写 race，触发 `TestCoverageRPCCommandsModelRunnerAndTail` 和 `TestWorkspaceCommandsEndpointFallsBackWhenPiCommandsFail`；相关文件超出本 Task 允许修改范围，未处理。

## Concerns

全包 race 仍受上述既有 race 阻断；本 Task 授权 focused race、focused tests 和 package tests 均通过。工作区原有 static 变更保持不动。

## Fix round 1 追加报告

- 复审确认 `server.go` 中全部 workspace/session routes 均通过对应授权 wrapper；`context-usage` 与 `model` 未遗漏。
- `server_workspace_policy_test.go` 增加受限配置下允许 session 的两个 status route 可达回归，以及 `/api/sessions/{sessionID}/status` 保持 404 的回归。
- 未新增 `/status` endpoint，未修改 static、前端或无关业务代码。

### Fix round 1 验证

- `go test ./internal/piweb -run 'TestWorkspaceRoutesRejectForbiddenWorkspace|TestSessionRoutesRejectForbiddenWorkspace|TestAuthorizedSessionStatusRoutesRemainReachable|TestSessionStatusEndpointRemainsUnregistered' -count=1` — PASS
- `go test ./internal/piweb -count=1` — PASS
- `go test -race ./internal/piweb -run 'TestWorkspaceRoutesRejectForbiddenWorkspace|TestSessionRoutesRejectForbiddenWorkspace|TestAuthorizedSessionStatusRoutesRemainReachable|TestSessionStatusEndpointRemainsUnregistered' -count=1` — PASS

## Fix round 2 追加报告

- `internal/piweb/server_workspace_policy_test.go` 增加 invalid non-empty `allowedRoots`（`[42]`）配置，确认 `LoadWorkspacePolicy` 返回错误，并断言现有 `context-usage` 与 `model` session routes 返回 500。
- 保留既有 forbidden、404、allowed 断言；未修改生产代码、static、前端或无关文件。

### Fix round 2 验证

- `go test ./internal/piweb -run 'TestSessionRoutesReturnInternalServerErrorWhenWorkspacePolicyLoadFails' -count=1` — PASS
- `go test ./internal/piweb -run 'TestWorkspaceRoutesRejectForbiddenWorkspace|TestSessionRoutesRejectForbiddenWorkspace|TestAuthorizedSessionStatusRoutesRemainReachable|TestSessionStatusEndpointRemainsUnregistered|TestSessionRoutesReturnInternalServerErrorWhenWorkspacePolicyLoadFails' -count=1` — PASS
- `go test ./internal/piweb -count=1` — PASS
- `go test -race ./internal/piweb -run 'TestWorkspaceRoutesRejectForbiddenWorkspace|TestSessionRoutesRejectForbiddenWorkspace|TestAuthorizedSessionStatusRoutesRemainReachable|TestSessionStatusEndpointRemainsUnregistered|TestSessionRoutesReturnInternalServerErrorWhenWorkspacePolicyLoadFails' -count=1` — PASS
