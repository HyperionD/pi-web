package piweb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type EventStoreFaultPoint string

const (
	FaultAfterEventInsert     EventStoreFaultPoint = "after-event-insert"
	FaultAfterProjectionWrite EventStoreFaultPoint = "after-projection-write"
	FaultAfterStreamUpdate    EventStoreFaultPoint = "after-stream-update"
	FaultBeforeCommit         EventStoreFaultPoint = "before-commit"
)

func (s *SQLiteSessionEventStore) injectFault(point EventStoreFaultPoint) error {
	if s.eventStoreFaultHook == nil {
		return nil
	}
	return s.eventStoreFaultHook(point)
}

func (s *SQLiteSessionEventStore) StartRun(ctx context.Context, command StartRunCommand) (receipt RunReceipt, err error) {
	if command.SessionID == "" || command.ClientRequestID == "" || command.RequestHash == "" || command.RunID == "" {
		return RunReceipt{}, ErrInvalidSessionEvent
	}
	err = s.withImmediateTx(ctx, func(conn *sql.Conn) error {
		if existing, found, findErr := findCommand(conn, command); findErr != nil {
			return findErr
		} else if found {
			receipt = existing
			return nil
		}
		stream, streamErr := ensureStream(conn, command.SessionID)
		if streamErr != nil {
			return streamErr
		}
		if stream.CurrentRunID != "" || isActiveStatus(stream.Status) {
			return errSessionBusy
		}
		userPayload, payloadErr := json.Marshal(map[string]any{"text": command.DisplayText, "attachments": command.Attachments})
		if payloadErr != nil {
			return payloadErr
		}
		user := StoredSessionEvent{Seq: streamSeq(stream), SessionEventDraft: SessionEventDraft{
			SessionID: command.SessionID, SourceID: "command:" + command.ClientRequestID + ":user",
			Type: EventUserMessageCreated, Payload: userPayload, OccurredAt: time.Now().UTC(),
		}}
		if err := insertSessionEvent(conn, user); err != nil {
			return err
		}
		view, err := loadProjectionView(conn, command.SessionID, stream)
		if err != nil {
			return err
		}
		userChange, err := ReduceSessionEvent(view, user)
		if err != nil {
			return err
		}
		if err := persistProjectionChange(conn, command.SessionID, userChange); err != nil {
			return err
		}
		view = applyStoredChange(view, userChange)
		started := StoredSessionEvent{Seq: user.Seq + 1, SessionEventDraft: SessionEventDraft{
			SessionID: command.SessionID, RunID: command.RunID,
			SourceID: "command:" + command.ClientRequestID + ":run", Type: EventRunStarted,
			OccurredAt: time.Now().UTC(),
		}}
		if err := insertSessionEvent(conn, started); err != nil {
			return err
		}
		startedChange, err := ReduceSessionEvent(view, started)
		if err != nil {
			return err
		}
		startedChange.Stream.CurrentRunID = command.RunID
		startedChange.Stream.CurrentSeq = started.Seq
		startedChange.Stream.SourceOrdinal = 0
		startedChange.Stream.SourceHash = ""
		startedChange.Stream.SourceSeq = 0
		if err := persistProjectionChange(conn, command.SessionID, startedChange); err != nil {
			return err
		}
		if err := updateStream(conn, startedChange.Stream); err != nil {
			return err
		}
		receipt = RunReceipt{RunID: command.RunID, AcceptedSeq: started.Seq}
		response, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		_, err = conn.ExecContext(ctx, `INSERT INTO session_commands(session_id, client_request_id, request_hash, run_id, accepted_seq, status, response_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, command.SessionID, command.ClientRequestID, command.RequestHash, command.RunID, started.Seq, SessionStatusRunning, response, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	})
	if err != nil {
		return RunReceipt{}, err
	}
	s.notify(command.SessionID)
	return receipt, nil
}

func (s *SQLiteSessionEventStore) Append(ctx context.Context, sessionID string, draft SessionEventDraft) (StoredSessionEvent, error) {
	var stored StoredSessionEvent
	err := s.withImmediateTx(ctx, func(conn *sql.Conn) error {
		stream, err := loadStream(conn, sessionID)
		if err != nil {
			return err
		}
		payload, hash, err := redactAndHashPayload(draft.Payload)
		if err != nil {
			return err
		}
		if draft.SourceOrdinal == nil || *draft.SourceOrdinal <= 0 {
			return ErrInvalidSourceOrdinal
		}
		if *draft.SourceOrdinal == stream.SourceOrdinal {
			if hash != stream.SourceHash {
				return ErrInvalidSourceOrdinal
			}
			stored, err = loadEvent(conn, sessionID, stream.SourceSeq)
			if err != nil {
				return err
			}
			if stored.RunID != draft.RunID || stored.SourceID != draft.SourceID ||
				stored.Type != draft.Type || stored.EntityID != draft.EntityID {
				return ErrInvalidSourceOrdinal
			}
			return nil
		}
		if *draft.SourceOrdinal != stream.SourceOrdinal+1 {
			return ErrInvalidSourceOrdinal
		}
		draft.SessionID = sessionID
		draft.Payload = payload
		stored = StoredSessionEvent{Seq: streamSeq(stream), SessionEventDraft: draft}
		view, err := loadProjectionView(conn, sessionID, stream)
		if err != nil {
			return err
		}
		change, err := ReduceSessionEvent(view, stored)
		if err != nil {
			return err
		}
		if err := insertSessionEvent(conn, stored); err != nil {
			return err
		}
		if err := s.injectFault(FaultAfterEventInsert); err != nil {
			return err
		}
		if err := persistProjectionChange(conn, sessionID, change); err != nil {
			return err
		}
		if err := s.injectFault(FaultAfterProjectionWrite); err != nil {
			return err
		}
		if err := s.enforceEventRetention(conn, sessionID); err != nil {
			return err
		}
		change.Stream.CurrentSeq = stored.Seq
		change.Stream.SourceOrdinal = *draft.SourceOrdinal
		change.Stream.SourceHash = hash
		change.Stream.SourceSeq = stored.Seq
		if err := updateStream(conn, change.Stream); err != nil {
			return err
		}
		return s.injectFault(FaultAfterStreamUpdate)
	})
	if err != nil {
		return StoredSessionEvent{}, err
	}
	s.notify(sessionID)
	return stored, nil
}

func (s *SQLiteSessionEventStore) AppendControl(ctx context.Context, sessionID, sourceID, eventType string, rawPayload []byte) (StoredSessionEvent, error) {
	if sessionID == "" || sourceID == "" || eventType == "" {
		return StoredSessionEvent{}, ErrInvalidSessionEvent
	}
	var stored StoredSessionEvent
	err := s.withImmediateTx(ctx, func(conn *sql.Conn) error {
		stream, err := ensureStream(conn, sessionID)
		if err != nil {
			return err
		}
		payload, hash, err := redactAndHashPayload(rawPayload)
		if err != nil {
			return err
		}
		var existingSeq int64
		var existingHash, existingType string
		err = conn.QueryRowContext(context.Background(), `SELECT seq, payload_hash, event_type FROM session_events WHERE session_id=? AND source_id=?`, sessionID, sourceID).Scan(&existingSeq, &existingHash, &existingType)
		if err == nil {
			if existingHash != hash || existingType != eventType {
				return ErrCommandHashConflict
			}
			stored, err = loadEvent(conn, sessionID, existingSeq)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		ordinal := stream.SourceOrdinal + 1
		draft := SessionEventDraft{
			SessionID: sessionID, SourceID: sourceID, SourceOrdinal: &ordinal,
			Type: eventType, Payload: payload, OccurredAt: time.Now().UTC(),
		}
		stored = StoredSessionEvent{Seq: streamSeq(stream), SessionEventDraft: draft}
		view, err := loadProjectionView(conn, sessionID, stream)
		if err != nil {
			return err
		}
		change, err := ReduceSessionEvent(view, stored)
		if err != nil {
			return err
		}
		if err := insertSessionEvent(conn, stored); err != nil {
			return err
		}
		if err := persistProjectionChange(conn, sessionID, change); err != nil {
			return err
		}
		if err := s.enforceEventRetention(conn, sessionID); err != nil {
			return err
		}
		change.Stream.CurrentSeq = stored.Seq
		change.Stream.SourceOrdinal = ordinal
		change.Stream.SourceHash = hash
		change.Stream.SourceSeq = stored.Seq
		if err := updateStream(conn, change.Stream); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return StoredSessionEvent{}, err
	}
	s.notify(sessionID)
	return stored, nil
}

func (s *SQLiteSessionEventStore) notify(sessionID string) {
	if s.hub != nil {
		s.hub.Notify(sessionID)
	}
}

func ensureStream(conn *sql.Conn, sessionID string) (SessionStreamState, error) {
	stream, err := loadStream(conn, sessionID)
	if err == nil {
		return stream, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SessionStreamState{}, err
	}
	_, err = conn.ExecContext(context.Background(), `INSERT INTO session_streams(session_id, status, updated_at) VALUES (?, ?, ?)`, sessionID, SessionStatusIdle, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return SessionStreamState{}, err
	}
	return loadStream(conn, sessionID)
}

func loadStream(conn *sql.Conn, sessionID string) (SessionStreamState, error) {
	var stream SessionStreamState
	err := conn.QueryRowContext(context.Background(), `SELECT session_id, current_seq, status, coalesce(current_run_id, ''), projection_schema_version, current_run_source_ordinal, coalesce(current_run_source_hash, ''), coalesce(current_run_source_seq, 0) FROM session_streams WHERE session_id=?`, sessionID).Scan(&stream.SessionID, &stream.CurrentSeq, &stream.Status, &stream.CurrentRunID, &stream.ProjectionVersion, &stream.SourceOrdinal, &stream.SourceHash, &stream.SourceSeq)
	return stream, err
}

func streamSeq(stream SessionStreamState) int64 {
	return stream.CurrentSeq + 1
}

func insertSessionEvent(conn *sql.Conn, event StoredSessionEvent) error {
	payload, hash, err := redactAndHashPayload(event.Payload)
	if err != nil {
		return err
	}
	ordinal := int64(0)
	if event.SourceOrdinal != nil {
		ordinal = *event.SourceOrdinal
	}
	occurred := event.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	var runID any
	if event.RunID != "" {
		runID = event.RunID
	}
	_, err = conn.ExecContext(context.Background(), `INSERT INTO session_events(session_id, seq, run_id, source_id, source_ordinal, event_type, entity_id, payload_hash, payload_json, occurred_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.SessionID, event.Seq, runID, event.SourceID, ordinal, event.Type, event.EntityID, hash, payload, occurred.Format(time.RFC3339Nano))
	return err
}

func loadEvent(conn *sql.Conn, sessionID string, seq int64) (StoredSessionEvent, error) {
	var event StoredSessionEvent
	var ordinal int64
	var runID, sourceID, eventType, entityID, payload string
	var occurred string
	err := conn.QueryRowContext(context.Background(), `SELECT seq, coalesce(run_id, ''), source_id, source_ordinal, event_type, coalesce(entity_id, ''), payload_json, occurred_at FROM session_events WHERE session_id=? AND seq=?`, sessionID, seq).Scan(&event.Seq, &runID, &sourceID, &ordinal, &eventType, &entityID, &payload, &occurred)
	if err != nil {
		return StoredSessionEvent{}, err
	}
	when, _ := time.Parse(time.RFC3339Nano, occurred)
	event.SessionEventDraft = SessionEventDraft{SessionID: sessionID, RunID: runID, SourceID: sourceID, Type: eventType, EntityID: entityID, Payload: json.RawMessage(payload), OccurredAt: when}
	if ordinal > 0 {
		event.SourceOrdinal = &ordinal
	}
	return event, nil
}

func redactAndHashPayload(payload json.RawMessage) (json.RawMessage, string, error) {
	var value any
	if len(payload) > 0 && string(payload) != "null" {
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, "", err
		}
		value = RedactPayload(value)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(data)
	return data, hex.EncodeToString(digest[:]), nil
}

func findCommand(conn *sql.Conn, command StartRunCommand) (RunReceipt, bool, error) {
	var hash, runID string
	var seq int64
	var response []byte
	err := conn.QueryRowContext(context.Background(), `SELECT request_hash, run_id, accepted_seq, response_json FROM session_commands WHERE session_id=? AND client_request_id=?`, command.SessionID, command.ClientRequestID).Scan(&hash, &runID, &seq, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return RunReceipt{}, false, nil
	}
	if err != nil {
		return RunReceipt{}, false, err
	}
	if hash != command.RequestHash {
		return RunReceipt{}, false, ErrCommandHashConflict
	}
	var receipt RunReceipt
	if err := json.Unmarshal(response, &receipt); err != nil {
		return RunReceipt{}, false, err
	}
	receipt.RunID, receipt.AcceptedSeq, receipt.Existing = runID, seq, true
	return receipt, true, nil
}

func isActiveStatus(status string) bool {
	return status == SessionStatusRunning || status == SessionStatusThinking || status == SessionStatusCompacting
}
