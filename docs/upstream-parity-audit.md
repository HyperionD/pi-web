# 上游对照审查：会话窗口与流式输出

Last reviewed: 2026-09-14
本项目基线：`06985614`
上游基线：`agegr/pi-web@8366762`
范围：会话窗口、流式输出、直接相关的服务端与前端能力

## 结论

本项目的 durable event log、单调 `seq`、gap/reset/resync 和历史分页比上游更可诊断。
P0 修复已完成：`TranscriptState → ViewModel → DOM` 是唯一渲染链路；legacy SSE 仅通过 adapter 进入 reducer；AG-UI 传输层已删除。
虚拟滚动不再永久持有 DOM 节点，工具输出、失败状态和流恢复均由 reducer 状态驱动。

## 架构对照

| 维度 | 上游 `agegr/pi-web` | 本项目 |
| --- | --- | --- |
| 运行时 | Next.js 进程内 `AgentSession` | Go 服务管理 `pi --mode rpc` |
| 历史真源 | pi JSONL | pi JSONL + SQLite 投影 + Broker history |
| 主事件流 | `/api/agent/[id]/events` SSE | durable SSE；legacy SSE 仅作 adapter 输入 |
| 客户端状态 | React state/reducer | `TranscriptState → ViewModel → DOM` |
| 恢复 | 握手、快照、轮询兜底 | durable seq/gap/reset；旧通道按 Broker 游标恢复 |
| 长会话 | 尾部 50 条逐页扩展 | 120 条服务端分页 + DOM 高度虚拟滚动 |

## P0：正确性风险

### 1. durable reducer 与 DOM 双状态源（已修复）

- `TranscriptState` 统一归约 assistant/tool/status：`src/pi-app/sessions/transcript-reducer.ts`。
- `selectTranscriptView()` 生成 ViewModel，DOM 只由 `renderDurableTranscript()` 和虚拟滚动投影。
- 已移除 `dataset.streamText`、`__toolOutputText`、`latestToolCards` 和永久 `item.nodes`。
- legacy 事件通过 `legacy-session-event-adapter.ts` 转成 reducer 事件，不再直接改 DOM。

工具滚出窗口后继续收到 output/finish 时，状态仍可从 reducer 重建，且已有节点按 item key 增量 patch。

### 2. durable EventSource 旧连接竞态（已修复）

`session-stream-client.ts` 为每次连接保存 generation，并在 `open/message/error` 回调中校验当前连接；旧连接不能关闭或推进新连接。回归覆盖见 `src/pi-app/sessions/session-stream-client.test.ts`。

### 3. AG-UI 工具终态顺序（已移除）

AG-UI SDK、路由、Go adapter、API 和专用测试已删除。工具终态统一由 durable `tool.finished` reducer 事件产生，body、status、resultMeta 在同一状态投影中更新。

### 4. Broker 游标前置推进（已移除双通道风险）

客户端不再消费 AG-UI cursor；durable SSE 只按事件 `seq` 归约，并在 gap 时执行 snapshot resync。legacy SSE 也先经过同一 reducer 序列化路径。

## P1：性能与可维护性风险

### 1. 流式 Markdown 累积重渲染（已缓解）

assistant delta 先归约到 `TranscriptState`，再由 ViewModel/虚拟滚动 patch 对应 item；不再维护 DOM 流文本缓存或独立 rAF streaming DOM 状态。

### 2. resync 与 durable 历史前插全量重建

`renderMessages()` 清空容器和 transcript window：`src/pi-app/messages/message-methods.ts:18-30`。
durable resync 和 durable “加载更早”都走该路径：
`src/pi-app/workspace/workspace-bootstrap-methods.ts:379-383,414-416,612-618`。
非 durable 历史分页已使用增量 prepend，不应把问题扩大描述为所有路径。

影响：展开态、局部交互状态和滚动锚点更易丢失，长会话恢复成本高。

### 3. 每个合并事件都全量读取投影并独占 SQLite writer

