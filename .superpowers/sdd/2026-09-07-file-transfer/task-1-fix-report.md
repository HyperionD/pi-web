# Task 1 Review Findings 修复报告

## 修复范围

仅处理 review 指出的三个问题，未修改 Task 2 文件：

1. `DefaultMaxUploadBytes` 固定为 `100 MiB`（`104857600` bytes）。
2. `MaxMaxUploadBytes` 固定为 `2 GiB`（`2147483648` bytes）。
3. 增加固定规格值断言，以及供后续上传 handler 调用的 `LoadMaxUploadBytes() (int64, error)` helper。该 helper 每次读取 app config 的有效 `workspace.maxUploadBytes`，缺省时返回默认值，并透传配置读取/校验错误。

## 改动文件

- `internal/piweb/app_config.go`
  - 修正默认值与最大值常量。
  - 新增 `LoadMaxUploadBytes`。
- `internal/piweb/app_config_test.go`
  - 增加 `1 MiB`、`100 MiB`、`2 GiB` 的固定字面量断言。
  - 增加 helper 对默认值和已保存配置值的行为测试。
- `.superpowers/sdd/2026-09-07-file-transfer/task-1-fix-report.md`
  - 本报告。

## TDD 验证

### RED

先只补测试，未实现 helper 时运行：

```text
go test ./internal/piweb -run 'Test(MaxUploadBytesMatchFileTransferSpec|LoadAppConfigReturnsDefaultMaxUploadBytes|SaveAppConfigValidatesMaxUploadBytes|SaveAppConfigClearingMaxUploadBytesRestoresDefault|LoadMaxUploadBytesReturnsEffectiveConfiguredValue)$'
```

结果：FAIL（编译失败，`undefined: LoadMaxUploadBytes`），证明新增 helper 测试未被现有实现虚假通过。

### GREEN

实现常量和 helper 后运行同一 focused 命令：

```text
ok   github.com/Epsilondelta-ai/pi-web/internal/piweb
```

## 测试摘要

- `go test ./internal/piweb -run 'Test(MaxUploadBytesMatchFileTransferSpec|LoadAppConfigReturnsDefaultMaxUploadBytes|SaveAppConfigValidatesMaxUploadBytes|SaveAppConfigClearingMaxUploadBytesRestoresDefault|LoadMaxUploadBytesReturnsEffectiveConfiguredValue)$`：PASS
- `go test ./internal/piweb -run 'Test(AppConfig|LoadAppConfig|SaveAppConfig|LoadMaxUploadBytes|MaxUploadBytes)'`：PASS
- `bunx vitest run src/pi-app/workspace/app-config-methods.test.ts`：PASS，38/38
- `git diff --check`：PASS

完整 `go test ./internal/piweb` 仍有 4 个既有环境/fixture 相关失败，均与本次改动无关：

- `TestWorkspaceCommandsEndpointFallsBackWhenPiCommandsFail`：fixture workspace 被 allowed roots 拒绝（403）。
- `TestWorkspaceModelsEndpointUsesNoExtensions`：fixture workspace 被 allowed roots 拒绝（403）。
- `TestSessionCompactEndpointUsesPiRPC`：fixture workspace 被 allowed roots 拒绝（403）。
- `TestWorkspaceEndpointMarksRunningSessionsLive`：fixture `/home/hyperion/code` 不存在（500）。

## Concerns

- 完整 Go 包测试仍受上述环境路径/fixture 问题影响；Task 1 定向测试与现有前端 app-config 测试均通过。
- 本次未修改 `src/pi-app` 或任何 Task 2 文件。
- Conventional Commit hash 见最终提交记录。
