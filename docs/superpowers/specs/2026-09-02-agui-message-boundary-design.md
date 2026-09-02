# AG-UI Assistant 消息边界修复设计

## 问题

同一 agent run 内，工具调用前的多段 assistant 说明被实时页面拼接进最后一条总结；刷新后，JSONL 回放按原始消息重新拆分，因此最后一条只剩真正的总结。

## 已确认根因

Pi RPC 为每条 assistant 消息依次输出 `message_update(text_delta)` 和 `message_end`。`internal/piweb/runner_events.go` 会持久化 `message_end` 中的最终消息，但只要本轮曾出现文本 delta，就不向 Broker 发布对应的 `session.message`。

AG-UI 适配器依赖 `session.message` 关闭当前 `TEXT_MESSAGE_*` 生命周期。边界事件被抑制后，同一个 `messageId` 穿过多个 tool call 一直保持打开，`@ag-ui/client` 的 `textMessageBuffer` 因而累加本轮所有说明和最终总结。前端 `src/pi-app/index.ts` 在唯一一次 `onTextEnd` 中把该累计 buffer 作为最后消息提交。

截图对应 JSONL 已验证：该 run 有 7 条工具前说明和 1 条最终总结；实时页面合并 8 段，刷新回放则恢复消息边界。协议级失败用例也验证当前输出只有 1 组 `TEXT_MESSAGE_START/END`；补入 canonical `session.message` 后恢复为 2 组。

此前 `ec472b04` 修复的是 compaction 后的历史回放边界，`fd542c91` 修复的是前端流式 DOM/竞态；二者都未恢复 Pi RPC `message_end` 到 AG-UI 的边界传播，因此不能解决本问题。

## 修复策略

以持久化最终消息为 canonical event：`message_end` 中每个有效 `pi`/`think` 消息都写入 Store 并发布到 Broker；delta 只承担实时预览，不替代最终消息。删除跨整轮累计的 `streamedText`/`streamedThinking` 抑制状态。

现有 AG-UI `session.message` 分支会在文本流打开时发送 `TEXT_MESSAGE_END`，下一段 delta 自动获得新的 `messageId`。现有前端 `appendMessage()` 会移除同 kind 的 streaming row，再提交最终消息，因此无需新增字符串截断或“只保留最后一段”等脆弱逻辑。

## 验收标准

- 文本 → tool → 文本 → run 完成时产生两组不同 `messageId` 的 `TEXT_MESSAGE_START/CONTENT/END`。
- `TEXT_MESSAGE_END` 必须出现在对应 `TOOL_CALL_START` 前。
- 实时 transcript 中工具前说明和最终总结分别显示，最终总结不包含前文。
- 刷新前后消息文本、顺序和数量一致。
- fallback choice、thinking、纯文本最终响应、无 delta 响应和 EventSource fallback 不回归。

## 非目标

- 不修改 compaction/分页逻辑。
- 不用前端字符串比较猜测消息边界。
- 不升级 AG-UI 依赖，不重构 transcript 虚拟化。
