package piweb

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

const (
	defaultProjectionPageLimit = 120
	maxProjectionPageLimit     = 500
	defaultEventRows           = 128
	defaultEventBytes          = 1024 * 1024
)

type ProjectionCursor struct {
	Generation    int64 `json:"generation"`
	BeforeOrdinal int64 `json:"beforeOrdinal"`
}

func EncodeProjectionCursor(cursor ProjectionCursor) (string, error) {
	if cursor.Generation <= 0 || cursor.BeforeOrdinal <= 0 {
		return "", ErrInvalidProjectionCursor
	}
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func DecodeProjectionCursor(value string) (ProjectionCursor, error) {
	if value == "" {
		return ProjectionCursor{}, ErrInvalidProjectionCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return ProjectionCursor{}, ErrInvalidProjectionCursor
	}
	var cursor ProjectionCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.Generation <= 0 || cursor.BeforeOrdinal <= 0 {
		return ProjectionCursor{}, ErrInvalidProjectionCursor
	}
	return cursor, nil
}

func normalizeProjectionPageLimit(limit int) int {
	if limit <= 0 {
		return defaultProjectionPageLimit
	}
	if limit > maxProjectionPageLimit {
		return maxProjectionPageLimit
	}
	return limit
}

func normalizeEventReadLimit(limit EventReadLimit) EventReadLimit {
	if limit.MaxRows <= 0 {
		limit.MaxRows = defaultEventRows
	}
	if limit.MaxRows > defaultEventRows {
		limit.MaxRows = defaultEventRows
	}
	if limit.MaxBytes <= 0 {
		limit.MaxBytes = defaultEventBytes
	}
	if limit.MaxBytes > defaultEventBytes {
		limit.MaxBytes = defaultEventBytes
	}
	return limit
}

func (s *SQLiteSessionEventStore) Snapshot(ctx context.Context, sessionID string, request SessionPageRequest) (SessionStateSnapshot, error) {
	limit := normalizeProjectionPageLimit(request.Limit)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionStateSnapshot{}, err
	}
	defer tx.Rollback()
	stream, err := loadStreamTx(tx, sessionID)
	if err != nil {
		return SessionStateSnapshot{}, err
	}
	before, err := snapshotBefore(request.Before, stream.ProjectionVersion)
	if err != nil {
		return SessionStateSnapshot{}, err
	}
	metadata := SessionMetadataProjection{SessionID: sessionID}
	var metadataPayload []byte
	metadataErr := tx.QueryRowContext(ctx, `SELECT payload_json FROM session_metadata WHERE session_id=?`, sessionID).Scan(&metadataPayload)
	if metadataErr != nil && metadataErr != sql.ErrNoRows {
		return SessionStateSnapshot{}, metadataErr
	}
	if len(metadataPayload) > 0 {
		if err := json.Unmarshal(metadataPayload, &metadata); err != nil {
			return SessionStateSnapshot{}, fmt.Errorf("decode session metadata %s: %w", sessionID, err)
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT item_id, ordinal, payload_json FROM session_items WHERE session_id=? AND ordinal<? ORDER BY ordinal DESC LIMIT ?`, sessionID, before, limit)
	if err != nil {
		return SessionStateSnapshot{}, err
	}
	items, err := scanProjectionItems(rows)
	if err != nil {
		return SessionStateSnapshot{}, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Ordinal < items[j].Ordinal })
	hasMore := false
	cursor := ""
	if len(items) > 0 {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_items WHERE session_id=? AND ordinal<?`, sessionID, items[0].Ordinal).Scan(&count); err != nil {
			return SessionStateSnapshot{}, err
		}
		hasMore = count > 0
		if hasMore {
			cursor, err = EncodeProjectionCursor(ProjectionCursor{Generation: stream.ProjectionVersion, BeforeOrdinal: items[0].Ordinal})
			if err != nil {
				return SessionStateSnapshot{}, err
			}
		}
	}
	return SessionStateSnapshot{Metadata: metadata, Items: items, Status: stream.Status, RunID: stream.CurrentRunID, Seq: stream.CurrentSeq, Cursor: cursor, HasMore: hasMore, Limit: limit, ProjectionSchemaVersion: 1, ProjectionGeneration: stream.ProjectionVersion}, nil
}

func snapshotBefore(value string, generation int64) (int64, error) {
	if value == "" {
		return math.MaxInt64, nil
	}
	cursor, err := DecodeProjectionCursor(value)
	if err != nil {
		return 0, err
	}
	if cursor.Generation != generation {
		return 0, ErrProjectionCursorExpired
	}
	return cursor.BeforeOrdinal, nil
}

