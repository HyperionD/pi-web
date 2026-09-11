# Durable Session Event Stream Design

**Date:** 2026-09-11
**Status:** Approved design, pending implementation plan
**Scope:** Pi Web session output, refresh recovery, tool lifecycle, SSE delivery, AG-UI adaptation, and transcript rendering

## 1. Purpose

Replace the current mixed JSONL/cache/in-memory Broker recovery path with a durable, ordered session event stream and a materialized transcript projection.

The design must guarantee that:

1. A browser refresh cannot lose or duplicate assistant, thinking, tool, error, or terminal state.
2. A dropped connection resumes from the last applied session sequence.
3. A server restart preserves committed output and marks unfinished work as interrupted without rerunning it.
4. Virtual scrolling cannot affect message or tool state.
5. Slow clients and long runs have bounded memory use.
6. The Web UI uses one authoritative event transport.
7. Existing Pi JSONL sessions remain readable.

## 2. Current Problems

The current implementation combines three state sources:

- Pi JSONL for durable history.
- `Store.conversations` for process-local display messages.
- Broker history and `activeRunHistory` for live replay.

These sources do not commit atomically. In particular, Pi emits runtime events before its JSONL persistence path necessarily completes. A snapshot can therefore return a Broker cursor that covers a `tool.finished` or `session.message` event while the corresponding JSONL row is still absent. Active replay then filters that event as already durable.

The Web UI also switches from an AG-UI POST stream to ordinary SSE after a run. AG-UI emits the Broker cursor separately from the translated payload, creating a disconnect window in which the cursor advances but the payload is not applied. Tool finish/result ordering can similarly drop the final result.

Finally, live transcript items often retain only DOM nodes. Tool merging and deduplication therefore fail when the virtual scroller unmounts those nodes.

## 3. Decisions

### 3.1 Authority boundary

- Pi JSONL remains the source for Pi model context and compatibility with existing Pi CLI sessions.
- SQLite becomes the source of truth for Web transcript state after a session is opened.
- JSONL is imported only while there is no active run.
- Runtime events never wait for JSONL before becoming recoverable in Web state.
- The Web UI never uses JSONL offsets as stream cursors.

### 3.2 Restart behavior

If the server starts with a run or tool still marked active:

- Preserve all committed text, thinking, arguments, and tool output.
- Append a durable `run.interrupted` event.
- Close every active assistant/thinking segment.
- Mark every unfinished tool `interrupted`.
- Mark the run terminal and non-running.
- Never reconnect to or rerun the prior command automatically.

### 3.3 Transport

- Browser commands use HTTP POST.
- Browser output uses one session SSE connection before, during, and after runs.
- AG-UI remains an external protocol adapter but is not the Web UI transport.
- Replacing SSE with WebSocket is out of scope because bidirectional transport does not solve cursor or state authority problems.

## 4. Architecture

```text
Pi runtime stdout
      |
      v
Runtime Event Adapter -- coalesces high-frequency deltas
      |
      v
Session Event Service
      |  validate + redact + assign session sequence
      |  BEGIN IMMEDIATE
      |  insert session_events
      |  apply materialized projection mutation
      |  update session_streams
      |  COMMIT
      v
Session Wake Hub ---------------> SSE readers query committed rows
      |
      +--------------------------> AG-UI protocol adapter

GET state ----------------------> materialized session_items/session_streams
                                        |
                                        v
                              Frontend transcript reducer
                                        |
                                        v
                              Virtualized DOM projection
```

The database is the delivery queue. The wake hub carries no event payload and is not a recovery source; it only wakes blocked readers after a successful commit.

## 5. Backend Components

### 5.1 `SessionEventStore`

Responsibilities:

- Own one long-lived `*sql.DB`, initialize versioned migrations, and close it during server shutdown.
- Allocate monotonically increasing sequence numbers per session.
- Commit events, command idempotency records, and projection mutations in one transaction.
- Return paginated projection snapshots and bounded event batches.
- Rebase inactive sessions from Pi JSONL as a new projection generation.
- Compact retained events without deleting projected state.
- Recover interrupted runs before HTTP handlers accept traffic.

The core write operations are conceptually:

```go
Append(ctx context.Context, sessionID string, draft EventDraft) (StoredSessionEvent, error)
StartRun(ctx context.Context, command StartRunCommand) (RunReceipt, error)
```

