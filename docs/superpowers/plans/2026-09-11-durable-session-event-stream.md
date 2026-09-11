# Durable Session Event Stream Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace JSONL/Broker/DOM-owned stream recovery with a durable SQLite event stream, canonical transcript projection, resumable SSE, and state-driven frontend rendering.

**Architecture:** Pi runtime frames are normalized into ordered domain events, committed with projection mutations in SQLite, and then exposed through database-backed SSE. The Web UI sends commands through HTTP, receives all output through one SSE connection, applies events to a normalized reducer, and treats DOM nodes as disposable projections.

**Tech Stack:** Go 1.26, `database/sql`, `modernc.org/sqlite`, HTTP/SSE, TypeScript 6, Astro 6, Vitest 4, virtual-scroller, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-11-durable-session-event-stream-design.md`

## Global Constraints

- Execute in a dedicated git worktree created from the commit containing this plan; do not implement directly in the current checkout.
- Read the approved spec completely before Task 1 and preserve every event/state invariant defined there.
- Use TDD for every behavior change: failing focused test, minimal implementation, passing focused test, refactor, rerun.
- Keep touched source files at or below 300 lines and functions below 50 lines; split by responsibility.
- SQLite is the production authority for Web transcript state; no production fallback to an in-memory event store.
- Persist, project, and commit every event before notifying SSE or AG-UI consumers.
- Public stream sequences are decimal strings in `0..9223372036854775807`; JavaScript uses `BigInt`, never `Number`.
- Runtime tool entity IDs are `tool:<runId>:<toolCallId>`; all other entity IDs are also namespaced.
- One session has at most one active run; stale run events cannot mutate a newer run.
- Redact payloads before persistence and never log unredacted payloads.
- Existing Pi JSONL remains the model-context format and is imported only while no run is active.
- Service restart marks unfinished runs/tools interrupted and never reruns commands.
- Do not commit generated `static/` or `dist/` files.
- Every commit must contain exactly `Co-authored-by: JuunAI <juunai.ai.i@gmail.com>`.
- Required final checks: Prettier check, ESLint, TypeScript, frontend unit coverage, Playwright, Go tests, Go race tests, Go 100% coverage, production build.

---

### Task 1: Domain event types and pure projector

**Files:**
- Create: `internal/piweb/session_event_types.go`
- Create: `internal/piweb/session_projection.go`
- Create: `internal/piweb/session_projection_test.go`

**Interfaces:**
- Produces: `SessionEventDraft`, `StoredSessionEvent`, `SessionStreamState`, `ProjectedSessionItem`, `ProjectionView`, `ProjectionChange`.
- Produces: `ReduceSessionEvent(view ProjectionView, event StoredSessionEvent) (ProjectionChange, error)`.
- Produces: event constants for user, run, assistant, tool, rebase, rename, and workspace events.
- Consumes: existing `PromptAttachment` and redaction conventions from `internal/piweb/types.go` and `internal/piweb/redact.go`.

- [ ] **Step 1: Write projector transition tests**

Cover text segment creation/delta/commit, tool append/replace/finish, duplicate terminal transitions, conflicting finish, all terminal run events, cross-run tool IDs, stale run events, and `run.finished` with an active tool.

```go
func TestReduceSessionEventKeepsToolIdentityAcrossRuns(t *testing.T) {
	first := storedEvent("run-1", 1, EventToolStarted, "tool:run-1:call-1", map[string]any{"toolCallId": "call-1"})
	second := storedEvent("run-2", 2, EventToolStarted, "tool:run-2:call-1", map[string]any{"toolCallId": "call-1"})
	firstChange, err := ReduceSessionEvent(activeProjection("run-1"), first)
	if err != nil { t.Fatal(err) }
	secondChange, err := ReduceSessionEvent(activeProjection("run-2"), second)
	if err != nil { t.Fatal(err) }
	if firstChange.Upsert.ItemID == secondChange.Upsert.ItemID {
		t.Fatalf("tool IDs collided: %q", firstChange.Upsert.ItemID)
	}
}

func TestReduceSessionEventRejectsSuccessfulFinishWithRunningTool(t *testing.T) {
	view := activeProjectionWithTool("run-1", "tool:run-1:call-1")
	_, err := ReduceSessionEvent(view, storedEvent("run-1", 3, EventRunFinished, "", nil))
	if !errors.Is(err, ErrActiveToolAtRunFinish) { t.Fatalf("error = %v", err) }
}
```

- [ ] **Step 2: Run the tests and confirm RED**

Run: `go test ./internal/piweb -run 'TestReduceSessionEvent' -count=1`

Expected: FAIL because the event and projector types do not exist.

- [ ] **Step 3: Define the domain contract**

```go
type SessionEventDraft struct {
	RunID         string
	SourceID      string
	SourceOrdinal *int64
	Type          string
	EntityID      string
	Payload       json.RawMessage
	OccurredAt    time.Time
}

type StoredSessionEvent struct {
	Seq int64 `json:"-"`
	SessionEventDraft
}

type ProjectionChange struct {
	Stream         SessionStreamState
	Upsert         *ProjectedSessionItem
	Terminalize    []ProjectedSessionItem
	DeleteItemIDs  []string
	RequireRebase  bool
}
```

Define statuses and event constants exactly as section 7 of the spec. Validation rejects empty session/source/type, out-of-range payloads, unknown status, unnamespaced entity IDs, and run-scoped IDs that do not contain the run ID.

- [ ] **Step 4: Implement the pure projector**

Dispatch each event to a focused reducer under 50 lines.

```go
func ReduceSessionEvent(view ProjectionView, event StoredSessionEvent) (ProjectionChange, error) {
	if err := validateProjectionFence(view.Stream, event); err != nil { return ProjectionChange{}, err }
	switch event.Type {
	case EventUserMessageCreated:
		return reduceUserMessage(view, event)
	case EventAssistantSegmentStarted, EventAssistantSegmentDelta, EventAssistantSegmentCommitted:
		return reduceAssistantSegment(view, event)
	case EventToolStarted, EventToolArgsDelta, EventToolOutput, EventToolFinished:
		return reduceToolEvent(view, event)
	case EventRunFailed, EventRunCancelled, EventRunInterrupted, EventRunFinished:
		return reduceTerminalRun(view, event)
	case EventSessionRebased, EventSessionRenamed, EventWorkspaceFilesChanged:
		return reduceControlEvent(view, event)
	default:
		return ProjectionChange{}, ErrUnknownSessionEvent
	}
}
```

- [ ] **Step 5: Run focused tests and coverage**

Run: `go test ./internal/piweb -run 'TestReduceSessionEvent|TestValidateSessionEvent' -coverprofile=/tmp/task1.out -count=1`

Expected: PASS; `go tool cover -func=/tmp/task1.out` shows every new projector function covered.

- [ ] **Step 6: Commit**

```bash
git add internal/piweb/session_event_types.go internal/piweb/session_projection.go internal/piweb/session_projection_test.go
git commit -m "feat(stream): define durable session projection" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 2: Long-lived SQLite event store, migrations, and wake hub

**Files:**
- Create: `internal/piweb/session_event_store.go`
- Create: `internal/piweb/session_event_schema.go`
- Create: `internal/piweb/session_event_sqlite.go`
- Create: `internal/piweb/session_event_schema_test.go`
- Create: `internal/piweb/session_wake_hub.go`
- Create: `internal/piweb/session_wake_hub_test.go`
- Modify: `internal/piweb/web_db.go`

**Interfaces:**
- Consumes: Task 1 domain and projection types.
- Produces: `SessionEventStore` interface.
- Produces: `SessionWakeHub`, `WakeSubscription`, `NewSessionWakeHub`, `Subscribe`, `Notify`, and `Shutdown`.
- Produces: `OpenSQLiteSessionEventStore(path string, hub *SessionWakeHub) (*SQLiteSessionEventStore, error)`.
- Produces: `Close(ctx context.Context) error` and driver-level `withImmediateTx`.

