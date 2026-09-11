package piweb

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
	settings, err := WorkspaceSettingsForUser(root, session.Workspace, userID)
	if err != nil {
		return ModelStartupSettings{}, err
	}
	return modelStartupSettings(settings.Effective), nil
}

func (s *Server) overlayPersonalRuntimeModel(status RuntimeStatus, root, workspaceID string, actor *Account) (RuntimeStatus, error) {
	if actor == nil {
		return status, nil
	}
	settings, err := WorkspaceSettingsForUser(root, workspaceID, actor.ID)
	if err != nil {
		return RuntimeStatus{}, err
	}
	model := modelStartupSettings(settings.Effective)
	if model.Provider != "" && model.Model != "" {
		status.ModelProvider = model.Provider
		status.Model = model.Model
		status.ThinkingLevel = model.ThinkingLevel
	}
	return status, nil
}
