package piweb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	sseEventName    = "session.event"
	sseResetName    = "stream.reset"
	sseHeartbeat    = "heartbeat"
	sseHeartbeatGap = 15 * time.Second
	sseMaxBatchRows = 128
	sseMaxBatchSize = 1024 * 1024
)

type sessionEventEnvelope struct {
	Seq        string          `json:"seq"`
	SessionID  string          `json:"sessionId"`
	RunID      string          `json:"runId,omitempty"`
	SourceID   string          `json:"sourceId"`
	Type       string          `json:"type"`
	EntityID   string          `json:"entityId,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	OccurredAt time.Time       `json:"occurredAt"`
}

func (s *Server) sessionState(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if s.events == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("event store unavailable"))
		return
	}
	session, _, err := s.store.Session(sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if _, file, ok := s.store.SessionRuntime(sessionID); ok && file != "" {
		if _, err := s.events.OpenSession(r.Context(), SessionSource{SessionID: sessionID, Path: file}); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	snapshot, err := s.events.Snapshot(r.Context(), sessionID, SessionPageRequest{Limit: limit, Before: r.URL.Query().Get("before")})
	if err != nil {
		writeSessionEventError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":                 session,
		"items":                   snapshot.Items,
		"status":                  snapshot.Status,
		"runId":                   snapshot.RunID,
		"seq":                     strconv.FormatInt(snapshot.Seq, 10),
		"cursor":                  snapshot.Cursor,
		"hasMore":                 snapshot.HasMore,
		"limit":                   snapshot.Limit,
		"projectionSchemaVersion": snapshot.ProjectionSchemaVersion,
		"projectionGeneration":    strconv.FormatInt(snapshot.ProjectionGeneration, 10),
	})
}

func (s *Server) sessionEventsV2(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if s.events == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("event store unavailable"))
		return
	}
	if _, _, err := s.store.Session(sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	after, err := parseSessionEventCursorV2(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	var wake <-chan struct{}
	if s.hub != nil {
		subscription := s.hub.Subscribe(sessionID)
		defer subscription.Close()
		wake = subscription.Wake
	}
	ticker := time.NewTicker(sseHeartbeatGap)
	defer ticker.Stop()
	lastSent := after
	for {
		catchUp := false
		for {
			batch, err := s.events.EventsAfter(r.Context(), sessionID, lastSent, EventReadLimit{MaxRows: sseMaxBatchRows, MaxBytes: sseMaxBatchSize})
			if err != nil {
				return
			}
			if batch.ResetRequired {
				writeSSEReset(w, sessionID)
				flusher.Flush()
				return
			}
			if len(batch.Events) == 0 {
				break
			}
			for _, event := range batch.Events {
				if err := writeSessionEventSSE(w, event); err != nil {
					return
				}
				lastSent = event.Seq
				flusher.Flush()
				if event.Type == EventSessionRebased {
					catchUp = true
					break
				}
			}
			if catchUp {
				break
			}
		}
		if catchUp {
			return
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

func (s *Server) sessionItemContent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	itemID := r.PathValue("itemID")
	if s.events == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("event store unavailable"))
		return
	}
	if _, _, err := s.store.Session(sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	var version int64
	if value := r.URL.Query().Get("version"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid version"))
			return
		}
		version = parsed
	}
	start, end, err := parseByteRange(r.Header.Get("Range"))
	if err != nil {
		writeError(w, http.StatusRequestedRangeNotSatisfiable, err)
		return
	}
	data, resolvedVersion, err := s.events.ItemContent(r.Context(), sessionID, itemID, version, start, end)
	if err != nil {
		writeSessionEventError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Version", strconv.FormatInt(resolvedVersion, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func parseByteRange(header string) (int64, int64, error) {
	header = strings.TrimSpace(header)
	if header == "" || !strings.HasPrefix(header, "bytes=") {
		return 0, -1, nil
	}
	spec := strings.TrimPrefix(header, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, ErrInvalidContentRange
	}
	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || start < 0 {
		return 0, 0, ErrInvalidContentRange
	}
	if strings.TrimSpace(parts[1]) == "" {
		return start, -1, nil
	}
	end, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || end < start {
		return 0, 0, ErrInvalidContentRange
	}
	return start, end + 1, nil
}

func parseSessionEventCursorV2(r *http.Request) (int64, error) {
	value := r.URL.Query().Get("after")
	if value == "" {
		value = r.Header.Get("Last-Event-ID")
	}
	if value == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(value, 10, 64)
	if err != nil || cursor < 0 {
		return 0, errors.New("invalid event cursor")
	}
	return cursor, nil
}

func writeSessionEventSSE(w http.ResponseWriter, event StoredSessionEvent) error {
	envelope := sessionEventEnvelope{
		Seq: strconv.FormatInt(event.Seq, 10), SessionID: event.SessionID, RunID: event.RunID,
		SourceID: event.SourceID, Type: event.Type, EntityID: event.EntityID,
		Payload: event.Payload, OccurredAt: event.OccurredAt,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", sseEventName, event.Seq, data); err != nil {
		return err
	}
	return nil
}

func writeSSEReset(w http.ResponseWriter, sessionID string) {
	payload, _ := json.Marshal(map[string]any{"type": sseResetName, "sessionId": sessionID, "payload": map[string]string{"reason": "cursor_expired"}})
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sseResetName, payload)
}

func writeSSEComment(w http.ResponseWriter, name string) error {
	data, err := json.Marshal(map[string]string{"status": "ok"})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
	return err
}

func writeSessionEventError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrProjectionCursorExpired):
		writeError(w, http.StatusConflict, errors.New("cursor_expired"))
	case errors.Is(err, ErrInvalidProjectionCursor):
		writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, ErrContentVersionConflict):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, ErrInvalidContentRange):
		writeError(w, http.StatusRequestedRangeNotSatisfiable, err)
	case errors.Is(err, ErrNotFound), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, errors.New("not found"))
	default:
		writeError(w, http.StatusInternalServerError, errors.New("session event store failure"))
	}
}