- [ ] **Step 1: Write migration and lifecycle tests**

```go
func TestOpenSQLiteSessionEventStoreCreatesSchemaAndDurabilityPragmas(t *testing.T) {
	store, err := OpenSQLiteSessionEventStore(filepath.Join(t.TempDir(), "pi-web.db"), NewSessionWakeHub())
	if err != nil { t.Fatal(err) }
	defer store.Close(t.Context())
	for _, table := range []string{"session_streams", "session_events", "session_items", "session_item_chunks", "session_commands", "session_control_receipts", "session_run_manifests"} {
		if !sqliteTableExists(t, store.db, table) { t.Fatalf("missing table %s", table) }
	}
	if got := sqlitePragma(t, store.db, "synchronous"); got != "2" { t.Fatalf("synchronous = %s", got) }
}
```

Also test migration rollback, user-management reset preservation, reopening the database, and `Close` idempotency.

- [ ] **Step 2: Run the schema tests and confirm RED**

Run: `go test ./internal/piweb -run 'TestOpenSQLiteSessionEventStore|TestSessionEventMigration' -count=1`

Expected: FAIL because the store and schema do not exist.

- [ ] **Step 3: Define the event-store interface**

```go
type SessionEventStore interface {
	StartRun(context.Context, StartRunCommand) (RunReceipt, error)
	Append(context.Context, string, SessionEventDraft) (StoredSessionEvent, error)
	Snapshot(context.Context, string, SessionPageRequest) (SessionStateSnapshot, error)
	EventsAfter(context.Context, string, int64, EventReadLimit) (EventBatch, error)
	OpenSession(context.Context, SessionSource) (ImportReport, error)
	RecoverInterrupted(context.Context) error
	DeleteSession(context.Context, string) error
	Close(context.Context) error
}
```

Use a separate `SessionEventRepository` fake only for pure service tests; production constructors require SQLite.

- [ ] **Step 4: Implement versioned schema migration**

Create all DDL from spec section 6, including foreign keys, source uniqueness, run manifests, control receipts, indexes, and invariant checks. Add a dedicated `piweb_schema_versions` table and apply each migration in one transaction.

