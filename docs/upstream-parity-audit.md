# 上游对照审查：会话窗口与流式输出

Last reviewed: 2026-09-14
对照基线：`agegr/pi-web`（`@agegr/pi-web@0.9.1`）
审查范围：会话窗口、流式输出、相关技术架构与功能差距
约束：本文只记录结论，不修改代码

## 1. 架构速览

| 维度 | 上游 agegr/pi-web | 本 fork |
| --- | --- | --- |
| 服务端 | Next.js 进程内 SDK `AgentSession`（`lib/rpc-manager.ts`） | Go 服务 + `pi --mode rpc` 子进程（`internal/piweb/runner.go`） |
| 会话真源 | pi JSONL 文件（`lib/session-reader.ts`） | 三份：pi JSONL、`session_events` SQLite 投影、Broker 内存 history |
| 流式通道 | 单条 SSE `/api/agent/[id]/events` | 三条：`events-v2`(durable)、`events`(broker)、`ag-ui` |
| 客户端流式状态 | React `useReducer(streamReducer)` 单一来源 | `transcriptState` reducer **加** 旧 DOM 流式双轨 |
| 断线恢复 | `connected` 握手 + 缓冲事件 + snapshot 去重 | seq 游标 + gap/reset/resync 重建 |
| 工具输出 | 仅 bash/powershell 走 `activeToolResults` 旁路 | 全工具 `tool.output` 事件 + DOM 累积 |
| 长会话渲染 | 可见窗口（尾部 50 条）+ `memo(MessageView)` | `virtual-scroller` 高度虚拟化 + 序号 item 列表 |

## 2. Critical

### 2.1 双状态源漂移（核心问题）

`transcriptState` 名义上是权威状态，但渲染仍走旧 DOM 路径，DOM 实际承担状态职责：

- `appendDelta` 把文本写进 `dataset.streamText`（`src/pi-app/messages/message-methods.ts:109`）
- `isDuplicateMessage` 通过查询已挂载 DOM 判重（`src/pi-app/messages/message-methods.ts:76`）
- `appendToolOutput` 把输出写进 `body.__toolOutputText`（`src/pi-app/messages/tool-message-methods.ts:192`）
- `latestToolCards` 缓存 DOM 节点用于后续 `tool.output` / `tool.finished` 定位

虚拟滚动卸载卡片后这些状态即失效，与 `.pi/tasks/stream-rendering-recovery-audit.md` 记录的
“Bash 卡片丢失 / 结束态追加第二项” 同源。

上游做法：`streamReducer` 持有流式消息，`messages` 数组持有已提交消息，DOM 只是纯投影（`lib/streaming-message.ts`、`hooks/useAgentSession.ts:1202`）。

### 2.2 全量重绘

- `renderMessages` 执行 `termInner.replaceChildren()` 并重建全部 `transcriptItems`（`src/pi-app/messages/message-methods.ts:25`）
- 每次 resync 与每次“加载更早消息”都会全量重绘（`src/pi-app/workspace/workspace-bootstrap-methods.ts:414`、`:618`）
- 展开态、滚动锚点、已渲染代码块在重绘后全部丢失

上游：keyed list + `memo(MessageView)`，只有新增消息产生 DOM 变更（`components/ChatWindow.tsx:1042`、`components/MessageView.tsx:273`）。

### 2.3 持久化写放大

- 每个 delta 触发一次 SQLite `BEGIN IMMEDIATE` 事务，事务内做事件插入 + 投影写入 + stream 更新（`internal/piweb/session_event_append.go:110`）
- `withImmediateTx` 为全库单写者模型（`internal/piweb/session_event_sqlite.go:94`）
- 由 `RuntimeEventAdapter.emitWithType` 逐事件调用（`internal/piweb/runtime_event_adapter.go:351`），仅按 16 KiB / 20 ms 合并

上游流式状态纯内存，只有 pi 自身在 `message_end` 写 JSONL。本 fork 相当于在 pi 的持久化之外再叠两层写路径。

### 2.4 快照与 JSONL 时序不一致

- `/api/sessions/{id}/state` 读 SQLite 投影（`internal/piweb/session_sse.go:34`、`:52`）
- 对已有文件只做导入、不等待导入完成即返回快照（`internal/piweb/session_sse.go:47`）
- 可能返回落后于 pi JSONL 的 items，却携带偏新的 `seq`

后果：最后一条 assistant / tool 消息在下次完整刷新前可能永久缺失（终态 snapshot 同样中招）。

## 3. Important

### 3.1 流式渲染 O(n²)

每帧把完整文本重新渲染后整体替换 `innerHTML`（`src/pi-app/messages/message-methods.ts:127`），
代码块、copy 按钮、工具卡内嵌节点随帧重建。文本越长单帧越贵。

### 3.2 节流策略不一致

- assistant delta：32 次兜底 + `requestAnimationFrame`（`message-methods.ts:131`、`transcript-render-scheduler.ts`）
- `tool.output`：只有单次 rAF，无计数兜底（`tool-message-methods.ts:195`）

