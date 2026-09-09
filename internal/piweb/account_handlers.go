package piweb

import (
	"crypto/sha256"
	"errors"
	"net"
	"net/http"
	"time"
)

func (s *Server) accountRoutes() {
	s.mux.HandleFunc("GET /api/account/status", s.accountStatus)
	s.mux.HandleFunc("GET /api/account/me", s.currentAccount)
	s.mux.HandleFunc("POST /api/account/login", s.loginAccount)
	s.mux.HandleFunc("POST /api/account/logout", s.logoutAccount)
	s.mux.HandleFunc("GET /api/admin/users", s.listAccounts)
	s.mux.HandleFunc("GET /api/admin/users/import-template", s.userImportTemplate)
	s.mux.HandleFunc("POST /api/admin/users/import", s.importAccounts)
	s.mux.HandleFunc("POST /api/admin/users", s.createAccount)
	s.mux.HandleFunc("POST /api/admin/users/batch-workspaces", s.batchAssignWorkspaces)
	s.mux.HandleFunc("PATCH /api/admin/users/{username}", s.updateAccount)
	s.mux.HandleFunc("DELETE /api/admin/users/{username}", s.deleteAccount)
}

func (s *Server) accountStatus(w http.ResponseWriter, _ *http.Request) {
	configured, err := AdminPasswordConfigured()
	if s.config.UserManagement {
		configured, err = databaseAdminPasswordConfigured()
	}
	if err != nil {
		writeError(w, 503, err)
		return
	}
	writeJSON(w, 200, map[string]any{"configured": configured})
}

func (s *Server) currentAccount(w http.ResponseWriter, r *http.Request) {
	user, _ := requestAccount(r)
	writeJSON(w, 200, map[string]any{"user": user})
}

func (s *Server) loginAccount(w http.ResponseWriter, r *http.Request) {
	address, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		address = r.RemoteAddr
	}
	if !s.accounts.permitLogin(address) {
		writeError(w, 429, errors.New("too many login attempts; retry in one minute"))
		return
	}
	var req struct {
		Account  string `json:"account"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSONWithLimit(r, &req, 4096); err != nil {
		writeError(w, 400, err)
		return
	}
	if req.Account == "" {
		req.Account = req.Username
	}
	if !accountName.MatchString(req.Account) || validateAdminPassword(req.Password) != nil {
		writeError(w, 401, errors.New("invalid username or password"))
		return
	}
	// Serialize credential verification and token issuance with account updates/revocation.
	s.accounts.mu.Lock()
	user, valid := s.verifyAccount(req.Account, req.Password)
	if !valid {
		s.accounts.mu.Unlock()
		writeError(w, 401, errors.New("invalid username or password"))
		return
	}
	token := s.accounts.loginLocked(user)
	s.accounts.mu.Unlock()
	setAccountCookie(w, r, token, 12*60*60)
	writeJSON(w, 200, map[string]any{"user": user})
}

func (s *Server) verifyAccount(account, password string) (Account, bool) {
	if account == "admin" {
		return Account{ID: "admin", Account: "admin", Username: "管理员", Role: "admin", Workspaces: []string{}}, verifyDatabaseAdminPassword(password) == nil
	}
	record, exists := s.accounts.Users[account]
	if !exists {
		// Unknown users still perform the same expensive password derivation.
		deriveAdminPasswordKey(password, make([]byte, adminPasswordSaltLen), adminPasswordRounds, adminPasswordKeyLen)
		return Account{}, false
	}
	if record.Disabled {
		return Account{}, false
	}
	return record.Account, verifyAdminPasswordHash(password, record.PasswordHash)
}

func (s *Server) logoutAccount(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(accountCookieName); err == nil {
		key := sha256.Sum256([]byte(cookie.Value))
		s.accounts.mu.Lock()
		if login, ok := s.accounts.logins[key]; ok {
			login.Cancel()
			delete(s.accounts.logins, key)
		}
		s.accounts.mu.Unlock()
	}
	setAccountCookie(w, r, "", -1)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func setAccountCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	cookie := &http.Cookie{Name: accountCookieName, Value: value, Path: "/", HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode, MaxAge: maxAge}
	if maxAge < 0 {
		cookie.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, cookie)
}
