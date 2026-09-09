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
	Account        string   `json:"account"`
	Username       string   `json:"username"`
	Department     *string  `json:"department"`
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
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize != 10 && pageSize != 20 && pageSize != 50 && pageSize != 100 {
		pageSize = 50
	}
	s.accounts.mu.Lock()
	users := make([]Account, 0, len(s.accounts.Users))
	for _, record := range s.accounts.Users {
		if record.Role == "admin" {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(record.Account.Account), query) || strings.Contains(strings.ToLower(record.Username), query) || strings.Contains(strings.ToLower(record.Department), query) {
			users = append(users, record.Account)
		}
	}
	s.accounts.mu.Unlock()
	sort.Slice(users, func(i, j int) bool { return users[i].Account < users[j].Account })
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
		workspace, err := s.store.RegisterWorkspace(requestedPath)
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

func (s *Server) accountPathsWithRegistrations(ids, requestedPaths []string) (map[string]string, []string, []string, error) {
	return s.accountPathsWithRegistrationsInternal(ids, requestedPaths, false)
}

func (s *Server) accountPathsWithRegistrationsLocked(ids, requestedPaths []string) (map[string]string, []string, []string, error) {
	return s.accountPathsWithRegistrationsInternal(ids, requestedPaths, true)
}

func (s *Server) accountPathsWithRegistrationsInternal(ids, requestedPaths []string, storeLocked bool) (map[string]string, []string, []string, error) {
	paths := make(map[string]string, len(ids))
	policy := WorkspacePolicy{}
	for _, requestedPath := range requestedPaths {
		if requestedPath == "" {
			continue
		}
		if err := policy.CheckExistingDirectory(requestedPath); err != nil {
			return nil, nil, nil, err
		}
	}
	var grants map[string]workspaceGrant
	var databasePaths, memoryPaths []string
	var err error
	if storeLocked {
		grants, databasePaths, memoryPaths, err = s.store.registerWorkspacePaths(requestedPaths)
	} else {
		grants, databasePaths, memoryPaths, err = s.store.RegisterWorkspacePaths(requestedPaths)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	rollback := func(cause error) (map[string]string, []string, []string, error) {
		if storeLocked {
			s.store.mu.Lock()
			defer s.store.mu.Unlock()
			_ = s.store.rollbackWorkspaceRegistrationsLocked(databasePaths, memoryPaths)
		} else {
			_ = s.store.RollbackWorkspaceRegistrations(databasePaths, memoryPaths)
		}
		return nil, nil, nil, cause
	}
	for _, requestedPath := range requestedPaths {
		if requestedPath == "" {
			continue
		}
		realPath, err := filepath.EvalSymlinks(requestedPath)
		if err != nil {
			return rollback(err)
		}
		grant, ok := grants[realPath]
		if !ok {
			return rollback(errors.New("workspace registration missing"))
		}
		ids = append(ids, grant.ID)
		paths[grant.ID] = grant.Path
	}
	for _, id := range ids {
		if _, ok := paths[id]; ok {
			continue
		}
		path, err := s.store.WorkspacePath(id)
		if err != nil {
			return rollback(err)
		}
		if err := policy.CheckExistingDirectory(path); err != nil {
			return rollback(err)
		}
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return rollback(err)
		}
		paths[id] = realPath
	}
	return paths, databasePaths, memoryPaths, nil
}

func (s *Server) readAccountWrite(w http.ResponseWriter, r *http.Request, create bool) (accountRecord, bool) {
	var req accountWriteRequest
	if err := readJSONWithLimit(r, &req, 16384); err != nil {
		writeError(w, 400, err)
		return accountRecord{}, false
	}
	if !create {
		req.Account = r.PathValue("username")
	}
	if req.Account == "" {
		req.Account = req.Username
	}
	if req.Username == "" {
		req.Username = req.Account
	}
	if !accountName.MatchString(req.Account) || req.Account == "admin" || len(req.Workspaces)+len(req.WorkspacePaths) > 100 {
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
	department := ""
	if req.Department != nil {
		department = strings.TrimSpace(*req.Department)
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
	return accountRecord{Account: Account{Account: req.Account, Username: req.Username, Department: department, Role: "user", Disabled: disabled, Workspaces: ids},
		PasswordHash: hash, Paths: paths, workspaceSet: req.Workspaces != nil || req.WorkspacePaths != nil, departmentSet: req.Department != nil, disabledSet: req.Disabled != nil}, true
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	record, ok := s.readAccountWrite(w, r, true)
	if !ok {
		return
	}
	s.accounts.mu.Lock()
	defer s.accounts.mu.Unlock()
	if _, exists := s.accounts.Users[record.Account.Account]; exists {
		writeError(w, 409, errors.New("username already exists"))
		return
	}
	record.ID = accountID()
	users := maps.Clone(s.accounts.Users)
	users[record.Account.Account] = record
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
	previous, exists := s.accounts.Users[record.Account.Account]
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
	if !record.departmentSet {
		record.Department = previous.Department
	}
	if !record.disabledSet {
		record.Disabled = previous.Disabled
	}
	users := maps.Clone(s.accounts.Users)
	users[record.Account.Account] = record
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
	delete(users, record.Account.Account)
	// Keep ownership tombstones: deleting/recreating a username must not expose its old sessions.
	if err := s.accounts.save(users, s.accounts.Owners); err != nil {
		writeError(w, 500, err)
		return
	}
	s.accounts.revoke(record.ID)
	writeJSON(w, 200, map[string]bool{"deleted": true})
}
