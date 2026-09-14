# Durable Session Event Stream — 交接文件

**日期：** 2026-09-11
**分支：** `feat/light-zh-cn-ui`（工作区干净；未提交 `static/` 产物按原样保留）
**设计规范：** `docs/superpowers/specs/2026-09-11-durable-session-event-stream-design.md`（commit `7320068b`）
**实施计划：** `docs/superpowers/plans/2026-09-11-durable-session-event-stream.md`（commit `990804be`，15 个任务）
**备份确认：** `docs/superpowers/` 在 `.gitignore` 中，但规范与计划已用 `git add -f` 提交，可随时查看。

---

## 1. 目标（一句话）

用 SQLite 持久化事件流 + 投影替代 JSONL/Broker/DOM 混合恢复路径，彻底解决刷新、断线、重启时的流式输出丢失、重复与工具卡片（Bash）信息丢失。

## 2. 当前状态总览

- 后端 durable 栈：**已完成**，生产路径默认启用（`OpenServer` 打开事件库并执行启动恢复）。
- 前端 durable 渲染：**已接入**，通过 `/api/health` 的 `durableEvents` 能力位启用；无事件库时回退旧路径（mock/dev）。
- durable prompt 已接入普通 Web UI；新建会话也直接连接 v2 durable stream。
- Playwright 验收矩阵：**已启用并通过 8/8**。
- 旧 Broker 删除与 DOM-owned tool state 清理：**未完成**（见第 6 节）。

## 3. 已完成提交（`990804be..HEAD`，共 16 个）

| commit | 说明 |
|---|---|
| `b6195734` | 领域事件类型 + 纯投影 reducer（`session_event_types.go`、`session_projection.go`） |
| `80a58807` | SQLite schema/迁移、WAL+FULL、schema 版本表、有界 wake hub |
| `7f0544d0` | 原子 append：run fence、source ordinal 去重、请求幂等、故障注入回滚 |
| `dd0cb3b3` | 投影分页/generation cursor/chunk 内容/retention/级联删除 |
| `a815d7e2` | 严格 JSONL 导入 + 运行 manifest + 原子 rebase |
| `450689ae` | `/state`、`/events-v2`、item content 端点与数据库驱动 SSE |
| `22db20cb` | 有序 runtime adapter + 16KiB/20ms 合并缓冲 |
| `cdb55868` | 持久化命令服务（commit-before-send、幂等、hash 冲突） |
| `6b116da9` | 启动恢复 `run.interrupted` + `OpenServer` |
| `53f55953` | AG-UI durable 适配（payload→cursor、result→end） |
| `305e7e1c` | 前端签名 cursor helper + canonical transcript reducer |
| `5b39cc19` | 渲染调度器（32 次/16ms/128 实体上限）+ 状态选择器 |
| `edda4850` | 受控 SSE 客户端（appliedSeq 重连、250ms→5s 退避）+ v2 API |
| `1c5f2452` | 可配置 retention、10k 事件/10MiB 压力测试、Playwright 脚手架 |
| `ccbc42db` | 后端端到端：`StartDurablePrompt`、`/prompt` 走命令服务、生产 `OpenServer` |
| `48115aa4` | 前端端到端：`/state` + reducer + v2 SSE，health 能力探测与回退 |
| `5cc61bc3` | 修复 `OpenServer`：启动时正确 attach durable command service |
| `68adc223` | 普通 prompt 走 durable `/prompt`；真实快照字段归一化；durable 历史分页；启用并实现 Playwright fixture |
| `38cea484` | 新建会话直接连接 durable v2 stream |

## 4. 关键架构与不变量（继续开发前必读）

1. **持久化先于发布**：事件先在同一 `BEGIN IMMEDIATE` 事务内写 `session_events` 与 `session_items`，提交后才 `hub.Notify`；SSE 只从数据库读取已提交事件。
2. **会话内游标**：`seq` 为每会话自增的 SQLite signed int64；HTTP 层用十进制字符串；前端只用 `BigInt`（`normalizeSessionSeq/compareSessionSeq/nextSessionSeq`），绝不用 `Number`。
3. **run fence**：每会话至多一个活动 run；`Append` 校验 `current_run_id` 与严格连续的 `source_ordinal`；旧 run 事件被拒绝。
4. **工具实体 ID**：`tool:<runId>:<toolCallId>`；前端 `toolsByCallId` 键为 `<runId>:<toolCallId>`。
5. **终态**：`run.finished/failed/cancelled/interrupted` 关闭活动 segment 并终结工具；`run.finished` 在仍有 running 工具时被投影拒绝。
6. **rebase**：JSONL 仅在无活动 run 时导入；成功后追加 `session.rebased`、`projection_generation++`；客户端必须整表重载。
7. **retention**：活动会话保留最近 4096 事件/64MiB，终态保留 256/8MiB；投影与内容 chunk 不随事件删除。测试可用 `SetRetentionLimits` 覆盖。
8. **失败即停**：事件写入失败 → 毒化 run fence、停止发布、取消 runtime；启动恢复把未完成 run 标记 `interrupted`，绝不重跑。
9. **密钥**：payload 落库前经 `RedactPayload`；日志不得包含未脱敏内容。

## 5. 校验结果（可复现）

