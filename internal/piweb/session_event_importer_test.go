package piweb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePiSessionFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func sessionHeaderLine() string {
	return `{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00Z","cwd":"/tmp/project"}`
}

func userMessageLine(id, text string) string {
	return `{"type":"message","id":"` + id + `","message":{"role":"user","content":[{"type":"text","text":"` + text + `"}]}}`
}

func toolResultLine(id, callID, body string) string {
	return `{"type":"message","id":"` + id + `","message":{"role":"toolResult","toolName":"bash","toolCallId":"` + callID + `","content":[{"type":"text","text":"` + body + `"}]}}`
}

func TestStrictParsePiSessionBuildsStableItems(t *testing.T) {
	path := writePiSessionFile(t, sessionHeaderLine(), userMessageLine("e1", "hello"), toolResultLine("e2", "call-1", "output"))
	parsed, err := StrictParsePiSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.ID != "s1" || len(parsed.Items) != 2 {
		t.Fatalf("parsed = %#v", parsed)
	}
	if parsed.Items[0].ItemID != "import:e1:0" || parsed.Items[1].ItemID != "import:e2:0" {
		t.Fatalf("item ids = %q, %q", parsed.Items[0].ItemID, parsed.Items[1].ItemID)
	}
	if parsed.Items[1].ToolCallID != "call-1" || parsed.Items[1].Body != "output" {
		t.Fatalf("tool item = %#v", parsed.Items[1])
	}
	if parsed.Fingerprint.ContentHash == "" || parsed.Fingerprint.Size == 0 {
		t.Fatalf("fingerprint = %#v", parsed.Fingerprint)
	}
}

func TestStrictParsePiSessionKeepsAssistantFailureBetweenMessages(t *testing.T) {
	path := writePiSessionFile(t, sessionHeaderLine(),
		userMessageLine("e1", "first prompt"),
		`{"type":"message","id":"e2","message":{"role":"assistant","content":[],`+
			`"stopReason":"error","errorMessage":"429 rate limit"}}`,
		userMessageLine("e3", "retry prompt"),
		`{"type":"message","id":"e4","message":{"role":"assistant",`+
			`"content":[{"type":"text","text":"success"}],"stopReason":"stop"}}`,
	)
	parsed, err := StrictParsePiSession(path)
	if err != nil {
		t.Fatal(err)
	}
	var kinds, texts []string
	for _, item := range parsed.Items {
		kinds = append(kinds, item.Kind)
		texts = append(texts, item.Text)
	}
	if got := strings.Join(kinds, ","); got != "user,error,user,pi" {
		t.Fatalf("kinds = %s", got)
	}
	if got := strings.Join(texts, ","); got != "first prompt,429 rate limit,retry prompt,success" {
		t.Fatalf("texts = %s", got)
	}
}

func TestStrictParsePiSessionRejectsPartialAndMalformedLines(t *testing.T) {
	if _, err := StrictParsePiSession(writePiSessionFile(t, sessionHeaderLine(), `{"type":"message"`)); err == nil {
		t.Fatal("expected malformed line error")
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(sessionHeaderLine()), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := StrictParsePiSession(path); err == nil {
		t.Fatal("expected missing final newline error")
	}
}

func TestOpenSessionImportsBaselineThenDeduplicatesByHash(t *testing.T) {
	store := appendTestStore(t)
	path := writePiSessionFile(t, sessionHeaderLine(), userMessageLine("e1", "hello"))
	report, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Imported {
		t.Fatalf("report = %#v", report)
	}
	again, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if again.Imported || again.Deferred {
		t.Fatalf("second import report = %#v", again)
	}
	var generation int64
	if err := store.db.QueryRow(`SELECT projection_generation FROM session_streams WHERE session_id='s1'`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation != 2 {
		t.Fatalf("generation = %d, want 2", generation)
	}
}

func TestOpenSessionDefersRebaseUntilManifestMatches(t *testing.T) {
	store := appendTestStore(t)
	path := writePiSessionFile(t, sessionHeaderLine(), toolResultLine("e2", "call-1", "partial"))
	manifest := RunManifest{Entries: []ManifestEntry{{Kind: "tool", ToolCallID: "call-1", Tool: "bash", Args: "", Body: "final", Status: ToolStatusOK}}}
	payload, _ := json.Marshal(manifest)
	if _, err := store.db.Exec(`INSERT INTO session_streams(session_id, status, updated_at) VALUES ('s1', 'idle', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO session_run_manifests(session_id, run_id, manifest_hash, manifest_json, completed_seq) VALUES ('s1','run-1','h',?,1)`, payload); err != nil {
		t.Fatal(err)
	}
	report, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Deferred || report.Imported {
		t.Fatalf("report = %#v", report)
	}
}

func TestOpenSessionDefersWhileRunActive(t *testing.T) {
	store := appendTestStore(t)
	appendTestRun(t, store, "req-1")
	path := writePiSessionFile(t, sessionHeaderLine(), userMessageLine("e1", "hello"))
	report, err := store.OpenSession(t.Context(), SessionSource{SessionID: "s1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Deferred {
		t.Fatalf("report = %#v", report)
	}
}