```go
func configureEventDB(db *sql.DB) error {
	_, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON; PRAGMA wal_autocheckpoint=1000;`)
	if err == nil { db.SetMaxOpenConns(8); db.SetMaxIdleConns(8) }
	return err
}
```

Ensure the existing account reset code never drops the new tables.

- [ ] **Step 5: Implement immediate transactions and close**

Use a dedicated connection because `database/sql.BeginTx` does not guarantee `BEGIN IMMEDIATE`.

```go
func (s *SQLiteSessionEventStore) withImmediateTx(ctx context.Context, fn func(*sql.Conn) error) (err error) {
	conn, err := s.db.Conn(ctx)
	if err != nil { return err }
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil { return err }
	defer func() { if err != nil { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") } }()
	if err = fn(conn); err != nil { return err }
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}
```

`Close` runs a bounded WAL checkpoint once and closes the long-lived handle.

- [ ] **Step 6: Implement and test the bounded wake hub**

```go
type WakeSubscription struct {
	Wake  <-chan struct{}
	Close func()
}

func (h *SessionWakeHub) Notify(sessionID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for subscriber := range h.subscribers[sessionID] {
		select { case subscriber <- struct{}{}: default: }
	}
}
```

Each subscriber has capacity one. Test collapsed duplicate wakes, unsubscribe, subscribe/notify races, and shutdown.

- [ ] **Step 7: Run focused tests**

Run: `go test ./internal/piweb -run 'TestOpenSQLiteSessionEventStore|TestSessionEventMigration|TestSQLiteSessionEventStoreClose|TestSessionWakeHub' -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/piweb/session_event_store.go internal/piweb/session_event_schema.go internal/piweb/session_event_sqlite.go internal/piweb/session_event_schema_test.go internal/piweb/session_wake_hub.go internal/piweb/session_wake_hub_test.go internal/piweb/web_db.go
git commit -m "feat(stream): add durable session event database" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 3: Atomic append, fencing, idempotency, and projection persistence

**Files:**
- Create: `internal/piweb/session_event_append.go`
- Create: `internal/piweb/session_event_append_test.go`
- Create: `internal/piweb/session_event_fault_test.go`
- Modify: `internal/piweb/session_event_sqlite.go`

**Interfaces:**
- Consumes: `ReduceSessionEvent`, `withImmediateTx`.
- Produces: SQLite implementations of `StartRun` and `Append`.
- Produces: fault hook `eventStoreFaultHook func(EventStoreFaultPoint) error` available only inside package tests.

- [ ] **Step 1: Write failing atomicity and fencing tests**

```go
func TestAppendCommitsEventAndProjectionAtomically(t *testing.T) {
	store := openTestEventStore(t)
	receipt := startTestRun(t, store, "s1", "req-1")
	event := toolStartedDraft(receipt.RunID, 1, "call-1")
	stored, err := store.Append(t.Context(), "s1", event)
	if err != nil { t.Fatal(err) }
	assertStoredEvent(t, store.db, "s1", stored.Seq, EventToolStarted)
	assertProjectedTool(t, store.db, "s1", "tool:"+receipt.RunID+":call-1", "running")
}

func TestAppendRollbackNeverLeavesEventWithoutProjection(t *testing.T) {
	store := openTestEventStore(t)
	store.eventStoreFaultHook = failAt(FaultAfterEventInsert)
	_, err := store.Append(t.Context(), "s1", validDraft())
	if err == nil { t.Fatal("expected injected failure") }
	assertSessionEventCount(t, store.db, "s1", 0)
	assertSessionItemCount(t, store.db, "s1", 0)
}
```

Add cases for latest-source retry, conflicting hash, old ordinal, ordinal gap, stale run ID, concurrent run start, duplicate request/hash match, and duplicate request/hash conflict.

- [ ] **Step 2: Run the tests and confirm RED**

Run: `go test ./internal/piweb -run 'TestAppend|TestStartRun' -count=1`

Expected: FAIL because append and start-run transactions are not implemented.

- [ ] **Step 3: Implement atomic `StartRun`**

Within one immediate transaction: claim `session_commands`, reject another active run, create/reset run-source fence, append user and run-start events with consecutive session sequences, project both, and persist the receipt.

```go
type StartRunCommand struct {
	SessionID      string
	ClientRequestID string
	RequestHash    string
	RunID          string
	DisplayText    string
	Attachments    []PromptAttachment
	StartedAt      time.Time
}

type RunReceipt struct {
	RunID      string `json:"runId"`
	AcceptedSeq int64 `json:"-"`
	Existing   bool   `json:"existing"`
}
```

- [ ] **Step 4: Implement fenced `Append`**

Calculate the canonical payload hash after redaction. Validate run ID and ordinal against `session_streams`; load only the target item plus active entities needed by `ProjectionView`; apply `ProjectionChange`; write events/items/chunks/stream state in the same transaction.

```go
func nextSourceDecision(stream SessionStreamState, draft SessionEventDraft, hash string) (sourceDecision, error) {
	if draft.RunID != stream.CurrentRunID { return sourceDecision{}, ErrStaleRun }
	ordinal := *draft.SourceOrdinal
	if ordinal == stream.SourceOrdinal && hash == stream.SourceHash {
		return sourceDecision{DuplicateSeq: stream.SourceSeq}, nil
	}
	if ordinal != stream.SourceOrdinal+1 { return sourceDecision{}, ErrInvalidSourceOrdinal }
	return sourceDecision{Append: true}, nil
}
```

For control events, claim `session_control_receipts` and validate type/hash on duplicate source IDs.

- [ ] **Step 5: Inject failure at every transaction boundary**

Use table-driven tests for after command claim, event insert, item write, chunk write, retention delete, stream update, and before commit. Every case must leave either the complete old state or complete new state, never a partial state; no wake notification occurs on rollback.

- [ ] **Step 6: Run focused tests and race test**

Run: `go test ./internal/piweb -run 'TestAppend|TestStartRun|TestEventStoreFault' -count=1`

Run: `go test -race ./internal/piweb -run 'TestAppendConcurrent|TestStartRunConcurrent' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/piweb/session_event_append.go internal/piweb/session_event_append_test.go internal/piweb/session_event_fault_test.go internal/piweb/session_event_sqlite.go
git commit -m "feat(stream): persist session events atomically" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 4: Projection pages, large content, and retention

**Files:**
- Create: `internal/piweb/session_event_query.go`
- Create: `internal/piweb/session_content.go`
- Create: `internal/piweb/session_event_query_test.go`
- Create: `internal/piweb/session_content_test.go`
- Modify: `internal/piweb/session_event_append.go`
- Modify: `internal/piweb/types.go`

**Interfaces:**
- Produces: `SessionPageRequest`, `SessionStateSnapshot`, `ProjectionCursor`, `EventReadLimit`, `EventBatch`.
- Produces: `EncodeProjectionCursor`, `DecodeProjectionCursor`.
- Produces: `StreamItemContent(ctx, sessionID, itemID, version, byteRange)`.
- Consumes: Event-store schema and append path.

- [ ] **Step 1: Write failing query, cursor, content, and retention tests**

```go
func TestSnapshotRejectsCursorFromOldProjectionGeneration(t *testing.T) {
	store := populatedEventStore(t, 140)
	cursor := EncodeProjectionCursor(ProjectionCursor{Generation: 1, BeforeOrdinal: 21})
	forceProjectionGeneration(t, store.db, "s1", 2)
	_, err := store.Snapshot(t.Context(), "s1", SessionPageRequest{Limit: 120, Before: cursor})
	if !errors.Is(err, ErrProjectionCursorExpired) { t.Fatalf("error = %v", err) }
}

func TestRetentionPreservesProjectionAndExpiresOldCursor(t *testing.T) {
	store := eventStoreWithLimits(t, RetentionLimits{ActiveRows: 4, ActiveBytes: 1024})
	appendTextEvents(t, store, "s1", 8)
	batch, err := store.EventsAfter(t.Context(), "s1", 0, DefaultEventReadLimit())
	if err != nil { t.Fatal(err) }
	if !batch.ResetRequired { t.Fatalf("batch = %#v", batch) }
	if got := snapshotText(t, store, "s1"); got != "12345678" { t.Fatalf("text = %q", got) }
}
```

Add tests for ascending page results, `< beforeOrdinal`, base64url validation, signed-int64 cursor limit, 2 MiB state-page inline cap, 256 KiB event cap, 128-row/1 MiB event reads, 10 MiB output chunk fidelity, byte ranges, stale content version, terminal 256-row/8 MiB retention, and cascade deletion of stream/events/items/chunks/commands/receipts/manifests.

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./internal/piweb -run 'TestSnapshot|TestProjectionCursor|TestRetention|TestStreamItemContent' -count=1`

Expected: FAIL because query/content APIs do not exist.

- [ ] **Step 3: Implement generation-aware paging**

```go
type ProjectionCursor struct {
	Generation    int64 `json:"generation,string"`
	BeforeOrdinal int64 `json:"beforeOrdinal"`
}

type SessionStateSnapshot struct {
	Items                    []ProjectedSessionItem `json:"items"`
	Status                   string                 `json:"status"`
	RunID                    string                 `json:"runId,omitempty"`
	Seq                      int64                  `json:"-"`
	Cursor                   string                 `json:"cursor,omitempty"`
	HasMore                  bool                   `json:"hasMore"`
	Limit                    int                    `json:"limit"`
	ProjectionSchemaVersion  int                    `json:"projectionSchemaVersion"`
	ProjectionGeneration     int64                  `json:"-"`
}
```

Query `ordinal < beforeOrdinal` in descending order, enforce 120 default/500 maximum, reverse before returning, and return `ErrProjectionCursorExpired` on generation mismatch.

- [ ] **Step 4: Implement content chunking and streaming**

Split encoded content at 256 KiB boundaries. Store a 16,000-character preview and content reference on the item. The content endpoint reads fixed chunks and honors a single RFC 7233 byte range without assembling the entire body.

- [ ] **Step 5: Implement retention and session projection deletion**

After projection writes but before stream update, delete the oldest complete event prefix until active/terminal row and byte limits are both satisfied; update `min_retained_seq` in the same transaction. Never delete item/chunk projection rows during retention. Implement `DeleteSession` as one SQLite transaction deleting the parent `session_streams` row and relying on tested foreign-key cascades for every event-store child table.

- [ ] **Step 6: Run focused tests**

Run: `go test ./internal/piweb -run 'TestSnapshot|TestProjectionCursor|TestRetention|TestStreamItemContent' -count=1`

Expected: PASS, including byte-for-byte 10 MiB output verification.

- [ ] **Step 7: Commit**

```bash
git add internal/piweb/session_event_query.go internal/piweb/session_content.go internal/piweb/session_event_query_test.go internal/piweb/session_content_test.go internal/piweb/session_event_append.go internal/piweb/types.go
git commit -m "feat(stream): query durable transcript projections" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 5: Strict JSONL import, manifests, and atomic rebase

**Files:**
- Create: `internal/piweb/session_event_importer.go`
- Create: `internal/piweb/session_event_manifest.go`
- Create: `internal/piweb/session_event_importer_test.go`
- Modify: `internal/piweb/pi_session_messages.go`
- Modify: `internal/piweb/pi_sessions.go`

**Interfaces:**
- Consumes: `SessionSource`, event store, existing Pi session entry structs.
- Produces: `StrictParsePiSession(path string) (ImportedSession, SourceFingerprint, error)`.
- Produces: `BuildRunManifest(items []ProjectedSessionItem) RunManifest`.
- Produces: SQLite `OpenSession` implementation and durable `session.rebased` control event.

- [ ] **Step 1: Write strict import and manifest tests**

```go
func TestOpenSessionDefersRebaseUntilCompletedRunManifestMatches(t *testing.T) {
	store := eventStoreWithCompletedRun(t, "s1", completedToolManifest("run-1", "call-1", "final"))
	path := writePiSession(t, toolResultLine("call-1", "partial"))
	report, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil { t.Fatal(err) }
	if !report.Deferred { t.Fatalf("report = %#v", report) }
	assertProjectedToolBody(t, store.db, "s1", "tool:run-1:call-1", "final")
}
```

Add cases for first import, unchanged SHA-256, active-run defer, partial final line, malformed middle line, file change during parse, truncation/rewrite, multi-run manifests, tool args/thinking/attachments ordering, runtime-only metadata preservation, interrupted partial preservation, failed-candidate retry, and connected-client `session.rebased` sequence.

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./internal/piweb -run 'TestOpenSession|TestStrictParsePiSession|TestBuildRunManifest' -count=1`

Expected: FAIL because importer and manifest functions do not exist.

- [ ] **Step 3: Extend Pi parsing with stable source identity**

Retain entry ID, role, timestamp, block index, tool call ID, tool arguments, final result, status, text/thinking, and attachment metadata. Keep existing display conversion wrappers for legacy tests until Task 15.

```go
type ImportedSessionItem struct {
	SourceEntryID string
	BlockIndex    int
	Item          ProjectedSessionItem
}

func importedItemID(entryID string, blockIndex int) string {
	return "import:" + entryID + ":" + strconv.Itoa(blockIndex)
}
```

The strict parser rejects malformed complete lines and missing final newline; it reads file metadata before/after and hashes the accepted full contents.

- [ ] **Step 4: Implement canonical per-run manifests**

Canonical JSON uses explicit structs, stable item ordering, and SHA-256. Exclude only fields listed as runtime-only in the spec. Store one manifest per `(session_id, run_id)`.

- [ ] **Step 5: Implement atomic rebase**

Under one immediate transaction, verify inactive status and unchanged source fingerprint, compare all completed run manifests, append/claim `session.rebased`, increment projection generation, replace matched import/runtime rows, preserve overlays/annotations/interrupted output, and update the successful fingerprint. Deferred or failed imports do not mutate the fingerprint.

- [ ] **Step 6: Run focused tests**

Run: `go test ./internal/piweb -run 'TestOpenSession|TestStrictParsePiSession|TestBuildRunManifest' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/piweb/session_event_importer.go internal/piweb/session_event_manifest.go internal/piweb/session_event_importer_test.go internal/piweb/pi_session_messages.go internal/piweb/pi_sessions.go
git commit -m "feat(stream): import Pi history into projections" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 6: Database-backed SSE

**Files:**
- Create: `internal/piweb/session_sse.go`
- Create: `internal/piweb/session_sse_test.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/server_session_handlers.go`

**Interfaces:**
- Produces handlers `sessionState`, `sessionEventsV2`, `sessionItemContent`.
- Consumes: Task 2 wake hub plus `SessionEventStore.Snapshot`, `EventsAfter`, and content streaming.

- [ ] **Step 1: Write wake-hub and SSE recovery tests**

```go
func TestSessionSSEDoesNotLoseCommitBetweenCatchupAndWait(t *testing.T) {
	store, hub, server := newEventServer(t)
	first := appendTestMessage(t, store, "s1", "one")
	writer := newBlockingSSEWriter()
	writer.AfterCatchup(func() { appendTestMessage(t, store, "s1", "two") })
	serveEventsV2(t, server, writer, "s1", first.Seq)
	if got := writer.EventTypes(); !slices.Equal(got, []string{EventAssistantSegmentCommitted}) {
		t.Fatalf("events = %#v", got)
	}
	if hub.MaxPendingWake() > 1 { t.Fatalf("pending wake = %d", hub.MaxPendingWake()) }
}
```

Add tests for query `after` precedence, Last-Event-ID fallback, decimal bounds, heartbeat without ID, ascending exact sequences, 128-row/1 MiB batches, slow client, expired cursor ID-less reset, rebase-close, authorization wrappers, disconnect cleanup, and shutdown.

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./internal/piweb -run 'TestSessionWakeHub|TestSessionSSE|TestSessionStateHandler|TestSessionItemContentHandler' -count=1`

Expected: FAIL because v2 handlers and database-drain SSE do not exist.

- [ ] **Step 3: Implement state/content HTTP handlers**

Resolve the session's JSONL path and call `OpenSession` before the first-page snapshot; inactive imports may rebase, while active imports defer. Map `ErrProjectionCursorExpired` to HTTP 409 `cursor_expired`, not-found to 404, invalid cursors/ranges to 400/416, and database failures to sanitized 500 responses. Serialize sequence and projection generation as decimal strings.

- [ ] **Step 4: Implement the SSE database-drain loop**

Write committed envelopes as `event: session.event`; reserve `stream.reset` and `heartbeat` as separate event names. Register wake before first query. Drain batches until caught up, then wait for wake/heartbeat/context. Emit `stream.reset` without SSE ID when cursor expired. Deliver `session.rebased`, flush, and close.

- [ ] **Step 5: Run focused and race tests**

Run: `go test ./internal/piweb -run 'TestSessionWakeHub|TestSessionSSE|TestSessionStateHandler|TestSessionItemContentHandler' -count=1`

Run: `go test -race ./internal/piweb -run 'TestSessionWakeHub|TestSessionSSEDoesNotLose' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/piweb/session_sse.go internal/piweb/session_sse_test.go internal/piweb/server.go internal/piweb/server_session_handlers.go
git commit -m "feat(stream): serve committed session events" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 7: Ordered runtime adapter and coalescing

**Files:**
- Create: `internal/piweb/runtime_event_adapter.go`
- Create: `internal/piweb/runtime_event_coalescer.go`
- Create: `internal/piweb/runtime_event_adapter_test.go`
- Modify: `internal/piweb/runner_events.go`

**Interfaces:**
- Produces: `NewRuntimeEventAdapter(sessionID, runID string, sink SessionEventSink, clock Clock) *RuntimeEventAdapter`.
- Produces: `Consume(raw []byte) error`, `Flush() error`, `Fail(error) error`.
- Produces: `SessionEventSink.AppendRunEvent(context.Context, string, string, int64, SessionEventDraft) (StoredSessionEvent, error)`.
- Consumes: Pi RPC event shapes and Task 3 append path.

- [ ] **Step 1: Write adapter and coalescer tests**

```go
func TestRuntimeEventAdapterFlushesFinalToolBodyBeforeFinish(t *testing.T) {
	sink := &recordingSessionEventSink{}
	adapter := NewRuntimeEventAdapter("s1", "run-1", sink, fakeClock())
	consumeJSON(t, adapter, toolStartJSON("call-1", "bash"))
	consumeJSON(t, adapter, toolUpdateJSON("call-1", "partial"))
	consumeJSON(t, adapter, toolFinishJSON("call-1", "final", false))
	if got := sink.Types(); !slices.Equal(got, []string{EventToolStarted, EventToolOutput, EventToolFinished}) {
		t.Fatalf("types = %#v", got)
	}
	if sink.Events[1].PayloadString("body") != "partial" || sink.Events[2].PayloadString("body") != "final" {
		t.Fatalf("events = %#v", sink.Events)
	}
}
```

Add tests for 20 ms flush, 16 KiB flush, text/thinking separation, append concatenation, replace latest-only, 256 KiB split, stable namespaced IDs, contiguous source ordinals, user message association for manifests, malformed frames, redaction, finish with active tool fallback to run failure, and sink failure poisoning.

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./internal/piweb -run 'TestRuntimeEventAdapter|TestRuntimeEventCoalescer' -count=1`

Expected: FAIL because adapter types do not exist.

- [ ] **Step 3: Implement coalescing buffers**

```go
type pendingRuntimeEvent struct {
	Kind     string
	EntityID string
	Mode     string
	Data     []byte
	Started  time.Time
}

func shouldFlushPending(p pendingRuntimeEvent, now time.Time) bool {
	return len(p.Data) >= 16*1024 || now.Sub(p.Started) >= 20*time.Millisecond
}
```

Use one timer owned by the adapter serializer. Lifecycle boundaries synchronously flush pending data before appending their event.

- [ ] **Step 4: Implement Pi frame normalization**

Move behavioral conversion out of `handlePiJSONEvent`. Generate assistant/thinking segment IDs from run ID and segment ordinal; use Pi tool call IDs with run namespace; map output modes/status explicitly; split oversized payloads without changing byte order.

- [ ] **Step 5: Implement poisoned-run behavior**

The first sink failure marks the adapter poisoned before returning. Later `Consume` calls return `ErrRunPoisoned` without appending. `Fail` attempts one run-failed event only when persistence remains available.

- [ ] **Step 6: Run focused tests**

Run: `go test ./internal/piweb -run 'TestRuntimeEventAdapter|TestRuntimeEventCoalescer' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/piweb/runtime_event_adapter.go internal/piweb/runtime_event_coalescer.go internal/piweb/runtime_event_adapter_test.go internal/piweb/runner_events.go
git commit -m "feat(stream): normalize ordered runtime output" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 8: Durable command service and Runner integration

**Files:**
- Create: `internal/piweb/session_command_service.go`
- Create: `internal/piweb/session_command_service_test.go`
- Modify: `internal/piweb/runner.go`
- Modify: `internal/piweb/runner_tail.go`
- Modify: `internal/piweb/compaction.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Modify: `internal/piweb/types.go`
- Modify: `internal/piweb/runner_test.go`
- Modify: `internal/piweb/compaction_test.go`

**Interfaces:**
- Produces: `StartPrompt(ctx context.Context, command StartPromptCommand) (RunReceipt, error)`.
- Produces: `Steer(ctx context.Context, sessionID, text string, attachments []PromptAttachment) error` that persists before runtime send.
- Produces: `Cancel(ctx context.Context, sessionID string) (bool, error)` with one fenced terminal event.
- Consumes: event store, runtime manager, runtime adapter.

- [ ] **Step 1: Write command-ordering and idempotency tests**

```go
func TestStartPromptCommitsBeforeSendingRuntimeCommand(t *testing.T) {
	order := []string{}
	store := recordingEventStore(func() { order = append(order, "commit") })
	runtime := recordingRuntime(func() { order = append(order, "send") })
	service := newCommandServiceForTest(store, runtime)
	_, err := service.StartPrompt(t.Context(), StartPromptCommand{SessionID: "s1", ClientRequestID: "req-1", Text: "hello"})
	if err != nil { t.Fatal(err) }
	if !slices.Equal(order, []string{"commit", "send"}) { t.Fatalf("order = %#v", order) }
}
```

Add duplicate request after restart, hash conflict, startup/send failure terminalization, concurrent prompt conflict, steer persist-before-send, cancel/finish race, cancellation failure fencing, compaction state, mock execution, durable auto/manual rename, and no old Broker publish before commit.

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./internal/piweb -run 'TestStartPrompt|TestSessionCommandService|TestRunnerDurable|TestCompactDurable' -count=1`

Expected: FAIL because the command service is absent and Runner still depends on Broker.

- [ ] **Step 3: Extend prompt request and API receipt**

```go
type PromptRequest struct {
	ClientRequestID string             `json:"clientRequestId"`
	Text            string             `json:"text"`
	Attachments     []PromptAttachment `json:"attachments,omitempty"`
}

type PromptResponse struct {
	Accepted bool   `json:"accepted"`
	RunID    string `json:"runId"`
	Existing bool   `json:"existing"`
	RealPi   bool   `json:"realPi"`
}
```

Validate the request ID as a canonical UUID and compute request hash over normalized text plus attachment metadata/content.

- [ ] **Step 4: Implement command service and refactor Runner**

Runner owns runtime process lifecycle but emits only through `RuntimeEventAdapter`. It no longer appends display messages to `Store` or publishes run events directly to Broker. Create adapter/serializer before runtime send so post-commit send failure can append `run.failed`.

- [ ] **Step 5: Refactor steer, cancel, compaction, rename, and mock flow**

Persist steering user events before `runtime.Send`. Serialize cancellation with runtime output and let the first terminal commit win. Represent compaction status/error/result through durable control/run events. Persist auto-name and manual rename as idempotent control events after the metadata mutation succeeds. Replace `PublishMockPrompt` and JSONL tail publication with deterministic event-service appends.

- [ ] **Step 6: Run focused and race tests**

Run: `go test ./internal/piweb -run 'TestStartPrompt|TestSessionCommandService|TestRunnerDurable|TestCompactDurable' -count=1`

Run: `go test -race ./internal/piweb -run 'TestSessionCommandService.*Concurrent|TestRunner.*Cancel' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/piweb/session_command_service.go internal/piweb/session_command_service_test.go internal/piweb/runner.go internal/piweb/runner_tail.go internal/piweb/compaction.go internal/piweb/server_session_handlers.go internal/piweb/types.go internal/piweb/runner_test.go internal/piweb/compaction_test.go
git commit -m "feat(stream): persist commands before execution" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 9: Startup recovery and production lifecycle

**Files:**
- Create: `internal/piweb/session_event_recovery.go`
- Create: `internal/piweb/session_event_recovery_test.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/store_shutdown.go`
- Modify: `server.go`
- Modify: `server_test.go`

**Interfaces:**
- Produces: `OpenServer(config Config, store *Store) (*Server, error)` for production.
- Produces: `NewServerWithDependencies(config Config, store *Store, manager *RuntimeManager, events SessionEventStore, hub *SessionWakeHub) *Server` for tests.
- Consumes: `RecoverInterrupted`, EventStore `Close`, server lifecycle.

- [ ] **Step 1: Write restart and startup-failure tests**

```go
func TestOpenServerRecoversInterruptedRunsBeforeServing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi-web.db")
	seedActiveTool(t, path, "s1", "run-1", "call-1", "partial")
	server, err := OpenServer(Config{}, webStoreAt(path))
	if err != nil { t.Fatal(err) }
	defer server.Shutdown(t.Context())
	snapshot := requestState(t, server, "s1")
	assertToolStatus(t, snapshot, "call-1", "interrupted")
	if snapshot.Status != "interrupted" { t.Fatalf("status = %s", snapshot.Status) }
}
```

Add migration failure prevents listen, recovery failure prevents listen, no auto runtime send, shutdown order, EventStore close/checkpoint, SSE drain, repeated recovery idempotency, and mock server construction.

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./... -run 'TestOpenServer|TestRunServer.*EventStore|TestRecoverInterrupted' -count=1`

Expected: FAIL because production initialization and recovery are absent.

- [ ] **Step 3: Implement startup recovery**

Scan active stream rows inside bounded batches. For each run, append one deterministic next-ordinal `run.interrupted` event through the normal projector. Recovery completes before routes are exposed.

- [ ] **Step 4: Introduce fallible production construction**

`runServer` dependencies use a constructor returning `(*piweb.Server, error)`. Existing unrelated server tests use injected temporary SQLite or repository fakes. Production always opens `store.dbPath`; empty production path is an error.

- [ ] **Step 5: Implement shutdown order**

Reject new commands, close hub subscriptions and wait for SSE/AG-UI handlers, stop runtimes, run WAL checkpoint, close EventStore, then close legacy resources still present during migration. Join all errors explicitly.

- [ ] **Step 6: Run focused tests**

Run: `go test ./... -run 'TestOpenServer|TestRunServer.*EventStore|TestRecoverInterrupted|TestServerShutdown' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/piweb/session_event_recovery.go internal/piweb/session_event_recovery_test.go internal/piweb/server.go internal/piweb/store_shutdown.go server.go server_test.go
git commit -m "feat(stream): recover interrupted session runs" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 10: AG-UI durable adapter

**Files:**
- Create: `internal/piweb/agui_event_adapter.go`
- Create: `internal/piweb/agui_event_adapter_test.go`
- Modify: `internal/piweb/agui.go`
- Modify: `internal/piweb/agui_test.go`

**Interfaces:**
- Consumes: command service, EventStore `EventsAfter`, wake hub.
- Produces: `ConsumeDomainEvent(event StoredSessionEvent, stream *aguiEventStream) (terminal bool, err error)`.
- Preserves: existing `POST /api/sessions/{sessionID}/ag-ui` external route.

- [ ] **Step 1: Write AG-UI ordering and disconnect tests**

```go
func TestAguiToolResultPrecedesToolEndAndCursorFollowsPayload(t *testing.T) {
	body := renderAguiDomainEvents(t,
		storedToolOutput("run-1", "call-1", "final"),
		storedToolFinished("run-1", "call-1", "ok"),
	)
	result := strings.Index(body, `"type":"TOOL_CALL_RESULT"`)
	end := strings.Index(body, `"type":"TOOL_CALL_END"`)
	cursor := strings.LastIndex(body, `"name":"piweb.broker-cursor"`)
	if result < 0 || end < result || cursor < end { t.Fatalf("body = %s", body) }
}
```

Add error status mapping, text/thinking boundaries, run terminal mapping, cursor string range, database reset behavior, request disconnect does not cancel run, and AG-UI start command idempotency.

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./internal/piweb -run 'TestAgui.*Durable|TestAguiToolResultPrecedes' -count=1`

Expected: FAIL because AG-UI still consumes Broker events and emits cursor first.

- [ ] **Step 3: Implement domain-to-AG-UI translation**

Read only committed events. Emit translated payload first, final tool result before end, explicit failed status metadata, then cursor custom event. Do not let request cancellation propagate to the command service run context.

- [ ] **Step 4: Remove AG-UI process-local replay authority**

Delete direct Broker subscription and tool replay assumptions from `agui.go`. Keep only per-connection protocol assembly state; recovery cursor points to `/events-v2`.

- [ ] **Step 5: Run focused tests**

Run: `go test ./internal/piweb -run 'TestAgui' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/piweb/agui_event_adapter.go internal/piweb/agui_event_adapter_test.go internal/piweb/agui.go internal/piweb/agui_test.go
git commit -m "refactor(stream): adapt AG-UI to durable events" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 11: Frontend cursor helpers and canonical reducer

**Files:**
- Create: `src/pi-app/sessions/transcript-state.ts`
- Create: `src/pi-app/sessions/transcript-reducer.ts`
- Create: `src/pi-app/sessions/transcript-reducer.test.ts`
- Modify: `src/pi-app/sessions/session-recovery.ts`
- Modify: `src/pi-app/sessions/session-recovery.test.ts`

**Interfaces:**
- Produces: `TranscriptState`, `TranscriptItem`, `DurableSessionEvent`, `SessionStateSnapshot`.
- Produces: `installSessionSnapshot`, `reduceSessionEvent`, `mergeHistoryPage`.
- Produces: signed-int64 decimal helpers `normalizeSessionSeq`, `compareSessionSeq`, `nextSessionSeq`.

- [ ] **Step 1: Write pure reducer tests**

```ts
it("updates an offscreen tool by run and call id", () => {
  const initial = installSessionSnapshot(emptyState("s1"), snapshotAt("4", [runningTool("run-1", "call-1")]));
  const next = reduceSessionEvent(initial, toolOutputEvent("5", "run-1", "call-1", "hello", "append"));
  const itemId = next.toolsByCallId.get("run-1:call-1");
  expect(next.itemsById.get(itemId)?.bodyPreview).toBe("hello");
  expect(next.appliedSeq).toBe("5");
});

it("rejects a forward sequence gap", () => {
  const initial = { ...emptyState("s1"), appliedSeq: "9" };
  expect(() => reduceSessionEvent(initial, runFinishedEvent("11", "run-1"))).toThrow("sequence gap");
});
```

Cover snapshot replacement, duplicate sequence, BigInt 9→10 and max signed value, stale view generation, projection generation, text/thinking lifecycle, append/replace output, final body/status, all terminal states, conflicting entities, run rebase reset, pagination merge/dedup, and cross-session isolation.

- [ ] **Step 2: Run tests and confirm RED**

Run: `bunx vitest run src/pi-app/sessions/transcript-reducer.test.ts src/pi-app/sessions/session-recovery.test.ts`

Expected: FAIL because reducer modules do not exist and cursor max is still uint64.

- [ ] **Step 3: Implement decimal cursor helpers**

```ts
const MAX_SESSION_SEQ = 9_223_372_036_854_775_807n;

export function nextSessionSeq(value: unknown): string | undefined {
  const normalized = normalizeSessionSeq(value);
  if (normalized === undefined) return undefined;
  const next = BigInt(normalized) + 1n;
  return next <= MAX_SESSION_SEQ ? next.toString() : undefined;
}
```

Never serialize `BigInt`; state and API values remain decimal strings.

- [ ] **Step 4: Define normalized state and pure reducer**

```ts
export type TranscriptState = {
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

Use immutable top-level replacements and copy only maps/items touched by an event. Throw typed reset errors for gaps, rebase, incompatible entities, and malformed payloads.

- [ ] **Step 5: Run focused coverage**

Run: `bunx vitest run src/pi-app/sessions/transcript-reducer.test.ts src/pi-app/sessions/session-recovery.test.ts`

Run: `bun run test:coverage`

Expected: PASS and 100% statements/branches/functions/lines, including the new reducer and cursor modules.

- [ ] **Step 6: Commit**

```bash
git add src/pi-app/sessions/transcript-state.ts src/pi-app/sessions/transcript-reducer.ts src/pi-app/sessions/transcript-reducer.test.ts src/pi-app/sessions/session-recovery.ts src/pi-app/sessions/session-recovery.test.ts
git commit -m "feat(stream): add canonical transcript reducer" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 12: State-driven transcript rendering and virtual tools

**Files:**
- Create: `src/pi-app/transcript/transcript-render-scheduler.ts`
- Create: `src/pi-app/transcript/transcript-render-scheduler.test.ts`
- Create: `src/pi-app/transcript/transcript-state-methods.ts`
- Create: `src/pi-app/transcript/transcript-state-methods.test.ts`
- Modify: `src/pi-app/messages/message-methods.ts`
- Modify: `src/pi-app/messages/tool-message-methods.ts`
- Modify: `src/pi-app/transcript/transcript-window-methods.ts`
- Modify: `src/pi-app/transcript/transcript-virtual-scroller.ts`
- Modify: `src/pi-app/transcript/transcript-window.test.ts`
- Modify: `src/pi-app/messages/messages.test.ts`
- Modify: `src/pi-app/index.ts`

**Interfaces:**
- Consumes: Task 11 `TranscriptState` and reducer.
- Produces: `scheduleTranscriptEntities(ids)`, `flushTranscriptEntities()`, `renderTranscriptItem(itemID)`.
- Removes: business-state reads from `.tool-card`, detached node caches, and DOM-based duplicate detection.

- [ ] **Step 1: Write pure scheduler and state-selection tests**

```ts
it("selects the latest offscreen tool state without a DOM dependency", () => {
  const initial = installSessionSnapshot(emptyState("s1"), runningToolSnapshot("run-1", "call-1"));
  const finished = reduceSessionEvent(initial, toolFinishedEvent("8", "run-1", "call-1", "final", "ok"));
  const selected = selectTranscriptRenderItem(finished, "tool:run-1:call-1");
  expect(selected).toMatchObject({ status: "ok", bodyPreview: "final" });
  expect(finished.itemOrder.filter((id) => id === "tool:run-1:call-1")).toHaveLength(1);
});
```

Use pure fake-clock tests for 32-update forced flush, 16 ms timer flush, 128-entity cap, session switch queue reset, snapshot queue reset, item selection, prepend ordering, and full-output content references. Do not add unit render snapshots; actual mount/unmount behavior belongs to Task 14 Playwright tests.

- [ ] **Step 2: Run tests and confirm RED**

Run: `bunx vitest run src/pi-app/transcript/transcript-render-scheduler.test.ts src/pi-app/transcript/transcript-state-methods.test.ts src/pi-app/transcript/transcript-window.test.ts src/pi-app/messages/messages.test.ts`

Expected: FAIL because rendering still treats DOM nodes as canonical state.

- [ ] **Step 3: Implement shared render scheduler**

```ts
export const MAX_PENDING_ENTITY_RENDERS = 128;
export const MAX_UPDATES_BEFORE_FLUSH = 32;
export const MAX_RENDER_DELAY_MS = 16;

export function enqueueEntityRender(queue: RenderQueue, itemId: string, now: number): "queued" | "flush" {
  queue.ids.add(itemId);
  queue.updates += 1;
  if (queue.startedAt === undefined) queue.startedAt = now;
  return queue.ids.size >= MAX_PENDING_ENTITY_RENDERS || queue.updates >= MAX_UPDATES_BEFORE_FLUSH ? "flush" : "queued";
}
```

Use rAF plus a 16 ms timeout fallback; cancel both on flush/reset.

- [ ] **Step 4: Render virtual items from state**

Transcript items contain stable IDs, not durable node arrays. `renderVirtualTranscriptItem` asks `renderTranscriptItem(itemID)` for current state. Resize observers remain DOM-only and cannot mutate message/tool content.

- [ ] **Step 5: Remove DOM-owned tool state**

`toolCard` becomes a pure renderer for one `TranscriptItem`. Delete `latestToolCards`, `__pendingToolOutput`, `__toolOutputText`, and detached-node update paths. Full-output button fetches content by `contentRef`; ordinary card state retains only preview metadata.

- [ ] **Step 6: Run focused tests and coverage**

Run: `bunx vitest run src/pi-app/transcript/transcript-render-scheduler.test.ts src/pi-app/transcript/transcript-state-methods.test.ts src/pi-app/transcript/transcript-window.test.ts src/pi-app/messages/messages.test.ts`

Run: `bun run test:coverage`

Expected: PASS with 100% coverage; no test asserts state through a detached cached tool element.

- [ ] **Step 7: Commit**

```bash
git add src/pi-app/transcript/transcript-render-scheduler.ts src/pi-app/transcript/transcript-render-scheduler.test.ts src/pi-app/transcript/transcript-state-methods.ts src/pi-app/transcript/transcript-state-methods.test.ts src/pi-app/messages/message-methods.ts src/pi-app/messages/tool-message-methods.ts src/pi-app/transcript/transcript-window-methods.ts src/pi-app/transcript/transcript-virtual-scroller.ts src/pi-app/transcript/transcript-window.test.ts src/pi-app/messages/messages.test.ts src/pi-app/index.ts
git commit -m "refactor(stream): render transcript from canonical state" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 13: Controlled SSE client and single prompt transport

**Files:**
- Create: `src/pi-app/sessions/session-stream-client.ts`
- Create: `src/pi-app/sessions/session-stream-client.test.ts`
- Modify: `src/lib/api.ts`
- Modify: `src/lib/api.test.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `src/pi-app/input/input-methods.ts`
- Modify: `src/pi-app/input/input-methods-coverage.test.ts`
- Modify: `src/pi-app/workspace/workspace-bootstrap-methods.ts`
- Modify: `src/pi-app/status/toast-methods.ts`
- Modify: `src/pi-app/sessions/session-isolation.test.ts`
- Modify: `src/pi-app/sessions/session-recovery.test.ts`

**Interfaces:**
- Produces: `getSessionState`, `sessionEventsV2`, `getSessionItemContent`, updated `postPrompt` with client request ID.
- Produces: `SessionStreamClient` with `start(snapshotSeq)`, `stop()`, `reset()`, controlled retry.
- Consumes: transcript reducer and render scheduler.
- Removes: Web UI calls to `runAguiSessionPrompt`.

- [ ] **Step 1: Write controlled reconnect tests**

```ts
it("reconnects from reducer-applied sequence, not EventSource lastEventId", async () => {
  const sources = installControlledEventSources();
  const client = new SessionStreamClient({ sessionId: "s1", appliedSeq: () => "7", onEvent: vi.fn(), onReset: vi.fn() });
  client.start("7");
  sources[0].emitRaw({ lastEventId: "8", data: "{" });
  sources[0].emitError();
  await advanceReconnectTimer();
  expect(sources[1].url).toContain("after=7");
});
```

Add 250 ms→5 s bounded exponential backoff, success reset, parse/reducer reset, ID-less stream reset, rebase reset, session switch cancellation, background terminal recovery, snapshot generation replacement, prompt UUID reuse on fetch retry, and no AG-UI request.

- [ ] **Step 2: Run tests and confirm RED**

Run: `bunx vitest run src/pi-app/sessions/session-stream-client.test.ts src/lib/api.test.ts src/pi-app/input/input-methods-coverage.test.ts src/pi-app/sessions/session-recovery.test.ts`

Expected: FAIL because v2 client/API are absent and prompt still uses AG-UI.

- [ ] **Step 3: Implement v2 API functions**

```ts
export function sessionEventsV2(sessionId: string, after: string) {
  const query = new URLSearchParams({ after });
  return new EventSource(`${apiBase()}/api/sessions/${encodeURIComponent(sessionId)}/events-v2?${query}`);
}

export function postPrompt(sessionId: string, clientRequestId: string, text: string, attachments = []) {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/prompt`, {
    method: "POST",
    body: JSON.stringify({ clientRequestId, text, attachments }),
  });
}
```

Validate response state with explicit guards before installing it.

- [ ] **Step 4: Implement controlled EventSource lifecycle**

Register `session.event`, `stream.reset`, and `heartbeat` listeners explicitly. Close native EventSource on every error. Recreate it using current reducer `appliedSeq`, starting at 250 ms and doubling to 5 s. Parse and validate before calling reducer. Any parse, sequence, rebase, or reset error invokes one deduplicated state reload.

- [ ] **Step 5: Switch prompt and background flows**

Keep the selected-session SSE open during prompt submission. Generate one `crypto.randomUUID()` per user submission and reuse it for request-level retries. POST returns receipt only; rendered user/run/output state comes from SSE. Background watches use the same durable client and cannot suppress reconnect replay.

- [ ] **Step 6: Run focused tests and coverage**

Run: `bunx vitest run src/pi-app/sessions/session-stream-client.test.ts src/lib/api.test.ts src/pi-app/input/input-methods-coverage.test.ts src/pi-app/sessions/session-recovery.test.ts src/pi-app/sessions/session-isolation.test.ts`

Run: `bun run test:coverage`

Expected: PASS with 100% coverage and no frontend test observes an `/ag-ui` prompt request.

- [ ] **Step 7: Commit**

```bash
git add src/pi-app/sessions/session-stream-client.ts src/pi-app/sessions/session-stream-client.test.ts src/lib/api.ts src/lib/api.test.ts src/pi-app/index.ts src/pi-app/input/input-methods.ts src/pi-app/input/input-methods-coverage.test.ts src/pi-app/workspace/workspace-bootstrap-methods.ts src/pi-app/status/toast-methods.ts src/pi-app/sessions/session-isolation.test.ts src/pi-app/sessions/session-recovery.test.ts
git commit -m "refactor(stream): use one resumable SSE channel" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 14: Browser lifecycle and stress regressions

