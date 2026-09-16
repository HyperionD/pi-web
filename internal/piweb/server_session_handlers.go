package piweb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	session, page, cursors, err := s.sessionSnapshot(sessionID, limit, r.URL.Query().Get("before"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	status := s.sessionStatus(sessionID)
	if cursors.Status != "" {
		status = cursors.Status
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":           session,
		"messages":          page.Messages,
		"cursor":            page.Cursor,
		"hasMore":           page.HasMore,
		"limit":             page.Limit,
		"status":            status,
		"latestEventId":     strconv.FormatUint(cursors.LatestEventID, 10),
		"activeEventCursor": strconv.FormatUint(cursors.ActiveRunAfter, 10),
		"activeRun":         cursors.ActiveRunRunning,
	})
}

func (s *Server) sessionSnapshot(sessionID string, limit int, before string) (Session, SessionMessagePage, SessionEventCursors, error) {
	if s.events != nil {
		session, _, err := s.store.Session(sessionID)
		if err != nil {
			return Session{}, SessionMessagePage{}, SessionEventCursors{}, err
		}
		snapshot, err := s.events.Snapshot(s.context(), sessionID, SessionPageRequest{Limit: limit, Before: before})
		if err != nil {
			return Session{}, SessionMessagePage{}, SessionEventCursors{}, err
		}
		if snapshot.Metadata.Title != "" {
			session.Title = snapshot.Metadata.Title
		}
		if snapshot.Metadata.WorkspaceID != "" {
			session.Workspace = snapshot.Metadata.WorkspaceID
		}
		return session, projectedMessagePage(snapshot), SessionEventCursors{
			LatestEventID:    uint64(snapshot.Seq),
			ActiveRunAfter:   uint64(snapshot.Seq),
			ActiveRunRunning: snapshot.Status == SessionStatusRunning || snapshot.Status == SessionStatusThinking,
			Status:           snapshot.Status,
		}, nil
	}
	s.store.sessionMutationMu.Lock()
	defer s.store.sessionMutationMu.Unlock()
	var session Session
	var page SessionMessagePage
	cursors, err := s.broker.WithSessionSnapshot(sessionID, func() error {
		if s.store.sessionSnapshotHook != nil {
			s.store.sessionSnapshotHook()
		}
		var readErr error
		session, page, readErr = s.store.SessionPage(sessionID, limit, before)
		return readErr
	})
	if err != nil {
		return Session{}, SessionMessagePage{}, SessionEventCursors{}, err
	}
	return session, page, cursors, nil
}

func projectedMessagePage(snapshot SessionStateSnapshot) SessionMessagePage {
	messages := make([]Message, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		messages = append(messages, Message{
			ID: item.ItemID, Kind: item.Kind, Text: item.Text, Tool: item.Tool, Args: item.Args,
			Status: item.Status, DurationMs: item.DurationMs, ResultMeta: item.ResultMeta,
			Body: item.Body, Attachments: item.Attachments,
		})
	}
	return SessionMessagePage{Messages: messages, Cursor: snapshot.Cursor, HasMore: snapshot.HasMore, Limit: snapshot.Limit}
}

