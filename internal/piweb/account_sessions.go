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
	tokens := make([]uint64, len(sessions))
	var closeErr error
	for index, session := range sessions {
		tokens[index], closeErr = s.runtimeManager.CloseSessionToken(s.context(), session.ID)
		if closeErr != nil {
			break
		}
	}
	if closeErr != nil {
		for index, session := range sessions {
			if tokens[index] != 0 {
				_ = s.runtimeManager.RollbackSessionCloseToken(session.ID, tokens[index])
			}
		}
		writeError(w, http.StatusInternalServerError, closeErr)
		return
	}
	cleaned, err := s.deleteDurableSessions(r.Context(), sessions)
	if err != nil {
		for index, session := range sessions {
			if index >= cleaned {
				_ = s.runtimeManager.RollbackSessionCloseToken(session.ID, tokens[index])
			}
		}
		writeStoreError(w, err)
		return
	}
	deleted := 0
	for index, session := range sessions {
		if err := s.store.DeleteSession(session.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				_ = s.runtimeManager.FinalizeSessionClose(session.ID, tokens[index])
				deleted++
				continue
			}
			for index, rollback := range sessions {
				if index < deleted {
					_ = s.runtimeManager.FinalizeSessionClose(rollback.ID, tokens[index])
				} else if s.events == nil {
					_ = s.runtimeManager.RollbackSessionCloseToken(rollback.ID, tokens[index])
				}
			}
			writeStoreError(w, err)
			return
		}
		deleted++
	}
	for index, session := range sessions {
		_ = s.runtimeManager.FinalizeSessionClose(session.ID, tokens[index])
	}
	writeJSON(w, 200, map[string]any{"deletedCount": deleted, "sessions": []Session{}})
}