**Files:**
- Create: `.env.test`
- Create: `playwright.config.ts`
- Create: `e2e/session-stream-recovery.spec.ts`
- Create: `e2e/fixtures/session-stream-backend.ts`
- Create: `internal/piweb/session_event_stress_test.go`
- Modify: `package.json`
- Modify: `bun.lock`

**Interfaces:**
- Consumes: completed v2 state/SSE frontend and backend.
- Produces: `bun run test:e2e` with URL loaded only from `PI_WEB_E2E_BASE_URL`.
- Produces: deterministic mocked stream fixture with explicit lifecycle barriers.

- [ ] **Step 1: Install Playwright through the official package**

Run: `bun add --dev @playwright/test`

Run: `bunx playwright install chromium`

Expected: `package.json` and `bun.lock` contain the pinned Playwright dependency and Chromium installation succeeds.

- [ ] **Step 2: Add environment-driven Playwright configuration**

`.env.test`:

```dotenv
PI_WEB_E2E_BASE_URL=http://127.0.0.1:4321
```

`playwright.config.ts` reads `process.env.PI_WEB_E2E_BASE_URL` and throws when absent. Add script:

```json
"test:e2e": "set -a; . ./.env.test; set +a; playwright test"
```

Configure one Chromium project, no shared state, trace on first retry, and Astro `webServer` using the same environment URL.