`Append` performs validation and redaction before opening the transaction. Within one driver-supported `BEGIN IMMEDIATE` transaction it locks the stream row, validates the current run fence and strict source ordinal, increments `current_seq`, inserts the event, applies the projection mutation, enforces retention, and updates stream metadata. The wake hub is notified only after commit succeeds.

All run events, including cancellation and failure, pass through one serializer and receive contiguous source ordinals. The stream row stores the latest source ordinal, payload hash, and resulting event sequence. Retrying that latest ordinal with the same type/entity/payload hash returns its original sequence; a conflicting hash, an older ordinal, or a forward gap is rejected without mutation. Events from an old run ID are rejected by the run fence even after their retained event rows are compacted. This keeps deduplication bounded without an unbounded tombstone table.

`StartRun` claims `(session_id, client_request_id)`, checks that no run is active, and appends `user.message.created` plus `run.started` atomically. Reusing the same request ID and request hash returns the existing receipt; reusing it with different content returns conflict.

The production store does not reuse the current open-per-operation database helpers. It configures WAL, `synchronous=FULL`, foreign keys, a busy timeout, bounded read connections, and an explicit shutdown checkpoint.

### 5.2 `SessionProjector`

Responsibilities:

- Define the session/run/message/tool state machine.
- Convert one validated event into deterministic projection mutations.
- Reject impossible transitions instead of silently corrupting state.
- Treat an immediate identical source retry and identical terminal transitions as idempotent.

Each session permits at most one active run. Starting a run is a compare-and-set from a terminal status. Every run-scoped append must match `current_run_id`; after the first terminal transition commits, late events from that run are rejected as stale and cannot affect a newer run. Cancel, failure, normal finish, and startup interruption compete through this same transaction fence.

Projection logic is independent from HTTP, SSE, SQLite, and DOM code. Its transition rules are covered by table-driven tests.

### 5.3 `RuntimeEventAdapter`

Responsibilities:

- Parse Pi RPC events through one ordered serializer per run.
- Assign a stable source ID and contiguous source ordinal from the run ID and serialized domain-event ordinal.
- Assign stable run and assistant/thinking segment IDs.
- Preserve Pi `toolCallId` in payload and use `tool:<runId>:<toolCallId>` as the session-wide entity ID, because Pi tool IDs are only assumed unique within one run.
- Normalize tool arguments, output mode, status, duration, and error metadata.
- Flush pending text/thinking/tool output before lifecycle boundaries.
- Convert malformed or failed runtime frames into explicit diagnostic events.

Cancellation and runtime failure enter the same run serializer. Start commands are deduplicated by `session_commands`; importer and startup-recovery transactions are serialized before request readiness.

High-frequency events are coalesced for at most 20 ms or 16 KiB, whichever is reached first:

- Consecutive text deltas for one segment are concatenated.
- Consecutive thinking deltas for one segment are concatenated.
- Consecutive append-mode tool chunks are concatenated.
- For replace-mode tool output, only the newest snapshot in the batch is retained.
- A segment commit, tool start/finish, error, cancellation, or run terminal event forces an immediate flush.

Every flushed event is still persisted before publication.

### 5.4 `SessionWakeHub`

Responsibilities:

- Maintain small per-session subscriber sets.
- Send non-blocking wake signals after database commits.
- Never queue output payloads.
- Never track replay cursors.

A full wake channel is equivalent to an already-pending wake and is safe to collapse.

### 5.5 JSONL importer

Responsibilities:

- Strictly parse the current Pi branch only while no run is active.
- Produce deterministic imported item IDs from the Pi session entry ID and content-block index.
- Derive a completed-run manifest from normalized, ordered user-visible items recorded from runtime events; no new Pi JSONL marker is assumed.
- Preserve Web-only annotations and interrupted partial output.
- Store source path, size, modification time, and a successful full-file SHA-256 fingerprint.

Import rules:

