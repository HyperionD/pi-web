package piweb

import (
	"errors"
	"path/filepath"
)

type workspaceGrant struct {
	ID   string
	Path string
}

// WorkspaceAccess is the seam for workspace visibility, lifecycle, and session ownership.
type WorkspaceAccess struct {
	store    *Store
	accounts *accountStore
}

func NewWorkspaceAccess(store *Store, accounts *accountStore) *WorkspaceAccess {
	return &WorkspaceAccess{store: store, accounts: accounts}
}

func (m *WorkspaceAccess) List(actor *Account) ([]Workspace, error) {
	if actor == nil || actor.Role == "admin" {
		return m.store.Workspaces(), nil
	}
	grants, ok := m.accounts.workspaceGrants(*actor)
	if !ok {
		return nil, errAccountDenied
	}
	workspaces := make([]Workspace, 0, len(grants))
	for _, grant := range grants {
		workspace, err := m.store.EnsureWorkspace(grant.ID, grant.Path)
		if err != nil {
			continue
		}
		workspace, err = m.store.Workspace(workspace.ID)
		if err != nil {
			continue
		}
		workspace.Sessions = m.visibleSessions(actor, workspace.Sessions)
		workspace.SessionCount = len(workspace.Sessions)
		workspace.Live = false
		workspace.LastUsed = ""
		workspaces = append(workspaces, workspace)
	}
	return workspaces, nil
}

func (m *WorkspaceAccess) Open(actor *Account, path string) (Workspace, error) {
	if actor != nil && actor.Role != "admin" {
		return Workspace{}, errAccountDenied
	}
	return m.store.OpenWorkspace(path)
}

func (m *WorkspaceAccess) Close(actor *Account, workspaceID string) error {
	if actor != nil && actor.Role != "admin" {
		return errAccountDenied
	}
	return m.store.CloseWorkspace(workspaceID)
}

func (m *WorkspaceAccess) CreateSession(actor *Account, workspaceID string) (Session, error) {
	if _, err := m.ResolveWorkspace(actor, workspaceID); err != nil {
		return Session{}, err
	}
	session, err := m.store.CreateSession(workspaceID)
	if err != nil || actor == nil {
		return session, err
	}
	if err := m.accounts.own(session.ID, actor.ID); err != nil {
		if cleanupErr := m.store.DeleteSession(session.ID); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		return Session{}, err
	}
	return session, nil
}

func (m *WorkspaceAccess) Sessions(actor *Account, workspaceID string) ([]Session, error) {
	if _, err := m.ResolveWorkspace(actor, workspaceID); err != nil {
		return nil, err
	}
	sessions, err := m.store.Sessions(workspaceID)
	if err != nil {
		return nil, err
	}
	return m.visibleSessions(actor, sessions), nil
}

func (m *WorkspaceAccess) ResolveWorkspace(actor *Account, workspaceID string) (string, error) {
	if actor == nil || actor.Role == "admin" {
		return m.store.WorkspacePath(workspaceID)
	}
	grant, ok := m.accounts.workspaceGrant(*actor, workspaceID)
	if !ok {
		return "", ErrNotFound
	}
	workspace, err := m.store.EnsureWorkspace(grant.ID, grant.Path)
	if err != nil || !sameWorkspacePath(workspace.Path, grant.Path) {
		return "", ErrNotFound
	}
	return workspace.Path, nil
}

func (m *WorkspaceAccess) ResolveSession(actor *Account, sessionID string) (string, error) {
	if actor != nil && actor.Role != "admin" && !m.accounts.owns(*actor, sessionID) {
		return "", ErrNotFound
	}
	session, _, err := m.store.sessionMeta(sessionID)
	if err != nil {
		return "", err
	}
	return m.ResolveWorkspace(actor, session.Workspace)
}

func (m *WorkspaceAccess) visibleSessions(actor *Account, sessions []Session) []Session {
	if actor == nil || actor.Role == "admin" {
		return sessions
	}
	visible := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		if m.accounts.owns(*actor, session.ID) {
			visible = append(visible, session)
		}
	}
	return visible
}

func sameWorkspacePath(left, right string) bool {
	leftPath, leftErr := filepath.EvalSymlinks(left)
	rightPath, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftPath) == filepath.Clean(rightPath)
}