assistant delta 虽按 16 KiB/20 ms 合并，但每次 flush 仍调用一次 `Append()`：
`internal/piweb/runtime_event_adapter.go:198-220,342-355`。`Append()` 在事务中加载该会话全部
`session_items`、归约、写事件、写投影和更新 stream：
`internal/piweb/session_event_append.go:108-174`、
`internal/piweb/session_event_projection_sql.go:11-33`。所有会话又共享单一 `writeSlot` 和
`BEGIN IMMEDIATE`：`internal/piweb/session_event_sqlite.go:90-153`。

影响：长会话和多会话并发时，token 消费路径会被 O(历史项数) 的投影读取及
全库单写者串行化反压。
上游流式增量主要留在 React 内存状态，不为每个增量额外维护 SQLite event projection：
`agegr/pi-web/hooks/useAgentSession.ts:1202-1221`。

### 4. 大工具输出的分段接口未接入会话 UI

后端已有 range content API：`internal/piweb/session_sse.go:152-183`，但 snapshot 的
`ProjectedSessionItem` 仍序列化完整 `Body`：
`internal/piweb/session_event_types.go:65-87`、
`internal/piweb/session_event_query.go:76-119`。前端 `getSessionItemContent()` 无生产调用，
完整 body 又保存在 DOM 私有属性：`src/lib/api.ts:398-405`、
`src/pi-app/messages/tool-output-rendering.ts:5-35`。

影响：同一大输出同时放大 SQLite payload、chunk 表、snapshot JSON、浏览器字符串和 DOM 缓存。
上游至少对 bash/powershell 使用受控按需读取/下载：
`agegr/pi-web/app/api/agent/[id]/bash-output/route.ts:12-63`。

### 5. 虚拟滚动节点内存（已修复）

虚拟滚动 item 只保存消息/ViewModel 数据，卸载时释放 DOM 节点；重新挂载通过 `messageNode()` 从状态重建。

### 6. runtime 工具事件适配不完整

- `message_update` 只接收 text/thinking delta，忽略 pi 的 `toolcall_start/toolcall_delta`：
  `internal/piweb/runtime_event_adapter.go:84-91,142-160`。
- 缺少 `toolCallId` 时，start/update/end 每次调用 `toolEntity()` 都递增计数并生成不同实体：
  `internal/piweb/runtime_event_adapter.go:285-318`。

影响：工具参数不能实时显示；缺 ID 的异常帧会让投影找不到工具实体并中止 run。

### 7. 后台运行会话按会话维持 durable SSE

每个后台运行会话分别请求 `/state` 并建立一条 SSE：
`src/pi-app/status/toast-methods.ts:286-357`。会话数增加时连接、handler 和 SQLite 查询线性增加。
上游使用聚合 `/api/agent/running` 快照：
`agegr/pi-web/app/api/agent/running/route.ts:10-18`。

## 会话窗口功能差距

### P1

1. **从历史消息 fork/edit**：本项目会话 API 支持创建、重命名、删除、取消、压缩与 steer，
   但缺历史 `fork`/`fork_branch`。
   - 本项目：`src/lib/api.ts:149-153,275-298,383-388`
   - 上游：`agegr/pi-web/hooks/useAgentSession.ts:1534-1555`
   - 上游：`agegr/pi-web/lib/rpc-manager.ts:728-795`
2. **会话全文搜索与 HTML 导出**：本项目无对应路由/UI。
   - 上游：`agegr/pi-web/components/SessionSearch.tsx:1-70`
   - 上游：`agegr/pi-web/app/api/sessions/[id]/export/route.ts:241-270`
3. **worktree 管理与分组**：本项目无 worktree API；上游支持创建、删除、切换和项目分组。
   - 上游：`agegr/pi-web/app/api/worktrees/route.ts:17-97`
   - 上游：`agegr/pi-web/components/SessionSidebar.tsx:714-906`
4. **扩展交互 UI**：本项目 durable 协议只覆盖消息、工具和运行状态；
   缺 dialog、custom UI、status、widget。
   - 本项目：`internal/piweb/session_event_types.go:10-25`
   - 上游：`agegr/pi-web/hooks/useAgentSession.ts:786-876,1359-1363`
5. **持久化交互终端**：本项目是一次性 shell 请求；缺 xterm 会话、tab、resize 和重连。
   - 本项目：`src/pi-app/input/input-methods.ts:137-180`
   - 上游：`agegr/pi-web/components/TerminalPanel.tsx:46-123`
