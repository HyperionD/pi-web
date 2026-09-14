package piweb

import (
	"encoding/json"
	"strconv"
	"testing"
)

func appendRuntimeSegment(t *testing.T, store *SQLiteSessionEventStore, runID, entityID, kind, text string, ordinal int64) int64 {
	t.Helper()
	started, _ := json.Marshal(map[string]any{"kind": kind})
	if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", RunID: runID, SourceID: runID + ":event:" + strconv.FormatInt(ordinal, 10),
		SourceOrdinal: &ordinal, Type: EventAssistantSegmentStarted, EntityID: entityID, Payload: started,
	}); err != nil {
		t.Fatal(err)
	}
	ordinal++
	committed, _ := json.Marshal(map[string]any{"kind": kind, "text": text})
	if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", RunID: runID, SourceID: runID + ":event:" + strconv.FormatInt(ordinal, 10),
		SourceOrdinal: &ordinal, Type: EventAssistantSegmentCommit, EntityID: entityID, Payload: committed,
	}); err != nil {
		t.Fatal(err)
	}
	return ordinal + 1
}

func appendRuntimeTool(t *testing.T, store *SQLiteSessionEventStore, runID, callID, tool, args, body, status string, ordinal int64) int64 {
	t.Helper()
	entityID := "tool:" + runID + ":" + callID
	started, _ := json.Marshal(map[string]any{"toolCallId": callID, "tool": tool, "args": args})
	if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", RunID: runID, SourceID: runID + ":event:" + strconv.FormatInt(ordinal, 10),
		SourceOrdinal: &ordinal, Type: EventToolStarted, EntityID: entityID, Payload: started,
	}); err != nil {
		t.Fatal(err)
	}
	ordinal++
	finished, _ := json.Marshal(map[string]any{"body": body, "status": status})
	if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", RunID: runID, SourceID: runID + ":event:" + strconv.FormatInt(ordinal, 10),
		SourceOrdinal: &ordinal, Type: EventToolFinished, EntityID: entityID, Payload: finished,
	}); err != nil {
		t.Fatal(err)
	}
	return ordinal + 1
}

func appendRuntimeTerminal(t *testing.T, store *SQLiteSessionEventStore, runID, eventType, payload string, ordinal int64) {
	t.Helper()
	if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", RunID: runID, SourceID: runID + ":event:" + strconv.FormatInt(ordinal, 10),
		SourceOrdinal: &ordinal, Type: eventType, Payload: json.RawMessage(payload),
	}); err != nil {
		t.Fatal(err)
	}
}

func assistantMessageLine(id, thinking, text string) string {
	content := `[{"type":"thinking","thinking":"` + thinking + `"}]`
	if text != "" {
		content = `[{"type":"thinking","thinking":"` + thinking + `"},{"type":"text","text":"` + text + `"}]`
	}
	return `{"type":"message","id":"` + id + `","message":{"role":"assistant","content":` + content + `}}`
}

func assistantToolLine(id, callID, tool, args string) string {
	return `{"type":"message","id":"` + id + `","message":{"role":"assistant","content":[{"type":"toolCall","id":"` + callID + `","name":"` + tool + `","arguments":` + args + `}]}}`
}

func toolCallIDLine(id, callID, tool, body string) string {
	return `{"type":"message","id":"` + id + `","message":{"role":"toolResult","toolName":"` + tool + `","toolCallId":"` + callID + `","content":[{"type":"text","text":"` + body + `"}]}}`
}

func countSessionItems(t *testing.T, store *SQLiteSessionEventStore, query string, args ...any) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestOpenSessionRebaseReplacesCoveredRuntimeItems(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	next := appendRuntimeSegment(t, store, receipt.RunID, "thinking:"+receipt.RunID+":1", "think", "prior thought", 1)
	next = appendRuntimeSegment(t, store, receipt.RunID, "assistant:"+receipt.RunID+":2", "pi", "answer", next)
	appendRuntimeTerminal(t, store, receipt.RunID, EventRunFinished, `{}`, next)

	path := writePiSessionFile(t, sessionHeaderLine(),
		userMessageLine("e1", "hello"),
		assistantMessageLine("e2", "prior thought", "answer"),
	)
	report, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Imported || report.Deferred {
		t.Fatalf("report = %#v", report)
	}
	if duplicated := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1' AND kind='think'`); duplicated != 1 {
		t.Fatalf("think items = %d, want 1", duplicated)
	}
	if duplicated := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1' AND kind='user'`); duplicated != 1 {
		t.Fatalf("user items = %d, want 1", duplicated)
	}
	if runtime := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1' AND origin='runtime'`); runtime != 0 {
		t.Fatalf("runtime items = %d, want 0", runtime)
	}
	if total := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1'`); total != 3 {
		t.Fatalf("items = %d, want 3", total)
	}

	// A second rebase must not duplicate the already imported history.
	path = writePiSessionFile(t, sessionHeaderLine(),
		userMessageLine("e1", "hello"),
		assistantMessageLine("e2", "prior thought", "answer"),
		userMessageLine("e3", "again"),
	)
	if _, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path}); err != nil {
		t.Fatal(err)
	}
	if total := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1'`); total != 4 {
		t.Fatalf("items after second rebase = %d, want 4", total)
	}
}

