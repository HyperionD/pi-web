package piweb

import (
	"errors"
	"net/http"
)

func (s *Server) appConfig(w http.ResponseWriter, r *http.Request) {
	load := LoadAppConfig
	if s.config.UserManagement {
		load = databaseAppConfig
	}
	response, err := load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if user, ok := requestAccount(r); ok && user.Role != "admin" {
		workspace, _ := response.Config["workspace"].(map[string]any)
		memberWorkspace := map[string]any{}
		if value, exists := workspace["maxUploadBytes"]; exists && value != nil {
			memberWorkspace["maxUploadBytes"] = value
		}
		response = AppConfigResponse{Config: map[string]any{
			"version":   response.Config["version"],
			"brand":     response.Config["brand"],
			"workspace": memberWorkspace,
		}}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) patchAppConfig(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if err := readJSON(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if patch == nil {
		writeError(w, http.StatusBadRequest, errors.New("app config patch must be a JSON object"))
		return
	}
	if len(patch) > 0 {
		if err := s.requireAdminPassword(r); err != nil {
			if errors.Is(err, ErrAdminPasswordInvalid) || errors.Is(err, ErrAdminPasswordRequired) {
				writeError(w, http.StatusUnauthorized, errors.New("admin password is invalid or missing"))
			} else {
				writeError(w, http.StatusInternalServerError, err)
			}
			return
		}
	}
	save := SaveAppConfig
	if s.config.UserManagement {
		save = saveDatabaseAppConfig
	}
	response, err := save(patch)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrAppConfigValidation) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}
