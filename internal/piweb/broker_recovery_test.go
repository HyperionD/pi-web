package piweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBrokerRecoveryUsesLatestSessionEventIDAndAfter(t *testing.T) {
	broker := NewBroker()
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	second := broker.Publish("s1", "session.status", map[string]string{"status": "running"})

	if got := broker.LatestEventID("s1"); got != second.ID {
		t.Fatalf("latest session event ID = %d, want %d", got, second.ID)
	}

	subscription := broker.SubscribeFrom("s1", first.ID)
	defer subscription.Close()
	if len(subscription.Replay) != 1 || subscription.Replay[0].ID != second.ID {
		t.Fatalf("after replay = %#v, want event %d", subscription.Replay, second.ID)
	}

	third := broker.Publish("s1", "session.message", map[string]string{"text": "third"})
	select {
	case event := <-subscription.Events:
		if event.ID != third.ID {
			t.Fatalf("live recovery ID = %d, want %d", event.ID, third.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live recovery")
	}
}

func TestSubscribeFromReportsHistoryGapForSnapshotRecovery(t *testing.T) {
	broker := NewBroker()
	broker.historySize = 1
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	latest := broker.Publish("s1", "session.status", map[string]string{"status": "idle"})

	subscription := broker.SubscribeFrom("s1", first.ID)
	defer subscription.Close()
	if len(subscription.Replay) != 1 || subscription.Replay[0].Type != "broker_gap" {
		t.Fatalf("history gap replay = %#v, want broker_gap", subscription.Replay)
	}
	payload := subscription.Replay[0].Payload.(map[string]any)
	if payload["after"] != first.ID || payload["latestEventId"] != latest.ID {
		t.Fatalf("history gap payload = %#v", payload)
	}
}

func TestSessionSnapshotIncludesLatestEventID(t *testing.T) {
	broker := NewBroker()
	server := NewServer(Config{}, NewMockStore(), broker)
	first := broker.Publish("8e7c-44ff", "session.message", map[string]string{"text": "first"})
	latest := broker.Publish("8e7c-44ff", "session.status", map[string]string{"status": "idle"})

	request := httptest.NewRequest(http.MethodGet, "/api/sessions/8e7c-44ff", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		LatestEventID uint64 `json:"latestEventId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.LatestEventID != latest.ID || payload.LatestEventID == first.ID {
		t.Fatalf("snapshot latestEventId = %d, want %d", payload.LatestEventID, latest.ID)
	}
}

func TestServeSessionRecoversFromAfterQuery(t *testing.T) {
	broker := NewBroker()
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	second := broker.Publish("s1", "session.status", map[string]string{"status": "running"})
	writer := newTestSSEWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/events?after="+strconv.FormatUint(first.ID, 10), nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		broker.ServeSession(writer, request, "s1")
		close(done)
	}()
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not flush initial response")
	}
	third := broker.Publish("s1", "session.message", map[string]string{"text": "third"})
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not flush recovered events")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not stop after cancellation")
	}
	body := writer.String()
	secondIndex := strings.Index(body, "id: "+strconv.FormatUint(second.ID, 10)+"\n")
	thirdIndex := strings.Index(body, "id: "+strconv.FormatUint(third.ID, 10)+"\n")
	if secondIndex < 0 || thirdIndex < 0 || secondIndex > thirdIndex {
		t.Fatalf("after replay/live order is wrong: %q", body)
	}
}

func TestRuntimeEntryPoisonWaitsForSteerExecutionGate(t *testing.T) {
	runtime := newBlockingSteerRuntime()
	entry := &runtimeEntry{
		runtime:  runtime,
		manager:  &RuntimeManager{now: time.Now},
		events:   make(chan PiEvent, 1),
		stop:     make(chan struct{}),
		activity: map[string]int{},
	}
	steerDone := make(chan struct{})
	go func() {
		_, _ = entry.Send(context.Background(), RPCCommand{
			Type:    "prompt",
			Payload: []byte(`{"streamingBehavior":"steer"}`),
		})
		close(steerDone)
	}()
	select {
	case <-runtime.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("steer send did not start")
	}

	poisonDone := make(chan struct{})
	go func() {
		entry.poison(context.Canceled)
		close(poisonDone)
	}()
	select {
	case <-runtime.closeStarted:
		t.Fatal("poison closed runtime during in-flight steer")
	case <-time.After(20 * time.Millisecond):
	}
	close(runtime.releaseSend)
	<-steerDone
	select {
	case <-runtime.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("poison did not close runtime after steer completed")
	}
	select {
	case <-poisonDone:
	case <-time.After(time.Second):
		t.Fatal("poison did not complete")
	}
}

type blockingSteerRuntime struct {
	fakePiRuntime
	sendStarted  chan struct{}
	releaseSend  chan struct{}
	closeStarted chan struct{}
}

func newBlockingSteerRuntime() *blockingSteerRuntime {
	return &blockingSteerRuntime{
		fakePiRuntime: *newFakePiRuntime(),
		sendStarted:   make(chan struct{}),
		releaseSend:   make(chan struct{}),
		closeStarted:  make(chan struct{}),
	}
}

func (r *blockingSteerRuntime) Send(ctx context.Context, command RPCCommand) (RPCResponse, error) {
	if isSteerRPCCommand(command) {
		select {
		case <-r.sendStarted:
		default:
			close(r.sendStarted)
		}
		select {
		case <-r.releaseSend:
		case <-ctx.Done():
			return RPCResponse{}, ctx.Err()
		}
	}
	return RPCResponse{ID: command.ID, Type: "response", Command: command.Type, Success: true}, nil
}

func (r *blockingSteerRuntime) Close(ctx context.Context) error {
	select {
	case <-r.closeStarted:
	default:
		close(r.closeStarted)
	}
	return nil
}