回放洪峰下工具输出会被延迟，与 assistant 体验不一致。

### 3.3 多会话连接数线性增长

每个后台运行会话各自建立一条 SSE 并单独请求 `/state`（`src/pi-app/status/toast-methods.ts:317`）。
上游用 `/api/agent/running` 汇总运行态（`app/api/agent/running/route.ts`），不按会话开长连接。

### 3.4 终态信息易丢失

- `idle` / `cancelled` 触发 resync 时，瞬时错误与中断工具状态会被清掉
- AG-UI 路径不向后端透传工具失败状态（`internal/piweb/agui_event_adapter.go`）
- `tool.finished` 与最终 `tool.output` 的顺序在 AG-UI 侧与普通 SSE 侧不一致

### 3.5 自造协议面偏大

本 fork 定义 12 类会话事件、`entityId` 命名规则、`replace` / `append` 语义与投影校验
（`internal/piweb/session_projection.go`、`session_event_types.go`）。
上游直接透传 SDK 事件并只在 wire 层收敛（`lib/agent-event-wire.ts`），边界 case 更少。

## 4. 会话窗口功能差距

| 能力 | 上游 | 本 fork |
| --- | --- | --- |
| 分支 | 「新会话」「从此处编辑」两条 fork 路径 | 仅新建 / 重命名 / 删除 |
| 会话搜索 | `/api/sessions/search` | 无 |
| 导出 | 导出 HTML（`sessions/[id]/export`） | 无 |
| worktree 分组 | 有 | 无 |
| 消息元信息 | 时间戳、token、花费、上下文占用、压缩摘要卡片 | 仅 prompt 区上下文占用 |
| 导航 | ChatMinimap 小地图 + 大纲 | 无 |
| 大输出 | bash 专用通道 + 按需拉取 | 16k 截断预览 + `show full output` |
| 扩展 UI | extension widgets / status bar / dialog | 无 |
| 子代理 | subagent 面板与运行时 | 无 |
| 终端 | xterm 面板 | 无 |
| 通知 | PWA + Web Push | 无 |
| 输入 | `!` 直执行、队列消息展示（steering / followUp） | 仅 steer，无队列展示 |
| 渲染 | KaTeX、Mermaid、PDF、DOCX、音频、语法高亮 | 仅 markdown-it，无高亮 |
| i18n | en / zh-CN / zh-TW 完整 | 声明 8 语言，实际只有 en / zh-CN 完整（`src/i18n/ui.ts`） |

## 5. 本 fork 的设计优势

以下方面强于上游，不应在重构中丢失：

- Event-Sourcing：`seq` 单调、投影可重放、`gap` 可检测、rebased 可恢复（`internal/piweb/session_event_*.go`）
- 游标语义明确：`events-v2` 的 `after` + `stream.reset` 比“猜断线丢没丢事件”更可诊断（`src/pi-app/sessions/session-stream-client.ts`）
- 历史分页按 `ordinal` 游标 + `virtual-scroller` 高度虚拟化，长会话内存占用优于“尾部 50 条”方案
- 幂等命令：`session_commands` 带 `request_hash`，重复 prompt 不会重复执行（`internal/piweb/session_event_append.go:29`）

问题集中在集成层：durable 通道没有成为唯一渲染来源，旧的 DOM-first 流式代码仍是实际写路径。

## 6. 建议优先级

1. **单一状态源**：删除 DOM 作为状态源的部分（`dataset.streamText`、`body.__toolOutputText`、`latestToolCards`、DOM 判重），DOM 只作投影。
2. **统一节流**：`tool.output` 复用 assistant 的 32 次兜底；大工具输出改走 `items/{id}/content` 分段拉取。
3. **快照一致性**：`/state` 只返回“已覆盖到 `seq` 的确定态”，并与 pi JSONL 同步点对齐。
4. **增量渲染**：以 `itemId` 做 diff 替代 `replaceChildren` 全量重建，保留展开态与滚动锚点。
5. **功能补齐顺序**：会话 fork / 搜索 / 导出 / token 与花费 → 扩展与终端。

## 7. 证据索引

| 主题 | 位置 |
| --- | --- |
| SSE（durable） | `internal/piweb/session_sse.go` |
| 事件写入与事务 | `internal/piweb/session_event_append.go` |
| 投影归约 | `internal/piweb/session_projection.go` |
| pi JSONL 到事件 | `internal/piweb/runtime_event_adapter.go` |
| Broker（旧通道） | `internal/piweb/broker.go` |
| 客户端流式传输 | `src/pi-app/sessions/session-stream-client.ts` |
| 客户端 transcript 归约 | `src/pi-app/sessions/transcript-reducer.ts` |
| DOM 渲染路径 | `src/pi-app/messages/message-methods.ts`、`tool-message-methods.ts` |
| 虚拟滚动 | `src/pi-app/transcript/transcript-virtual-scroller.ts` |
| 既有同源审计 | `.pi/tasks/stream-rendering-recovery-audit.md`（未纳入版本控制） |
