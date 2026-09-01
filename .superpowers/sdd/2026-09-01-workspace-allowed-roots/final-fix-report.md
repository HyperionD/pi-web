# Workspace Allowed Roots 最终修复波次报告

日期：2026-09-01

## 修复范围

1. **配置验证错误分类**
   - 新增 `ErrAppConfigValidation` sentinel error。
   - brand、`workspace.allowedRoots` 及 workspace 结构验证统一可分类。
   - PATCH 通过 `errors.Is` 将配置验证错误返回 `400`；配置文件读写错误仍返回 `500`。
   - 新增 allowedRoots PATCH `400` 与存储故障 `500` 回归测试。

2. **异步 workspace/session 请求隔离**
   - `loadWorkspaceMeta`、`loadWorkspaceCommands` 使用 workspace ID + loader token 守卫成功和失败路径。
   - 初始 session 加载使用 session/workspace 资源校验及 session token。
   - 历史消息加载使用 session/workspace ID、cursor 和请求 token 守卫。
   - workspace settings modal 的异步设置加载补充同样的 workspace/token 守卫。
   - API 请求错误保留 HTTP status，便于可靠识别 403。
   - 新增旧 workspace metadata/commands、旧 session history/settings 延迟 403 回归测试。

3. **clone 目标 symlink/TOCTOU 防护**
   - clone 前在已解析且已授权的真实父目录中原子创建目标目录，阻止目标被预先替换为 symlink。
   - git clone 后重新解析并校验真实目标；异常目标清理后返回错误，不注册 workspace。
   - 新增 fake git 在校验后执行 symlink swap 的回归测试，并通过 focused `-race` 测试。

4. **Windows `~/` 语义**
   - `ExpandUserPath` 通过分隔符感知的纯函数接受文档中的 `~/code`，Windows 分隔符语义得到覆盖。
   - 新增可在当前平台运行的 Windows 语义纯函数测试。

## TDD 证据

先写回归测试并运行旧实现：

- 旧 app-config handler：allowedRoots 验证返回 `500`，测试失败。
- 旧 clone 实现：symlink swap 后错误注册不受控目标，测试失败。
- 旧 bootstrap 实现：3 个延迟 403 测试均清空新 workspace/session，测试失败。
- 旧 Windows helper：目标纯函数不存在，编译失败，修复后测试通过。

修复后 focused 测试全部通过。

## 最终验证

- `go test ./internal/piweb`：通过
- `go test -race ./internal/piweb -run 'TestCloneGitWorkspaceRejectsSymlinkSwapAfterValidation'`：通过
- `bunx vitest run ...` focused workspace/session：41/41 通过
- `go test ./...`：通过
- `bun run backend:test`：通过
- `bun run test`：37 个测试文件、328/328 通过
- `bun run typecheck`：通过
- `bun run build`：通过；仅有既有 chunk-size warning
- `git diff --check -- . ':(exclude)static'`：通过

## 其他

- 未修改或暂存 `static/` 现有生成文件变更。
- policy invalid 仍 fail-closed；缺失/空 roots 仍不限制；403/404/500 分类及 static 输出语义保持不变。
- focused race 通过；未额外扩大既有全包 race 检查范围。
