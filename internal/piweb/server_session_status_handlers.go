package piweb

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (s *Server) handleSessionContextUsage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if !s.config.EnablePiExecution {
		if _, _, err := s.store.Session(sessionID); err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"contextUsage": MockContextUsage()})
		return
	}
	sessionFile, cwd, ok := s.store.SessionRuntime(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, ErrNotFound)
		return
	}
	usage, err := CurrentPiContextUsage(r.Context(), sessionFile, cwd)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contextUsage": usage})
}

func (s *Server) handleSessionSetModel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if actor := requestActor(r); actor != nil && s.accounts != nil {
		if ownerID, ok := s.accounts.sessionOwnerID(sessionID); ok && ownerID != actor.ID {
			writeError(w, http.StatusForbidden, errAccountDenied)
			return
		}
	}
	if !s.config.EnablePiExecution {
		writeError(w, http.StatusNotImplemented, errors.New("model switching requires pi execution"))
		return
	}
	if s.commands == nil {
		writeError(w, http.StatusServiceUnavailable, ErrEventStoreNotReady)
		return
	}
	if _, ok := s.commands.events.(SessionControlEventStore); !ok {
		writeError(w, http.StatusServiceUnavailable, ErrEventStoreNotReady)
		return
	}
	var req struct {
		Provider      string `json:"provider"`
		Model         string `json:"model"`
		ThinkingLevel string `json:"thinkingLevel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if (req.Provider == "") != (req.Model == "") || (req.Provider == "" && req.ThinkingLevel == "") {
		writeError(w, http.StatusBadRequest, errors.New("provider and model, or thinkingLevel, are required"))
		return
	}
	if req.ThinkingLevel != "" {
		if _, valid := thinkingLevels[req.ThinkingLevel]; !valid {
			writeError(w, http.StatusBadRequest, errors.New("invalid thinking level"))
			return
		}
	}
	session, _, err := s.store.Session(sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	root, err := s.store.WorkspacePath(session.Workspace)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	catalog, err := configuredModels(r.Context(), root)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	settings := ModelStartupSettings{Provider: req.Provider, Model: req.Model, ThinkingLevel: req.ThinkingLevel}
	if settings.Provider == "" {
		current, currentErr := s.modelSettingsForSession(sessionID)
		if currentErr != nil {
			writeError(w, http.StatusBadGateway, currentErr)
			return
		}
		settings.Provider, settings.Model = current.Provider, current.Model
	}
	if !catalogContains(catalog, settings.Provider, settings.Model) {
		writeError(w, http.StatusBadRequest, errors.New("model is not in the configured catalog"))
		return
	}
	previous, err := s.modelSettingsForSession(sessionID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if err := s.runtimeManager.ConfigureModel(r.Context(), sessionID, settings); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	requestID := strings.TrimSpace(r.Header.Get("X-Client-Request-ID"))
	if requestID == "" {
		requestID = createSessionID()
	}
	event, err := s.commands.AppendControlEvent(r.Context(), sessionID, requestID, EventSessionModelChanged, settings)
	if err != nil {
		if restoreErr := s.runtimeManager.ConfigureModel(r.Context(), sessionID, previous); restoreErr != nil {
			writeError(w, http.StatusBadGateway, errors.Join(err, restoreErr))
			return
		}
		writeSessionEventError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "acceptedSeq": event.Seq})
}
