# 会话域单事件流迁移设计

- 日期：2026-09-15
- 状态：待审阅
- 迁移策略：分阶段迁移（staged）
- 范围：仅会话域

## 1. 目标

将会话域改为“一个权威事件流、一个投影模型、一个客户端订阅入口”：

```text
命令 REST
  → SessionCommandService
  → SQLite session_events（唯一写入真源）
  → projection / seq / wake hub
  → /state 快照 + /events-v2 实时流
  → TranscriptState reducer
  → ViewModel / DOM、通知和会话列表
```

最终要求：

1. 会话状态不再由 Broker、JSONL、REST 回读结果、前端 optimistic state 或 `setMode()` 独立维护。
2. 所有影响会话可见状态的操作都追加事件，并在同一事务中更新投影。
3. 页面刷新、断线重连、服务重启、多标签页观察同一会话时，均由 snapshot + event replay 重建相同状态。
4. 旧 `/events` 只能在迁移期间作为由新事件流派生的兼容读接口，不能再作为写入或状态真源。
5. 迁移完成后删除旧 Broker 会话流、legacy adapter、legacy fallback 和旧接口。

## 2. 非目标

本次不改造：

- 工作区文件树、编辑器、Git、workspace settings；
- 账号、OAuth、全局布局、语言和 prompt 草稿；
- 工作区 shell 的独立终端能力；
- 非会话域的 REST/API 设计。

工作区授权仍可作为访问边界，但不作为会话状态来源。

## 3. 当前问题边界

当前 durable 主会话已经具备 `session_events`、单调 `seq`、projection、`/state` 和 `/events-v2`，但仍存在以下旁路：

- `compact` 直接写 Store + Broker；
- 自动重命名通过 `broker.Publish("session.renamed")`；
- legacy `/events` 与 `/events-v2` 并存；
- durable 与 legacy 由 `durableEvents` 分支选择；
- steer/cancel 在客户端存在本地追加或 `setMode()`；
- 会话元数据、model、context usage、部分状态仍由 REST 直接返回或写入；
- JSONL/Store 既承担导入，又被部分会话接口作为运行时读取源。

## 4. 目标架构

### 4.1 唯一写入入口

所有会话命令必须进入 `SessionCommandService`：

- `session.prompt.submit`
- `session.steer`
- `session.cancel`
- `session.compact`
- `session.rename`
- `session.model.set`
- `session.delete`
- 会话创建也必须产生 `session.created`

命令需要 `clientRequestId` 和请求 hash，使用现有幂等 receipt 机制。命令 HTTP 响应只返回：

```json
{
  "accepted": true,
  "commandId": "...",
  "runId": "...",
  "acceptedSeq": "..."
}
```

HTTP handler 不得直接调用 `Broker.Publish`、`appendMessageAndPublish` 或直接修改会话投影。

### 4.2 会话事件分类

保留当前已验证的运行事件，并补齐控制和元数据事件：

| 类别 | 事件 |
| --- | --- |
| 生命周期 | `session.created`, `session.renamed`, `session.deleted`, `session.model.changed` |
| 用户输入 | `user.message.created`, `steer.accepted` |
| 运行控制 | `run.started`, `run.cancel.requested`, `run.cancelled`, `run.finished`, `run.failed`, `run.interrupted` |
| assistant | `assistant.segment.started`, `assistant.segment.committed` |
| tool | `tool.started`, `tool.args.delta`, `tool.output`, `tool.finished` |
| 压缩 | `compaction.started`, `compaction.committed`, `compaction.failed` |
| 运行指标 | `context.usage.updated`, `runtime.model.updated` |
| 恢复 | `session.rebased` |

事件必须携带 `sessionId`、`runId`（适用时）、`sourceId`、`sourceOrdinal`、`occurredAt` 和可校验 payload。所有状态改变在 append + projection + stream cursor 更新的同一事务内完成。

### 4.3 投影

SQLite event store 维护以下投影，客户端只能读取投影或事件流：

- `SessionMetadataProjection`：标题、workspace、创建/删除状态、model；
- `TranscriptProjection`：消息、assistant segment、tool item、内容版本；
- `RunProjection`：running/thinking/compacting/idle/cancelled/error；
- `UsageProjection`：context usage、token/usage 信息；
- `NotificationProjection`：后台会话完成、失败、fallback choice 等派生通知。

JSONL 只允许作为首次导入或显式 rebase 的输入；导入完成后，运行时读取不得再次绕过 event store。

### 4.4 HTTP 读取接口

保留并收敛为：

- `GET /api/sessions/:id/state`：初始/恢复/历史分页快照；
- `GET /api/sessions/:id/events-v2`：唯一实时事件流；
- `GET /api/sessions/:id/items/:itemId/content`：事件投影内容的 range 读取；
- 会话列表/访问控制可读取 `SessionMetadataProjection`，不得重新从 JSONL/Store 拼装运行状态。

`GET /api/sessions/:id` 迁移期可返回兼容格式，但数据必须来自同一 projection，不能继续使用 Broker cursor 或独立 `sessionStatus()`。

## 5. 前端改造

### 5.1 删除双轨选择

