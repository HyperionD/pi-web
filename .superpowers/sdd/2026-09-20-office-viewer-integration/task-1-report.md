# Task 1 报告：Office viewer 资源交付边界

## 状态

已完成并提交原子 commit：`30cafcff`（`build(files): add scoped office viewer assets`）。

## 实施内容

- 固定 `@file-viewer/web`、`@file-viewer/preset-office`、`@file-viewer/assets-standard` 为 `3.1.2`，同步更新 `bun.lock`。
- 新增 `bun run prepare:file-viewer-assets`，调用标准 assets CLI 到同文件系统临时目录。
- 仅复制 PDF、DOCX、XLSX、PPTX 及共享字体/WASM 资源；识别并排除 archive/CAD/3D/Typst 等非 Office 输出。
- 对输出路径执行 traversal、符号链接、未知资产路径和缺失必需资源校验。
- 在所有资源和 SHA-256 manifest 校验完成后，以临时目录替换 `static/file-viewer/`。
- 新增契约测试和 README 部署说明。

## 验证

- `bun run lint`：通过
- `bun run typecheck`：通过
- `bun test scripts/prepare-file-viewer-assets.test.mjs`：1 pass
- `bun run test`：54 files / 608 tests passed
- `bun run backend:test`：通过
- `bun run build`：通过
- `bun run check:embedded-assets`：通过
- 临时 staging 检查：303 个文件、约 12 MiB；包含 Office worker，未包含 `libarchive`/CAD 路径。

## Concerns

生成的 `static/file-viewer/` 资源按 brief 未纳入 commit；部署或 `build:single` 前需运行 `bun run prepare:file-viewer-assets`。

提交前曾在与前端构建并行时观察到一次后端 `broken pipe`，单独重跑及最终串行验证均通过，未发现代码相关回归。

## Task 1 fix round 1

### 修复范围

- 使用 Linux `renameat2(RENAME_EXCHANGE)` 对已存在的资源目录执行真正的原子目录交换；交换后清理旧目录失败时再次交换回滚，交换失败时保留新旧两棵树。
- `--target` 仅允许默认 `static/file-viewer/`，或 `/tmp/pi-web-file-viewer-*` 专用临时目录；拒绝仓库根、项目根、`static/`、系统临时根、根目录和符号链接路径。
- 校验标准 manifest 中所有 renderer 的 `defaultPath`，拒绝绝对路径、Windows 路径、反斜杠、`.`/`..` 和空路径。
- 测试覆盖禁止资源、路径遍历、manifest 路径、SHA-256 内容匹配、target safety、原子替换失败保留和清理失败回滚。

### TDD 红阶段证据

```text
$ bun test scripts/prepare-file-viewer-assets.test.mjs --test-name-pattern 'dangerous target'
FAIL rejects dangerous target roots before modifying them
EACCES: permission denied, mkdtemp '/.pi-web-file-viewer-rOA1mX'
```

失败原因是旧实现未在创建临时目录前校验 `--target /tmp`，而是将临时目录创建位置解析为 `/`。

### 修复与验证命令摘要

```text
$ bun test scripts/prepare-file-viewer-assets.test.mjs
7 pass, 0 fail

$ bun run lint
$ bun run typecheck
```

`lint` 和 `typecheck` 均以退出码 0 完成；Task 1 测试 7 个用例全部通过，覆盖原子交换/失败回滚、target safety、路径遍历、禁止资源及 manifest SHA-256 校验。
