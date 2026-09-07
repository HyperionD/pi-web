# Task 2 Review 修复报告

## 状态

已修复 Task 2 review findings；未修改前端。

## 修复内容

- 上传目标在创建目标目录/临时文件前解析工作区真实路径，并检查已有祖先与最终父目录均位于真实工作区内；父目录 symlink 越界被拒绝。
- `overwrite=false` 最终提交改为临时文件 `os.Link` 的原子 no-clobber，竞态下仅一个上传成功；`overwrite=true` 仍使用原子 `Rename`。
- multipart 先将 `file` 流式暂存到 `0600` 临时文件，再处理 `path`/`overwrite`，接受合法字段任意顺序；所有失败路径清理暂存和目标临时文件。
- multipart handler 拆分为暂存、字段处理、提交和错误映射函数，每个相关函数不超过 50 行。
- 修复测试中 `os.Stat` 失败时仍解引用 nil `info` 的报告路径。
- 保留 0600、413 可读限制错误、旧 JSON/Base64 兼容、动态 `LoadMaxUploadBytes` 配置和临时文件清理。

## TDD 证据

### RED

先添加 symlink、原子 no-clobber 和 file-first multipart 测试，运行：

```text
go test ./internal/piweb -run 'TestUploadWorkspaceFileEndpointAcceptsMultipartFieldsInAnyOrder|TestUploadWorkspaceFileStreamRejectsParentSymlinkOutsideWorkspace|TestUploadWorkspaceFileStreamNoOverwriteIsAtomicNoClobber' -count=1
```

预期失败已确认：

- symlink 测试：`expected parent symlink to be rejected`
- no-clobber 测试：`expected exactly one no-clobber winner, got 2`
- 顺序测试：HTTP 400，`path is required before file`

### GREEN

通过：

```text
go test -race ./internal/piweb -run 'TestUploadWorkspaceFileEndpoint|TestUploadWorkspaceFileStream|TestUploadWorkspaceFile$' -count=1
ok   github.com/Epsilondelta-ai/pi-web/internal/piweb

go test ./internal/piweb -run 'Test(AppConfig|UploadWorkspaceFileEndpoint|UploadWorkspaceFileStream|UploadWorkspaceFile$)' -count=1
ok   github.com/Epsilondelta-ai/pi-web/internal/piweb
```

## 完整包测试

`go test ./internal/piweb -count=1` 仍有 4 个既有环境依赖失败，均与本次上传修复无关：

- `TestWorkspaceCommandsEndpointFallsBackWhenPiCommandsFail`：默认 workspace 不在 allowed roots，返回 403。
- `TestWorkspaceModelsEndpointUsesNoExtensions`：默认 workspace 不在 allowed roots，返回 403。
- `TestSessionCompactEndpointUsesPiRPC`：默认 workspace 不在 allowed roots，返回 403。
- `TestWorkspaceEndpointMarksRunningSessionsLive`：环境中的 `/home/hyperion/code` 不存在，返回 500。
