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
		covered, preserved, planErr := runtimeRebasePlan(conn, source.SessionID, stream, imported.Items)
		if planErr != nil {
			return planErr
		}
		if !covered {
			report = ImportReport{Deferred: true}
			return nil
		}
		if err := rebaseProjection(conn, source, imported, stream, preserved); err != nil {
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

// runtimeRebasePlan reports whether the strict JSONL import fully represents the
// current runtime projection. Items the JSONL cannot represent are returned so a
// rebase can re-append them instead of dropping them, and any runtime item the
// import misses defers the rebase so the projection stays intact.
func runtimeRebasePlan(conn *sql.Conn, sessionID string, stream SessionStreamState, imported []ProjectedSessionItem) (bool, []ProjectedSessionItem, error) {
	view, err := loadProjectionView(conn, sessionID, stream)
	if err != nil {
		return false, nil, err
	}
	index := newImportIndex(imported)
	preserved := []ProjectedSessionItem{}
	for _, itemID := range view.Order {
		item := view.Items[itemID]
		if item.Origin == "import" {
			continue
		}
		if item.Origin == "runtime" && jsonlRepresentableKind(item.Kind) {
			if !index.covers(item) {
				return false, nil, nil
			}
			continue
		}
		preserved = append(preserved, item)
	}
	return true, preserved, nil
}

func jsonlRepresentableKind(kind string) bool {
	switch kind {
	case "user", "pi", "think", "tool":
		return true
	default:
		return false
	}
}

type importedTool struct {
	toolCallID string
	tool       string
	args       string
	body       string
	status     string
}

type importIndex struct {
	texts map[string]int
	tools []importedTool
}

func newImportIndex(items []ProjectedSessionItem) importIndex {
	index := importIndex{texts: map[string]int{}}
	for _, item := range items {
		if item.Kind == "tool" {
			index.tools = append(index.tools, importedTool{
				toolCallID: item.ToolCallID, tool: item.Tool, args: RedactSecrets(item.Args), body: RedactSecrets(item.Body),
				status: canonicalManifestStatus(item.Status),
			})
			continue
		}
		index.texts[item.Kind+"\x00"+RedactSecrets(item.Text)]++
	}
	return index
}

func (index importIndex) covers(item ProjectedSessionItem) bool {
	if item.Kind == "tool" {
		return index.coversTool(item)
	}
	return index.texts[item.Kind+"\x00"+item.Text] > 0
}

// coversTool accepts the split JSONL shape where a tool call and its result are
// separate rows: any single imported row can supply the arguments or the body.
func (index importIndex) coversTool(item ProjectedSessionItem) bool {
	argsMatched := false
	bodyMatched := item.Body == ""
	status := canonicalManifestStatus(item.Status)
	for _, candidate := range index.tools {
		if candidate.tool != item.Tool {
			continue
		}
		if item.ToolCallID != "" && candidate.toolCallID != item.ToolCallID {
			continue
		}
		if !argsMatched && candidate.args == item.Args {
			argsMatched = true
		}
		if !bodyMatched && candidate.body == item.Body && candidate.status == status {
			bodyMatched = true
		}
		if argsMatched && bodyMatched {
			return true
		}
	}
	return argsMatched && bodyMatched
}

func rebaseProjection(conn *sql.Conn, source SessionSource, imported ImportedSession, stream SessionStreamState, preserved []ProjectedSessionItem) error {
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
	// The verified import is the source of truth: replace every row so runtime
	// history is never re-appended next to its imported copy.
	if _, err := conn.ExecContext(context.Background(), `DELETE FROM session_items WHERE session_id=?`, source.SessionID); err != nil {
		return err
	}
	ordinal := int64(0)
	for index := range imported.Items {
		item := imported.Items[index]
		ordinal++
		item.Ordinal = ordinal
		item.CreatedSeq, item.UpdatedSeq = seq, seq
		syncContentMetadata(&item, seq)
		if err := upsertProjectedItem(conn, source.SessionID, item); err != nil {
			return err
		}
	}
	for index := range preserved {
		item := preserved[index]
		ordinal++
		item.Ordinal = ordinal
		item.CreatedSeq, item.UpdatedSeq = seq, seq
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