```bash
bun run test          # 557 passed
bun run lint          # ok
bun run typecheck     # ok
go build ./...        # ok
go test ./...         # ok
bun run backend:test:coverage   # total 100.0%（门禁通过）
go test -race ./internal/piweb -run 'TestAppend|TestStartRun|TestStartPrompt|TestOpenServer|TestSessionWakeHub|TestSessionEventsV2|TestRuntimeEventAdapter|TestSessionEventStress'
```

- 新增前端模块覆盖率均为 **100%**：`transcript-reducer.ts`、`transcript-state.ts`、`transcript-render-scheduler.ts`、`transcript-state-methods.ts`、`session-stream-client.ts`。
- `bun run test:coverage` **仍失败**，但为改动前既有缺口（`tool-message-methods.ts` 90%、`session-recovery.ts` 94.7% 等），与本次新增模块无关。

## 6. 未完成事项（下周继续）

### 6.1 删除旧链路（最大项）

仍在使用的旧机制，需要迁移/删除：

- `internal/piweb/broker.go`、`broker_types.go` 及 `broker_test.go`、`broker_recovery_test.go`。
- `Broker` 仍服务：mock 模式（`PublishMockPrompt`）、`steer`、`cancel`、`compact`、rename、后台会话监听，以及 `/events`（旧 SSE）回退。
- 前端仍保留：`connectEvents`（旧 SSE）、`applyEvent`、`broker_gap` 处理、AG-UI `runAguiSessionPrompt`、DOM 持有的工具状态（`latestToolCards`、`__pendingToolOutput`、`__toolOutputText`）。
- 目标：所有命令与渲染走 durable 路径后删除以上代码与对应测试，保留 `/ag-ui` 作为外部客户端适配器。

### 6.2 启用 Playwright 验收矩阵

- `e2e/session-stream-recovery.spec.ts` 已移除 `test.fixme()`。
- fixture 已实现 health/workspaces/state、动态 projection、`MockEventSource` 推送、断流后的按 cursor replay。
- `bun run test:e2e` 当前 8/8 通过。
- E2E 仍是确定性 UI fixture，不替代真实 SQLite backend 的浏览器联调。

### 6.3 前端覆盖率缺口（既有）

- 若计划要求“触及前端 100%”，需先补齐 `tool-message-methods.ts` 等既有缺口；与 durable 任务独立。

### 6.4 可选加固

- `Append` 的 retention 每次全表扫描该会话事件；长会话可改为增量/批量。
- `session_items.payload_json` 内联完整 body，超大 append 输出存在 O(n²) 序列化；已用 chunk 存储 + 增量写缓解，但可进一步只存预览。
- `SessionStreamClient` 的 `reset()` 会立即重连；可加入抖动。

## 7. 关键文件索引

**后端新增**
- 类型/投影：`session_event_types.go`、`session_projection.go`
- 存储：`session_event_store.go`、`session_event_schema.go`、`session_event_sqlite.go`、`session_event_append.go`、`session_event_projection_sql.go`
- 查询/内容/保留：`session_event_query.go`、`session_content.go`
- 导入：`session_event_importer.go`、`session_event_manifest.go`、`session_event_rebase.go`
- 传输：`session_wake_hub.go`、`session_sse.go`
- 运行时/命令：`runtime_event_adapter.go`、`session_command_service.go`、`session_durable_runner.go`
- 恢复：`session_event_recovery.go`
- AG-UI：`agui_event_adapter.go`

**前端新增**
- `src/pi-app/sessions/transcript-state.ts`、`transcript-reducer.ts`
- `src/pi-app/sessions/session-stream-client.ts`
- `src/pi-app/transcript/transcript-render-scheduler.ts`、`transcript-state-methods.ts`
- `src/lib/api.ts` 新增 `getSessionState`、`sessionEventsV2`、`getSessionItemContent`

**接入点**
- 后端：`internal/piweb/server.go`（路由/依赖）、`server_session_handlers.go`（prompt durable 分支）、`server_workspace_handlers.go`（health `durableEvents`）、根目录 `server.go`（`OpenServer`）
- 前端：`src/pi-app/workspace/workspace-bootstrap-methods.ts`（`loadDurableSessionState`/`applyLoadedDurableSession`/`connectDurableEvents`/`applyDurableEvent`）、`src/pi-app/transcript/transcript-window-methods.ts`（`clearStreamingTail` 停止流）

## 8. 数据库

- 库文件：`store.dbPath`（默认 `DefaultWebDBPath()`，即 sessions 目录同级的 `pi-web.db`）。
- 表：`session_streams`、`session_events`、`session_items`、`session_item_chunks`、`session_commands`、`session_control_receipts`、`session_run_manifests`、`piweb_schema_versions`。
- schema 版本常量：`sessionEventSchemaVersion = 1`。

## 9. 约束与偏好（延续）

- 所有提交包含 `Co-authored-by: JuunAI <juunai.ai.i@gmail.com>`。
- 不修改/提交生成的 `static/`、`dist/`；当前工作区已有未提交 static 变更，继续开发时不要 stage。
- 最近一次 Go 全量测试曾出现一次 `TestSnapshotCursorSafelyBridgesPublishBeforeSubscribe` flaky 失败；单独连续复跑 3 次通过，未修改旧 Broker 代码。
- TDD：先失败测试再实现；遵循 Conventional Commits。
- 当前分支非 `main`，可直接继续；如需隔离可建 worktree。
