package piweb

import (
	"errors"
	"maps"
	"net/http"
	"sort"
)

type batchWorkspaceRequest struct {
	Usernames      []string `json:"usernames"`
	Workspaces     []string `json:"workspaces"`
	WorkspacePaths []string `json:"workspacePaths"`
}

func (s *Server) batchAssignWorkspaces(w http.ResponseWriter, r *http.Request) {
	var req batchWorkspaceRequest
	if err := readJSONWithLimit(r, &req, 1<<20); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Usernames) == 0 || len(req.Usernames) > 200 || len(req.Workspaces)+len(req.WorkspacePaths) > 100 {
		writeError(w, http.StatusBadRequest, errors.New("invalid batch size"))
		return
	}
	s.accounts.mu.Lock()
	for _, username := range req.Usernames {
		record, ok := s.accounts.Users[username]
		if !ok || record.Role == "admin" {
			s.accounts.mu.Unlock()
			writeError(w, http.StatusNotFound, errAccountDenied)
			return
		}
	}
	s.accounts.mu.Unlock()
	s.store.workspaceMutationMu.Lock()
	defer s.store.workspaceMutationMu.Unlock()
	paths, databasePaths, memoryPaths, err := s.accountPathsWithRegistrationsLocked(append([]string(nil), req.Workspaces...), req.WorkspacePaths)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rollback := func() error {
		s.store.mu.Lock()
		defer s.store.mu.Unlock()
		return s.store.rollbackWorkspaceRegistrationsLocked(databasePaths, memoryPaths)
	}
	ids := make([]string, 0, len(paths))
	for id := range paths {
		ids = append(ids, id)
	}
	s.accounts.mu.Lock()
	defer s.accounts.mu.Unlock()
	users := maps.Clone(s.accounts.Users)
	for _, username := range req.Usernames {
		record, ok := users[username]
		if !ok || record.Role == "admin" {
			_ = rollback()
			writeError(w, http.StatusNotFound, errAccountDenied)
			return
		}
		workspaceSet := make(map[string]bool, len(record.Workspaces)+len(ids))
		for _, id := range record.Workspaces {
			workspaceSet[id] = true
		}
		for _, id := range ids {
			workspaceSet[id] = true
		}
		mergedIDs := make([]string, 0, len(workspaceSet))
		for id := range workspaceSet {
			mergedIDs = append(mergedIDs, id)
		}
		sort.Strings(mergedIDs)
		mergedPaths := maps.Clone(record.Paths)
		if mergedPaths == nil {
			mergedPaths = make(map[string]string, len(record.Paths)+len(paths))
		}
		for id, path := range paths {
			mergedPaths[id] = path
		}
		record.Workspaces = mergedIDs
		record.Paths = mergedPaths
		users[username] = record
	}
	if err := s.accounts.save(users, s.accounts.Owners); err != nil {
		_ = rollback()
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, username := range req.Usernames {
		s.accounts.revoke(users[username].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": len(req.Usernames), "workspaces": ids})
}
