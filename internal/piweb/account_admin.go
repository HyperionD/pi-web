package piweb

import (
	"errors"
	"maps"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type accountWriteRequest struct {
	Username       string   `json:"username"`
	Password       string   `json:"password"`
	Workspaces     []string `json:"workspaces"`
	WorkspacePaths []string `json:"workspacePaths"`
	Disabled       *bool    `json:"disabled"`
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize := 50
	s.accounts.mu.Lock()
	users := make([]Account, 0, len(s.accounts.Users))
	for _, record := range s.accounts.Users {
		if record.Role == "admin" {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(record.Username), query) {
			users = append(users, record.Account)
		}
	}
	s.accounts.mu.Unlock()
	sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
	total := len(users)
	maxPage := (total + pageSize - 1) / pageSize
	if maxPage < 1 {
		maxPage = 1
	}
	if page > maxPage {
		page = maxPage
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > total {
		end = total
	}
	writeJSON(w, 200, map[string]any{"users": users[start:end], "total": total, "page": page, "pageSize": pageSize})
}

func (s *Server) accountPaths(ids, requestedPaths []string) (map[string]string, error) {
	paths := make(map[string]string, len(ids))
	policy := WorkspacePolicy{}
	for _, requestedPath := range requestedPaths {
		if requestedPath == "" {
			continue
		}
		if err := policy.CheckExistingDirectory(requestedPath); err != nil {
			return nil, err
		}
		workspace, err := s.store.OpenWorkspace(requestedPath)
		if err != nil {
			return nil, err
		}
		ids = append(ids, workspace.ID)
	}
	for _, id := range ids {
		path, err := s.store.WorkspacePath(id)
		if err != nil {
			return nil, err
		}
		if err := policy.CheckExistingDirectory(path); err != nil {
			return nil, err
		}
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, err
		}
		paths[id] = realPath
	}
	return paths, nil
}

func (s *Server) readAccountWrite(w http.ResponseWriter, r *http.Request, create bool) (accountRecord, bool) {
	var req accountWriteRequest
	if err := readJSONWithLimit(r, &req, 16384); err != nil {
		writeError(w, 400, err)
		return accountRecord{}, false
	}
	if !create {
		req.Username = r.PathValue("username")
	}
	if !accountName.MatchString(req.Username) || req.Username == "admin" || len(req.Workspaces)+len(req.WorkspacePaths) > 100 {
		writeError(w, 400, errors.New("invalid username or too many workspaces"))
		return accountRecord{}, false
	}
	hash := ""
	if create || req.Password != "" {
		if err := validateAdminPassword(req.Password); err != nil {
			writeError(w, 400, err)
			return accountRecord{}, false
		}
		var err error
		hash, err = hashAdminPassword(req.Password)
		if err != nil {
			writeError(w, 500, err)
			return accountRecord{}, false
		}
	}
	paths, err := s.accountPaths(req.Workspaces, req.WorkspacePaths)
	if err != nil {
		writeError(w, 400, err)
		return accountRecord{}, false
	}
	ids := make([]string, 0, len(paths))
	for id := range paths {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	disabled := false
	if req.Disabled != nil {
		disabled = *req.Disabled
	}
	return accountRecord{Account: Account{Username: req.Username, Role: "user", Disabled: disabled, Workspaces: ids},
		PasswordHash: hash, Paths: paths, workspaceSet: req.Workspaces != nil || req.WorkspacePaths != nil, disabledSet: req.Disabled != nil}, true
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	record, ok := s.readAccountWrite(w, r, true)
	if !ok {
		return
	}
	s.accounts.mu.Lock()
	defer s.accounts.mu.Unlock()
	if _, exists := s.accounts.Users[record.Username]; exists {
		writeError(w, 409, errors.New("username already exists"))
		return
	}
	record.ID = accountID()
	users := maps.Clone(s.accounts.Users)
	users[record.Username] = record
	if err := s.accounts.save(users, s.accounts.Owners); err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 201, map[string]any{"user": record.Account})
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	record, ok := s.readAccountWrite(w, r, false)
	if !ok {
		return
	}
	s.accounts.mu.Lock()
	defer s.accounts.mu.Unlock()
	previous, exists := s.accounts.Users[record.Username]
	if !exists {
		writeError(w, 404, errAccountDenied)
		return
	}
	record.ID = previous.ID
	if record.PasswordHash == "" {
		record.PasswordHash = previous.PasswordHash
	}
	if !record.workspaceSet {
		record.Workspaces, record.Paths = previous.Workspaces, previous.Paths
	}
	if !record.disabledSet {
		record.Disabled = previous.Disabled
	}
	users := maps.Clone(s.accounts.Users)
	users[record.Username] = record
	if err := s.accounts.save(users, s.accounts.Owners); err != nil {
		writeError(w, 500, err)
		return
	}
	s.accounts.revoke(record.ID)
	writeJSON(w, 200, map[string]any{"user": record.Account})
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("username") == "admin" {
		writeError(w, http.StatusForbidden, errors.New("the admin account cannot be deleted"))
		return
	}
	s.accounts.mu.Lock()
	defer s.accounts.mu.Unlock()
	record, ok := s.accounts.Users[r.PathValue("username")]
	if !ok {
		writeError(w, 404, errAccountDenied)
		return
	}
	users := maps.Clone(s.accounts.Users)
	delete(users, record.Username)
	// Keep ownership tombstones: deleting/recreating a username must not expose its old sessions.
	if err := s.accounts.save(users, s.accounts.Owners); err != nil {
		writeError(w, 500, err)
		return
	}
	s.accounts.revoke(record.ID)
	writeJSON(w, 200, map[string]bool{"deleted": true})
}
