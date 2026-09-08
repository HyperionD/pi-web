package piweb

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now().UTC()})
}
func (s *Server) listFolders(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "~"
	}
	purpose := r.URL.Query().Get("purpose")
	if purpose == "" {
		purpose = folderPurposeWorkspace
	}
	if purpose != folderPurposeWorkspace && purpose != folderPurposeAllowedRoot {
		writeError(w, http.StatusBadRequest, errors.New("invalid folder purpose"))
		return
	}
	var policy WorkspacePolicy
	if purpose == folderPurposeWorkspace {
		var err error
		policy, err = s.workspacePolicy()
		if err != nil {
			writeWorkspaceSelectionError(w, err)
			return
		}
	}
	if policy.restricted {
		if err := policy.CheckFolderPath(path); err != nil {
			if errors.Is(err, ErrWorkspaceForbidden) {
				listing, listingErr := policy.AllowedRootListing(path)
				if listingErr != nil {
					writeWorkspaceSelectionError(w, listingErr)
					return
				}
				writeJSON(w, http.StatusOK, listing)
				return
			}
			writeWorkspaceSelectionError(w, err)
			return
		}
	}
	folders, err := ListFolders(path)
	if err != nil {
		if policy.restricted {
			writeWorkspaceSelectionError(w, err)
		} else {
			writeError(w, http.StatusBadRequest, err)
		}
		return
	}
	if policy.restricted {
		folders, err = policy.FilterFolderListing(folders)
		if err != nil {
			writeWorkspaceSelectionError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, folders)
}
func (s *Server) workspaces(w http.ResponseWriter, r *http.Request) {
	policy, err := s.workspacePolicy()
	if err != nil {
		writeWorkspaceSelectionError(w, err)
		return
	}
	workspaces, err := s.workspaceAccess.List(requestActor(r))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if policy.restricted {
		filtered := make([]Workspace, 0, len(workspaces))
		for _, workspace := range workspaces {
			if err := policy.CheckExistingDirectory(workspace.Path); err != nil {
				if errors.Is(err, ErrWorkspaceForbidden) {
					continue
				}
				writeWorkspaceSelectionError(w, err)
				return
			}
			filtered = append(filtered, workspace)
		}
		workspaces = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": s.withLiveSessions(workspaces)})
}
func (s *Server) adminWorkspaces(w http.ResponseWriter, r *http.Request) {
	workspaces, err := s.workspaceAccess.Catalog(requestActor(r))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": workspaces})
}

func (s *Server) openWorkspace(w http.ResponseWriter, r *http.Request) {
	var req OpenWorkspaceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	policy, err := s.workspacePolicy()
	if err != nil {
		writeWorkspaceSelectionError(w, err)
		return
	}
	if err := policy.CheckExistingDirectory(req.Path); err != nil {
		writeWorkspaceSelectionError(w, err)
		return
	}
	workspace, err := s.workspaceAccess.Open(requestActor(r), req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}
func (s *Server) cloneWorkspace(w http.ResponseWriter, r *http.Request) {
	var req CloneWorkspaceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	policy, err := s.workspacePolicy()
	if err != nil {
		writeWorkspaceSelectionError(w, err)
		return
	}
	workspace, output, err := CloneGitWorkspace(s.context(), s.store, req, policy)
	if err != nil {
		message := errors.New(strings.TrimSpace(err.Error() + "\n" + output))
		writeError(w, workspaceSelectionErrorStatus(err), message)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"workspace": workspace, "output": output})
}
func (s *Server) closeWorkspace(w http.ResponseWriter, r *http.Request) {
	if err := s.workspaceAccess.Close(requestActor(r), r.PathValue("workspaceID")); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"closed": true})
}
func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.workspaceAccess.CreateSession(requestActor(r), r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"session": session})
}
func (s *Server) workspaceSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.workspaceAccess.Sessions(requestActor(r), r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.withLiveSessionFlags(sessions)})
}
func (s *Server) withLiveSessions(workspaces []Workspace) []Workspace {
	for workspaceIndex := range workspaces {
		workspaces[workspaceIndex].Sessions = s.withLiveSessionFlags(workspaces[workspaceIndex].Sessions)
		for _, session := range workspaces[workspaceIndex].Sessions {
			workspaces[workspaceIndex].Live = workspaces[workspaceIndex].Live || session.Live
		}
	}
	return workspaces
}

func (s *Server) withLiveSessionFlags(sessions []Session) []Session {
	running := s.runningSessionIDs()
	for index := range sessions {
		if !running[sessions[index].ID] {
			continue
		}
		sessions[index].Live = true
		sessions[index].Active = true
		sessions[index].LastUsed = "live"
	}
	return sessions
}

func (s *Server) runningSessionIDs() map[string]bool {
	s.runner.mu.Lock()
	defer s.runner.mu.Unlock()
	running := make(map[string]bool, len(s.runner.running))
	for sessionID := range s.runner.running {
		running[sessionID] = true
	}
	return running
}

func (s *Server) deleteWorkspaceSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := requestAccount(r); ok {
		s.deleteAccountSessions(w, r)
		return
	}
	deletedCount, err := s.store.DeleteWorkspaceSessions(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deletedCount": deletedCount, "sessions": []Session{}})
}
func (s *Server) workspaceFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.store.Files(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}
func (s *Server) searchWorkspaceFiles(w http.ResponseWriter, r *http.Request) {
	matches, err := s.store.SearchFiles(r.PathValue("workspaceID"), r.URL.Query().Get("q"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}
