package piweb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
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
		legacyRepair := false
		if stream.ProjectionVersion > 0 {
			hash, fingerprintErr := loadSourceHash(conn, source.SessionID)
			if fingerprintErr != nil {
				return fingerprintErr
			}
			if hash == imported.Fingerprint.ContentHash {
				// Earlier imports omitted assistant failures even when the source hash matched.
				legacyRepair, err = missingImportedErrors(conn, source.SessionID, imported.Items)
				if err != nil {
					return err
				}
				if !legacyRepair {
					legacyRepair, err = needsRuntimeErrorRepair(conn, source.SessionID)
					if err != nil {
						return err
					}
					if !legacyRepair {
						report = ImportReport{}
						return nil
					}
				}
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
		covered, preserved, planErr := runtimeRebasePlan(conn, source.SessionID, stream, imported.Items, legacyRepair)
		if planErr != nil {
			return planErr
		}
		if !covered {
			report = ImportReport{Deferred: true}
			return nil
		}
		if err := rebaseProjection(conn, source, imported, stream, preserved, legacyRepair); err != nil {
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

func missingImportedErrors(conn *sql.Conn, sessionID string, imported []ProjectedSessionItem) (bool, error) {
	for _, item := range imported {
		if item.Kind != "error" {
			continue
		}
		var found int
		err := conn.QueryRowContext(context.Background(),
			`SELECT 1 FROM session_items WHERE session_id=? AND item_id=? AND kind='error'`, sessionID, item.ItemID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
	return false, nil
}

func needsRuntimeErrorRepair(conn *sql.Conn, sessionID string) (bool, error) {
	var hasError, marked int
	err := conn.QueryRowContext(context.Background(), `SELECT EXISTS(
		SELECT 1 FROM session_items WHERE session_id=? AND origin='runtime' AND kind='error')`, sessionID).Scan(&hasError)
	if err != nil || hasError == 0 {
		return false, err
	}
	err = conn.QueryRowContext(context.Background(), `SELECT EXISTS(
		SELECT 1 FROM session_control_receipts WHERE session_id=? AND source_id='control:error-order:v1')`, sessionID).Scan(&marked)
	return marked == 0, err
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
type preservedRebaseItem struct {
	item   ProjectedSessionItem
	before int
}

type runImportInterval struct {
	afterUser int
	before    int
}

func runtimeRebasePlan(
	conn *sql.Conn, sessionID string, stream SessionStreamState, imported []ProjectedSessionItem, legacyRepair bool,
) (bool, []preservedRebaseItem, error) {
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
	anchors := map[string]runImportInterval(nil)
	if legacyRepair && len(preserved) > 0 {
		anchors, err = importedRunAnchors(conn, sessionID, imported)
		if err != nil {
			return false, nil, err
		}
	}
	return true, positionPreservedItems(view, imported, preserved, legacyRepair, anchors), nil
}

// Place runtime-only entries before the next imported counterpart, not after
// the entire JSONL history. Unmatched trailing entries remain at the end.
func positionPreservedItems(
	view ProjectionView, imported, preserved []ProjectedSessionItem, legacyRepair bool,
	anchors map[string]runImportInterval,
) []preservedRebaseItem {
	if len(preserved) == 0 {
		return nil
	}
	matches := matchImportedAnchors(view, imported)
	nextMatches := futureImportPositions(view.Order, matches, len(imported))
	pending := make(map[string]bool, len(preserved))
	for _, item := range preserved {
		pending[item.ItemID] = true
	}
	usedErrors := make(map[int]bool)
	result := make([]preservedRebaseItem, 0, len(preserved))
	previous := -1
	for _, id := range view.Order {
		if match, ok := matches[id]; ok {
			previous = match
		}
		if !pending[id] {
			continue
		}
		item := view.Items[id]
		before := nextMatches[id]
		if item.Origin == "runtime" && item.Kind == "error" {
			// Match within this run: identical failures in adjacent runs are distinct.
			if interval, ok := anchors[item.RunID]; ok {
				if consumeImportedError(imported, usedErrors, interval.afterUser, interval.before, item.Text) {
					continue
				}
				before = interval.before
			} else if consumeImportedError(imported, usedErrors, previous+1, before, item.Text) ||
				(legacyRepair && consumeImportedError(imported, usedErrors, 0, len(imported), item.Text)) {
				continue
			}
		}
		result = append(result, preservedRebaseItem{item: item, before: before})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].before < result[j].before })
	return result
}

func futureImportPositions(order []string, matches map[string]int, total int) map[string]int {
	positions := make(map[string]int, len(order))
	before := total
	for index := len(order) - 1; index >= 0; index-- {
		id := order[index]
		if match, ok := matches[id]; ok {
			before = match
		}
		positions[id] = before
	}
	return positions
}

func importedRunAnchors(
	conn *sql.Conn, sessionID string, imported []ProjectedSessionItem,
) (map[string]runImportInterval, error) {
	rows, err := conn.QueryContext(context.Background(), `SELECT r.run_id, u.payload_json FROM session_events r
		JOIN session_events u ON u.session_id=r.session_id AND u.seq=r.seq-1
		WHERE r.session_id=? AND r.event_type=? AND u.event_type=? ORDER BY r.seq DESC`,
		sessionID, EventRunStarted, EventUserMessageCreated)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	anchors := make(map[string]runImportInterval)
	cursor := len(imported) - 1
	for rows.Next() {
		var runID string
		var raw []byte
		if err := rows.Scan(&runID, &raw); err != nil {
			return nil, err
		}
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, err
		}
		for index := cursor; index >= 0; index-- {
			if imported[index].Kind != "user" || imported[index].Text != payload.Text {
				continue
			}
			anchors[runID] = runImportInterval{afterUser: index + 1, before: nextImportedUser(imported, index+1)}
			cursor = index - 1
			break
		}
	}
	return anchors, rows.Err()
}

func nextImportedUser(imported []ProjectedSessionItem, start int) int {
	for index := start; index < len(imported); index++ {
		if imported[index].Kind == "user" {
			return index
		}
	}
	return len(imported)
}

func matchImportedAnchors(view ProjectionView, imported []ProjectedSessionItem) map[string]int {
	matches := make(map[string]int)
	cursor := 0
	for _, id := range view.Order {
		item := view.Items[id]
		if item.Kind == "error" || (item.Origin != "runtime" && item.Origin != "import") {
			continue
		}
		for index := cursor; index < len(imported); index++ {
			if !sameImportedPosition(item, imported[index]) {
				continue
			}
			matches[id], cursor = index, index+1
			break
		}
	}
	return matches
}

func consumeImportedError(imported []ProjectedSessionItem, used map[int]bool, start, end int, text string) bool {
	for index := start; index < end; index++ {
		if !used[index] && imported[index].Kind == "error" && imported[index].Text == text {
			used[index] = true
			return true
		}
	}
	return false
}

func sameImportedPosition(runtime, imported ProjectedSessionItem) bool {
	if runtime.Kind != imported.Kind {
		return false
	}
	if runtime.Kind != "tool" {
		return runtime.Text == imported.Text
	}
	return runtime.Tool == imported.Tool && runtime.ToolCallID == imported.ToolCallID &&
		(runtime.Args == imported.Args || runtime.Body == imported.Body)
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

func rebaseProjection(
	conn *sql.Conn, source SessionSource, imported ImportedSession, stream SessionStreamState,
	preserved []preservedRebaseItem, legacyRepair bool,
) error {
	rebaseSourceID := "control:rebase:" + imported.Fingerprint.ContentHash
	if legacyRepair {
		rebaseSourceID = "control:rebase:error-order:v1"
	}
	if err := claimControlReceipt(conn, source.SessionID, rebaseSourceID); err != nil {
		return err
	}
	if err := claimControlReceipt(conn, source.SessionID, "control:error-order:v1"); err != nil {
		return err
	}
	seq := stream.CurrentSeq + 1
	payload, err := json.Marshal(map[string]string{"reason": "jsonl-import"})
	if err != nil {
		return err
	}
	event := StoredSessionEvent{Seq: seq, SessionEventDraft: SessionEventDraft{
		SessionID: source.SessionID, SourceID: rebaseSourceID,
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
	if err := insertRebasedItems(conn, source.SessionID, imported.Items, preserved, seq); err != nil {
		return err
	}
	nextGeneration := stream.ProjectionVersion + 1
	if _, err := conn.ExecContext(context.Background(), `UPDATE session_streams SET current_seq=?, projection_generation=?, source_path=?, source_size=?, source_mtime_ns=?, source_content_hash=?, updated_at=? WHERE session_id=?`, seq, nextGeneration, source.Path, imported.Fingerprint.Size, imported.Fingerprint.ModTimeNano, imported.Fingerprint.ContentHash, time.Now().UTC().Format(time.RFC3339Nano), source.SessionID); err != nil {
		return err
	}
	return nil
}

func insertRebasedItems(
	conn *sql.Conn, sessionID string, imported []ProjectedSessionItem, preserved []preservedRebaseItem, seq int64,
) error {
	ordinal := int64(0)
	nextPreserved := 0
	for index := 0; index <= len(imported); index++ {
		for nextPreserved < len(preserved) && preserved[nextPreserved].before == index {
			item := preserved[nextPreserved].item
			ordinal++
			item.Ordinal = ordinal
			item.CreatedSeq, item.UpdatedSeq = seq, seq
			if err := upsertProjectedItem(conn, sessionID, item); err != nil {
				return err
			}
			nextPreserved++
		}
		if index == len(imported) {
			break
		}
		item := imported[index]
		ordinal++
		item.Ordinal = ordinal
		item.CreatedSeq, item.UpdatedSeq = seq, seq
		syncContentMetadata(&item, seq)
		if err := upsertProjectedItem(conn, sessionID, item); err != nil {
			return err
		}
	}
	return nil
}

func claimControlReceipt(conn *sql.Conn, sessionID, sourceID string) error {
	_, err := conn.ExecContext(context.Background(), `INSERT OR IGNORE INTO session_control_receipts(session_id, source_id, event_type, payload_hash, committed_seq) VALUES (?, ?, ?, ?, 0)`, sessionID, sourceID, EventSessionRebased, sourceID)
	return err
}
