package piweb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"maps"
	"regexp"
	"sync"
	"time"
)

type Account struct {
	ID         string   `json:"id"`
	Account    string   `json:"account"`
	Username   string   `json:"username"`
	Role       string   `json:"role"`
	Disabled   bool     `json:"disabled"`
	Workspaces []string `json:"workspaces"`
}
type accountRecord struct {
	Account
	PasswordHash string            `json:"passwordHash"`
	Paths        map[string]string `json:"paths"`
	workspaceSet bool
	disabledSet  bool
}
type accountLogin struct {
	User    Account
	Expires time.Time
	Context context.Context
	Cancel  context.CancelFunc
}
type accountStore struct {
	mu       sync.Mutex
	dbPath   string
	dbErr    error
	Users    map[string]accountRecord
	Owners   map[string]string
	logins   map[[32]byte]accountLogin
	attempts map[string][]time.Time
}

var accountName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
var errAccountDenied = errors.New("account or resource is not accessible")

func newAccountStore() *accountStore {
	a := &accountStore{Users: map[string]accountRecord{}, Owners: map[string]string{}, logins: map[[32]byte]accountLogin{}, attempts: map[string][]time.Time{}}
	a.dbPath = DefaultWebDBPath()
	db, err := openWebDB(a.dbPath)
	if err != nil {
		a.dbErr = err
		return a
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, account, username, role, password_hash, disabled FROM users`)
	if err != nil {
		a.dbErr = err
		return a
	}
	defer rows.Close()
	for rows.Next() {
		var r accountRecord
		if err := rows.Scan(&r.ID, &r.Account.Account, &r.Username, &r.Role, &r.PasswordHash, &r.Disabled); err != nil {
			a.dbErr = err
			return a
		}
		r.Workspaces = []string{}
		r.Paths = map[string]string{}
		a.Users[r.Account.Account] = r
	}
	if err := rows.Err(); err != nil {
		a.dbErr = err
		return a
	}
	for _, r := range a.Users {
		a.loadWorkspaces(db, r)
	}
	owners, err := db.Query(`SELECT session_id, user_id FROM session_owners`)
	if err != nil {
		a.dbErr = err
		return a
	}
	defer owners.Close()
	for owners.Next() {
		var session, user string
		if err := owners.Scan(&session, &user); err != nil {
			a.dbErr = err
			return a
		}
		a.Owners[session] = user
	}
	if err := owners.Err(); err != nil {
		a.dbErr = err
		return a
	}
	return a
}
func (a *accountStore) loadWorkspaces(db *sql.DB, record accountRecord) {
	rows, err := db.Query(`SELECT workspace_id, workspace_path FROM user_workspaces WHERE user_id = ?`, record.ID)
	if err != nil {
		a.dbErr = err
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, path string
		if rows.Scan(&id, &path) == nil {
			record.Workspaces = append(record.Workspaces, id)
			record.Paths[id] = path
		}
	}
	a.Users[record.Account.Account] = record
}
func accountToken() string { return rand.Text() + rand.Text() }
func (a *accountStore) loginLocked(user Account) string {
	token := accountToken()
	ctx, cancel := context.WithCancel(context.Background())
	for key, login := range a.logins {
		if time.Now().After(login.Expires) {
			login.Cancel()
			delete(a.logins, key)
		}
	}
	a.logins[sha256.Sum256([]byte(token))] = accountLogin{user, time.Now().Add(12 * time.Hour), ctx, cancel}
	return token
}
func (a *accountStore) authenticate(token string) (accountLogin, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := sha256.Sum256([]byte(token))
	login, ok := a.logins[key]
	if !ok || time.Now().After(login.Expires) {
		if ok {
			login.Cancel()
			delete(a.logins, key)
		}
		return accountLogin{}, false
	}
	return login, true
}
func (a *accountStore) revoke(userID string) {
	for key, login := range a.logins {
		if login.User.ID == userID {
			login.Cancel()
			delete(a.logins, key)
		}
	}
}
func (a *accountStore) own(sessionID, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	db, err := openWebDB(a.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT OR REPLACE INTO session_owners(session_id, user_id) VALUES(?,?)`, sessionID, userID); err != nil {
		return err
	}
	a.Owners[sessionID] = userID
	return nil
}
func (a *accountStore) ensureAdmin() error {
	encoded, ok, err := databaseSetting(databaseAdminPasswordKey)
	if err != nil || !ok {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if existing, exists := a.Users["admin"]; exists {
		if len(existing.Workspaces) == 0 && len(existing.Paths) == 0 {
			return nil
		}
		users := maps.Clone(a.Users)
		existing.Workspaces = []string{}
		existing.Paths = map[string]string{}
		users["admin"] = existing
		return a.save(users, a.Owners)
	}
	record := accountRecord{Account: Account{ID: "admin", Account: "admin", Username: "管理员", Role: "admin", Workspaces: []string{}}, PasswordHash: encoded, Paths: map[string]string{}}
	users := map[string]accountRecord{"admin": record}
	for name, user := range a.Users {
		users[name] = user
	}
	return a.save(users, a.Owners)
}

func (a *accountStore) save(users map[string]accountRecord, owners map[string]string) error {
	for name, record := range users {
		if record.Role != "admin" {
			continue
		}
		record.Workspaces = []string{}
		record.Paths = map[string]string{}
		users[name] = record
	}
	db, err := openWebDB(a.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM users`); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, r := range users {
		if _, err = tx.Exec(`INSERT INTO users(id, account, username, role, password_hash, disabled, created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(account) DO UPDATE SET username=excluded.username, role=excluded.role, password_hash=excluded.password_hash, disabled=excluded.disabled`, r.ID, r.Account.Account, r.Username, r.Role, r.PasswordHash, r.Disabled, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.Exec(`DELETE FROM user_workspaces WHERE user_id=?`, r.ID); err != nil {
			_ = tx.Rollback()
			return err
		}
		for id, path := range r.Paths {
			if _, err = tx.Exec(`INSERT INTO user_workspaces(user_id, workspace_id, workspace_path) VALUES(?,?,?)`, r.ID, id, path); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	if err = saveSessionOwners(tx, users, owners); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.Users = users
	return nil
}

func saveSessionOwners(tx *sql.Tx, users map[string]accountRecord, owners map[string]string) error {
	activeUsers := make(map[string]bool, len(users))
	for _, user := range users {
		activeUsers[user.ID] = true
	}
	for sessionID, userID := range owners {
		if !activeUsers[userID] {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO session_owners(session_id, user_id) VALUES(?,?)`, sessionID, userID); err != nil {
			return err
		}
	}
	return nil
}

func (a *accountStore) owns(user Account, sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if user.Role == "admin" {
		return true
	}
	return a.Owners[sessionID] == user.ID
}
func (a *accountStore) workspaceGrants(user Account) ([]workspaceGrant, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.Users[user.Account]
	if !ok || record.ID != user.ID {
		return nil, false
	}
	grants := make([]workspaceGrant, 0, len(record.Workspaces))
	for _, id := range record.Workspaces {
		if path := record.Paths[id]; path != "" {
			grants = append(grants, workspaceGrant{ID: id, Path: path})
		}
	}
	return grants, true
}

func (a *accountStore) workspaceGrant(user Account, workspaceID string) (workspaceGrant, bool) {
	grants, ok := a.workspaceGrants(user)
	if !ok {
		return workspaceGrant{}, false
	}
	for _, grant := range grants {
		if grant.ID == workspaceID {
			return grant, true
		}
	}
	return workspaceGrant{}, false
}

func (a *accountStore) permitLogin(address string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	kept := a.attempts[address][:0]
	for _, t := range a.attempts[address] {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 10 {
		a.attempts[address] = kept
		return false
	}
	a.attempts[address] = append(kept, now)
	return true
}
func accountID() string {
	sum := sha256.Sum256([]byte(accountToken()))
	return hex.EncodeToString(sum[:16])
}
