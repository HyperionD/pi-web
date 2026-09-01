# Task 1 报告：workspace.allowedRoots

## 状态

已完成配置规范化与校验：

- `workspace.allowedRoots` 只接受字符串数组；非空项必须是已存在目录。
- 路径会 trim、展开 `~`、转绝对路径、clean、解析 symlink，并按真实路径去重。
- 缺少 `workspace`、缺少 `allowedRoots`、空数组保持无限制语义。
- `workspace` 的未知嵌套字段、顶层 `brand` 与其他配置字段保持不变。
- 无效 PATCH 在写盘前失败，`workspace: null` 删除该配置段。
- 读取到无效已存配置时返回错误，不降级为无限制。

## TDD 证据

- RED：`go test ./internal/piweb -run 'TestSaveAppConfig(NormalizesAllowedRoots|RejectsInvalidAllowedRootsWithoutChangingConfig|.*Workspace)'` 按预期失败，原实现未规范化或校验 workspace 配置。
- GREEN：同一 focused 测试通过。

## 验证

- `go test ./internal/piweb -run 'TestSaveAppConfig(NormalizesAllowedRoots|RejectsInvalidAllowedRootsWithoutChangingConfig|.*Workspace)'`：通过。
- `go test ./internal/piweb`：通过。
- `go test ./...`：通过。
- `git diff --check -- . ':(exclude)static'`：通过。

## Concerns

无。工作区中预先存在的 `static/` 生成文件变更未修改、未暂存。