- [ ] **Step 3: Write the failing lifecycle matrix**

Use `page.route("**/api/**", fixture.handleHTTP)` for state, prompt, and content. Install a deterministic `MockEventSource` with `page.addInitScript` before application code; it records URLs and accepts explicit `session.event`, `stream.reset`, heartbeat, open, and error frames from the fixture. Parameterize refresh/disconnect at assistant delta, tool start, each output mode, tool finish, final assistant commit, run finish, cancellation, error, stream reset, offscreen virtualization, and two-session switching.

```ts
for (const boundary of ["assistant-delta", "tool-start", "tool-output", "tool-finish", "run-finish"] as const) {
  test(`refresh at ${boundary} preserves exact transcript`, async ({ page }) => {
    const fixture = await installSessionStreamFixture(page, boundary);
    await page.goto("/");
    await fixture.runUntilBoundary();
    await page.reload();
    await fixture.finishRun();
    await expect(page.getByText("final bash output", { exact: true })).toBeVisible();
    await expect(page.locator(".tool-card")).toHaveCount(1);
    await expect(page.locator(".tool-card")).toHaveAttribute("data-status", "ok");
  });
}
```

- [ ] **Step 4: Run browser tests and confirm failures before completing fixture support**

Run: `bun run test:e2e`

Expected: lifecycle cases fail at the first unsupported boundary; retain the failure output in task notes.