1. If no stream projection exists, import the complete JSONL branch as the baseline.
2. If the source fingerprint is unchanged, do nothing.
3. If a run is active, defer import.
4. Read file metadata before and after parsing, require a final newline, reject malformed middle lines, and accept the parse only if both fingerprints match.
5. Build a canonical manifest over every ordered user-visible field that JSONL can represent: role/kind, source entry identity and block index, text/thinking, attachment metadata, tool call ID/name/arguments/final output/error status, and ordering. Runtime completion stores the same manifest after associating Pi `message_end` records with provisional Web items.
6. Require the imported manifest for every completed Web run to equal its stored manifest. Runtime-only metadata such as measured delivery duration is explicitly preserved as an overlay. If a source identity is missing or any canonical field differs, retain the SQLite projection and retry on the next inactive open.
7. Otherwise append `session.rebased`, increment `projection_generation`, atomically replace matched imported/runtime history, preserve runtime-only overlays plus Web-only annotations/interrupted partial output, and update the successful full-file SHA-256 fingerprint. Connected clients treat `session.rebased` as a mandatory snapshot reset.
8. A truncated or rewritten JSONL file triggers a full inactive rebase, never an incremental offset guess.
9. Candidate fingerprints are never recorded on failed or deferred imports, so the next inactive open retries.
10. An import failure leaves the last valid SQLite projection untouched and returns an observable warning.

## 6. SQLite Model

SQLite uses the existing `pi-web.db`, `WAL`, `foreign_keys=ON`, and the existing busy timeout.

### 6.1 `session_streams`

```sql
CREATE TABLE session_streams (
  session_id TEXT PRIMARY KEY,
  current_seq INTEGER NOT NULL DEFAULT 0 CHECK(current_seq >= 0),
  min_retained_seq INTEGER NOT NULL DEFAULT 1 CHECK(min_retained_seq >= 1),
  projection_schema_version INTEGER NOT NULL,
  projection_generation INTEGER NOT NULL DEFAULT 1 CHECK(projection_generation > 0),
  status TEXT NOT NULL,
  current_run_id TEXT,
  current_run_source_ordinal INTEGER NOT NULL DEFAULT 0,
  current_run_source_hash TEXT,
  current_run_source_seq INTEGER,
  next_ordinal INTEGER NOT NULL DEFAULT 1,
  source_path TEXT,
  source_size INTEGER,
  source_mtime_ns INTEGER,
  source_content_hash TEXT,
  updated_at TEXT NOT NULL,
  CHECK(min_retained_seq <= current_seq + 1)
);
```

Sequences are positive signed 64-bit integers in SQLite and JSON strings at the HTTP boundary to avoid JavaScript precision loss.

### 6.2 `session_events`

```sql
CREATE TABLE session_events (
  session_id TEXT NOT NULL REFERENCES session_streams(session_id) ON DELETE CASCADE,
  seq INTEGER NOT NULL CHECK(seq > 0),
  run_id TEXT,
  source_id TEXT NOT NULL,
  source_ordinal INTEGER,
  event_type TEXT NOT NULL,
  entity_id TEXT,
  payload_hash TEXT NOT NULL,
  payload_json BLOB NOT NULL,
  occurred_at TEXT NOT NULL,
  PRIMARY KEY(session_id, seq),
  UNIQUE(session_id, source_id),
  UNIQUE(session_id, run_id, source_ordinal)
);
CREATE INDEX idx_session_events_time
  ON session_events(session_id, occurred_at);

CREATE TABLE session_control_receipts (
  session_id TEXT NOT NULL REFERENCES session_streams(session_id) ON DELETE CASCADE,
  source_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  committed_seq INTEGER NOT NULL,
  PRIMARY KEY(session_id, source_id)
);

CREATE TABLE session_run_manifests (
  session_id TEXT NOT NULL REFERENCES session_streams(session_id) ON DELETE CASCADE,
  run_id TEXT NOT NULL,
  manifest_hash TEXT NOT NULL,
  manifest_json BLOB NOT NULL,
  completed_seq INTEGER NOT NULL,
  PRIMARY KEY(session_id, run_id)
);
```

### 6.3 `session_items`

```sql
CREATE TABLE session_items (
  session_id TEXT NOT NULL REFERENCES session_streams(session_id) ON DELETE CASCADE,
  item_id TEXT NOT NULL,
  ordinal INTEGER NOT NULL,
  kind TEXT NOT NULL,
  origin TEXT NOT NULL,
  run_id TEXT,
  entity_id TEXT,
  payload_json BLOB NOT NULL,
  created_seq INTEGER NOT NULL,
  updated_seq INTEGER NOT NULL,
  PRIMARY KEY(session_id, item_id),
  UNIQUE(session_id, ordinal)
);
CREATE INDEX idx_session_items_page
  ON session_items(session_id, ordinal DESC);
CREATE UNIQUE INDEX idx_session_items_entity
  ON session_items(session_id, entity_id)
  WHERE entity_id IS NOT NULL;
```

