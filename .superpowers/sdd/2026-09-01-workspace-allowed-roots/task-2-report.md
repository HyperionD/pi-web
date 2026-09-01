# Task 2 报告：real-path workspace policy

## 状态

已完成 workspace policy：

- 从完整 app config 提取 `workspace.allowedRoots`，缺少或空数组保持无限制语义。
- roots 与候选路径均 trim、展开 `~`、clean、转绝对路径并解析 symlink。
- 使用 `filepath.Rel` 进行目录边界判断，拒绝 sibling、`.`/`..` 越界及指向 allowed roots 外部的 symlink。
- `ValidateCloneDestination` 校验现有 parent、单段 clone name，以及已存在的目标目录。
- 无效 policy 返回 `ErrWorkspacePolicyInvalid`，并通过 invalid state fail-closed。

## 修改文件

- `internal/piweb/workspace_policy.go`：新增 `WorkspacePolicy`、构造/加载函数、路径检查、clone 目标检查及 sentinel errors。
- `internal/piweb/workspace_policy_test.go`：新增 policy 边界、symlink、tilde、多 roots、clone destination、fail-closed 与 app config 测试。

## TDD

- RED：`go test ./internal/piweb -run 'TestWorkspacePolicy'` 按预期因 policy 类型、函数和 sentinel 未定义而编译失败。
- GREEN：实现后同一 focused 测试通过。

## Commit

- `feat(workspace): add allowed roots policy`

## 测试

- `go test ./internal/piweb -run 'TestWorkspacePolicy'`：通过。
- `go test -race ./internal/piweb -run 'TestWorkspacePolicy'`：通过。
- `go test ./internal/piweb`：通过。
- `gofmt -w internal/piweb/workspace_policy.go internal/piweb/workspace_policy_test.go`：完成。
- `git diff --check`：通过。

## Concerns

- 工作区原有 `static/` 生成文件变更未修改、未暂存。
- 无其他 concerns。
