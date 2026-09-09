package piweb

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
	workspace := s.addWorkspaceLocked(clean, storedID)
	if err := saveWebWorkspaceRegistered(s.dbPath, webWorkspaceRecord{ID: workspace.ID, Path: workspace.Path}); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
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