func loadStreamTx(tx *sql.Tx, sessionID string) (SessionStreamState, error) {
	var stream SessionStreamState
	var schemaVersion int64
	err := tx.QueryRow(`SELECT session_id, current_seq, status, coalesce(current_run_id, ''), projection_schema_version, projection_generation, current_run_source_ordinal, coalesce(current_run_source_hash, ''), coalesce(current_run_source_seq, 0) FROM session_streams WHERE session_id=?`, sessionID).Scan(&stream.SessionID, &stream.CurrentSeq, &stream.Status, &stream.CurrentRunID, &schemaVersion, &stream.ProjectionVersion, &stream.SourceOrdinal, &stream.SourceHash, &stream.SourceSeq)
	return stream, err
}

func scanProjectionItems(rows *sql.Rows) ([]ProjectedSessionItem, error) {
	defer rows.Close()
	items := []ProjectedSessionItem{}
	for rows.Next() {
		var item ProjectedSessionItem
		var itemID, payload string
		if err := rows.Scan(&itemID, &item.Ordinal, &payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return nil, fmt.Errorf("decode projected item %s: %w", itemID, err)
		}
		item.ItemID = itemID
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLiteSessionEventStore) EventsAfter(ctx context.Context, sessionID string, after int64, requested EventReadLimit) (EventBatch, error) {
	limit := normalizeEventReadLimit(requested)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EventBatch{}, err
	}
	defer tx.Rollback()
	stream, err := loadStreamTx(tx, sessionID)
	if err != nil {
		return EventBatch{}, err
	}
	var minRetained int64
	if err := tx.QueryRowContext(ctx, `SELECT min_retained_seq FROM session_streams WHERE session_id=?`, sessionID).Scan(&minRetained); err != nil {
		return EventBatch{}, err
	}
	if after < minRetained-1 {
		return EventBatch{ResetRequired: true}, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT seq, coalesce(run_id, ''), source_id, source_ordinal, event_type, coalesce(entity_id, ''), payload_json, occurred_at FROM session_events WHERE session_id=? AND seq>? ORDER BY seq ASC LIMIT ?`, sessionID, after, limit.MaxRows+1)
	if err != nil {
		return EventBatch{}, err
	}
	defer rows.Close()
	batch := EventBatch{Events: []StoredSessionEvent{}}
	var bytes int
	for rows.Next() {
		var event StoredSessionEvent
		var runID, sourceID, eventType, entityID, payload, occurred string
		var ordinal int64
		if err := rows.Scan(&event.Seq, &runID, &sourceID, &ordinal, &eventType, &entityID, &payload, &occurred); err != nil {
			return EventBatch{}, err
		}
		if len(batch.Events) >= limit.MaxRows || (len(batch.Events) > 0 && bytes+len(payload) > limit.MaxBytes) {
			break
		}
		when, _ := parseEventTime(occurred)
		event.SessionEventDraft = SessionEventDraft{SessionID: sessionID, RunID: runID, SourceID: sourceID, Type: eventType, EntityID: entityID, Payload: json.RawMessage(payload), OccurredAt: when}
		if ordinal > 0 {
			event.SourceOrdinal = &ordinal
		}
		batch.Events = append(batch.Events, event)
		bytes += len(payload)
	}
	if err := rows.Err(); err != nil {
		return EventBatch{}, err
	}
	if len(batch.Events) > 0 && batch.Events[0].Seq != after+1 && after < stream.CurrentSeq {
		return EventBatch{ResetRequired: true}, nil
	}
	return batch, nil
}

func loadStreamDB(ctx context.Context, db *sql.DB, sessionID string) (SessionStreamState, error) {
	var stream SessionStreamState
	var schemaVersion int64
	err := db.QueryRowContext(ctx, `SELECT session_id, current_seq, status, coalesce(current_run_id, ''), projection_schema_version, projection_generation, current_run_source_ordinal, coalesce(current_run_source_hash, ''), coalesce(current_run_source_seq, 0) FROM session_streams WHERE session_id=?`, sessionID).Scan(&stream.SessionID, &stream.CurrentSeq, &stream.Status, &stream.CurrentRunID, &schemaVersion, &stream.ProjectionVersion, &stream.SourceOrdinal, &stream.SourceHash, &stream.SourceSeq)
	return stream, err
}

func streamMinRetained(ctx context.Context, db *sql.DB, sessionID string) int64 {
	var value int64
	_ = db.QueryRowContext(ctx, `SELECT min_retained_seq FROM session_streams WHERE session_id=?`, sessionID).Scan(&value)
	return value
}

func parseEventTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
