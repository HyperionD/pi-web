# 浅色简体中文离线 Web UI 实施计划与交接记录

> **历史记录说明（2026-08-31）：** 本文保留原始实施上下文；其中关于更新提示、远程通知、语音输入、Whisper 和回答朗读的内容已由后续提交 `63e880d` 移除，不代表当前支持功能。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 基于 `Epsilondelta-ai/pi-web v1.9.0` 完成浅色蓝灰主题、默认简体中文、清除韩语 UI、Chrome 107 兼容，并构建为复用现有 `pi --mode rpc` 的离线单二进制。

**Architecture:** 保持现有 Go 后端、内嵌 Astro 静态前端和 `pi --mode rpc` 子进程架构不变。前端通过现有 i18n、设计令牌和组件实现中文浅色界面；Go 二进制继续嵌入构建产物。所有行为变更按 TDD 完成。

**Tech Stack:** Go、Astro、React、TypeScript、Vite 7、Vitest、Bun、CodeMirror、Shiki。

**Spec:** `docs/superpowers/plans/2026-08-27-light-zh-cn-ui-handoff.md` 中“已批准规格”一节。

## Global Constraints

- 完全离线运行，复用服务器已有 `pi` 二进制。
- 最终额外部署一个静态二进制，不要求目标服务器安装 Node.js/npm。
- 前端最低兼容 Chrome 107。
- 默认 UI locale 为 `zh-CN`，移除韩语 UI locale 和韩语字典。
- 保留韩语语音识别能力，但选项标签显示为“韩语”。
- 使用浅色蓝灰主题：主文字 `#0F172A`、次级文字 `#475569`、弱化文字 `#64748B`、强调色 `#2563EB`、Hover/Focus `#1D4ED8`、页面背景 `#F8FAFC`、卡片背景 `#FFFFFF`、边框 `#CBD5E1`。
- 不改变后端整体架构。
- 按用户决定，不处理默认监听、身份认证和 CORS 三项安全建议。
- 不在 `main` 上修改；当前工作必须留在功能分支。

---

## 已批准规格

1. 基线仓库：`https://github.com/Epsilondelta-ai/pi-web`。
2. 基线 tag：`v1.9.0`。
3. 基线 commit：`fa6512552e1cb6a736e614fa557dd1cd0f357e06`。
4. License：MIT。
5. 工作目录：`/home/hyperion/projects/pi-web-offline`。
6. 当前分支：`feat/light-zh-cn-ui`。
7. 中文策略：默认简体中文、删除韩语 UI 选项、替换硬编码韩语界面文案；语音识别的 `ko` 能力保留。
8. 浏览器策略：Vite 显式使用 `build.target = "chrome107"`，避免或降级 Chrome 107 不支持的 CSS。
9. 交付策略：执行完整 TypeScript、前端、Go、Astro 和单二进制构建验证后再交付。

## 当前仓库状态

- 工作目录：`/home/hyperion/projects/pi-web-offline`
- 分支：`feat/light-zh-cn-ui`
- HEAD：`fa6512552e1cb6a736e614fa557dd1cd0f357e06`
- 当前所有改动均未提交。
- 当前有 18 个已跟踪文件发生修改，另有 2 个新测试文件。
- 第二轮实现已完成；保留当前功能分支及全部相关改动，未重置或清理工作树。

## 已完成：第一轮 TDD（曾验证为 GREEN）

第一轮定向验证结果：6 个测试文件、31 个测试通过。

### 已完成实现

- `astro.config.ts`
  - 已设置 `vite.build.target = "chrome107"`。
- `src/design-system/tokens.css`
  - 已将黑绿深色令牌替换为浅色蓝灰令牌。
  - 已调整 ANSI 色、强调色、选择色、警告/危险背景和浅色阴影。
- `src/i18n/locales.ts`
  - 默认 locale 已改为 `zh-CN`。
  - 已从 `UI_LOCALES` 删除 `ko`。
- `src/i18n/ui.ts`
  - 已删除韩语 UI 字典。
  - 缺失翻译沿用现有默认 locale 回退机制，目标为回退中文。
- `src/pi-app/editor/file-highlight.ts`
  - Shiki 主题已由 `github-dark` 改为 `github-light`。
- `src/pi-app/editor/file-preview-methods.ts`
  - 文件过大、图片只读预览和不支持预览提示已改为简体中文。
- `src/pi-app/workspace/settings-schema.ts`
  - 保留 `{ value: "ko" }`，显示标签改为“韩语”。

### 第一轮新增或更新测试

- `src/browser-compatibility.test.ts`
- `src/design-system/tokens.test.ts`
- `src/i18n/locales.test.ts`
- `src/pi-app/editor/file-highlight.test.ts`
- `src/pi-app/editor/file-preview.test.ts`
- `src/pi-app/workspace/settings-schema.test.ts`

