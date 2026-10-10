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
	commands := []SlashCommand{}
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
		models, err = configuredModels(r.Context(), root)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
	}
	if actor := requestActor(r); actor != nil && actor.Role == "admin" {
		path, pathErr := modelConfigPath()
		if pathErr != nil {
			writeError(w, http.StatusInternalServerError, pathErr)
			return
		}
		count := 0
		for _, provider := range models.Providers {
			count += len(provider.Models)
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": models.Providers, "path": path, "count": count})
		return
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
	settings, err := s.accountWorkspaceSettings(root, workspaceID, userID)
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
			location = personalGlobalScope
		}
		if location != personalGlobalScope {
			writeError(w, http.StatusBadRequest, errors.New("personal project model settings are no longer supported"))
			return
		}
		catalog := fallbackWorkspaceModels(root, nil)
		if s.config.EnablePiExecution {
			catalog, err = configuredModels(r.Context(), root)
			if err != nil {
				writeError(w, http.StatusBadGateway, err)
				return
			}
		}
		if err := validateAccountModelPatch(req.Settings, catalog); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := savePersonalModelSettings(actor.ID, location, workspaceID, req.Settings); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		settings, err := s.accountWorkspaceSettings(root, workspaceID, actor.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
		return
	}
	if containsModelSettings(req.Settings) {
		writeError(w, http.StatusBadRequest, errors.New("model defaults are personal; configure the catalog in models.json"))
		return
	}
	settings, err := SaveWorkspaceSettings(root, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if actor != nil {
		settings, err = s.accountWorkspaceSettings(root, workspaceID, actor.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
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
