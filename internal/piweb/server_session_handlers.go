package piweb

import (
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
	writeJSON(w, http.StatusOK, map[string]any{
		"session":           session,
		"messages":          page.Messages,
		"cursor":            page.Cursor,
		"hasMore":           page.HasMore,
		"limit":             page.Limit,
		"status":            s.sessionStatus(sessionID),
		"latestEventId":     strconv.FormatUint(cursors.LatestEventID, 10),
		"activeEventCursor": strconv.FormatUint(cursors.ActiveRunAfter, 10),
		"activeRun":         cursors.ActiveRunRunning,
	})
}

func (s *Server) sessionSnapshot(sessionID string, limit int, before string) (Session, SessionMessagePage, SessionEventCursors, error) {
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
	var req RenameSessionRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	session, err := s.store.RenameSession(r.PathValue("sessionID"), req.Title)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": session})
}
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	token, err := s.runtimeManager.CloseSessionToken(s.context(), sessionID)
	if err != nil {
		_ = s.runtimeManager.RollbackSessionCloseToken(sessionID, token)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.DeleteSession(sessionID); err != nil {
		_ = s.runtimeManager.RollbackSessionCloseToken(sessionID, token)
		writeStoreError(w, err)
		return
	}
	_ = s.runtimeManager.FinalizeSessionClose(sessionID, token)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
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
	if session, changed, err := s.store.AutoNameSession(sessionID, displayText); err != nil {
		writeStoreError(w, err)
		return
	} else if changed {
		s.broker.Publish(sessionID, "session.renamed", session)
	}
	if s.commands != nil {
		startupSettings, err := s.modelSettingsForSession(sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		clientRequestID := strings.TrimSpace(req.ClientRequestID)
		if clientRequestID == "" {
			clientRequestID = createSessionID()
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
		return
	}
	if s.config.EnablePiExecution {
		startupSettings, err := s.modelSettingsForSession(sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		err = s.runner.StartPiPrompt(
			s.context(),
			s.broker,
			s.store,
			sessionID,
			promptText,
			imageAttachments,
			displayText,
			startupSettings,
		)
		if err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
	} else {
		go s.broker.PublishMockPrompt(s.context(), s.store, sessionID, displayText)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "realPi": s.config.EnablePiExecution})
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
	result, err := s.runner.Compact(s.context(), s.broker, s.store, sessionID, startupSettings)
	if err != nil {
		if errors.Is(err, errSessionBusy) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"compacted": true, "result": result})
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
	if s.config.EnablePiExecution {
		if err := s.runner.SteerContext(r.Context(), sessionID, promptText, imageAttachments); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
	}
	if s.events == nil {
		user := Message{Kind: "user", Text: displayText, Attachments: imageAttachments}
		_ = appendMessageAndPublish(s.store, s.broker, sessionID, user)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "realPi": s.config.EnablePiExecution})
}
func (s *Server) cancelSession(w http.ResponseWriter, r *http.Request) {
	cancelled := s.runner.Cancel(r.PathValue("sessionID"))
	if cancelled && s.events == nil {
		s.broker.Publish(r.PathValue("sessionID"), "session.status", map[string]string{"status": "cancelled"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}
func (s *Server) sessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, _, err := s.store.Session(sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.broker.ServeSession(w, r, sessionID)
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