`origin` is one of `import`, `runtime`, or `annotation`. Entity IDs use mandatory namespaces (`user:`, `assistant:`, `thinking:`, `tool:`, or `annotation:`); every runtime namespace also includes `runId`, so entities cannot collide across kinds or runs. Runtime rows are recoverable immediately; imported rows provide compatibility history; annotation rows survive JSONL rebases.

Large tool bodies use chunk storage instead of growing a single JSON value:

```sql
CREATE TABLE session_item_chunks (
  session_id TEXT NOT NULL,
  item_id TEXT NOT NULL,
  content_version INTEGER NOT NULL,
  chunk_index INTEGER NOT NULL,
  content BLOB NOT NULL,
  PRIMARY KEY(session_id, item_id, content_version, chunk_index),
  FOREIGN KEY(session_id, item_id)
    REFERENCES session_items(session_id, item_id) ON DELETE CASCADE
);
```

Each event payload is at most 256 KiB after encoding. Larger runtime values are split by the adapter. State pages inline at most the newest 16,000 characters of a tool body and return a `contentRef` plus exact character/byte counts; the existing full-output action reads a bounded streaming endpoint. SSE database reads are limited to 128 events and 1 MiB per batch.

Prompt idempotency is persisted separately:

```sql
CREATE TABLE session_commands (
  session_id TEXT NOT NULL REFERENCES session_streams(session_id) ON DELETE CASCADE,
  client_request_id TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  run_id TEXT NOT NULL,
  accepted_seq INTEGER NOT NULL,
  status TEXT NOT NULL,
  response_json BLOB NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(session_id, client_request_id)
);
```

The transcript projection is relational rather than one large JSON blob. Updating Bash output changes only its item/chunks, not the full conversation.

Strict source ordinals provide bounded deduplication for high-volume run events after compaction. `session_control_receipts` provides durable type/hash validation for low-volume non-run events such as rebase and rename; conflicting reuse is rejected. Prompt retries use `session_commands`. `session_run_manifests` stores one canonical manifest per completed run, allowing multi-turn JSONL rebase validation.

### 6.4 Migration properties

- Migrations only add tables and indexes and are recorded in a dedicated schema-version table.
- Existing workspace, user, and settings tables are unchanged; user-management reset paths must not drop stream tables.
- Startup order is database migration → interrupted-run recovery → Store/Runner/HTTP readiness.
- Shutdown order is reject new commands → close/wait SSE and AG-UI handlers → stop runtimes → checkpoint WAL → close the event store.
- Startup fails with an actionable error if migration or recovery cannot complete.
- There is no silent fallback to an in-memory production event store.
- Tests use the same SQLite implementation against a temporary database; focused projector tests use an in-memory repository fake.

## 7. Event Contract

Every SSE domain event has this shape:

```json
{
  "seq": "42",
  "sessionId": "session-id",
  "runId": "run-id",
  "sourceId": "run-id:event:17",
  "sourceOrdinal": 17,
  "type": "tool.output",
  "entityId": "tool:run-id:tool-call-id",
  "payload": {},
  "occurredAt": "2026-09-11T00:00:00Z"
}
```

The SSE `id` field equals `seq`. `sessionId`, `seq`, source ID, namespaced entity ID, type, encoded payload size, and type-specific fields are validated before persistence. `sourceOrdinal` is required for run-serializer events and omitted for command/control transactions, whose durable receipts provide idempotency. Public cursors are decimal strings in the range `0..9223372036854775807`, matching SQLite signed integers.

### 7.1 Transcript and run events

| Event | Projection effect |
|---|---|
| `user.message.created` | Append one user item. |
| `run.started` | Set status active and current run ID. |
| `assistant.segment.started` | Append or activate a text/thinking item with stable entity ID. |
| `assistant.segment.delta` | Append text to the matching active segment. |
| `assistant.segment.committed` | Replace with canonical final text and close the segment. |
| `tool.started` | Append one running tool item keyed by `(runId, toolCallId)`. |
| `tool.args.delta` | Update arguments for the matching tool. |
| `tool.output` | Append or replace tool body according to `mode`. |
| `tool.finished` | Atomically apply final body, status, duration, and result metadata. |
| `run.failed` | Close active segments, append an error annotation, mark active tools `err`, and terminalize the run. |
| `run.cancelled` | Close active segments, mark active tools `cancelled`, and terminalize the run. |
| `run.interrupted` | Close active segments, append an interruption annotation, and mark active tools `interrupted`. |
| `run.finished` | Commit success only when no tool remains running, after closing active segments. |
| `session.rebased` | Increment projection generation and require connected clients to reload state. |
| `session.renamed` | Update session metadata; transcript is unchanged. |
| `workspace.files.changed` | Durable notification event; transcript is unchanged. |