## 当前状态：第二轮 TDD（已完成并验证 GREEN）

2026-08-27 已执行：

```bash
bun x vitest run \
  src/components/WorkspaceFileTree.test.tsx \
  src/pi-app/editor/file-preview.test.ts \
  src/pi-app/input/fallback-choices.test.ts \
  src/pi-app/input/controls.test.ts \
  src/pi-app/status/toasts.test.ts \
  src/pi-app/core-events.test.ts
```

结果：6 个测试文件失败，64 个测试中 20 个失败、44 个通过。失败项准确暴露尚未中文化的文件树、编辑器搜索、fallback choice、Toast、认证警告和语音输入文案。`controls.test.ts` 另有 1 个 Whisper 测试在本次批量运行中超时，完成实现后需单独复验。

2026-08-27 已执行：

```bash
go test ./internal/piweb -run 'TestNotify(Discord|Telegram)ResponseCompleted|TestPiRPCArgsUseRPCMode'
```

结果：3 个测试失败，准确暴露 Go 后端 choice 协议和 Discord/Telegram 通知文案仍为韩语。

### 第二轮测试与实现结果

- `src/components/WorkspaceFileTree.test.tsx`
  - 期望搜索标签为“搜索”。
  - 期望占位符为“搜索全部内容”。
- `src/pi-app/editor/file-preview.test.ts`
  - 期望编辑器搜索标签为“搜索”。
  - 期望占位符为“搜索文件内容”。
- `src/pi-app/input/fallback-choices.test.ts`
  - 期望回复协议前缀为 `选项回复：`。
- `src/pi-app/input/controls.test.ts`
  - 已将 UI locale 测试目标改为 `zh-CN`。
  - 已将语音输入、Whisper 错误提示断言改为中文。
  - 已将韩语语音显示值断言改为“韩语”。
- `src/pi-app/status/toasts.test.ts`
  - 已将成功、失败、断线、更新、认证和选项通知断言改为中文。
- `src/pi-app/core-events.test.ts`
  - 已将认证警告断言改为中文。
- `internal/piweb/discord_notifications_test.go`
  - 已将 Discord/Telegram 通知断言改为 `回答完成：` 和 `问题：`。
- `internal/piweb/prompt_attachments_test.go`
  - `TestPiRPCArgsUseRPCMode` 已增加 system prompt 必须包含 `选项回复：` 的断言。

## Task 1：完成散落 UI 文案中文化

**Files:**
- Modify: `src/components/WorkspaceFileTree.tsx`
- Modify: `src/pi-app/editor/file-editor.ts`
- Test: `src/components/WorkspaceFileTree.test.tsx`
- Test: `src/pi-app/editor/file-preview.test.ts`

**Interfaces:**
- Consumes: 现有 React 文件树和 CodeMirror 搜索控件。
- Produces: 中文搜索标签、占位符和空结果提示，不改变搜索行为。

- [x] **Step 1: 修改文件树文案**

将以下界面文本替换：

```text
검색                -> 搜索
전체 검색           -> 搜索全部内容
검색 결과가 없습니다. -> 没有搜索结果。
```

可同时将同一组件中直接可见的英文辅助文案改为中文：

```text
file tree loads from backend.                    -> 文件树由后端加载。
tip: right-click or tap ⋯ for file actions.      -> 提示：右键单击或点击 ⋯ 可执行文件操作。
```

保留 `aria-label="search all files"` 不会阻断当前 RED 测试；若一并中文化，使用 `aria-label="搜索全部文件"`。

- [x] **Step 2: 修改编辑器搜索文案**

在 `createEditorSearchNode` 中进行精确替换：

```ts
label.textContent = "搜索";
input.placeholder = "搜索文件内容";
```

- [x] **Step 3: 运行定向测试**

```bash
bun x vitest run src/components/WorkspaceFileTree.test.tsx src/pi-app/editor/file-preview.test.ts
```

预期：两个测试文件全部通过。

## Task 2：完成 fallback choice 协议中文化

**Files:**
- Modify: `src/pi-app/input/fallback-choices.ts`
- Modify: `internal/piweb/runner.go`
- Test: `src/pi-app/input/fallback-choices.test.ts`
- Test: `internal/piweb/prompt_attachments_test.go`

**Interfaces:**
- Consumes: `choiceReplyPrompt(id, value)` 和 `parseChoiceReply(text, id)`。
- Produces: 前后端一致的 `选项回复：\nid: ...\nvalue: ...` 协议。

- [x] **Step 1: 修改前端回复和解析前缀**

```ts
export function choiceReplyPrompt(id, value) {
  return `选项回复：\nid: ${id}\nvalue: ${value}`;
}
```

