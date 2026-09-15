# SDD ledger — plan: docs/superpowers/plans/2026-09-15-session-single-event-stream.md

## Preflight scan

| Item | Shared files/interfaces | Finding | Ruling |
|---|---|---|---|
| Task 1 ↔ Task 2 | `session_domain_integration_test.go`, `SessionEventStore`, event constants | Task 1 fixture must expose helpers and Task 2 expands event constants/projection. Compatible. | None. |
| Task 1 ↔ Task 4 | `repository_contract_test.go`, static stream check | Task 1 adds baseline route assertions; Task 4 temporarily retains `/events` as derived compatibility. Compatible if Task 1 `--compat` mode excludes the adapter. | None. |
| Task 1 ↔ Task 8 | `check-session-single-stream.mjs` | Task 1 needs compatibility mode; Task 8 switches strict mode after adapter removal. | None. |
| Task 2 ↔ Task 3 | `session_event_types.go`, `session_projection.go`, `ProjectionChange` | Task 3 depends on metadata/control event constants and projection fields from Task 2. Compatible. | None. |
| Task 2 ↔ Task 4 | metadata/transcript projection and `/state` | Task 4 must read the same projection, not add a second read model. | None. |
| Task 3 ↔ Task 4 | `server_session_handlers.go`, `server.go`, command receipts | Task 3 converts writes; Task 4 converts reads and compatibility output. Compatible and sequential. | None. |
| Task 3 ↔ Task 6 | `compaction.go`, `runner.go`, cancel/compact receipts | Task 6 assumes backend emits durable terminal events. Task 3 must land first. | None. |
| Task 4 ↔ Task 8 | `session_legacy_compat.go`, `/events` route | Task 4 creates temporary derived adapter; Task 8 removes it. Compatible. | None. |
| Task 5 ↔ Task 6 | `index.ts`, `TranscriptState`, durable stream | Task 6 removes optimistic state after Task 5 removes legacy path. Compatible. | None. |
| Task 5 ↔ Task 8 | `legacy-session-event-adapter.ts` | Plan listed physical deletion in both tasks. | Ruling: Task 5 removes production imports and keeps the file only if Task 4 compatibility needs it; physical deletion happens once in Task 8. |
| Task 6 ↔ Task 7 | command receipt UI and real E2E | Task 7 relies on accepted/terminal event semantics from Task 6. Compatible. | None. |
| Task 7 ↔ Task 8 | real durable E2E and route removal | Task 8 may remove old routes only after Task 7 passes. Compatible. | None. |
| Task 3 ↔ session creation/list/model/delete | `server_workspace_handlers.go`, `workspace_access.go`, `server_session_status_handlers.go`, `account_sessions.go` | Original Task 3 file list omitted session creation, workspace bulk deletion, and model handler files even though the spec includes these session state mutations. | Ruling: add these files/tests to Task 3; all session metadata mutations must be routed through the same command/event service, while workspace/account authorization remains outside the event stream. |
| Task 8 ↔ Broker uses | `broker.go`, `rg Broker/Publish` | Broker may have non-session infrastructure; deleting the whole type without call-site inventory could exceed scope. | Ruling: remove only session history/cursor/running and session publish paths; preserve unrelated infrastructure until call sites prove unused. |

| Task | Internal consistency | Finding | Ruling |
|---|---|---|---|
| Task 1 | test helpers and static check | Initial test examples referenced helpers not defined in the plan. | Added exact helper signatures before Task 1. |
| Task 2 | event constants vs reducer | `ProjectionChange.Metadata` is introduced after the initial failing test. | Valid TDD order; the implementation step defines the field before green tests. |
| Task 3 | compact HTTP semantics | Existing compact returns 200; plan expects command receipt/202. | Ruling: change compact to accepted/receipt semantics as part of the migration; update existing tests, no compatibility 200 response. |
| Task 4 | legacy endpoint and single stream goal | Keeping `/events` during migration could look like a second source. | Ruling: adapter is read-only, derived from `EventsAfter`, and must never write Broker or projection. |
| Task 5 | old fallback | Durable failure must not silently use legacy. | None; fail-closed is required by spec. |
| Task 6 | session vs workspace settings | `settings-methods.ts` contains workspace settings and session model writes. | Ruling: change only the session-model branch; leave workspace settings unchanged. |
| Task 7 | real vs mock E2E | Existing fixture mocks all APIs. | Ruling: add a separate real-server fixture; do not weaken the existing recovery test. |
| Task 8 | strict static scan | Scanning tests and compatibility code could flag test fixtures or the temporary adapter. | Ruling: strict scan targets production files and excludes explicit test fixtures; compatibility mode handles the temporary adapter until Task 8. |

## Execution

- Baseline branch: `feat/session-single-event-stream`
- Baseline commit: `b1cf33cc`
- Baseline verification: `bun run test` → 49 files / 532 tests passed; `go test ./...` passed.
- Existing unrelated changes in the original checkout under `static/` are not present in this worktree and must not be imported.
- Task 1: complete (commits `e6553ae5..e8cb25fe`, review clean). Minor suggestions deferred: strengthen receipt state fixture and normalize `acceptedSeq` protocol type before frontend consumption.
- Ruling: from Task 2 onward, execute inline in this session; no further subagents will be dispatched, per user instruction.
- Task 2: complete (commit `024a08aa`, inline verification: targeted projection/schema tests and `go test ./...` passed).
- Ruling: Task 3 keeps the old no-event-store runner path temporarily for the staged compatibility window; fail-closed removal is deferred to Task 8 so existing mock/legacy tests remain runnable. Durable production handlers now use event store commands for prompt, rename, create, delete, model, cancel, and compact.
- Task 3: complete (commit `f8433373`, inline verification: durable command/compact/integration tests and `go test ./...` passed; legacy compatibility branches intentionally remain for Task 4/8).
- Task 4: complete (commit `1a946196`, inline verification: durable snapshot metadata, projection-backed legacy read adapter, compatibility translation tests, and `go test ./...` passed).
- Task 5/6 progress: reducer accepts metadata/control events; session load and history pagination are now durable-only; legacy `connectEvents`/`/events` client subscription has been removed. Session command handlers now require the durable event store; the legacy session `/events` route and Broker-based prompt/steer/cancel/compact/rename/delete fallbacks were removed. Durable prompt/steer/cancel/compact paths no longer optimistically append facts. Background notifications now use `createSessionStreamClient`; compaction/model/context commands no longer refresh REST state after acceptance; cancel persists `run.cancel.requested`. Durable status sync includes compaction and projected model/context metadata; durable model metadata remains authoritative over runtime polling; rename/delete UI mutations wait for durable events. Added an HTTP integration test proving rename → rebased delete shares the durable projection and ordered sequence. Latest targeted verification: Go durable integration and migrated handler tests passed. Remaining Task 8 work is the Runner/compaction Broker write paths and frontend legacy reducer adapter.
