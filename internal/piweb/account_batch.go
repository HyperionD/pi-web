package piweb

import (
	"errors"
	"maps"
	"net/http"
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
	paths, err := s.accountPaths(append([]string(nil), req.Workspaces...), req.WorkspacePaths)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
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
			writeError(w, http.StatusNotFound, errAccountDenied)
			return
		}
		record.Workspaces = ids
		record.Paths = paths
		users[username] = record
	}
	if err := s.accounts.save(users, s.accounts.Owners); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, username := range req.Usernames {
		s.accounts.revoke(users[username].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": len(req.Usernames), "workspaces": ids})
}