并将解析条件改为：

```ts
if (!text.startsWith("选项回复：") || !id) return undefined;
```

- [x] **Step 2: 修改 Go system prompt**

在 `internal/piweb/runner.go` 的 `piWebChoiceSystemPrompt` 中，将提供给模型的回复格式从韩语前缀改为：

```text
选项回复：
id: <id>
value: <value>
```

其余 JSON schema 和英文协议说明保持不变，避免扩大改动范围。

- [x] **Step 3: 运行前后端定向测试**

```bash
bun x vitest run src/pi-app/input/fallback-choices.test.ts
go test ./internal/piweb -run TestPiRPCArgsUseRPCMode
```

预期：两个命令均退出码 0。

## Task 3：完成语音输入和 Whisper 提示中文化

**Files:**
- Modify: `src/pi-app/input/input-methods.ts`
- Modify: `src/pi-app/test-helper.ts`
- Test: `src/pi-app/input/controls.test.ts`

**Interfaces:**
- Consumes: `showSystemToast(level, title, detail, key)` 和语音语言 datalist。
- Produces: 不改变 Web Speech/Whisper 行为，仅更新用户可见文案和测试 fixture。

- [x] **Step 1: 替换 input-methods.ts 文案**

使用第二轮测试中已经写明的精确中文：

```text
语音输入需要安全上下文
语音输入仅支持 HTTPS 或 localhost 等安全上下文。
不支持语音输入
当前浏览器不支持 Web Speech API 语音输入，请使用 Chrome 或 Safari。
语音输入错误
无法启动语音输入。
不支持 Whisper 录音
当前浏览器不支持本地录音。
Whisper 录音错误
Whisper 转写错误
此 Whisper 模型对当前设备过大，请选择更小的模型。
Whisper 下载错误
```

只替换 Toast 标题和详情；错误对象中的原始英文消息继续透传。

- [x] **Step 2: 同步测试 fixture**

在 `src/pi-app/test-helper.ts`：

```html
<option value="zh-CN">简体中文</option>
```

替换测试 fixture 中的韩语 UI locale option。语音 datalist 中保留 `data-language-value="ko"`，将显示值改为：

```html
<option value="韩语" label="ko" data-language-value="ko"></option>
```

- [x] **Step 3: 单独运行 controls 测试**

```bash
bun x vitest run src/pi-app/input/controls.test.ts --testTimeout=10000
```

预期：27 个测试全部通过。若 “records local Whisper audio and writes the transcription” 仍超时，单独执行：

```bash
bun x vitest run src/pi-app/input/controls.test.ts -t 'records local Whisper audio and writes the transcription' --testTimeout=10000
```

在没有复现证据前不要修改异步实现。

## Task 4：完成 Toast、认证和远程通知中文化

**Files:**
- Modify: `src/pi-app/status/toast-methods.ts`
- Modify: `internal/piweb/discord_notifications.go`
- Test: `src/pi-app/status/toasts.test.ts`
- Test: `src/pi-app/core-events.test.ts`
- Test: `internal/piweb/discord_notifications_test.go`

**Interfaces:**
- Consumes: `TOAST_COPY`、`showRuntimeUpdateWarnings()`、`showRuntimeAuthWarning()` 和远程通知发送函数。
- Produces: 浏览器 Toast、Discord 和 Telegram 的简体中文通知。

- [x] **Step 1: 中文化 TOAST_COPY 和默认值**

使用以下文案：

```text
回答完成 / 会话回答已完成。
需要选择 / 会话请求你选择一个选项。
警告 / 需要检查。
回答失败 / 获取回答失败。
后端连接已断开 / 前端与后端的连接已断开，正在重新连接。
无工作区
无会话
未知
关闭全部
```

- [x] **Step 2: 中文化更新与认证警告**

标题使用：

```text
pi-web 有可用更新
pi 有可用更新
身份验证警告
```

认证详情必须包含“请在设置中重新登录”。更新详情保留可复制执行的 `pi-web update` 和 `pi update` 命令。

- [x] **Step 3: 中文化远程通知**

在 `internal/piweb/discord_notifications.go` 中使用全角冒号，与测试保持一致：

```go
content := "✅ 回答完成：" + truncateDiscordSessionTitle(sanitizeDiscordContent(title))
content += "\n问题：" + question
```

- [x] **Step 4: 运行定向测试**

```bash
bun x vitest run src/pi-app/status/toasts.test.ts src/pi-app/core-events.test.ts
go test ./internal/piweb -run 'TestNotify(Discord|Telegram)ResponseCompleted'
```

预期：两个命令均退出码 0。

## Task 5：扫描残留韩语并区分界面文案与测试样本

