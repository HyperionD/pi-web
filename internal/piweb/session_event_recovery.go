package piweb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (s *SQLiteSessionEventStore) RecoverInterrupted(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT session_id FROM session_streams WHERE current_run_id IS NOT NULL AND current_run_id <> ''`)
	if err != nil {
		return err
	}
	var sessionIDs []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			rows.Close()
			return err
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, sessionID := range sessionIDs {
		if err := s.recoverSession(ctx, sessionID); err != nil {
			return err
		}
		s.notify(sessionID)
	}
	return nil
}

func (s *SQLiteSessionEventStore) recoverSession(ctx context.Context, sessionID string) error {
	return s.withImmediateTx(ctx, func(conn *sql.Conn) error {
		stream, err := loadStream(conn, sessionID)
		if err != nil {
			return err
		}
		if stream.CurrentRunID == "" {
			return nil
		}
		ordinal := stream.SourceOrdinal + 1
		eventType := EventRunInterrupted
		var cancelRequestSeq int64
		if err := conn.QueryRowContext(context.Background(), `SELECT coalesce(max(seq), 0) FROM session_events WHERE session_id=? AND event_type=? AND json_extract(payload_json, '$.runId')=?`, sessionID, EventRunCancelRequested, stream.CurrentRunID).Scan(&cancelRequestSeq); err != nil {
			return err
		}
		if cancelRequestSeq > 0 {
			eventType = EventRunCancelled
		}
		reason := "restart"
		if eventType == EventRunCancelled {
			reason = "restart-after-cancel"
		}
		payload, err := json.Marshal(map[string]string{"reason": reason})
		if err != nil {
			return err
		}
		event := StoredSessionEvent{Seq: stream.CurrentSeq + 1, SessionEventDraft: SessionEventDraft{
			SessionID: sessionID, RunID: stream.CurrentRunID,
			SourceID:      fmt.Sprintf("%s:recovery:%d", stream.CurrentRunID, ordinal),
			SourceOrdinal: &ordinal, Type: eventType, Payload: payload, OccurredAt: time.Now().UTC(),
		}}
		view, err := loadProjectionView(conn, sessionID, stream)
		if err != nil {
			return err
		}
		change, err := ReduceSessionEvent(view, event)
		if err != nil {
			return err
		}
		if err := insertSessionEvent(conn, event); err != nil {
			return err
		}
		if err := persistProjectionChange(conn, sessionID, change); err != nil {
			return err
		}
		change.Stream.CurrentSeq = event.Seq
		change.Stream.SourceOrdinal = ordinal
		_, hash, err := redactAndHashPayload(payload)
		if err != nil {
			return err
		}
		change.Stream.SourceHash = hash
		change.Stream.SourceSeq = event.Seq
		change.Stream.CurrentRunID = ""
		change.Stream.Status, _ = terminalStatuses(eventType)
		return updateStream(conn, change.Stream)
	})
}

var errEventStorePathMissing = errors.New("session event store path is missing")

func OpenServer(config Config, store *Store, broker *Broker) (*Server, error) {
	if store == nil || store.dbPath == "" {
		return nil, errEventStorePathMissing
	}
	hub := NewSessionWakeHub()
	events, err := OpenSQLiteSessionEventStore(store.dbPath, hub)
	if err != nil {
		return nil, err
	}
	if err := events.RecoverInterrupted(context.Background()); err != nil {
		_ = events.Close(context.Background())
		return nil, err
	}
	server := NewServer(config, store, broker)
	server.attachEventStore(store, events, hub)
	return server, nil
}