- 删除 `durableEvents` 对新旧加载路径的行为分支；
- 启动时若 event store 不可用，明确显示“不支持会话流”，不得 fallback 到 legacy `/events`；
- `loadSession()` 只调用 `getSessionState()`；
- `connectEvents()` 只替换为 `createSessionStreamClient()`；
- 删除 `legacy-session-event-adapter.ts` 及其生产调用。

### 5.2 单一状态来源

- `TranscriptState` 负责消息、tool 和运行状态；
- `selectTranscriptView()` 负责 UI projection；
- `setMode()` 改为 dispatch/等待事件，不再写 `running`、`compacting` 作为事实状态；
- prompt 不再先插入持久消息，等待 `user.message.created`；loading 只能是事件派生的临时 ViewModel；
- steer 不再在 REST 成功后 `appendMessage()`；
- cancel/compact 不再在 REST 返回后直接设置终态，统一等待 `run.*`/`compaction.*` 事件；
- 后台通知只消费同一 `session-stream-client`，通知状态由事件派生。

### 5.3 会话元数据

重命名、删除、model 变更和运行指标必须通过 event projection 更新。REST 成功不代表 UI 已完成变更；UI 只在收到对应事件后更新。

## 6. 分阶段迁移

### 阶段 0：建立不变量和集成基线

- 增加命令 → event store → projection → SSE 的真实服务集成测试；
- 为 prompt、steer、cancel、compact、rename、model、delete 建立事件序列 fixture；
- 增加静态检查：会话域禁止新增 `Broker.Publish`、`appendMessageAndPublish`、`/events` 客户端调用；
- 记录当前 legacy/durable 输出差异，作为迁移前基线。

### 阶段 1：后端事件完整化

- 将 `compact` 改为 durable command 和 `compaction.*` 事件；
- 将自动/手动 rename 改为 `session.renamed`；
- 将 steer/cancel 的所有接受、失败和终态写入 event store；
- 将 session create/delete/model/usage 纳入 metadata/runtime projection；
- 将 JSONL 导入改为显式 event import/rebase，不允许运行时读 Store 作为状态真源；
- Broker 暂时保留为兼容读适配器：只能订阅新事件并转换旧格式，禁止业务代码直接 publish。

### 阶段 2：前端切换

- 主会话、历史分页、恢复、后台通知全部切换到 `/state` + `/events-v2`；
- 删除 optimistic transcript、旧 cursor、legacy adapter 和 REST 后置状态写入；
- 对 command receipt 与事件完成分离建模：按钮反馈显示“已接受/处理中”，终态由事件产生；
- 增加刷新、断线、重连、切换会话和多标签页竞态测试。

### 阶段 3：兼容收缩

- `/events` 改为明确标记 deprecated，只能由新事件流适配生成；
- legacy 客户端连接收到迁移提示，不再允许服务端走旧执行路径；
- durable event store 不可用时返回 503/功能不可用，不 fallback 到 Broker；
- 观察一段版本周期内旧接口访问指标和错误日志。

### 阶段 4：删除旧实现

- 删除 Broker 会话历史、session cursor 和旧恢复逻辑；
- 删除 `/api/sessions/:id/events`；
- 删除 `legacy-session-event-adapter.ts`、legacy `applyEvent()` 和 `durableEvents` 分支；
- 删除 `appendMessageAndPublish` 在会话运行链路中的调用；
- 删除 `sessionStatus()` 对 runner/Broker 的直接判断；
- 更新文档和路由契约，完成单事件流架构验收。

## 7. 一致性与错误处理

- Event store append、projection 更新、seq 更新必须事务化；
- event store 写入失败时命令返回失败，不能只更新内存或 Store；
- 事件 gap/reset 时只能重新读取同一 event store snapshot；
- 重复命令通过 `clientRequestId + requestHash` 返回原 receipt，不重复产生 user/run 事件；
- 运行时异常必须产生 `run.failed` 或 `run.interrupted`，不能只发 toast/error REST 响应；
- 兼容 adapter 失败不得影响主 event store，也不得反向写回 Broker 真源。

## 8. 验收标准

### 静态验收

- 会话生产代码中无 `Broker.Publish`；
- 前端生产代码中无 `/events`、`sessionEvents()` 和 legacy adapter；
- 会话状态更新无 `setMode()` 事实写入、optimistic transcript 或 `finishTool()` 旁路；
- session handler 不直接写 Store/Broker 组合状态。

### 自动化验收

- Go：每个会话命令均验证完整事件序列和 projection；
- 前端：reducer 对所有事件类型覆盖重复、乱序、gap、reset、重连；
- 集成：真实 Go server + SQLite + runtime fixture + SSE；
- E2E：prompt、assistant/tool streaming、steer、cancel、compact、rename、model、delete、历史分页、刷新、重启恢复、后台通知；
- 故障：SQLite 写入失败、runtime 中断、SSE 断开、重复提交、多标签页并发；
- `bun run check`、`go test ./...`、真实 E2E 和嵌入资源检查全部通过。

最终验收问题只有一个：给定 session snapshot 和从 snapshot seq 开始的事件序列，任何客户端状态是否可完全重建，且没有第二个会话状态真源。
