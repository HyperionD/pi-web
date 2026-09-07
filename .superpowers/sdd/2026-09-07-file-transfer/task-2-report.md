# Task 2 报告：流式工作区上传与服务端兼容

## 状态

实现完成。multipart 上传、配置字节限制、413 可读错误、临时文件事务、覆盖语义和旧 JSON/Base64 分支均已落地；未修改前端文件。

## 实现

- `POST /api/workspaces/{workspaceID}/files/upload` 按 `Content-Type` 分流：
  - multipart 字段：`path`、`overwrite`、`file`。
  - 旧 JSON/Base64 请求形状保持不变。
- 通过既有 `LoadMaxUploadBytes()` 使用有效工作区上传限制，没有重复配置字段。
- multipart 文件使用 `limit+1` 流读取，仅允许多读一个字节判断超限，不把完整文件载入内存。
- 超限返回 HTTP 413，错误包含配置最大字节数。
- 路径在创建临时文件前经过 `SafeJoin`；既有 workspace access middleware 保持不变。
- 临时文件创建在目标目录，权限为 `0600`；成功后原子 `rename`，写入、超限、关闭或 rename 失败都会清理临时文件。
- 保留 `overwrite=false` 冲突和 `overwrite=true` 原子替换语义。
- 旧 JSON 分支按 Base64 解码后的文件字节数执行配置限制，并保留 JSON 请求体上限保护。

## TDD 证据

### RED

首次运行聚焦测试：

```text
go test ./internal/piweb -run 'TestUploadWorkspaceFileEndpoint|TestUploadWorkspaceFile.*Temp|TestUploadWorkspaceFileStream'
FAIL ... undefined: UploadWorkspaceFileStream
```

失败原因是待实现的流式文件操作不存在。

### GREEN

实现后运行：

```text
go test ./internal/piweb -run 'TestUploadWorkspaceFileEndpoint|TestUploadWorkspaceFile.*Temp'
ok   github.com/Epsilondelta-ai/pi-web/internal/piweb
```

覆盖：multipart 超限 413 与可读错误、成功写入、0600、overwrite 冲突与替换、旧 JSON 超限与上限内上传、临时文件清理。

## 验证

通过：

```text
go test ./internal/piweb -run 'TestUploadWorkspaceFileEndpoint|TestUploadWorkspaceFile.*Temp'
go test ./internal/piweb -run 'Test(UploadWorkspaceFile|Server|WorkspaceFile|WorkspacePolicy)'
git diff --check
```

`go test ./internal/piweb -count=1` 未全通过，失败均为现有环境依赖，非本任务上传测试：

- `TestWorkspaceCommandsEndpointFallsBackWhenPiCommandsFail`：默认 workspace 路径不在 allowed roots，返回 403。
- `TestWorkspaceModelsEndpointUsesNoExtensions`：默认 workspace 路径不在 allowed roots，返回 403。
- `TestSessionCompactEndpointUsesPiRPC`：默认 workspace 路径不在 allowed roots，返回 403。
- `TestWorkspaceEndpointMarksRunningSessionsLive`：环境中的 `/home/hyperion/code` 不存在，workspace policy resolve path 失败。

## 变更文件

- `internal/piweb/files.go`
- `internal/piweb/server.go`
- `internal/piweb/server_workspace_handlers.go`
- `internal/piweb/files_test.go`
- `internal/piweb/server_workspace_handlers_test.go`
