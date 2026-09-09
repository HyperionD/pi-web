package piweb

import (
	"errors"
	"path/filepath"
)

func (s *Store) Workspaces() []Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshAllWorkspaceSessionsLocked()
	workspaces := make([]Workspace, 0, len(s.recentWorkspaceIDs))
	for _, id := range s.recentWorkspaceIDs {
		if workspace, ok := s.workspaceLocked(id); ok {
			workspaces = append(workspaces, workspace)
		}
	}
	return cloneWorkspaces(workspaces)
}

func (s *Store) AllWorkspaces() []Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshAllWorkspaceSessionsLocked()
	return cloneWorkspaces(s.workspaces)
}

func (s *Store) Workspace(workspaceID string) (Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index := s.workspaceIndexLocked(workspaceID); index >= 0 {
		s.refreshWorkspaceSessionsLocked(index, false)
		return cloneWorkspaces([]Workspace{s.workspaces[index]})[0], nil
	}
	return Workspace{}, ErrNotFound
}

func (s *Store) OpenWorkspace(path string) (Workspace, error) {
	clean, err := ValidateWorkspacePath(path)
	if err != nil {
		return Workspace{}, err
	}
	clean = canonicalWorkspacePath(clean)
	storedID, err := loadWebWorkspaceID(s.dbPath, clean)
	if err != nil {
		return Workspace{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	workspace := s.addWorkspaceLocked(clean, storedID)
	previousRecents := append([]string(nil), s.recentWorkspaceIDs...)
	s.markWorkspaceRecentLocked(workspace.ID)
	if err := saveWebWorkspaceOpened(s.dbPath, webWorkspaceRecord{ID: workspace.ID, Path: workspace.Path}); err != nil {
		s.recentWorkspaceIDs = previousRecents
		return Workspace{}, err
	}
	return workspace, nil
}

// RegisterWorkspace adds a workspace to the catalog without making it recent.
func (s *Store) RegisterWorkspace(path string) (Workspace, error) {
	clean, err := ValidateWorkspacePath(path)
	if err != nil {
		return Workspace{}, err
	}
	clean = canonicalWorkspacePath(clean)
	storedID, err := loadWebWorkspaceID(s.dbPath, clean)
	if err != nil {
		return Workspace{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	wasKnown := workspacePathKnown(s.workspaces, clean)
	workspace := s.addWorkspaceLocked(clean, storedID)
	if err := saveWebWorkspaceRegistered(s.dbPath, webWorkspaceRecord{ID: workspace.ID, Path: workspace.Path}); err != nil {
		if !wasKnown {
			s.removeWorkspaceLocked(clean)
		}
		return Workspace{}, err
	}
	return workspace, nil
}

func (s *Store) RemoveWorkspaceRegistration(path string) error {
	clean, err := ValidateWorkspacePath(path)
	if err != nil {
		return err
	}
	clean = canonicalWorkspacePath(clean)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dbPath != "" {
		deleted, err := deleteWebWorkspaceRegistration(s.dbPath, clean)
		if err != nil {
			s.removeWorkspaceLocked(clean)
			return err
		}
		if !deleted {
			return nil
		}
	}
	s.removeWorkspaceLocked(clean)
	return nil
}

func (s *Store) RegisterWorkspacePaths(paths []string) (map[string]workspaceGrant, []string, error) {
	grants := make(map[string]workspaceGrant, len(paths))
	entries := make([]struct {
		clean, real string
		existing    bool
	}, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		clean, err := ValidateWorkspacePath(path)
		if err != nil {
			return nil, nil, err
		}
		clean = canonicalWorkspacePath(clean)
		realPath, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return nil, nil, err
		}
		if seen[realPath] {
			continue
		}
		existing, err := webWorkspaceExists(s.dbPath, clean)
		if err != nil {
			return nil, nil, err
		}
		seen[realPath] = true
		entries = append(entries, struct {
			clean, real string
			existing    bool
		}{clean: clean, real: realPath, existing: existing})
	}
	storedIDs := make(map[string]string, len(entries))
	for _, entry := range entries {
		storedID, err := loadWebWorkspaceID(s.dbPath, entry.clean)
		if err != nil {
			return nil, nil, err
		}
		storedIDs[entry.clean] = storedID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	created := make([]string, 0, len(entries))
	for _, entry := range entries {
		wasKnown := workspacePathKnown(s.workspaces, entry.clean) || entry.existing
		workspace := s.addWorkspaceLocked(entry.clean, storedIDs[entry.clean])
		if !wasKnown {
			created = append(created, workspace.Path)
		}
		if err := saveWebWorkspaceRegistered(s.dbPath, webWorkspaceRecord{ID: workspace.ID, Path: workspace.Path}); err != nil {
			rollbackErr := s.rollbackWorkspaceRegistrationsLocked(created)
			return nil, nil, errors.Join(err, rollbackErr)
		}
		grants[entry.real] = workspaceGrant{ID: workspace.ID, Path: entry.real}
	}
	return grants, created, nil
}

func workspacePathKnown(workspaces []Workspace, path string) bool {
	for _, workspace := range workspaces {
		if workspace.Path == path {
			return true
		}
	}
	return false
}

func (s *Store) rollbackWorkspaceRegistrationsLocked(paths []string) error {
	var rollbackErr error
	for _, path := range paths {
		if s.dbPath != "" {
			_, err := deleteWebWorkspaceRegistration(s.dbPath, path)
			if err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		s.removeWorkspaceLocked(path)
	}
	return rollbackErr
}

func (s *Store) removeWorkspaceLocked(path string) {
	index := -1
	for i, workspace := range s.workspaces {
		if workspace.Path == path {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	workspaceID := s.workspaces[index].ID
	for _, session := range s.workspaces[index].Sessions {
		delete(s.sessionFiles, session.ID)
		delete(s.sessionCWD, session.ID)
		delete(s.conversations, session.ID)
	}
	s.workspaces = append(s.workspaces[:index], s.workspaces[index+1:]...)
	delete(s.files, workspaceID)
	delete(s.workspacePath, workspaceID)
	delete(s.workspaceSessionDir, workspaceID)
	delete(s.sessionDirModTime, workspaceID)
	delete(s.refreshDisabledWorkspace, workspaceID)
	recent := s.recentWorkspaceIDs[:0]
	for _, id := range s.recentWorkspaceIDs {
		if id != workspaceID {
			recent = append(recent, id)
		}
	}
	s.recentWorkspaceIDs = recent
}

func (s *Store) EnsureWorkspace(workspaceID, path string) (Workspace, error) {
	clean, err := ValidateWorkspacePath(path)
	if err != nil {
		return Workspace{}, err
	}
	clean = canonicalWorkspacePath(clean)
	s.mu.Lock()
	defer s.mu.Unlock()
	workspace := s.addWorkspaceLocked(clean, workspaceID)
	if workspace.ID != workspaceID || !sameWorkspacePath(workspace.Path, clean) {
		return Workspace{}, ErrNotFound
	}
	return workspace, nil
}

func (s *Store) CloseWorkspace(workspaceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, recentID := range s.recentWorkspaceIDs {
		if recentID != workspaceID {
			continue
		}
		previousRecents := append([]string(nil), s.recentWorkspaceIDs...)
		s.recentWorkspaceIDs = append(s.recentWorkspaceIDs[:index], s.recentWorkspaceIDs[index+1:]...)
		if err := saveWebWorkspaceClosed(s.dbPath, s.workspacePath[workspaceID]); err != nil {
			s.recentWorkspaceIDs = previousRecents
			return err
		}
		return nil
	}
	return ErrNotFound
}

func (s *Store) markWorkspaceRecentLocked(workspaceID string) {
	recent := []string{workspaceID}
	for _, id := range s.recentWorkspaceIDs {
		if id != workspaceID {
			recent = append(recent, id)
		}
	}
	s.recentWorkspaceIDs = recent
}

func (s *Store) workspaceLocked(workspaceID string) (Workspace, bool) {
	index := s.workspaceIndexLocked(workspaceID)
	if index < 0 {
		return Workspace{}, false
	}
	return s.workspaces[index], true
}

func (s *Store) workspaceIndexLocked(workspaceID string) int {
	for index := range s.workspaces {
		if s.workspaces[index].ID == workspaceID {
			return index
		}
	}
	return -1
}
func (s *Store) WorkspacePath(workspaceID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	root := s.workspacePath[workspaceID]
	if root == "" {
		return "", ErrNotFound
	}
	return root, nil
}
func (s *Store) Files(workspaceID string) ([]FileNode, error) {
	s.mu.RLock()
	root := s.workspacePath[workspaceID]
	mock := append([]FileNode(nil), s.files[workspaceID]...)
	_, exists := s.files[workspaceID]
	s.mu.RUnlock()
	if root != "" {
		if files, err := RealFileTree(root, 3); err == nil {
			return files, nil
		}
	}
	if !exists {
		return nil, ErrNotFound
	}
	return mock, nil
}
func (s *Store) SearchFiles(workspaceID, query string) ([]string, error) {
	s.mu.RLock()
	root := s.workspacePath[workspaceID]
	s.mu.RUnlock()
	if root == "" {
		return nil, ErrNotFound
	}
	return SearchWorkspaceFiles(root, query)
}

func (s *Store) ReadFile(workspaceID, rel string) (FileContent, error) {
	s.mu.RLock()
	root := s.workspacePath[workspaceID]
	s.mu.RUnlock()
	if root == "" {
		return FileContent{}, ErrNotFound
	}
	return ReadWorkspaceFile(root, rel, 256*1024)
}
func (s *Store) WriteFile(workspaceID, rel, content string) (FileContent, error) {
	s.mu.RLock()
	root := s.workspacePath[workspaceID]
	s.mu.RUnlock()
	if root == "" {
		return FileContent{}, ErrNotFound
	}
	return WriteWorkspaceFile(root, rel, content)
}
func (s *Store) CreateFile(workspaceID, rel, kind, content string) (FileContent, error) {
	s.mu.RLock()
	root := s.workspacePath[workspaceID]
	s.mu.RUnlock()
	if root == "" {
		return FileContent{}, ErrNotFound
	}
	return CreateWorkspacePath(root, rel, kind, content)
}
func (s *Store) RenameFile(workspaceID, oldRel, newRel string) error {
	s.mu.RLock()
	root := s.workspacePath[workspaceID]
	s.mu.RUnlock()
	if root == "" {
		return ErrNotFound
	}
	return RenameWorkspacePath(root, oldRel, newRel)
}
func (s *Store) DeleteFile(workspaceID, rel string) error {
	s.mu.RLock()
	root := s.workspacePath[workspaceID]
	s.mu.RUnlock()
	if root == "" {
		return ErrNotFound
	}
	return DeleteWorkspacePath(root, rel)
}
func (s *Store) UploadFile(workspaceID, rel string, data []byte, overwrite bool) (FileContent, error) {
	s.mu.RLock()
	root := s.workspacePath[workspaceID]
	s.mu.RUnlock()
	if root == "" {
		return FileContent{}, ErrNotFound
	}
	return UploadWorkspaceFile(root, rel, data, overwrite)
}
func (s *Store) GitStatus(workspaceID string) (GitStatus, error) {
	s.mu.RLock()
	root := s.workspacePath[workspaceID]
	found := false
	for _, workspace := range s.workspaces {
		if workspace.ID == workspaceID {
			found = true
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return GitStatus{}, ErrNotFound
	}
	if root != "" {
		if status, err := RealGitStatus(root); err == nil {
			return status, nil
		}
	}
	return GitStatus{Branch: "main", Dirty: 3, Files: map[string]string{}}, nil
}