Tool terminal status is one of `ok`, `err`, `cancelled`, or `interrupted`. A finish event cannot regress a terminal tool to running. The projector rejects `run.finished` while any tool remains active; the adapter then uses the same unconsumed source ordinal to append `run.failed`, which marks those tools `err`. Retrying the latest source ordinal with the same payload hash returns its original sequence without adding a row. Older, conflicting, or gapped source ordinals are rejected. A repeated identical terminal transition from another valid source is a no-op event; a conflicting finish is a projector error and terminates the run with a diagnostic.

## 8. Snapshot and Pagination API

### 8.1 State endpoint

During migration, add:

```text
GET /api/sessions/{sessionID}/state?limit=120&before=<ordinal>
```

The response contains:

```json
{
  "session": {},
  "items": [],
  "status": "running",
  "runId": "run-id",
  "seq": "42",
  "cursor": "eyJnZW5lcmF0aW9uIjoiMyIsImJlZm9yZU9yZGluYWwiOjEwMDF9",
  "hasMore": true,
  "limit": 120,
  "projectionSchemaVersion": 1,
  "projectionGeneration": "3"
}
```

The stream row and requested item page are read in one SQLite read transaction. `seq` therefore names exactly the committed event boundary represented by that projection; `projectionGeneration` identifies the current pagination/rebase generation.

`before` is base64url-encoded JSON containing `{ "generation": "3", "beforeOrdinal": 1001 }`, independent from SSE sequence numbers. The page query uses `ordinal < beforeOrdinal`, reads descending for efficiency, then returns items in ascending display order. The next response cursor contains the current generation and the oldest returned ordinal. A generation mismatch returns HTTP 409 `cursor_expired`; the frontend reloads the first state page instead of mixing generations. Older-page loading never changes the stream cursor.

Oversized content is fetched through:

```text
GET /api/sessions/{sessionID}/items/{itemID}/content
```

The endpoint is access-controlled, supports byte ranges, streams from `session_item_chunks`, and returns the content version. A stale content version returns conflict so the caller can reload current item metadata.

### 8.2 SSE endpoint

During migration, add:

```text
GET /api/sessions/{sessionID}/events-v2?after=<seq>
```

The endpoint accepts the standard `Last-Event-ID` header only when `after` is absent. The Web client always supplies its last successfully applied sequence in `after` and therefore takes precedence over any browser-managed header.

SSE reader algorithm:

1. Validate authorization and cursor.
2. Register a wake subscription.
3. Query committed events with `seq > lastSent` in ascending order, bounded to 128 rows/1 MiB.
4. Write and flush each event, then update only the connection-local `lastSent`.
5. Repeat the query until caught up.
6. Wait for a wake, heartbeat interval, request cancellation, or retention reset.
7. A wake always causes another database query; wake payloads are never trusted as events.
8. `session.rebased` is delivered and then the connection closes so every pre-rebase client installs the new projection generation.

Registering before the first query closes the snapshot-to-subscription race. A commit between a query and wait leaves a buffered wake. A missed duplicate wake is harmless because the next query reads all committed rows.

### 8.3 Retention reset

If `after < min_retained_seq - 1`, SSE emits an ID-less `stream.reset` event and closes:

```json
{
  "type": "stream.reset",
  "sessionId": "session-id",
  "payload": { "reason": "cursor_expired" }
}
```

The browser fetches a fresh state snapshot, atomically replaces its session state, and reconnects after the returned `seq`.

Heartbeat events have no ID and do not affect replay state.

## 9. Event Retention and Checkpointing

The materialized projection is updated on every event commit and is always a valid checkpoint.

Retention policy:

- While a run is active, retain at most the newest 4096 events and 64 MiB of encoded payload for that session, deleting the oldest complete prefix when either bound is exceeded.
- After a terminal event, retain the newest 256 events and at most 8 MiB for diagnostics.
- Projection rows and content chunks are not removed by event compaction.
- A client behind the retained range receives `stream.reset`.
- Session deletion removes stream, commands, control receipts, run manifests, events, items, and content chunks in one transaction.
- Periodic SQLite maintenance uses incremental checkpoints; request handlers do not run blocking full vacuum operations.

