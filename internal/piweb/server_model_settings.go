package piweb

import (
	"context"
	"errors"
)

func (s *Server) modelSettingsForSession(sessionID string) (ModelStartupSettings, error) {
	session, _, err := s.store.Session(sessionID)
	if err != nil {
		return ModelStartupSettings{}, err
	}
	root, err := s.store.WorkspacePath(session.Workspace)
	if err != nil {
		return ModelStartupSettings{}, err
	}
	if s.accounts == nil {
		return ModelStartupSettings{}, nil
	}
	userID, _ := s.accounts.sessionOwnerID(sessionID)
	settings, err := s.accountWorkspaceSettings(root, session.Workspace, userID)
	if err != nil {
		return ModelStartupSettings{}, err
	}
	if settings.ModelSelection != nil && settings.ModelSelection.CatalogError != "" {
		return ModelStartupSettings{}, errors.New(settings.ModelSelection.CatalogError)
	}
	selected := modelStartupSettings(settings.Effective)
	reader, ok := s.events.(interface {
		LatestSessionModel(context.Context, string) (ModelStartupSettings, bool, error)
	})
	if !ok {
		return selected, nil
	}
	last, found, err := reader.LatestSessionModel(s.context(), sessionID)
	if err != nil || !found {
		return selected, err
	}
	catalog, err := configuredModels(s.context(), root)
	if err != nil {
		return ModelStartupSettings{}, err
	}
	if !catalogContains(catalog, last.Provider, last.Model) {
		return ModelStartupSettings{}, errors.New("session model is no longer in the configured catalog")
	}
	if last.ThinkingLevel == "" {
		last.ThinkingLevel = selected.ThinkingLevel
	}
	return last, nil
}

func (s *Server) overlayPersonalRuntimeModel(status RuntimeStatus, root, workspaceID, sessionID string) (RuntimeStatus, error) {
	if sessionID == "" || s.accounts == nil {
		return status, nil
	}
	session, _, err := s.store.Session(sessionID)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if session.Workspace != workspaceID {
		return RuntimeStatus{}, ErrNotFound
	}
	_, ok := s.accounts.sessionOwnerID(sessionID)
	if !ok {
		return status, nil
	}
	model, err := s.modelSettingsForSession(sessionID)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if model.Provider != "" && model.Model != "" {
		status.ModelProvider = model.Provider
		status.Model = model.Model
		status.ThinkingLevel = model.ThinkingLevel
	}
	return status, nil
}
