package piweb

import "net/http"

func (s *Server) deleteAccountSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.Sessions(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	deleted := 0
	for _, session := range s.accountSessions(r, sessions) {
		if err := s.store.DeleteSession(session.ID); err != nil {
			writeStoreError(w, err)
			return
		}
		deleted++
	}
	writeJSON(w, 200, map[string]any{"deletedCount": deleted, "sessions": []Session{}})
}