Wake-hub memory is bounded by one buffered signal per subscriber. SSE reads use fixed-size batches, state pages contain at most 120 items and 2 MiB of inline content, and oversized bodies use the chunk endpoint. Transcript storage can grow on disk with user-visible history, but event queues and request memory remain bounded.

## 10. Web Frontend State

### 10.1 Canonical state

The Web component owns a session state object equivalent to:

```ts
type TranscriptState = {
  sessionId: string;
  viewGeneration: symbol;
  projectionGeneration: string;
  appliedSeq: string;
  status: "idle" | "running" | "cancelled" | "interrupted" | "error";
  runId?: string;
  itemOrder: string[];
  itemsById: Map<string, TranscriptItem>;
  toolsByCallId: Map<`${string}:${string}`, string>;
  activeSegments: Map<string, string>;
  historyCursor?: string;
  historyHasMore: boolean;
};
```

No canonical message body, tool status, output buffer, or deduplication marker is stored only on an element.

### 10.2 Frontend reducer

A pure reducer applies one validated snapshot or event:

- Events from another session or stale view generation are ignored.
- `seq <= appliedSeq` is idempotently ignored.
- The next event must equal `(BigInt(appliedSeq) + 1n).toString()`; any forward jump closes the stream and triggers snapshot reset.
- `session.rebased` always triggers snapshot reset because its projection generation cannot be patched incrementally.
- Entity updates require a known compatible entity type.
- `tool.started` creates at most one item for the `(runId, toolCallId)` pair.
- `tool.output` updates that entity even while it is offscreen.
- `tool.finished` writes all terminal fields in one state transition.
- Every terminal run event closes all active assistant/thinking segments; failed, cancelled, and interrupted runs also terminalize every running tool with the matching status.
- Snapshot replacement clears queued updates and installs exactly the returned state.

All cursor normalization, ordering, and increment operations use decimal-string helpers backed by `BigInt`; cursors are never converted to JavaScript `Number` or compared lexicographically.

The application records `appliedSeq` only after successful validation and reducer application. Native EventSource automatic retry is not trusted: every `error` handler closes that instance and creates a new one with `?after=<appliedSeq>` after bounded exponential backoff. If JSON parsing or reducer application fails, it also closes the source and requests a fresh snapshot. This prevents the browser's internally advanced `Last-Event-ID` from skipping a payload the reducer did not apply.

### 10.3 Rendering and virtual scrolling

- The virtual scroller receives stable item IDs and reads item content from `itemsById`.
- Unmounting an item removes only its element and observer.
- An updated visible item rerenders or patches from current state.
- An updated offscreen item renders its latest state when mounted again.
- No detached element is retained as the source of tool state.
- Prepending historical pages merges by item ID and preserves scroll position.
- A page cursor from another projection generation is rejected and causes a first-page reload.

All event types use one render scheduler. It commits when any of these occurs:

- The next animation frame runs.
- 32 state updates have accumulated.
- 16 ms has elapsed since the first pending update.
- A lifecycle boundary or snapshot replacement requires an immediate commit.

This applies equally to assistant text, thinking, tool arguments, and tool output. Tool entities retain only the newest 16,000-character preview plus content metadata in normal frontend state; full output is fetched on demand. The pending render queue is capped at 128 entity IDs because repeated updates collapse by entity. Reaching the cap forces a synchronous state-to-view commit.

## 11. Prompt and Output Flow

### 11.1 Normal prompt

1. The Web UI has an SSE connection for the selected session.
2. The user submits `POST /api/sessions/{sessionID}/prompt` with a cryptographically random client request ID.
3. `StartRun` atomically claims the request ID and persists `user.message.created` plus `run.started`.
4. Only after commit does the command service send the prompt to Pi runtime.
5. Pi output passes through the ordered runtime adapter and event store.
6. SSE readers wake and query committed events.
7. The frontend reducer applies events and schedules rendering.
8. The run's final output is flushed before the first terminal event commits.

A duplicate client request ID with the same request hash returns the persisted run receipt and never executes the prompt twice, including after server restart. A hash mismatch returns HTTP 409. If runtime startup/send fails after `StartRun`, the command service appends `run.failed` for that run.

### 11.2 Refresh during a tool

