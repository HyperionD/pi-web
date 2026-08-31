# Task 1 报告：用户级 JSON 配置存储

## 完成内容

- 新增 `AppConfigPath()`：从 `DefaultPiSessionDir()` 的父目录派生 `pi-web-config.json`，空 session directory 返回显式错误。
- 新增 `LoadAppConfig()` 与 `SaveAppConfig()`，提供默认配置、JSON 读取、合并、brand 校验、未知顶层字段保留及有效响应。
- 保存时创建 `0700` 父目录，使用同目录 `0600` 临时文件，缩进编码、`Sync`、关闭后通过 `os.Rename` 原子替换。
- 新增覆盖测试：默认无文件创建、有效保存、brand reset、未知字段、错误类型、控制字符、121-rune brand、空文件、 malformed JSON 路径错误及文件权限。

## 修改文件

- `internal/piweb/app_config.go`
- `internal/piweb/app_config_test.go`
- `.superpowers/sdd/2026-08-31-user-config/task-1-report.md`（本报告）

## Commit

`a00d5ae8d9c21315ee621df1d9c9eb12736a0d93`

## 测试及结果

- `go test ./internal/piweb -run 'Test(AppConfig|LoadAppConfig)'`（RED 阶段：按预期因接口尚不存在而编译失败；GREEN 阶段：PASS）
- `go test ./internal/piweb`：PASS
- `go test ./internal/piweb -coverprofile=/tmp/piweb-cover.out && go tool cover -func=/tmp/piweb-cover.out | grep -E 'app_config.go|total:'`：app-config 新增代码 100.0% statements/functions；包总覆盖率 79.9%
- `go test ./...`：PASS
- `gofmt` 检查及 `git diff --cached --check`：PASS

## Concerns

无功能性 concerns。工作区原有 static 构建产物变更未修改、未回滚，仍保持未暂存状态；本报告按要求写入，未纳入 Task 1 storage commit。

# Fix Round 1

## 修改

- `readAppConfig` 读取已有 JSON 后复用 `brand` 规范化校验：trim 字符串，空白或 `null` 删除显式值并回落默认值，拒绝错误类型、控制字符及超过 120 Unicode 字符的值。
- `writeAppConfig` 在 `MkdirAll` 后始终将配置父目录校正为精确 `0700`。
- 回归测试改为通过公开 `AppConfigPath()` 验证默认 session directory 为空的错误，并覆盖新建及已有目录的精确目录/文件权限。
- 新增已有 JSON 中 brand 的 trim、reset、类型、控制字符和 Unicode 长度校验覆盖。

## 测试

- `go test ./internal/piweb -run 'Test(AppConfig|LoadAppConfig)'`（RED：新增已有 brand 校验测试按预期失败；GREEN：PASS）
- `go test ./internal/piweb`：PASS
- `go test ./...`：PASS

## Concerns

无本轮功能性 concerns。`git diff --check` 仍因工作区原有 `static/index.html` 生成物的 trailing whitespace 报错；static 变更未修改且不会纳入本次 commit。

# Fix Round 2

## 修改

- 新增 `TestSaveAppConfigCorrectsExistingFilePermissions`：先创建权限为 `0644` 的已有配置文件，保存更新后验证目标文件权限精确为 `0600`。
- 未修改生产代码；现有同目录 `0600` 临时文件配合 `os.Rename` 已能校正已有配置文件权限。

## 测试

- `go test ./internal/piweb -run 'Test(AppConfig|LoadAppConfig)'`：PASS。
- `go test ./internal/piweb`：PASS。
- `go test ./...`：PASS。
- `gofmt -w internal/piweb/app_config_test.go`：PASS。

## Concerns

无本轮功能性 concerns。新增回归测试在当前实现上直接通过，因此无需实现修复。