6. **steering/follow-up 队列可视化**：本项目运行中直接 `steer`，没有队列展示、移除与召回。
   - 本项目：`src/pi-app/input/input-methods.ts:9-15,114-127`
   - 上游：`agegr/pi-web/hooks/useAgentSession.ts:1329-1335,1801-1869`
7. **消息级元信息**：本项目会话消息模型不保留 timestamp、provider/model、usage/cost。
   - 本项目：`internal/piweb/types.go:36-50`、`internal/piweb/pi_session_messages.go:30-69`
   - 上游：`agegr/pi-web/lib/types.ts:52-93`、`agegr/pi-web/components/MessageView.tsx:759-842`
8. **会话富媒体**：本项目会话渲染支持基础 Markdown/table/code，
   但无 KaTeX、Mermaid、assistant/tool 图片。
   - 本项目：`src/lib/renderers.ts:1-66`
   - 上游：`agegr/pi-web/components/MessageView.tsx:336-377,1034-1123`

### P2

1. **长会话导航**：缺 ChatMinimap、分支导航和大纲。
   - 上游：`agegr/pi-web/components/ChatMinimap.tsx:230-271,615-732`
   - 上游：`agegr/pi-web/components/BranchNavigator.tsx:1-15`
2. **系统通知**：只有页内 toast，缺浏览器通知、PWA/Web Push。
   - 本项目：`src/pi-app/status/toast-methods.ts:155-186,250-282`
   - 上游：`agegr/pi-web/lib/browser-notifications.ts:39-92`
3. **子代理专用面板**：已有 subagent/team 父子会话发现和分组，
   但缺运行详情、控制和工具卡跳转。
   - 本项目：`src/pi-app/sessions/session-hierarchy.ts:1-50`、`internal/piweb/team_sessions.go:37-89`
   - 上游：`agegr/pi-web/components/AgentSessionPanel.tsx:153-231`
4. **完整 i18n**：8 个 locale 的设置词典均存在，但词典仅约 31 个设置项；
   主会话大量文案硬编码。因此不是“仅 en/zh-CN 有翻译”，
   而是所有语言都未覆盖核心会话 UI。
   - 本项目：`src/i18n/ui.ts:3-66`、`src/i18n/client.ts:34-55`、`src/App.astro:47-58,201-203`

## 本项目应保留的优势

- durable `seq`、gap/reset/resync、投影重放和 rebased 恢复。
- `session_commands` 的 request hash 幂等语义，避免重复 prompt。
- 服务端历史分页、内容 range API 和 team child session 发现。
- Go 单二进制、账户/工作区授权能力；这些不属于上游会话 UI 的直接替代项。

## 后续实施顺序

1. 将大输出 snapshot 改为 preview + `contentRef`，UI 按 range 加载，不再内联完整 body。
2. 避免每事件全量加载 projection；采用实体级 SQL 更新或内存 actor + 批量 checkpoint。
3. 再补 fork/search/export、队列、扩展 UI、终端和富媒体。

P0 架构迁移、虚拟滚动重建、工具终态、legacy adapter 和 AG-UI 删除已完成；本次验证覆盖 Vitest、Go tests、lint、typecheck 与 build。

## 对旧版审查结论的修正

- `/state` 在 `Snapshot()` 前同步执行 `OpenSession()`；“不等待 JSONL 导入”不是当前实现：
  `internal/piweb/session_sse.go:34-59`。
- 上游不是单一 state 变量，而是 React 管理的 `messages`、`activeToolResults`、`streamState`；
  关键区别是不用 DOM 作为状态存储。
- 写放大发生在每个**合并后的**事件，不是每个原始 token delta。
- durable “加载更早”会全量重绘，legacy prepend 不会。
- 本项目已有 subagent/team 基础分组，不能写成“无子代理”。
- 本项目 8 个 locale 词典对其有限 key 均完整；问题是核心会话 UI 未接入，
  而非只有两种语言完整。
- 文件预览已有 Shiki/CodeMirror；“无高亮”只适用于会话 Markdown 代码块。