1. The refreshed page reads `/state` and receives the latest projected running tool body/status at sequence `N`.
2. It renders that state and opens SSE after `N`.
3. Output committed after `N` is replayed from SQLite.
4. `tool.finished` updates the same tool entity by `toolCallId`.
5. No JSONL timing, DOM presence, or active-run event filtering participates.

### 11.3 Browser disconnect

Pi continues running. The event store continues committing output. On EventSource error, the Web client closes the native source and creates a new connection whose `after` query is its last successfully applied sequence. The server queries every retained event after that sequence. If retention has advanced, the browser receives `stream.reset` and reloads the projection.

### 11.4 Server restart

Before serving session state or accepting prompts, recovery scans streams with an active status and appends `run.interrupted` through the normal transaction path. Previously committed output remains in projection rows. No command is replayed.

## 12. AG-UI Adapter

The AG-UI endpoint is retained for external clients but no longer subscribes directly to an in-memory Broker.

- Starting an AG-UI run uses the same command service and idempotency rules.
- The adapter reads committed domain events from `SessionEventStore`.
- Domain payload is emitted before any custom cursor metadata referring to its sequence.
- Final tool result is emitted before `TOOL_CALL_END`.
- Error status is mapped explicitly rather than defaulting every tool to success.
- Disconnecting an AG-UI request does not cancel the underlying run.
- The AG-UI POST stream itself does not auto-resume. Its final emitted custom cursor can be supplied to the durable `/events-v2` endpoint for recovery; no process-local AG-UI replay state is authoritative.

## 13. Error Handling

### 13.1 Persistence failure

- A prompt is not sent to Pi if initial user/run events fail to commit.
- The first append failure atomically poisons the in-process run fence before cancellation begins; the adapter drops all later mutations from that run.
- The server stops publishing, closes affected streams, and attempts bounded runtime abort followed by runtime retirement.
- Operational reporting uses structured server logs because the failed database cannot be trusted to persist an error event.
- On the next successful startup, the still-active database row is converted to `run.interrupted` before traffic is accepted.
- The server never emits an event that was not committed.
- The UI shows a connection/persistence failure and reloads the last committed snapshot.

If cancellation also fails, the runtime is fenced from all event and command paths and forcibly retired; shutdown surfaces an operational error. A tool may already have produced an external side effect before a later database failure. The system does not claim rollback; it prevents untracked continuation and reports the interruption.

### 13.2 Runtime protocol failure

Malformed runtime frames become sanitized diagnostic events. If the frame prevents lifecycle correctness, the run is failed and cancelled. Unknown ignorable Pi notification types are logged and do not mutate transcript state.

### 13.3 Import failure

A failed or partial JSONL import never replaces an existing projection. The state endpoint returns the last valid projection and an observable import warning.

### 13.4 Slow client

A slow client consumes committed events at its own pace from SQLite. It does not backpressure runtime persistence through a bounded Go channel. Event compaction may force a snapshot reset, which is a normal recovery path.

## 14. Security and Isolation

- Existing `requireSessionAccess` authorization remains on state, event, prompt, cancellation, and AG-UI routes.
- Every event-store query includes `session_id`; no global replay cursor is exposed.
- Event payloads pass through the existing redaction layer before persistence.
- Prompt attachments and tool outputs retain existing size validation and truncation policies.
- SQLite errors and logs must not include unredacted payloads.
- Session deletion removes all Web projection and event data.

## 15. Migration Plan Boundaries

The implementation plan will use atomic phases:

1. Add schema, event-store interface, SQL implementation, projector, importer, and recovery tests without changing the current UI.
2. Add the runtime adapter/event service; commit to the new store before publishing compatibility events to the old Broker.
3. Add v2 state/SSE endpoints and deterministic recovery/retention tests.
4. Add the frontend normalized reducer and state-driven virtual rendering.
5. Switch prompt submission from AG-UI to HTTP POST while keeping one v2 SSE connection.
6. Adapt AG-UI to the durable event store.
7. Run parity and fault-injection verification, then remove legacy active replay, AG-UI Web transport, DOM-owned tool state, and obsolete tests.

Database changes are additive during migration. The frontend can temporarily roll back to legacy endpoints without removing committed event data. Legacy code is deleted only after parity tests pass; maintaining two permanent implementations is not an accepted end state.

## 16. Testing Strategy

### 16.1 Backend unit and integration tests

Cover:

