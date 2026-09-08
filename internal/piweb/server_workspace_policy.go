package piweb

import (
	"errors"
	"net/http"
)

func (s *Server) requireWorkspaceAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path, err := s.workspaceAccess.ResolveWorkspace(requestActor(r), r.PathValue("workspaceID"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		policy, err := s.workspacePolicy()
		if err != nil {
			writeWorkspacePolicyError(w, err)
			return
		}
		if err := policy.CheckExistingDirectory(path); err != nil {
			writeWorkspacePolicyError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func (s *Server) requireSessionAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path, err := s.workspaceAccess.ResolveSession(requestActor(r), r.PathValue("sessionID"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		policy, err := s.workspacePolicy()
		if err != nil {
			writeWorkspacePolicyError(w, err)
			return
		}
		if err := policy.CheckExistingDirectory(path); err != nil {
			writeWorkspacePolicyError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func writeWorkspacePolicyError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, ErrWorkspaceForbidden) {
		status = http.StatusForbidden
	}
	writeError(w, status, err)
}
