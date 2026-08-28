# 流式 Markdown DOM 隔离设计

## 目标

降低模型高速输出时的 Markdown 重排、虚拟列表高度修正和滚动位置写入之间的竞争，保留当前 `markdown-it` 渲染结果和现有 AG-UI 协议。

## 范围

本次只改造流式消息的 DOM 生命周期与渲染调度，不替换 Markdown 引擎，不引入 React，不迁移历史消息分页方案。

## 架构

当前 `.term` 内的 `.term-inner` 继续作为历史 transcript 和虚拟滚动容器。新增同级的 `.streaming-tail`，专门承载：

- 当前 assistant Markdown 流；
- 当前 thinking 流；
- 等待响应 loading 状态（如没有活动流式内容）。

流式节点不加入 `transcriptItems`，因此不会触发虚拟滚动 item 数量变化、流式 item 卸载重建或动态 padding 修正。流结束时，将最终消息一次性提交到历史 transcript，再移除对应 tail 节点。

## 数据流

```text
AG-UI delta
  -> 保留现有 choice 过滤与文本累积
  -> 更新 streaming-tail 中的单一节点
  -> requestAnimationFrame 合并 Markdown 渲染
  -> 仅替换该节点 body 内容

final message
  -> flush pending render
  -> 将最终消息加入历史 transcript
  -> 删除 streaming-tail
```

同一时刻每种流式类型最多一个节点。重复 delta、reset、session 切换和组件销毁都必须清理挂起的渲染 frame。

## Markdown 渲染策略

- 继续使用 `renderPiBody()` 和 `renderUserBody()`。
- 流式 assistant Markdown 仍按 rAF 批处理，避免每个 delta 立即解析。
- 历史消息仍只在提交/加载时渲染。
- 本次不新增数学公式、Mermaid 或语法高亮能力。
- 保留现有特殊 PI inline markup 和 fallback choice 处理。
- 后续可单独增加超大文本保护，不与本次 DOM 改造耦合。

## 滚动策略

保留已有滚动修复，并将流式 tail 视为普通内容尾部：

- 流式更新只请求一个可取消的 bottom-follow rAF；
- 流式跟随使用即时滚动，不使用 smooth scrolling；
- 用户离开底部或 transcript reset 时取消挂起的跟随；
- 不因为 tail 高度变化将 streaming item 交给 virtual-scroller 重新测量；
- 最终消息提交后只执行一次必要的历史列表布局和滚动。

## 错误与生命周期

- 没有 `.streaming-tail` 时，相关方法安全返回或按需创建。
- reset、session 切换和销毁时清除 streaming render frame、scroll frame 和 tail 节点。
- final message 到达时，即使 tail 节点已被清理，也必须正常追加最终消息且不得重复。
- Markdown 仍通过现有转义和 `markdown-it` 配置处理，禁止新增未转义 HTML 输入路径。

## 测试

先新增失败测试，再实现：

1. 流式 assistant 节点不出现在 `transcriptItems` 中，而出现在 `.streaming-tail`。
2. 连续 delta 只保留一个 streaming 节点并合并渲染。
3. final message 提交后 tail 被移除且历史消息只保留一份。
4. reset/session render 会清除 tail 和挂起的 streaming frame。
5. thinking/loading 状态仍能显示、切换和清理。
6. 现有 Markdown、代码复制、fallback choice 和滚动回归测试继续通过。

## 非目标

- 不替换 `markdown-it` 为 `react-markdown`。
- 不重写完整 transcript 虚拟化实现。
- 不改变 AG-UI 事件协议和后端接口。
- 不做与流式渲染无关的 CSS 或 UI 重构。
