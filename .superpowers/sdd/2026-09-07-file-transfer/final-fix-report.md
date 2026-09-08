# Final branch review 修复报告

## Status

已修复 `final-review.md` 的 6 项 blocking/important findings。当前修复基于 `3ca7a582`，尚未包含本报告提交。

## Changes

1. **下载 TOCTOU**
   - 新增 Linux dirfd 安全打开逻辑：root 与父目录逐级 `openat` + `O_NOFOLLOW`，目标文件使用 pinned fd。
   - `workspaceFileAsset` 直接基于该 fd 调用 `ServeContent`，不再对文件执行 `EvalSymlinks` 后再路径 `os.Open`。
   - 非 Linux 返回 HTTP 501；symlink 目标/父目录拒绝并返回安全错误。
   - 增加 symlink 拒绝和 rename 后 pinned handle 回归测试。

2. **上传 root 身份校验**
   - `EvalSymlinks(root)` 后先 snapshot 预期 device/inode，再打开 dirfd 并使用 `fstat` 比较。
   - 保留已有 `O_NOFOLLOW`、dirfd 相对操作和原子 no-clobber 语义。
   - 增加不同目录 device/inode mismatch 测试。

3. **multipart 资源边界**
   - 限制总 part 数为允许字段的 3 个。
   - 未知字段立即拒绝，不再 drain 任意 part 内容。
   - 增加未知字段不 drain、part count overflow 测试。

4. **no-clobber committed state**
   - `Linkat` 成功后立即记录 committed state。
   - 临时链接 unlink 失败时进行重试；仍失败则保留 pending cleanup 状态，向用户报告已提交成功，defer cleanup 再次清理，避免把已提交操作报告成失败。
   - 增加可注入 temp remover 的 committed-state/重试测试。

5. **前端错误提示**
   - `status === 413` 映射为：`文件超过上传上限，请先上传到工作区`。
   - `status === 501` 映射为清晰的安全上传平台提示。
   - 增加 WorkspaceFileTree 映射测试和 file-transfer 501 状态保留测试。

## TDD / Verification

- RED：新增前端 413/501 回归断言在修复前分别收到原始 HTTP 错误；新增 Go 安全接口测试在实现前无法编译。
- GREEN：
  - `go test ./internal/piweb -run 'Test(AppConfig|UploadWorkspaceFile|ServerWorkspace|WorkspaceFile)'`：通过。
  - `bun run test`：38 个测试文件、464 个测试通过。
  - `bun run typecheck`：通过。
  - `bun run build`：通过，仅有既有 Vite chunk size warning。
  - `GOOS=darwin GOARCH=amd64 go test -c ./internal/piweb -o /tmp/piweb-darwin.test`：通过。
  - `git diff --check`：通过。

## Concerns

`go test ./...` 仍有 4 个既有环境依赖失败：测试中的 allowed-roots 配置指向不存在/不允许的 `/home/hyperion/code`，导致 commands/models/compact/workspace live-session 测试分别返回 403/500；与本次文件传输改动无关。相关 `internal/piweb` 定向测试已通过。
