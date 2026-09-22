package piweb

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	promptPresetIDLimit    = 80
	promptPresetLabelLimit = 120
	promptPresetSkillLimit = 160
	promptPresetTextLimit  = 32 * 1024
)

var errPromptPresetValidation = errors.New("prompt preset validation error")
var errPromptPresetNotFound = errors.New("prompt preset not found")
var errPromptPresetConflict = errors.New("prompt preset was modified concurrently")
var errPromptPresetAdminRequired = errors.New("administrator access required")

type PromptPreset struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Skill     string `json:"skill"`
	Prompt    string `json:"prompt"`
	Enabled   bool   `json:"enabled"`
	Order     int    `json:"order"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type promptPresetWriteRequest struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Skill   string `json:"skill"`
	Prompt  string `json:"prompt"`
	Enabled *bool  `json:"enabled"`
	Order   *int   `json:"order"`
}

type promptPresetPatchRequest struct {
	Label   *string `json:"label"`
	Skill   *string `json:"skill"`
	Prompt  *string `json:"prompt"`
	Enabled *bool   `json:"enabled"`
	Order   *int    `json:"order"`
}

func (s *Server) promptPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		presets, err := listPromptPresets()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"presets": presets})
		return
	}
	if err := s.requirePromptPresetAdmin(r); err != nil {
		writePromptPresetError(w, err)
		return
	}
	var request promptPresetWriteRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	preset, err := createPromptPreset(request)
	if err != nil {
		writePromptPresetError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"preset": preset})
}

func (s *Server) promptPreset(w http.ResponseWriter, r *http.Request) {
	if err := s.requirePromptPresetAdmin(r); err != nil {
		writePromptPresetError(w, err)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	switch r.Method {
	case http.MethodPatch:
		var request promptPresetPatchRequest
		if err := readJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		preset, err := updatePromptPreset(id, request)
		if err != nil {
			writePromptPresetError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"preset": preset})
	case http.MethodDelete:
		if err := deletePromptPreset(id); err != nil {
			writePromptPresetError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) requirePromptPresetAdmin(r *http.Request) error {
	if s.config.UserManagement {
		user, ok := requestAccount(r)
		if !ok || user.Role != "admin" {
			return errPromptPresetAdminRequired
		}
	}
	return s.requireAdminPassword(r)
}

func writePromptPresetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPromptPresetValidation):
		writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, errPromptPresetNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, errPromptPresetConflict):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, errPromptPresetAdminRequired):
		writeError(w, http.StatusForbidden, err)
	case errors.Is(err, ErrAdminPasswordInvalid), errors.Is(err, ErrAdminPasswordRequired):
		writeError(w, http.StatusUnauthorized, errors.New("admin password is invalid or missing"))
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

func listPromptPresets() ([]PromptPreset, error) {
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
SELECT id, label, skill, prompt, enabled, sort_order, created_at, updated_at
FROM prompt_presets
ORDER BY sort_order ASC, updated_at ASC, id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	presets := make([]PromptPreset, 0)
	for rows.Next() {
		preset, err := scanPromptPreset(rows)
		if err != nil {
			return nil, err
		}
		presets = append(presets, preset)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return presets, nil
}

func createPromptPreset(request promptPresetWriteRequest) (PromptPreset, error) {
	request, err := normalizePromptPresetWrite(request)
	if err != nil {
		return PromptPreset{}, err
	}
	if request.ID == "" {
		request.ID, err = newPromptPresetID()
		if err != nil {
			return PromptPreset{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		return PromptPreset{}, err
	}
	defer db.Close()
	_, err = db.Exec(`
