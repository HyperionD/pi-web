package piweb

import (
	"encoding/json"
	"errors"
	"net/http"
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
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sessionFile, cwd, ok := s.store.SessionRuntime(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, ErrNotFound)
		return
	}
	if err := CurrentPiSetModel(r.Context(), sessionFile, cwd, req.Provider, req.Model); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
