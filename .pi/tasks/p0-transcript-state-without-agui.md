# P0：统一 TranscriptState 并移除 AG-UI

实施计划已保存至：
`docs/superpowers/plans/2026-09-14-p0-transcript-state-without-agui.md`

- [x] 建立 `TranscriptState → ViewModel → DOM` 单向渲染链路
- [x] 移除虚拟滚动中的 DOM 节点业务缓存
- [x] 将 assistant/tool streaming 全部收敛到 reducer
- [x] 将 legacy event adapter 接入同一 reducer
- [x] 删除 AG-UI endpoint、client、SDK 与测试
- [x] 修复工具失败状态和 durable cursor 恢复边界
- [x] 更新 P0 审查文档
- [x] 运行 Vitest、Go test、lint、typecheck、build