- Sequence allocation, source-ID deduplication, per-session active-run fencing, and concurrent append ordering.
- Atomic command claim plus user/run events, including concurrent duplicate request IDs and hash conflict.
- Event plus projection atomic commit at every injected SQL operation boundary.
- Commit failure publishes no wake/event; poisoned-run fencing rejects later runtime frames.
- Runtime coalescing, payload splitting, and forced boundary flushes.
- Every valid and invalid projector transition, including competing finish/cancel/failure and stale old-run events.
- JSONL first import, unchanged open, external inactive change, rewrite, partial line, malformed middle line, full-manifest mismatch, and deferred active import.
- Rebase generation event and stale pagination cursor rejection.
- Startup interruption recovery before handler readiness.
- SSE event ordering, controlled reconnect from applied sequence, snapshot-to-subscribe race, heartbeat, expired cursor reset, and slow readers.
- Event row/byte retention limits, fixed query batches, projection survival, and chunked full-output reads.
- Session deletion and authorization isolation.
- AG-UI payload/cursor and result/end ordering.

Run ordinary and race-enabled Go suites and preserve 100% statement, branch, function, and line coverage for touched backend code.

### 16.2 Frontend unit tests

Use `bun:test`/Vitest-compatible pure tests without behaviorless render snapshots:

- Snapshot installation and pagination merge.
- Sequence idempotency and stale generation isolation.
- Text/thinking segment lifecycle.
- Tool start/output append/output replace/finish/error/interruption.
- Tool updates while the item is not mounted.
- Render scheduler frame starvation threshold.
- Stream reset and reducer failure recovery.
- Session switching with queued events.

Touched frontend logic must meet 100% statement, branch, function, and line coverage.

### 16.3 Browser tests

Playwright tests use visible roles/text and a mocked deterministic backend. Refresh or disconnect at each boundary:

1. Before `run.started`.
2. During assistant text.
3. After `tool.started`.
4. Between multiple `tool.output` events.
5. Immediately before and after `tool.finished`.
6. Between final assistant commit and `run.finished`.
7. During cancellation and runtime failure.
8. While the active tool is virtualized offscreen.
9. While switching between two simultaneously running sessions.
10. After a forced `stream.reset`.

Each test asserts exact item count, exact accumulated text/output, stable tool status, and no cross-session mutation.

### 16.4 Stress and verification

- Process at least 10,000 mixed events while asserting one buffered wake per subscriber, at most 128 rows/1 MiB per SSE read, at most 128 pending frontend entity renders, and configured event row/byte limits.
- Verify a 10 MiB tool output is split, projected exactly, previewed without full frontend retention, and downloadable byte-for-byte through the content endpoint.
- Verify progressive rendering during replay bursts: visible text/tool output must update no later than 32 applied events or the 16 ms scheduler deadline under fake time.
- Run frontend tests, ESLint, Prettier check, TypeScript typecheck, Playwright, Go tests, Go race tests, coverage gates, and production build.
- Do not commit generated `static/` assets.

## 17. Acceptance Criteria

The architecture is complete only when all conditions hold:

1. At every documented lifecycle boundary, refreshed and uninterrupted clients have identical ordered item IDs, kinds, visible text, attachments, tool arguments, tool output bytes, statuses, and run terminal state.
2. A completed tool has one card keyed by `toolCallId`, with exact final body and status.
3. A virtualized active tool continues receiving output and finishes in place.
4. Reconnect tests observe every committed sequence exactly once at reducer state, including a disconnect after SSE parsing but before application.
5. A server restart preserves committed output and displays unfinished work as interrupted before accepting traffic.
6. A cursor older than retention automatically restores through snapshot reset.
7. Event, command idempotency, projection, retention, and rebase writes are atomic under injected database failures.
8. The 10,000-event and 10 MiB-output tests satisfy all queue, batch, preview, and event-retention bounds defined in this document.
9. The Web UI has no AG-UI run transport and no durable state stored only in DOM.
10. Legacy active replay code is removed after old/new parity fixtures match the equality definition in criterion 1.
11. All required tests, coverage gates, lint, typecheck, race checks, and builds pass.

## 18. Non-goals

- Automatically resuming or rerunning execution after server restart.
- Replacing Pi JSONL as Pi's model-context format.
- Real-time bidirectional reconciliation with another Pi CLI process editing the same active session.
- Introducing WebSocket solely for event delivery.
- Keeping the old and new recovery architectures permanently.
