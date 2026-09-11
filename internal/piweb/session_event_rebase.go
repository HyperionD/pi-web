package piweb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (s *SQLiteSessionEventStore) OpenSession(ctx context.Context, source SessionSource) (report ImportReport, err error) {
	if source.SessionID == "" || source.Path == "" {
		return ImportReport{}, ErrInvalidSessionEvent
	}
	imported, err := StrictParsePiSession(source.Path)
	if err != nil {
		return ImportReport{}, err
	}
	err = s.withImmediateTx(ctx, func(conn *sql.Conn) error {
		stream, streamErr := loadStream(conn, source.SessionID)
		if errors.Is(streamErr, sql.ErrNoRows) {
			stream, streamErr = ensureStream(conn, source.SessionID)
		}
		if streamErr != nil {
			return streamErr
		}
		if isActiveStatus(stream.Status) {
			report = ImportReport{Deferred: true}
			return nil
		}
		if stream.ProjectionVersion > 0 {
			hash, fingerprintErr := loadSourceHash(conn, source.SessionID)
			if fingerprintErr != nil {
				return fingerprintErr
			}
			if hash == imported.Fingerprint.ContentHash {
				report = ImportReport{}
				return nil
			}
		}
		matches, matchErr := storedManifestsMatch(conn, source.SessionID, imported.Items)
		if matchErr != nil {
			return matchErr
		}
		if !matches {
			report = ImportReport{Deferred: true}
			return nil
		}
		if err := rebaseProjection(conn, source, imported, stream); err != nil {
			return err
		}
		report = ImportReport{Imported: true}
		return nil
	})
	if err != nil {
		return ImportReport{}, err
	}
	if report.Imported {
		s.notify(source.SessionID)
	}
	return report, nil
}

func loadSourceHash(conn *sql.Conn, sessionID string) (string, error) {
	var hash sql.NullString
	err := conn.QueryRowContext(context.Background(), `SELECT source_content_hash FROM session_streams WHERE session_id=?`, sessionID).Scan(&hash)
	return hash.String, err
}

func storedManifestsMatch(conn *sql.Conn, sessionID string, imported []ProjectedSessionItem) (bool, error) {
	rows, err := conn.QueryContext(context.Background(), `SELECT manifest_json FROM session_run_manifests WHERE session_id=?`, sessionID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return false, err
		}
		var manifest RunManifest
		if err := json.Unmarshal(payload, &manifest); err != nil {
			return false, err
		}
		if !ManifestMatches(manifest, imported) {
			return false, nil
		}
	}
	return true, rows.Err()
}

func rebaseProjection(conn *sql.Conn, source SessionSource, imported ImportedSession, stream SessionStreamState) error {
	if err := claimControlReceipt(conn, source.SessionID, "control:rebase:"+imported.Fingerprint.ContentHash); err != nil {
		return err
	}
	seq := stream.CurrentSeq + 1
	payload, err := json.Marshal(map[string]string{"reason": "jsonl-import"})
	if err != nil {
		return err
	}
	event := StoredSessionEvent{Seq: seq, SessionEventDraft: SessionEventDraft{
		SessionID: source.SessionID, SourceID: "control:rebase:" + imported.Fingerprint.ContentHash,
		Type: EventSessionRebased, Payload: payload, OccurredAt: time.Now().UTC(),
	}}
	if err := insertSessionEvent(conn, event); err != nil {
		return err
	}
	if _, err := conn.ExecContext(context.Background(), `DELETE FROM session_items WHERE session_id=? AND origin='import'`, source.SessionID); err != nil {
		return err
	}
	if _, err := conn.ExecContext(context.Background(), `DELETE FROM session_items WHERE session_id=? AND run_id IN (SELECT run_id FROM session_run_manifests WHERE session_id=?)`, source.SessionID, source.SessionID); err != nil {
		return err
	}
	var maxOrdinal int64
	if err := conn.QueryRowContext(context.Background(), `SELECT coalesce(max(ordinal), 0) FROM session_items WHERE session_id=?`, source.SessionID).Scan(&maxOrdinal); err != nil {
		return err
	}
	for index := range imported.Items {
		item := imported.Items[index]
		item.Ordinal = maxOrdinal + int64(index) + 1
		item.CreatedSeq, item.UpdatedSeq = seq, seq
		syncContentMetadata(&item, seq)
		if err := upsertProjectedItem(conn, source.SessionID, item); err != nil {
			return err
		}
	}
	nextGeneration := stream.ProjectionVersion + 1
	if _, err := conn.ExecContext(context.Background(), `UPDATE session_streams SET current_seq=?, projection_generation=?, source_path=?, source_size=?, source_mtime_ns=?, source_content_hash=?, updated_at=? WHERE session_id=?`, seq, nextGeneration, source.Path, imported.Fingerprint.Size, imported.Fingerprint.ModTimeNano, imported.Fingerprint.ContentHash, time.Now().UTC().Format(time.RFC3339Nano), source.SessionID); err != nil {
		return err
	}
	return nil
}

func claimControlReceipt(conn *sql.Conn, sessionID, sourceID string) error {
	_, err := conn.ExecContext(context.Background(), `INSERT OR IGNORE INTO session_control_receipts(session_id, source_id, event_type, payload_hash, committed_seq) VALUES (?, ?, ?, ?, 0)`, sessionID, sourceID, EventSessionRebased, sourceID)
	return err
}