func (s *Server) sessionStatus(sessionID string) string {
	if s.runner.IsCompacting(sessionID) {
		return "compacting"
	}
	if s.runner.IsRunning(sessionID) || s.broker.IsRunning(sessionID) {
		return "running"
	}
	return "idle"
}
func (s *Server) renameSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RenameSessionRequest
		ClientRequestID string `json:"clientRequestId"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sessionID := r.PathValue("sessionID")
	if s.commands == nil {
		writeError(w, http.StatusServiceUnavailable, ErrEventStoreNotReady)
		return
	}
	requestID := strings.TrimSpace(req.ClientRequestID)
	if requestID == "" {
		requestID = createSessionID()
	}
	event, err := s.commands.AppendControlEvent(r.Context(), sessionID, requestID, EventSessionRenamed, map[string]string{"title": req.Title})
	if err != nil {
		writeSessionEventError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "acceptedSeq": event.Seq})
}
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if s.commands == nil {
		writeError(w, http.StatusServiceUnavailable, ErrEventStoreNotReady)
		return
	}
	token, err := s.runtimeManager.CloseSessionToken(s.context(), sessionID)
	if err != nil {
		_ = s.runtimeManager.RollbackSessionCloseToken(sessionID, token)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	requestID := strings.TrimSpace(r.Header.Get("X-Client-Request-ID"))
	if requestID == "" {
		requestID = createSessionID()
	}
	event, eventErr := s.commands.AppendControlEvent(r.Context(), sessionID, requestID, EventSessionDeleted, nil)
	if eventErr != nil {
		_ = s.runtimeManager.RollbackSessionCloseToken(sessionID, token)
		writeSessionEventError(w, eventErr)
		return
	}
	if err := s.store.DeleteSession(sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = s.runtimeManager.FinalizeSessionClose(sessionID, token)
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "deleted": true, "acceptedSeq": event.Seq})
}
func (s *Server) prompt(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, _, err := s.store.Session(sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	var req PromptRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	promptText := mergePromptAttachments(req.Text, req.Attachments)
	imageAttachments := imagePromptAttachments(req.Attachments)
	if strings.TrimSpace(promptText) == "" && len(imageAttachments) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("text is required"))
		return
	}
	displayText := promptDisplayText(req.Text, req.Attachments)
	clientRequestID := strings.TrimSpace(req.ClientRequestID)
	if clientRequestID == "" {
		clientRequestID = createSessionID()
	}
	if s.commands == nil {
		writeError(w, http.StatusServiceUnavailable, ErrEventStoreNotReady)
		return
	}
	if title := trimTitle(displayText); title != "" {
		if _, err := s.commands.AppendControlEvent(r.Context(), sessionID, clientRequestID+":autoname", EventSessionRenamed, map[string]string{"title": title}); err != nil {
			writeSessionEventError(w, err)
			return
		}
	}
	startupSettings, err := s.modelSettingsForSession(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	receipt, err := s.commands.StartPromptWith(r.Context(), StartPromptCommand{
		SessionID: sessionID, ClientRequestID: clientRequestID, DisplayText: displayText,
		Text: promptText, Attachments: req.Attachments,
	}, runnerPromptStarter{runner: s.runner, store: s.store, settings: []ModelStartupSettings{startupSettings}})
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"accepted":    true,
		"runId":       receipt.RunID,
		"acceptedSeq": receipt.AcceptedSeq,
		"existing":    receipt.Existing,
		"realPi":      s.config.EnablePiExecution,
	})
}
func (s *Server) compactSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, _, err := s.store.Session(sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.config.EnablePiExecution {
		writeError(w, http.StatusNotImplemented, errors.New("pi execution is disabled"))
		return
	}
	startupSettings, err := s.modelSettingsForSession(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	requestID := strings.TrimSpace(r.Header.Get("X-Client-Request-ID"))
	if requestID == "" {
		requestID = createSessionID()
	}
	if s.commands == nil {
		writeError(w, http.StatusServiceUnavailable, ErrEventStoreNotReady)
		return
	}
	controlEvents, ok := s.events.(SessionControlEventStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, ErrEventStoreNotReady)
		return
	}
	result, err := s.runner.CompactDurable(s.context(), controlEvents, s.store, sessionID, requestID, startupSettings)
	if err != nil {
		if errors.Is(err, errSessionBusy) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "compacted": true, "result": result})
}
func (s *Server) steerSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, _, err := s.store.Session(sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	var req PromptRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	promptText := mergePromptAttachments(req.Text, req.Attachments)
	imageAttachments := imagePromptAttachments(req.Attachments)
	if strings.TrimSpace(promptText) == "" && len(imageAttachments) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("text is required"))
		return
	}
	displayText := promptDisplayText(req.Text, req.Attachments)
	if s.commands == nil {
		writeError(w, http.StatusServiceUnavailable, ErrEventStoreNotReady)
		return
	}
	if s.config.EnablePiExecution {
		if err := s.runner.SteerContext(r.Context(), sessionID, promptText, imageAttachments); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
	}
	requestID := strings.TrimSpace(r.Header.Get("X-Client-Request-ID"))
	if requestID == "" {
		requestID = createSessionID()
	}
	event, err := s.commands.AppendControlEvent(r.Context(), sessionID, requestID, EventSteerAccepted, map[string]any{
		"text": displayText,
	})
	if err != nil {
		writeSessionEventError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "acceptedSeq": event.Seq, "realPi": s.config.EnablePiExecution})
}
func (s *Server) cancelSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if s.commands == nil {
		writeError(w, http.StatusServiceUnavailable, ErrEventStoreNotReady)
		return
	}
	expectedRunID := ""
	if s.events != nil {
		snapshot, snapshotErr := s.events.Snapshot(r.Context(), sessionID, SessionPageRequest{Limit: 1})
		if snapshotErr != nil && !errors.Is(snapshotErr, sql.ErrNoRows) {
			writeSessionEventError(w, snapshotErr)
			return
		}
		expectedRunID = snapshot.RunID
	}
	if expectedRunID == "" {
		expectedRunID = s.runner.ActiveRunID(sessionID)
	}
	requestID := strings.TrimSpace(r.Header.Get("X-Client-Request-ID"))
	if requestID == "" {
		requestID = createSessionID()
	}
	event, err := s.commands.AppendControlEvent(r.Context(), sessionID, requestID, EventRunCancelRequested, map[string]string{"runId": expectedRunID})
	if err != nil {
		writeSessionEventError(w, err)
		return
	}
	var cancelPayload struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal(event.Payload, &cancelPayload); err == nil && cancelPayload.RunID != "" {
		expectedRunID = cancelPayload.RunID
	}
	cancelled := s.runner.CancelExpected(sessionID, expectedRunID)
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "acceptedSeq": event.Seq, "cancelled": cancelled})
}
func mergePromptAttachments(text string, attachments []PromptAttachment) string {
	if len(attachments) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	for i, attachment := range attachments {
		if attachment.Type == "image" || strings.TrimSpace(attachment.Content) == "" {
			continue
		}
		b.WriteString("\n\n<attachment index=\"")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString("\">\n")
		b.WriteString(attachmentPromptText(attachment))
		b.WriteString("\n</attachment>")
	}
	return b.String()
}

func attachmentPromptText(attachment PromptAttachment) string {
	if strings.TrimSpace(attachment.Name) == "" {
		return attachment.Content
	}
	return "File: " + attachment.Name + "\n\n" + attachment.Content
}

func imagePromptAttachments(attachments []PromptAttachment) []PromptAttachment {
	var images []PromptAttachment
	for _, attachment := range attachments {
		if attachment.Type == "image" && strings.TrimSpace(attachment.DataURL) != "" {
			images = append(images, attachment)
		}
	}
	return images
}

func promptDisplayText(text string, attachments []PromptAttachment) string {
	if strings.TrimSpace(text) != "" {
		return text
	}
	for _, attachment := range attachments {
		kind := attachment.Type
		if kind == "" {
			kind = "file"
		}
		if strings.TrimSpace(attachment.Name) == "" {
			return "[" + kind + "]"
		}
		return "[" + kind + ": " + attachment.Name + "]"
	}
	return text
}