func TestOpenSessionDefersRebaseWhenImportMissingRuntimeItem(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	next := appendRuntimeSegment(t, store, receipt.RunID, "thinking:"+receipt.RunID+":1", "think", "prior thought", 1)
	appendRuntimeTerminal(t, store, receipt.RunID, EventRunFinished, `{}`, next)

	path := writePiSessionFile(t, sessionHeaderLine(),
		userMessageLine("e1", "hello"),
		assistantMessageLine("e2", "different thought", "answer"),
	)
	report, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Deferred || report.Imported {
		t.Fatalf("report = %#v", report)
	}
	if imported := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1' AND origin='import'`); imported != 0 {
		t.Fatalf("import items = %d, want 0", imported)
	}
	if runtime := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1' AND origin='runtime'`); runtime != 2 {
		t.Fatalf("runtime items = %d, want 2", runtime)
	}
}

func TestOpenSessionRebasePreservesRuntimeOnlyFailureItem(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	next := appendRuntimeSegment(t, store, receipt.RunID, "thinking:"+receipt.RunID+":1", "think", "prior thought", 1)
	appendRuntimeTerminal(t, store, receipt.RunID, EventRunFailed, `{"error":"quota exceeded"}`, next)

	path := writePiSessionFile(t, sessionHeaderLine(),
		userMessageLine("e1", "hello"),
		assistantMessageLine("e2", "prior thought", ""),
	)
	report, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Imported {
		t.Fatalf("report = %#v", report)
	}
	if failures := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1' AND kind='error'`); failures != 1 {
		t.Fatalf("error items = %d, want 1", failures)
	}
	if thinks := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1' AND kind='think'`); thinks != 1 {
		t.Fatalf("think items = %d, want 1", thinks)
	}
}

func TestOpenSessionRebasePreservesForeignOriginItems(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	next := appendRuntimeSegment(t, store, receipt.RunID, "thinking:"+receipt.RunID+":1", "think", "prior thought", 1)
	appendRuntimeTerminal(t, store, receipt.RunID, EventRunFinished, `{}`, next)
	if _, err := store.db.Exec(`INSERT INTO session_items(session_id, item_id, ordinal, kind, origin, payload_json, created_seq, updated_seq) VALUES ('s1','annotation:1',99,'pi','annotation','{"itemId":"annotation:1","ordinal":99,"kind":"pi","origin":"annotation","text":"note"}',1,1)`); err != nil {
		t.Fatal(err)
	}

	path := writePiSessionFile(t, sessionHeaderLine(),
		userMessageLine("e1", "hello"),
		assistantMessageLine("e2", "prior thought", ""),
	)
	if _, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path}); err != nil {
		t.Fatal(err)
	}
	if annotations := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1' AND origin='annotation'`); annotations != 1 {
		t.Fatalf("annotation items = %d, want 1", annotations)
	}
}

func TestOpenSessionRebaseMatchesSplitImportedToolItems(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	args := `{"command":"pwd"}`
	next := appendRuntimeTool(t, store, receipt.RunID, "call-1", "bash", args, "output", ToolStatusOK, 1)
	appendRuntimeTerminal(t, store, receipt.RunID, EventRunFinished, `{}`, next)

	path := writePiSessionFile(t, sessionHeaderLine(),
		userMessageLine("e1", "hello"),
		assistantToolLine("e2", "call-1", "bash", args),
		toolCallIDLine("e3", "call-1", "bash", "output"),
	)
	report, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Imported {
		t.Fatalf("report = %#v", report)
	}
	if runtime := countSessionItems(t, store, `SELECT count(*) FROM session_items WHERE session_id='s1' AND origin='runtime'`); runtime != 0 {
		t.Fatalf("runtime items = %d, want 0", runtime)
	}
}

func TestOpenSessionDefersRebaseWhenImportToolOutputDiffers(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	args := `{"command":"pwd"}`
	next := appendRuntimeTool(t, store, receipt.RunID, "call-1", "bash", args, "output", ToolStatusOK, 1)
	appendRuntimeTerminal(t, store, receipt.RunID, EventRunFinished, `{}`, next)

	path := writePiSessionFile(t, sessionHeaderLine(),
		userMessageLine("e1", "hello"),
		assistantToolLine("e2", "call-1", "bash", args),
		toolCallIDLine("e3", "call-1", "bash", "different output"),
	)
	report, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Deferred {
		t.Fatalf("report = %#v", report)
	}
}

func TestOpenSessionRebaseMatchesToolItemWithoutCallID(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	next := appendRuntimeTool(t, store, receipt.RunID, "", "bash", `{"command":"pwd"}`, "", ToolStatusOK, 1)
	appendRuntimeTerminal(t, store, receipt.RunID, EventRunFinished, `{}`, next)

	path := writePiSessionFile(t, sessionHeaderLine(),
		userMessageLine("e1", "hello"),
		assistantToolLine("e2", "", "bash", `{"command":"pwd"}`),
	)
	report, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Imported {
		t.Fatalf("report = %#v", report)
	}
}

func TestOpenSessionFailsOnCorruptRuntimeProjection(t *testing.T) {
	store := appendTestStore(t)
	seedEventStream(t, store, "s1")
	if _, err := store.db.Exec(`UPDATE session_streams SET projection_schema_version=1, source_content_hash='old' WHERE session_id='s1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO session_items(session_id, item_id, ordinal, kind, origin, payload_json, created_seq, updated_seq) VALUES ('s1','bad',1,'pi','runtime','not-json',1,1)`); err != nil {
		t.Fatal(err)
	}
	path := writePiSessionFile(t, sessionHeaderLine(), userMessageLine("e1", "hello"))
	if _, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path}); err == nil {
		t.Fatal("expected corrupt projection error")
	}
}
