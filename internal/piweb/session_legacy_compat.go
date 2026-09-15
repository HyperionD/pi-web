package piweb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) serveLegacySessionEventsFromDurable(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	after, err := parseLegacyEventCursor(r.URL.Query().Get("after"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if r.URL.Query().Get("replay") == "false" {
		snapshot, snapshotErr := s.events.Snapshot(r.Context(), sessionID, SessionPageRequest{})
		if snapshotErr != nil {
			writeSessionEventError(w, snapshotErr)
			return
		}
		after = snapshot.Seq
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Deprecation", "true")
	flusher.Flush()

	var wake <-chan struct{}
	if s.hub != nil {
		subscription := s.hub.Subscribe(sessionID)
		defer subscription.Close()
		wake = subscription.Wake
	}
	ticker := time.NewTicker(sseHeartbeatGap)
	defer ticker.Stop()
	for {
		batch, readErr := s.events.EventsAfter(r.Context(), sessionID, after, EventReadLimit{MaxRows: sseMaxBatchRows, MaxBytes: sseMaxBatchSize})
		if readErr != nil {
			return
		}
		if batch.ResetRequired {
			writeSSEReset(w, sessionID)
			flusher.Flush()
			return
		}
		for _, event := range batch.Events {
			after = event.Seq
			if err := writeLegacySessionEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if err := writeSSEComment(w, sseHeartbeat); err != nil {
				return
			}
			flusher.Flush()
		case <-wake:
		}
	}
}

func parseLegacyEventCursor(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(value, 10, 64)
	if err != nil || cursor < 0 {
		return 0, fmt.Errorf("invalid event cursor")
	}
	return cursor, nil
}

func writeLegacySessionEvent(w http.ResponseWriter, event StoredSessionEvent) error {
	legacyType, payload, ok := legacySessionEvent(event)
	if !ok {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", legacyType, event.Seq, data)
	return err
}

func legacySessionEvent(event StoredSessionEvent) (string, any, bool) {
	var payload map[string]any
	if len(event.Payload) > 0 && json.Unmarshal(event.Payload, &payload) != nil {
		return "", nil, false
	}
	switch event.Type {
	case EventSessionRenamed:
		return "session.renamed", payload, true
	case EventUserMessageCreated:
		return "session.message", legacyMessage("user", payload), true
	case EventAssistantSegmentDelta:
		return "session.delta", payload, true
	case EventAssistantSegmentCommit:
		return "session.message", legacyMessage(payloadString(payload, "kind"), payload), true
	case EventToolStarted:
		return "tool.started", payload, true
	case EventToolArgsDelta:
		return "tool.output", payload, true
	case EventToolOutput:
		return "tool.output", payload, true
	case EventToolFinished:
		return "tool.finished", payload, true
	case EventRunStarted, EventCompactionStarted:
		return "session.status", map[string]string{"status": SessionStatusRunning}, true
	case EventRunFinished, EventCompactionCommitted:
		return "session.status", map[string]string{"status": SessionStatusIdle}, true
	case EventRunCancelled:
		return "session.status", map[string]string{"status": SessionStatusCancelled}, true
	case EventRunInterrupted:
		return "session.status", map[string]string{"status": SessionStatusInterrupted}, true
	case EventRunFailed, EventCompactionFailed:
		return "error", payload, true
	default:
		return "", nil, false
	}
}

func legacyMessage(kind string, payload map[string]any) Message {
	return Message{Kind: kind, Text: payloadString(payload, "text"), Attachments: promptAttachments(payload["attachments"])}
}

func promptAttachments(value any) []PromptAttachment {
	data, _ := json.Marshal(value)
	var attachments []PromptAttachment
	_ = json.Unmarshal(data, &attachments)
	return attachments
}

func payloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}
