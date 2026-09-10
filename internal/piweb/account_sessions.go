package piweb

import (
	"errors"
	"net/http"
)

func (s *Server) deleteAccountSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.workspaceAccess.Sessions(requestActor(r), r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var closeErr error
	for _, session := range sessions {
		closeErr = errors.Join(closeErr, s.runtimeManager.CloseSession(s.context(), session.ID))
	}
	if closeErr != nil {
		for _, session := range sessions {
			_ = s.runtimeManager.RollbackSessionClose(session.ID)
		}
		writeError(w, http.StatusInternalServerError, closeErr)
		return
	}
	deleted := 0
	for _, session := range sessions {
		if err := s.store.DeleteSession(session.ID); err != nil {
			for _, rollback := range sessions[deleted:] {
				_ = s.runtimeManager.RollbackSessionClose(rollback.ID)
			}
			writeStoreError(w, err)
			return
		}
		deleted++
	}
	writeJSON(w, 200, map[string]any{"deletedCount": deleted, "sessions": []Session{}})
}
