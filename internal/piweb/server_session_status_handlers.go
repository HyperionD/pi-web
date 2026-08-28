package piweb

import "net/http"

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
