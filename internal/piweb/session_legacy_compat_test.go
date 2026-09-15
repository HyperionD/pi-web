package piweb

import (
	"encoding/json"
	"testing"
)

func TestLegacySessionEventTranslatesDurableEvents(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"text": "hello"})
	typeCase := StoredSessionEvent{Seq: 1, SessionEventDraft: SessionEventDraft{
		SessionID: "s1", SourceID: "source", Type: EventUserMessageCreated, Payload: payload,
	}}
	legacyType, legacyPayload, ok := legacySessionEvent(typeCase)
	if !ok || legacyType != "session.message" {
		t.Fatalf("legacy = (%q, %#v, %v)", legacyType, legacyPayload, ok)
	}
	message, ok := legacyPayload.(Message)
	if !ok || message.Kind != "user" || message.Text != "hello" {
		t.Fatalf("message = %#v", legacyPayload)
	}
}

func TestLegacySessionEventSkipsMetadataOnlyEvents(t *testing.T) {
	legacyType, payload, ok := legacySessionEvent(StoredSessionEvent{Seq: 1, SessionEventDraft: SessionEventDraft{
		SessionID: "s1", SourceID: "source", Type: EventSessionModelChanged,
	}})
	if ok || legacyType != "" || payload != nil {
		t.Fatalf("legacy = (%q, %#v, %v)", legacyType, payload, ok)
	}
}
