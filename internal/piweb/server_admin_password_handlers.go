package piweb

import (
	"errors"
	"net/http"
)

func (s *Server) adminPasswordStatus(w http.ResponseWriter, _ *http.Request) {
	if s.config.UserManagement {
		configured, err := databaseAdminPasswordConfigured()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, AdminPasswordStatusResponse{Configured: configured})
		return
	}
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
	setup := SetupAdminPassword
	if s.config.UserManagement {
		setup = setupDatabaseAdminPassword
	}
	if err := setup(req.Password); err != nil {
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
	if s.config.UserManagement {
		if err := s.accounts.ensureAdmin(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.adminPasswordStateMu.Lock()
	s.adminPasswordConfigured = true
	s.adminPasswordCheckError = nil
	s.adminPasswordStateMu.Unlock()
	writeJSON(w, http.StatusCreated, AdminPasswordStatusResponse{Configured: true})
}

func (s *Server) requireAdminPassword(r *http.Request) error {
	if s.config.UserManagement {
		if user, ok := requestAccount(r); ok && user.Role == "admin" {
			return nil
		}
		return verifyDatabaseAdminPassword(r.Header.Get(adminPasswordHeader))
	}
	return VerifyAdminPassword(r.Header.Get(adminPasswordHeader))
}
