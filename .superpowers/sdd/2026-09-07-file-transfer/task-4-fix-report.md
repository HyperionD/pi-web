# Task 4 修复报告：review warnings

## Status

已修复 Task 4 review 的四项 warning：

- 增加仅修改 `maxUploadBytes` 时保留既有 `allowedRoots` 的回归测试。
- 增加合法边界 `1` / `2048` MiB 测试，以及小数拒绝测试。
- 同步 `src/pi-app/test-helper.ts` fixture 与 `App.astro` 输入：`min="1"`、`max="2048"`、`step="1"`、`inputmode`、`autocomplete` 和提示文本。
- 为提示增加 `id="max-upload-mib-hint"`，并通过 `aria-describedby` 关联上传上限输入。

## TDD / Verification

- RED：focused suite 51 tests，1 failure；失败为 fixture 缺少上传输入的 `min` 约束。
- GREEN：`bunx vitest run src/pi-app/workspace/app-config-methods.test.ts` → 51/51 passed。
- Full Vitest：`bunx vitest run` → 38 files，432/432 passed。
- `git diff --check`：通过。
- `bun run typecheck`：仍失败于既有未修改文件 `src/components/WorkspaceFileTree.tsx:217,220`，均为 `string` 传入 `File` 参数的类型错误。

## Scope

未修改后端、传输或附件组件。