INSERT INTO prompt_presets(id, label, skill, prompt, enabled, sort_order, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)
`, request.ID, request.Label, request.Skill, request.Prompt,
		boolToSQLite(request.EnabledValue()), request.OrderValue(), now, now)
	if err != nil {
		if isPromptPresetUniqueError(err) {
			return PromptPreset{}, fmt.Errorf("%w: id already exists", errPromptPresetConflict)
		}
		return PromptPreset{}, fmt.Errorf("create prompt preset: %w", err)
	}
	return getPromptPreset(db, request.ID)
}

func updatePromptPreset(id string, patch promptPresetPatchRequest) (PromptPreset, error) {
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		return PromptPreset{}, err
	}
	defer db.Close()
	current, err := getPromptPreset(db, id)
	if err != nil {
		return PromptPreset{}, err
	}
	request := promptPresetWriteRequest{
		ID: current.ID, Label: current.Label, Skill: current.Skill, Prompt: current.Prompt,
		Enabled: promptPresetBoolPointer(current.Enabled), Order: promptPresetIntPointer(current.Order),
	}
	if patch.Label != nil {
		request.Label = *patch.Label
	}
	if patch.Skill != nil {
		request.Skill = *patch.Skill
	}
	if patch.Prompt != nil {
		request.Prompt = *patch.Prompt
	}
	if patch.Enabled != nil {
		request.Enabled = patch.Enabled
	}
	if patch.Order != nil {
		request.Order = patch.Order
	}
	request, err = normalizePromptPresetWrite(request)
	if err != nil {
		return PromptPreset{}, err
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`
UPDATE prompt_presets
SET label=?, skill=?, prompt=?, enabled=?, sort_order=?, updated_at=?
WHERE id=? AND updated_at=?
`, request.Label, request.Skill, request.Prompt,
		boolToSQLite(request.EnabledValue()), request.OrderValue(), updatedAt, id, current.UpdatedAt)
	if err != nil {
		return PromptPreset{}, fmt.Errorf("update prompt preset: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return PromptPreset{}, err
	}
	if count == 0 {
		_, lookupErr := getPromptPreset(db, id)
		if errors.Is(lookupErr, errPromptPresetNotFound) {
			return PromptPreset{}, errPromptPresetNotFound
		}
		if lookupErr != nil {
			return PromptPreset{}, lookupErr
		}
		return PromptPreset{}, errPromptPresetConflict
	}
	return getPromptPreset(db, id)
}

func isPromptPresetUniqueError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed: prompt_presets.id")
}

func deletePromptPreset(id string) error {
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := db.Exec(`DELETE FROM prompt_presets WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete prompt preset: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errPromptPresetNotFound
	}
	return nil
}

func getPromptPreset(db *sql.DB, id string) (PromptPreset, error) {
	row := db.QueryRow(`
SELECT id, label, skill, prompt, enabled, sort_order, created_at, updated_at
FROM prompt_presets WHERE id=?
`, id)
	preset, err := scanPromptPreset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PromptPreset{}, errPromptPresetNotFound
	}
	return preset, err
}

func scanPromptPreset(scanner interface{ Scan(...any) error }) (PromptPreset, error) {
	var preset PromptPreset
	var enabled int
	if err := scanner.Scan(
		&preset.ID, &preset.Label, &preset.Skill, &preset.Prompt, &enabled,
		&preset.Order, &preset.CreatedAt, &preset.UpdatedAt,
	); err != nil {
		return PromptPreset{}, err
	}
	preset.Enabled = enabled != 0
	return preset, nil
}

func normalizePromptPresetWrite(request promptPresetWriteRequest) (promptPresetWriteRequest, error) {
	request.ID = strings.TrimSpace(request.ID)
	request.Label = strings.TrimSpace(request.Label)
	request.Skill = strings.TrimSpace(request.Skill)
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.ID != "" && !validPromptPresetText(request.ID, promptPresetIDLimit) {
		return request, fmt.Errorf("%w: invalid id", errPromptPresetValidation)
	}
	if !validPromptPresetText(request.Label, promptPresetLabelLimit) {
		return request, fmt.Errorf(
			"%w: label is required and must be at most %d characters",
			errPromptPresetValidation, promptPresetLabelLimit,
		)
	}
	if request.Skill != "" && !validPromptPresetText(request.Skill, promptPresetSkillLimit) {
		return request, fmt.Errorf(
			"%w: skill must be at most %d characters",
			errPromptPresetValidation, promptPresetSkillLimit,
		)
	}
	if !validPromptPresetText(request.Prompt, promptPresetTextLimit) {
		return request, fmt.Errorf(
			"%w: prompt is required and must be at most %d characters",
			errPromptPresetValidation, promptPresetTextLimit,
		)
	}
	if request.OrderValue() < 0 {
		return request, fmt.Errorf("%w: order must not be negative", errPromptPresetValidation)
	}
	return request, nil
}

func validPromptPresetText(value string, limit int) bool {
	if value == "" || !utf8.ValidString(value) || len([]rune(value)) > limit {
		return false
	}
	for _, char := range value {
		if char == 0 {
			return false
		}
	}
	return true
}

func (request promptPresetWriteRequest) EnabledValue() bool {
	return request.Enabled == nil || *request.Enabled
}

func (request promptPresetWriteRequest) OrderValue() int {
	if request.Order == nil {
		return 0
	}
	return *request.Order
}

func newPromptPresetID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate prompt preset id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func boolToSQLite(value bool) int {
	if value {
		return 1
	}
	return 0
}

func promptPresetBoolPointer(value bool) *bool {
	return &value
}

func promptPresetIntPointer(value int) *int {
	return &value
}
