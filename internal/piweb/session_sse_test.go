package piweb

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newEventHTTPTestServer(t *testing.T, sessionID string) (*Server, *SQLiteSessionEventStore, *httptest.Server) {
	t.Helper()
	store := appendTestStore(t)
	hub := NewSessionWakeHub()
	store.hub = hub
	webStore := NewMockStore()
	seedEventStream(t, store, sessionID)
	server := NewServerWithEventStore(Config{}, webStore, NewBroker(), store, hub)
	return server, store, httptest.NewServer(server.Handler())
}

func seedEventStream(t *testing.T, store *SQLiteSessionEventStore, sessionID string) {
	t.Helper()
	if _, err := store.db.Exec(`INSERT OR IGNORE INTO session_streams(session_id, status, updated_at) VALUES (?, 'idle', '2026-01-01T00:00:00Z')`, sessionID); err != nil {
		t.Fatal(err)
	}
}

func seedSessionItem(t *testing.T, store *SQLiteSessionEventStore, sessionID, itemID, body string) {
	t.Helper()
	item := ProjectedSessionItem{ItemID: itemID, Ordinal: 1, Kind: "tool", Origin: "runtime", Tool: "bash", Status: ToolStatusOK, Body: body}
	syncContentMetadata(&item, 1)
	if _, err := store.db.Exec(`INSERT INTO session_items(session_id, item_id, ordinal, kind, origin, payload_json, created_seq, updated_seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, sessionID, item.ItemID, item.Ordinal, item.Kind, item.Origin, eventMustJSON(t, item), 1, 1); err != nil {
		t.Fatal(err)
	}
	seedEventStream(t, store, sessionID)
	if _, err := store.db.Exec(`UPDATE session_streams SET current_seq=1 WHERE session_id=?`, sessionID); err != nil {
		t.Fatal(err)
	}
}

func eventMustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSessionStateReturnsProjectionAndCursor(t *testing.T) {
	_, store, httpServer := newEventHTTPTestServer(t, "8e7c-44ff")
	defer httpServer.Close()
	seedSessionItem(t, store, "8e7c-44ff", "tool:run-1:call-1", "hello")
	response, err := http.Get(httpServer.URL + "/api/sessions/8e7c-44ff/state")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var payload struct {
		Seq    string `json:"seq"`
		Status string `json:"status"`
		Items  []struct {
			ItemID string `json:"itemId"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Seq != "1" || payload.Status != "idle" || len(payload.Items) != 1 || payload.Items[0].ItemID != "tool:run-1:call-1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestSessionEventsV2DeliversCommittedEventsInOrder(t *testing.T) {
	_, store, httpServer := newEventHTTPTestServer(t, "8e7c-44ff")
	defer httpServer.Close()
	receipt, err := store.StartRun(t.Context(), StartRunCommand{SessionID: "8e7c-44ff", ClientRequestID: "req-1", RequestHash: "h", RunID: "run-1", DisplayText: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	_ = receipt
	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/sessions/8e7c-44ff/events-v2?after=0", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	types := readSSETypes(t, reader, 2)
	if len(types) != 2 || types[0] != sseEventName || types[1] != sseEventName {
		t.Fatalf("types = %#v", types)
	}
}

func TestSessionEventsV2DeliversLiveEventAfterWake(t *testing.T) {
	_, store, httpServer := newEventHTTPTestServer(t, "8e7c-44ff")
	defer httpServer.Close()
	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/sessions/8e7c-44ff/events-v2?after=0", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if _, err := store.StartRun(t.Context(), StartRunCommand{SessionID: "8e7c-44ff", ClientRequestID: "req-1", RequestHash: "h", RunID: "run-1", DisplayText: "hi"}); err != nil {
		t.Fatal(err)
	}
	types := readSSETypes(t, reader, 1)
	if len(types) != 1 || types[0] != sseEventName {
		t.Fatalf("types = %#v", types)
	}
}

func TestSessionEventsV2SendsResetForExpiredCursor(t *testing.T) {
	_, store, httpServer := newEventHTTPTestServer(t, "8e7c-44ff")
	defer httpServer.Close()
	if _, err := store.StartRun(t.Context(), StartRunCommand{SessionID: "8e7c-44ff", ClientRequestID: "req-1", RequestHash: "h", RunID: "run-1", DisplayText: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE session_streams SET min_retained_seq=2 WHERE session_id='8e7c-44ff'`); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(httpServer.URL + "/api/sessions/8e7c-44ff/events-v2?after=0")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	types := readSSETypes(t, bufio.NewReader(response.Body), 1)
	if len(types) != 1 || types[0] != sseResetName {
		t.Fatalf("types = %#v", types)
	}
}

func TestSessionEventsV2RejectsInvalidCursor(t *testing.T) {
	_, _, httpServer := newEventHTTPTestServer(t, "8e7c-44ff")
	defer httpServer.Close()
	response, err := http.Get(httpServer.URL + "/api/sessions/8e7c-44ff/events-v2?after=nope")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestSessionItemContentHandlerServesRange(t *testing.T) {
	_, store, httpServer := newEventHTTPTestServer(t, "8e7c-44ff")
	defer httpServer.Close()
	seedSessionItem(t, store, "8e7c-44ff", "tool:run-1:call-1", "0123456789")
	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/sessions/8e7c-44ff/items/tool:run-1:call-1/content", nil)
	request.Header.Set("Range", "bytes=2-5")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Content-Version") != "1" {
		t.Fatalf("status=%d version=%q", response.StatusCode, response.Header.Get("X-Content-Version"))
	}
	body := readAll(t, response.Body)
	if body != "2345" {
		t.Fatalf("body = %q", body)
	}
}

func readSSETypes(t *testing.T, reader *bufio.Reader, want int) []string {
	t.Helper()
	var types []string
	deadline := time.Now().Add(3 * time.Second)
	for len(types) < want && time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(line, "event: ") {
			types = append(types, strings.TrimSpace(strings.TrimPrefix(line, "event: ")))
		}
	}
	return types
}

func readAll(t *testing.T, reader interface{ Read([]byte) (int, error) }) string {
	t.Helper()
	var builder strings.Builder
	buffer := make([]byte, 1024)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			builder.Write(buffer[:count])
		}
		if err != nil {
			break
		}
	}
	return builder.String()
}
