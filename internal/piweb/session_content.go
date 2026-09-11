package piweb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

func (s *SQLiteSessionEventStore) ItemContent(ctx context.Context, sessionID, itemID string, version, start, end int64) ([]byte, int64, error) {
	if sessionID == "" || itemID == "" || start < 0 || (end >= 0 && end < start) {
		return nil, 0, ErrInvalidContentRange
	}
	item, found, err := loadContentItem(ctx, s.db, sessionID, itemID)
	if err != nil {
		return nil, 0, err
	}
	if !found {
		return nil, 0, ErrNotFound
	}
	if version != 0 && version != item.ContentVersion {
		return nil, item.ContentVersion, ErrContentVersionConflict
	}
	total := int64(len(item.Body))
	if end < 0 || end > total {
		end = total
	}
	return []byte(item.Body[start:end]), item.ContentVersion, nil
}

func loadContentItem(ctx context.Context, db *sql.DB, sessionID, itemID string) (ProjectedSessionItem, bool, error) {
	var payload string
	err := db.QueryRowContext(ctx, `SELECT payload_json FROM session_items WHERE session_id=? AND item_id=?`, sessionID, itemID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectedSessionItem{}, false, nil
	}
	if err != nil {
		return ProjectedSessionItem{}, false, err
	}
	item, err := decodeProjectedItem(itemID, payload)
	if err != nil {
		return ProjectedSessionItem{}, false, err
	}
	return item, true, nil
}

func decodeProjectedItem(itemID, payload string) (ProjectedSessionItem, error) {
	var item ProjectedSessionItem
	if err := json.Unmarshal([]byte(payload), &item); err != nil {
		return ProjectedSessionItem{}, fmt.Errorf("decode projected item %s: %w", itemID, err)
	}
	item.ItemID = itemID
	return item, nil
}

const (
	retentionActiveRows  = 4096
	retentionActiveBytes = 64 * 1024 * 1024
	retentionFinalRows   = 256
	retentionFinalBytes  = 8 * 1024 * 1024
)

func (s *SQLiteSessionEventStore) activeRetentionLimits() (int, int) {
	rows, bytes := s.retention.ActiveRows, s.retention.ActiveBytes
	if rows <= 0 {
		rows = retentionActiveRows
	}
	if bytes <= 0 {
		bytes = retentionActiveBytes
	}
	return rows, bytes
}

func (s *SQLiteSessionEventStore) finalRetentionLimits() (int, int) {
	rows, bytes := s.retention.FinalRows, s.retention.FinalBytes
	if rows <= 0 {
		rows = retentionFinalRows
	}
	if bytes <= 0 {
		bytes = retentionFinalBytes
	}
	return rows, bytes
}

func (s *SQLiteSessionEventStore) enforceEventRetention(conn *sql.Conn, sessionID string) error {
	var status string
	if err := conn.QueryRowContext(context.Background(), `SELECT status FROM session_streams WHERE session_id=?`, sessionID).Scan(&status); err != nil {
		return err
	}
	maxRows, maxBytes := s.finalRetentionLimits()
	if isActiveStatus(status) {
		maxRows, maxBytes = s.activeRetentionLimits()
	}
	rows, err := conn.QueryContext(context.Background(), `SELECT seq, length(payload_json) FROM session_events WHERE session_id=? ORDER BY seq ASC`, sessionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type eventSize struct {
		seq  int64
		size int
	}
	var events []eventSize
	totalBytes := 0
	for rows.Next() {
		var event eventSize
		if err := rows.Scan(&event.seq, &event.size); err != nil {
			return err
		}
		events = append(events, event)
		totalBytes += event.size
	}
	if err := rows.Err(); err != nil {
		return err
	}
	keepFrom := 0
	for keepFrom < len(events) {
		overRows := len(events)-keepFrom > maxRows
		overBytes := totalBytes > maxBytes
		if !overRows && !overBytes {
			break
		}
		totalBytes -= events[keepFrom].size
		keepFrom++
	}
	if keepFrom == 0 {
		return nil
	}
	cutoff := events[keepFrom-1].seq + 1
	if _, err := conn.ExecContext(context.Background(), `DELETE FROM session_events WHERE session_id=? AND seq<?`, sessionID, cutoff); err != nil {
		return err
	}
	_, err = conn.ExecContext(context.Background(), `UPDATE session_streams SET min_retained_seq=? WHERE session_id=?`, cutoff, sessionID)
	return err
}

func (s *SQLiteSessionEventStore) SetRetentionLimits(limits RetentionLimits) {
	s.retention = limits
}

func (s *SQLiteSessionEventStore) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_streams WHERE session_id=?`, sessionID)
	return err
}
