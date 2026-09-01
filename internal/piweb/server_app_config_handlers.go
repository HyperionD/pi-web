package piweb

import (
	"errors"
	"net/http"
)

func (s *Server) appConfig(w http.ResponseWriter, _ *http.Request) {
	response, err := LoadAppConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
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
	response, err := SaveAppConfig(patch)
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
