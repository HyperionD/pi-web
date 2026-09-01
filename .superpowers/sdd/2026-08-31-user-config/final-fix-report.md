# User Config 最终修复波次报告

## 修复范围

本波次仅处理最终审查的两个 Important findings：

1. app config 加载失败不再静默吞掉：保留不阻断 workspace bootstrap、默认 brand 保持不变的行为；失败通过现有 `showSystemToast` system toast 通道显示“应用配置加载失败”和原始错误信息，并保留无 toast 方法时的连接错误兜底。
2. `SaveAppConfig` 的 read/merge/write 临界区增加 package-level `sync.Mutex`；`LoadAppConfig` 使用同一把锁，避免 Load 与 Save 交错读取，并保证同一进程内并发 PATCH 不发生 lost update 或未知顶层字段丢失。

## 回归测试与 TDD 证据

先新增测试并运行旧实现：

- Go `TestSaveAppConfigSerializesConcurrentReadMergeWrites` 失败，报告 `lost concurrent update`。
- 前端 `shows app config load failures while continuing workspace bootstrap` 失败，找不到 `.session-toast.error`。

随后完成最小实现，以上两条测试均通过。

## 验证结果

- focused Go：`go test ./internal/piweb -count=1` — PASS。
- focused frontend：`bunx vitest run src/pi-app/workspace/app-config-methods.test.ts src/pi-app/workspace/workspace-bootstrap-coverage.test.ts src/pi-app/input/input-methods-coverage.test.ts` — 3 files / 33 tests PASS。
- Go 全量：`go test ./...` — PASS。
- 前端全量：`bun run test` — 37 files / 303 tests PASS。
- 类型检查：`bun run typecheck` — PASS。
- 构建：`bun run build` — PASS；仅有既存 chunk size warning。

build 前后已恢复 `static/` 的既有工作区状态，未将任何 static 构建产物纳入本波次修改。

## 变更文件

- `internal/piweb/app_config.go`
- `internal/piweb/app_config_test.go`
- `src/pi-app/workspace/workspace-bootstrap-methods.ts`
- `src/pi-app/workspace/workspace-bootstrap-coverage.test.ts`
- `src/pi-app/status/toasts.test.ts`（为新增启动错误 toast 提供确定性的成功 bootstrap fixture）

## Concerns

无新的功能性 concern。全仓库无路径过滤的 `git diff --check` 仍会报告既有 `static/index.html` 生成文件尾随空白；该文件未被本波次修改或提交。
