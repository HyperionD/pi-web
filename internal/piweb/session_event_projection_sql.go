package piweb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func loadProjectionView(conn *sql.Conn, sessionID string, stream SessionStreamState) (ProjectionView, error) {
	view := ProjectionView{Stream: stream, Items: map[string]ProjectedSessionItem{}}
	rows, err := conn.QueryContext(context.Background(), `SELECT item_id, ordinal, payload_json FROM session_items WHERE session_id=? ORDER BY ordinal ASC`, sessionID)
	if err != nil {
		return ProjectionView{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID string
		var ordinal int64
		var payload []byte
		if err := rows.Scan(&itemID, &ordinal, &payload); err != nil {
			return ProjectionView{}, err
		}
		var item ProjectedSessionItem
		if err := json.Unmarshal(payload, &item); err != nil {
			return ProjectionView{}, fmt.Errorf("decode projected item %s: %w", itemID, err)
		}
		item.ItemID, item.Ordinal = itemID, ordinal
		view.Items[itemID] = item
		view.Order = append(view.Order, itemID)
	}
	return view, rows.Err()
}

func applyStoredChange(view ProjectionView, change ProjectionChange) ProjectionView {
	view.Stream = change.Stream
	if view.Items == nil {
		view.Items = map[string]ProjectedSessionItem{}
	}
	for _, item := range change.Terminalize {
		view.Items[item.ItemID] = item
	}
	if change.Upsert != nil {
		view.Items[change.Upsert.ItemID] = *change.Upsert
		if !containsProjectionItem(view.Order, change.Upsert.ItemID) {
			view.Order = append(view.Order, change.Upsert.ItemID)
		}
	}
	for _, itemID := range change.DeleteItemIDs {
		delete(view.Items, itemID)
	}
	return view
}

func persistProjectionChange(conn *sql.Conn, sessionID string, change ProjectionChange) error {
	if change.Upsert != nil {
		if err := upsertProjectedItem(conn, sessionID, *change.Upsert); err != nil {
			return err
		}
	}
	for _, item := range change.Terminalize {
		if err := upsertProjectedItem(conn, sessionID, item); err != nil {
			return err
		}
	}
	for _, itemID := range change.DeleteItemIDs {
		if _, err := conn.ExecContext(context.Background(), `DELETE FROM session_items WHERE session_id=? AND item_id=?`, sessionID, itemID); err != nil {
			return err
		}
	}
	return nil
}

func upsertProjectedItem(conn *sql.Conn, sessionID string, item ProjectedSessionItem) error {
	previous, found, err := loadStoredItem(conn, sessionID, item.ItemID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}
	var runID, entityID any
	if item.RunID != "" {
		runID = item.RunID
	}
	if item.EntityID != "" {
		entityID = item.EntityID
	}
	_, err = conn.ExecContext(context.Background(), `INSERT INTO session_items(session_id, item_id, ordinal, kind, origin, run_id, entity_id, payload_json, created_seq, updated_seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(session_id, item_id) DO UPDATE SET ordinal=excluded.ordinal, kind=excluded.kind, origin=excluded.origin, run_id=excluded.run_id, entity_id=excluded.entity_id, payload_json=excluded.payload_json, updated_seq=excluded.updated_seq`, sessionID, item.ItemID, item.Ordinal, item.Kind, item.Origin, runID, entityID, payload, item.CreatedSeq, item.UpdatedSeq)
	if err != nil {
		return err
	}
	if !found {
		previous = ProjectedSessionItem{}
	}
	return syncItemContent(conn, sessionID, previous, item)
}

func loadStoredItem(conn *sql.Conn, sessionID, itemID string) (ProjectedSessionItem, bool, error) {
	var payload string
	err := conn.QueryRowContext(context.Background(), `SELECT payload_json FROM session_items WHERE session_id=? AND item_id=?`, sessionID, itemID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectedSessionItem{}, false, nil
	}
	if err != nil {
		return ProjectedSessionItem{}, false, err
	}
	var item ProjectedSessionItem
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		return ProjectedSessionItem{}, false, err
	}
	return item, true, nil
}

func syncItemContent(conn *sql.Conn, sessionID string, previous, item ProjectedSessionItem) error {
	if item.ContentVersion <= 0 || item.Body == "" {
		return nil
	}
	start := 0
	if previous.ItemID == item.ItemID && previous.Body != "" && strings.HasPrefix(item.Body, previous.Body) {
		start = len(previous.Body)
	}
	for offset := start; offset < len(item.Body); {
		end := offset + contentChunkBytes
		if end > len(item.Body) {
			end = len(item.Body)
		}
		index := offset / contentChunkBytes
		if _, err := conn.ExecContext(context.Background(), `INSERT OR REPLACE INTO session_item_chunks(session_id, item_id, content_version, chunk_index, content) VALUES (?, ?, ?, ?, ?)`, sessionID, item.ItemID, item.ContentVersion, index, []byte(item.Body[offset:end])); err != nil {
			return err
		}
		offset = end
	}
	return nil
}

func updateStream(conn *sql.Conn, stream SessionStreamState) error {
	_, err := conn.ExecContext(context.Background(), `UPDATE session_streams SET current_seq=?, status=?, current_run_id=?, projection_schema_version=?, current_run_source_ordinal=?, current_run_source_hash=?, current_run_source_seq=?, updated_at=? WHERE session_id=?`, stream.CurrentSeq, stream.Status, nullableString(stream.CurrentRunID), stream.ProjectionVersion, stream.SourceOrdinal, nullableString(stream.SourceHash), nullableInt64(stream.SourceSeq), time.Now().UTC().Format(time.RFC3339Nano), stream.SessionID)
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

const contentChunkBytes = 64 * 1024

func containsProjectionItem(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
