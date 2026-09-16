package piweb

import (
	"errors"
	"net/http"
)

func (s *Server) workspaceCommands(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	commands := MockSlashCommands()
	var commandError string
	if s.config.EnablePiExecution {
		commands, err = ListPiCommands(r.Context(), root)
		if err != nil {
			commands = []SlashCommand{}
			commandError = err.Error()
		}
	}
	body := map[string]any{"commands": commands}
	if commandError != "" {
		body["error"] = commandError
	}
	writeJSON(w, http.StatusOK, body)
}
func (s *Server) workspaceModels(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	models := fallbackWorkspaceModels(root, nil)
	if s.config.EnablePiExecution {
		models, err = WorkspaceModels(r.Context(), root)
		if err != nil {
			models = fallbackWorkspaceModels(root, err)
		}
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) workspaceRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	status := MockRuntimeStatus()
	if s.config.EnablePiExecution {
		status, err = WorkspaceRuntimeStatus(r.Context(), root)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		sessionID := r.URL.Query().Get("sessionID")
		status, err = s.overlayPersonalRuntimeModel(status, root, r.PathValue("workspaceID"), sessionID)
		if err != nil {
			writeRuntimeModelError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}
func (s *Server) workspaceRuntimeModel(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	status := MockRuntimeModelStatus()
	if s.config.EnablePiExecution {
		status, err = WorkspaceRuntimeModelStatus(r.Context(), root)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		sessionID := r.URL.Query().Get("sessionID")
		status, err = s.overlayPersonalRuntimeModel(status, root, r.PathValue("workspaceID"), sessionID)
		if err != nil {
			writeRuntimeModelError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}
func (s *Server) workspaceRuntimeQuota(w http.ResponseWriter, r *http.Request) {
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	status := MockRuntimeQuotaStatus()
	if s.config.EnablePiExecution {
		status = WorkspaceRuntimeQuotaStatus(r.Context(), root, r.URL.Query().Get("model"))
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}
func (s *Server) workspaceSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	root, err := s.store.WorkspacePath(workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	userID := ""
	if actor := requestActor(r); actor != nil {
		userID = actor.ID
	}
	settings, err := WorkspaceSettingsForUser(root, workspaceID, userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}
func (s *Server) saveWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceID")
	root, err := s.store.WorkspacePath(workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req SettingsPatchRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	actor := requestActor(r)
	if actor != nil && actor.Role != "admin" && req.Scope != personalSettingsScope {
		writeError(w, http.StatusForbidden, errAccountDenied)
		return
	}
	if req.Scope == personalSettingsScope {
		if actor == nil {
			writeError(w, http.StatusForbidden, errAccountDenied)
			return
		}
		location := req.Location
		if location == "" {
			location = personalProjectScope
		}
		if err := savePersonalModelSettings(actor.ID, location, workspaceID, req.Settings); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		settings, err := WorkspaceSettingsForUser(root, workspaceID, actor.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
		return
	}
	if actor != nil && actor.Role == "admin" && containsModelSettings(req.Settings) {
		modelPatch, sharedPatch := splitModelSettings(req.Settings)
		if len(sharedPatch) > 0 {
			if _, err := SaveWorkspaceSettings(root, SettingsPatchRequest{Scope: req.Scope, Settings: sharedPatch}); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		if err := savePersonalModelSettings(actor.ID, req.Scope, workspaceID, modelPatch); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		settings, err := WorkspaceSettingsForUser(root, workspaceID, actor.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
		return
	}
	settings, err := SaveWorkspaceSettings(root, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func writeRuntimeModelError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeStoreError(w, err)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}

func containsModelSettings(settings map[string]any) bool {
	for key := range settings {
		if _, ok := modelSettingKeys[key]; ok {
			return true
		}
	}
	return false
}

func splitModelSettings(settings map[string]any) (map[string]any, map[string]any) {
	modelSettings := map[string]any{}
	sharedSettings := map[string]any{}
	for key, value := range settings {
		if _, ok := modelSettingKeys[key]; ok {
			modelSettings[key] = value
		} else {
			sharedSettings[key] = value
		}
	}
	return modelSettings, sharedSettings
}