**Files:**
- Inspect: `src/**`
- Inspect: `internal/**`
- Modify only when text is user-visible or protocol-visible.

**Interfaces:**
- Consumes: 完成 Task 1–4 后的源码。
- Produces: 生产界面和协议中无韩语文案；韩语语音能力仍可用。

- [x] **Step 1: 扫描生产源码**

```bash
rg -n '[가-힣]' src internal --glob '!**/*.test.*' --glob '!**/test-helper.ts'
```

预期：无生产源码匹配。允许保留的是语言代码 `ko`，不是韩文显示文字。

- [x] **Step 2: 扫描测试和 fixture**

```bash
rg -n '[가-힣]' src internal
```

以下韩文可作为语音识别、Unicode 或历史消息解析的非 UI 测试样本保留：

- `src/pi-app/input/controls.test.ts` 中的韩语转写输入和输出。
- `internal/piweb/prompt_attachments_test.go` 中的用户消息样本。
- `internal/piweb/agui_test.go` 中的流式文本样本。

若目标被解释为“仓库完全不含韩文字符”，可将这些测试样本改为中文，但不要删除 `ko` / `ko-KR` 语音能力测试。

## Task 6：完整验证与离线单二进制构建

**Files:**
- Verify: entire repository.
- Build output: `dist/pi-web`

**Interfaces:**
- Consumes: 所有实现任务的 GREEN 状态。
- Produces: 可部署的单二进制和完整验证证据。

- [x] **Step 1: TypeScript 类型检查**

```bash
bun run typecheck
```

预期：退出码 0。

- [x] **Step 2: 完整前端测试**

```bash
bun run test
```

预期：所有 Vitest 测试通过，无失败和超时。

- [x] **Step 3: 完整 Go 测试**

```bash
bun run backend:test
```

预期：所有 Go package 通过。

- [x] **Step 4: 构建 Astro 前端**

```bash
bun run build
```

预期：生成 `dist/`，退出码 0。

- [x] **Step 5: 构建单二进制**

```bash
bun run build:single
```

预期：生成 `dist/pi-web`。

- [x] **Step 6: 检查二进制类型和动态依赖**

```bash
file dist/pi-web
ldd dist/pi-web || true
```

目标：ELF 可执行文件；若按静态方式构建，`ldd` 应报告不是动态可执行文件。若默认 Go 环境产生动态链接，需要用以下命令重新构建并复验：

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=v$(node -p 'require(\"./package.json\").version')" -o dist/pi-web .
file dist/pi-web
ldd dist/pi-web || true
```

- [x] **Step 7: 检查构建产物未使用超出 Chrome 107 的显式 target**

```bash
rg -n 'chrome107' astro.config.ts
```

预期：匹配 `target: "chrome107"`。同时继续扫描并处理项目自行编写的 `color-mix()`；第三方已编译依赖不做无关重写：

```bash
rg -n 'color-mix\(' src
```

- [x] **Step 8: 查看最终差异**

```bash
git status --short
git diff --check
git diff --stat
```

预期：`git diff --check` 退出码 0；仅包含本功能相关文件。

## 明日开始时的推荐顺序

1. 读取本文和 `git status --short`，确认仍在 `feat/light-zh-cn-ui`。
2. 从 Task 1 开始完成第二轮最小实现，不重写已经通过的第一轮实现。
3. 每完成一个 Task 立即执行对应定向测试，保持 RED → GREEN 证据。
4. Task 1–4 全部 GREEN 后执行 Task 5 残留扫描。
5. 最后执行 Task 6 全量验证和单二进制构建。
6. 在全部验证通过前，不提交“已完成”结论；当前没有任何 commit。

## 当前未提交文件清单

```text
M  astro.config.ts
M  internal/piweb/discord_notifications_test.go
M  internal/piweb/prompt_attachments_test.go
M  src/components/WorkspaceFileTree.test.tsx
M  src/design-system/tokens.css
M  src/i18n/locales.test.ts
M  src/i18n/locales.ts
M  src/i18n/ui.ts
M  src/pi-app/core-events.test.ts
M  src/pi-app/editor/file-highlight.test.ts
M  src/pi-app/editor/file-highlight.ts
M  src/pi-app/editor/file-preview-methods.ts
M  src/pi-app/editor/file-preview.test.ts
M  src/pi-app/input/controls.test.ts
M  src/pi-app/input/fallback-choices.test.ts
M  src/pi-app/status/toasts.test.ts
M  src/pi-app/workspace/settings-schema.test.ts
M  src/pi-app/workspace/settings-schema.ts
?? src/browser-compatibility.test.ts
?? src/design-system/tokens.test.ts
?? docs/superpowers/plans/2026-08-27-light-zh-cn-ui-handoff.md
```
