package piweb

import (
	"errors"
	"net/http"
)

func (s *Server) adminPasswordStatus(w http.ResponseWriter, _ *http.Request) {
	s.adminPasswordStateMu.RLock()
	defer s.adminPasswordStateMu.RUnlock()
	if s.adminPasswordCheckError != nil {
		writeError(w, http.StatusInternalServerError, s.adminPasswordCheckError)
		return
	}
	writeJSON(w, http.StatusOK, AdminPasswordStatusResponse{Configured: s.adminPasswordConfigured})
}

func (s *Server) setupAdminPassword(w http.ResponseWriter, r *http.Request) {
	var req AdminPasswordSetupRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Password != req.Confirmation {
		writeError(w, http.StatusBadRequest, errors.New("admin password confirmation does not match"))
		return
	}
	if err := SetupAdminPassword(req.Password); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrAdminPasswordAlreadyConfigured):
			status = http.StatusConflict
		case errors.Is(err, ErrAdminPasswordValidation):
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	s.adminPasswordStateMu.Lock()
	s.adminPasswordConfigured = true
	s.adminPasswordCheckError = nil
	s.adminPasswordStateMu.Unlock()
	writeJSON(w, http.StatusCreated, AdminPasswordStatusResponse{Configured: true})
}

func (s *Server) requireAdminPassword(r *http.Request) error {
	return VerifyAdminPassword(r.Header.Get(adminPasswordHeader))
}