- [ ] **Step 5: Complete deterministic fixture and make tests pass**

The Node-side fixture emits exact decimal sequences and stores committed projection state independently from the page. Its browser-side `MockEventSource` exercises the real stream client callbacks without relying on Playwright route streaming. It must expose barriers without timers, preserve no state between tests, and emulate disconnect/reconnect URL capture, retention reset, and content ranges.

- [ ] **Step 6: Add backend stress tests**

Append 10,000 mixed events and one 10 MiB tool output. Assert active/terminal row and byte retention, one wake per subscriber, fixed read batch limits, exact projected bytes, startup recovery, and no goroutine leak after shutdown.

- [ ] **Step 7: Run browser and stress tests**

Run: `bun run test:e2e`

Run: `go test ./internal/piweb -run 'TestSessionEventStress|TestLargeToolOutputStress' -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add .env.test playwright.config.ts e2e/session-stream-recovery.spec.ts e2e/fixtures/session-stream-backend.ts internal/piweb/session_event_stress_test.go package.json bun.lock
git commit -m "test(stream): cover durable recovery boundaries" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

---

### Task 15: Remove legacy replay and complete repository verification

**Files:**
- Delete: `internal/piweb/broker.go`
- Delete: `internal/piweb/broker_types.go`
- Delete: `internal/piweb/broker_test.go`
- Delete: `internal/piweb/broker_recovery_test.go`
- Modify: `internal/piweb/server.go`
- Modify: `internal/piweb/server_session_handlers.go`
- Modify: `internal/piweb/store_sessions.go`
- Modify: `internal/piweb/runner_tail.go`
- Modify: `internal/piweb/repository_contract_test.go`
- Modify: `internal/piweb/account_isolation_test.go`
- Modify: `internal/piweb/accounts_test.go`
- Modify: `internal/piweb/admin_password_test.go`
- Modify: `internal/piweb/compaction_test.go`
- Modify: `internal/piweb/coverage_more_test.go`
- Modify: `internal/piweb/prompt_attachments_test.go`
- Modify: `internal/piweb/runtime_regression_test.go`
- Modify: `internal/piweb/server_app_config_test.go`
- Modify: `internal/piweb/server_test.go`
- Modify: `internal/piweb/server_workspace_handlers_test.go`
- Modify: `internal/piweb/server_workspace_policy_test.go`
- Modify: `internal/piweb/server_workspace_selection_test.go`
- Modify: `internal/piweb/session_pagination_test.go`
- Modify: `internal/piweb/session_status_test.go`
- Modify: `src/lib/api.ts`
- Modify: `src/pi-app/index.ts`
- Modify: `src/pi-app/messages/message-methods.ts`
- Modify: `src/pi-app/messages/tool-message-methods.ts`
- Modify: `src/pi-app/core-events.test.ts`
- Modify: `src/pi-app/core-events-coverage.test.ts`
- Modify: `src/pi-app/messages/messages.test.ts`
- Modify: `src/pi-app/sessions/session-recovery.test.ts`
- Modify: `src/pi-app/sessions/sessions.test.ts`
- Modify: `vitest.config.ts`

**Interfaces:**
- Consumes: all durable architecture tasks.
- Removes: Broker session history, `activeRunHistory`, `replay=active`, `snapshotAfter`, `broker_gap`, Web `runAguiSessionPrompt`, DOM tool caches, old JSONL snapshot cursor coupling.
- Produces: final server constructors requiring EventStore/hub dependencies and final v2-only Web stream path.

- [ ] **Step 1: Add repository contract failures for forbidden legacy symbols**

```go
func TestRepositoryHasNoLegacySessionReplay(t *testing.T) {
	assertRepositoryExcludes(t, []string{"activeRunHistory", "replayActiveRunAfter", "SubscribeFromActive", "snapshotAfter"})
}
```

Add frontend contract checks excluding `runAguiSessionPrompt(` from `input-methods.ts`, `latestToolCards`, `__pendingToolOutput`, and `broker_gap` recovery.

- [ ] **Step 2: Run contract tests and confirm RED**

Run: `go test ./internal/piweb -run 'TestRepositoryHasNoLegacySessionReplay' -count=1`

Run: `bunx vitest run src/pi-app/core-events.test.ts src/pi-app/messages/messages.test.ts`

Expected: FAIL because legacy Broker and DOM-owned state still exist.

- [ ] **Step 3: Remove compatibility publication and legacy endpoints**

Delete old Broker implementation/tests and `/events` replay behavior after every producer/consumer uses EventStore. Update constructors and all listed tests to inject temporary SQLite or focused fakes. Make session deletion close runtime ownership, delete the Pi session, then delete EventStore state; a failed EventStore cleanup returns an error and is retried by startup orphan cleanup. Remove Store display-message append helpers when no remaining call site exists. Keep `/ag-ui` as the durable external adapter.

- [ ] **Step 4: Remove frontend dual transport and DOM state**

Delete unused AG-UI Web client callbacks, old cursor maps, active replay query construction, streaming DOM buffers superseded by reducer entities, and obsolete tests. Update coverage includes so every new reducer/client/scheduler module is covered at 100%; do not hide new logic through exclusions.

- [ ] **Step 5: Run formatting without touching generated assets**

Run: `gofmt -w $(git diff --name-only --diff-filter=ACM 7320068b -- '*.go')`

Run: `bunx prettier --write $(git diff --name-only --diff-filter=ACM 7320068b -- '*.{ts,tsx,json,md}' | grep -v '^static/' | tr '\n' ' ')`

Expected: commands succeed; `git status --short static dist` is empty.

- [ ] **Step 6: Run all frontend checks**

Run: `bun run lint`

Run: `bun run typecheck`

Run: `bun run test:coverage`

Run: `bun run test:e2e`

Expected: all PASS; frontend coverage thresholds are 100%.

- [ ] **Step 7: Run all backend checks**

Run: `go test ./... -count=1`

Run: `go test -race ./... -count=1`

Run: `bun run backend:test:coverage`

Expected: all PASS; total Go coverage reports `100.0%`.

- [ ] **Step 8: Run production build and inspect generated-file boundary**

Run: `bun run build`

Run: `git status --short`

Expected: build succeeds; no `static/` or `dist/` path is staged or modified by the implementation.

- [ ] **Step 9: Review acceptance matrix**

Compare the completed behavior against all 11 acceptance criteria in the spec. Record exact commands and results in the task file. Any unverified criterion blocks completion.

- [ ] **Step 10: Commit legacy removal**

```bash
git add -A internal/piweb src package.json bun.lock playwright.config.ts e2e vitest.config.ts .env.test
git reset -- static dist
git commit -m "refactor(stream): remove legacy replay architecture" -m "Co-authored-by: JuunAI <juunai.ai.i@gmail.com>"
```

- [ ] **Step 11: Verify final commit and clean worktree**

Run: `git status --short --branch`

Run: `git log -15 --format='%h %s%n%b'`

Expected: worktree clean; every new commit has exactly one required co-author trailer.
