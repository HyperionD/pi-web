package piweb

import (
	"errors"
	"net/http"
)

func (s *Server) requireWorkspaceAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !s.accountWorkspaceAccess(w, r, r.PathValue("workspaceID"), path) {
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
		if !s.accountSessionAccess(w, r) {
			return
		}
		path, err := s.store.SessionWorkspacePath(r.PathValue("sessionID"))
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
