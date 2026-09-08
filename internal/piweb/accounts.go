package piweb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type Account struct {
	ID         string   `json:"id"`
	Username   string   `json:"username"`
	Role       string   `json:"role"`
	Workspaces []string `json:"workspaces"`
}

type accountRecord struct {
	Account
	PasswordHash string            `json:"passwordHash"`
	Paths        map[string]string `json:"paths"`
}

type accountLogin struct {
	User    Account
	Expires time.Time
	Context context.Context
	Cancel  context.CancelFunc
}

type accountStore struct {
	mu       sync.Mutex
	path     string
	err      error
	Users    map[string]accountRecord `json:"users"`
	Owners   map[string]string        `json:"owners"`
	logins   map[[32]byte]accountLogin
	attempts map[string][]time.Time
}

var accountName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
var errAccountDenied = errors.New("account or resource is not accessible")

func newAccountStore() *accountStore {
	a := &accountStore{Users: map[string]accountRecord{}, Owners: map[string]string{},
		logins: map[[32]byte]accountLogin{}, attempts: map[string][]time.Time{}}
	configPath, err := AppConfigPath()
	if err != nil {
		a.err = err
		return a
	}
	a.path = filepath.Join(filepath.Dir(configPath), "pi-web-users.json")
	data, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a
	}
	if err == nil {
		err = json.Unmarshal(data, a)
	}
	if err == nil && (a.Users == nil || a.Owners == nil) {
		err = errors.New("invalid account database")
	}
	a.err = err
	return a
}

func (a *accountStore) save(users map[string]accountRecord, owners map[string]string) error {
	if a.err != nil {
		return a.err
	}
	if err := writeAppConfig(a.path, map[string]any{"users": users, "owners": owners}); err != nil {
		return err
	}
	a.Users, a.Owners = users, owners
	return nil
}

func accountToken() string {
	// crypto/rand.Text uses the operating system CSPRNG and cannot return a partial token.
	return rand.Text() + rand.Text()
}

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
	if !ok {
		return accountLogin{}, false
	}
	if time.Now().After(login.Expires) {
		login.Cancel()
		delete(a.logins, key)
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
	owners := maps.Clone(a.Owners)
	owners[sessionID] = userID
	return a.save(a.Users, owners)
}

func (a *accountStore) owns(user Account, sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	owner := a.Owners[sessionID]
	return owner == user.ID || (owner == "" && user.Role == "admin")
}

func (a *accountStore) allows(user Account, workspaceID, path string) bool {
	if user.Role == "admin" {
		return true
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.Users[user.Username]
	return ok && record.ID == user.ID && record.Paths[workspaceID] == realPath
}

func (a *accountStore) permitLogin(address string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for key, attempts := range a.attempts {
		kept := attempts[:0]
		for _, attempt := range attempts {
			if now.Sub(attempt) < time.Minute {
				kept = append(kept, attempt)
			}
		}
		if len(kept) == 0 {
			delete(a.attempts, key)
		} else {
			a.attempts[key] = kept
		}
	}
	if len(a.attempts[address]) >= 10 || len(a.attempts) >= 10000 {
		return false
	}
	a.attempts[address] = append(a.attempts[address], now)
	return true
}

func accountID() string {
	sum := sha256.Sum256([]byte(accountToken()))
	return hex.EncodeToString(sum[:16])
}
