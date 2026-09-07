# Task 2 Fix Round 2 报告：安全上传提交与 multipart 合同

## 状态

完成 Task 2 review round 2 修复；未修改前端。

## 修复内容

- Linux 上传提交改为目录句柄 + `openat`/`mkdirat`/`linkat`/`renameat`，目录遍历使用 `O_NOFOLLOW`；不再以 `EvalSymlinks` 后的普通路径执行创建、临时文件或提交。
- 保留 `SafeJoin`；workspace access middleware 继续执行 allowed-roots/工作区策略检查。
- 父目录句柄在读取和提交期间保持打开，父目录被替换为 symlink 时仍写入原已打开目录；临时文件使用 `0600` 和 `O_EXCL`。
- `overwrite=false` 使用同目录 `linkat` 原子 no-clobber；竞态失败方清理临时文件并保留成功方最终内容。
- 非 Linux 构建提供显式拒绝实现，避免回退到不安全的普通路径写入。
- multipart 合同明确为 `path`、`overwrite` 必须先于 `file`；file-first 立即返回明确 400，不读取/排空文件 payload。
- 路径字段读取后立即 `SafeJoin` 校验；只有路径和 overwrite 均通过后才打开目标目录句柄并读取文件流。
- 未知 multipart part、path、overwrite 均有受限读取；未知 part 限制为 16 KiB，文件限制为配置值 + 1。
- 增加路径校验前不读取 payload、file-first 不消耗大 payload、未知 part 边界、父目录 symlink 替换、no-clobber 最终内容、竞态失败方和 source 失败方临时文件清理测试。

## TDD 证据

### RED

先补回归测试后运行：

```text
go test ./internal/piweb -run 'TestUploadWorkspaceFileEndpoint|TestUploadWorkspaceFile.*Temp|TestUploadWorkspaceFileStream' -count=1
```

确认失败：

- file-first 仍返回 413，并已消费完整大 payload；
- 父目录 symlink 替换测试未在固定目录产生文件；
- no-clobber/field-order 行为不满足新合同。

### GREEN

通过：

```text
go test ./internal/piweb -run 'TestUploadWorkspaceFileEndpoint|TestUploadWorkspaceFile.*Temp|TestUploadWorkspaceFileStream' -count=1
go test -race ./internal/piweb -run 'TestUploadWorkspaceFileEndpoint|TestUploadWorkspaceFileStream|TestUploadWorkspaceFile$' -count=1
go test ./internal/piweb -run 'Test(UploadWorkspaceFile|Server|WorkspaceFile|WorkspacePolicy)' -count=1
git diff --check
```

## 完整包测试

`go test ./internal/piweb -count=1` 仍返回非零，复现 4 个环境依赖失败，均非上传测试：

- `TestWorkspaceCommandsEndpointFallsBackWhenPiCommandsFail`：默认 workspace 不在 allowed roots，返回 403。
- `TestWorkspaceModelsEndpointUsesNoExtensions`：默认 workspace 不在 allowed roots，返回 403。
- `TestSessionCompactEndpointUsesPiRPC`：默认 workspace 不在 allowed roots，返回 403。
- `TestWorkspaceEndpointMarksRunningSessionsLive`：`/home/hyperion/code` 不存在，返回 500。

其余 package 测试通过，上传相关测试无失败。
