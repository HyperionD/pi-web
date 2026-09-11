package piweb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	personalSettingsScope = "personal"
	personalGlobalScope   = "global"
	personalProjectScope  = "project"
)

var modelSettingKeys = map[string]struct{}{
	"defaultProvider":      {},
	"defaultModel":         {},
	"defaultThinkingLevel": {},
}

var thinkingLevels = map[string]struct{}{
	"off": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {},
}

type ModelStartupSettings struct {
	Provider      string
	Model         string
	ThinkingLevel string
}

func validatePersonalModelPatch(patch map[string]any) error {
	for key, value := range patch {
		if _, ok := modelSettingKeys[key]; !ok {
			return fmt.Errorf("personal settings only support model fields")
		}
		if value == nil {
			continue
		}
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" || len(text) > 256 {
			return fmt.Errorf("%s must be a non-empty string", key)
		}
		if key == "defaultThinkingLevel" {
			if _, ok := thinkingLevels[text]; !ok {
				return fmt.Errorf("invalid thinking level %q", text)
			}
		}
	}
	return nil
}

func personalSettingsKey(scope, workspaceID string) (string, error) {
	switch scope {
	case personalGlobalScope:
		return "", nil
	case personalProjectScope:
		if strings.TrimSpace(workspaceID) == "" {
			return "", errors.New("workspace is required for personal project settings")
		}
		return workspaceID, nil
	default:
		return "", errors.New("personal settings location must be global or project")
	}
}

func readPersonalModelSettings(userID, scope, workspaceID string) (map[string]any, error) {
	key, err := personalSettingsKey(scope, workspaceID)
	if err != nil {
		return nil, err
	}
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var encoded string
	err = db.QueryRow(`SELECT settings_json FROM user_model_settings WHERE user_id=? AND scope=? AND workspace_id=?`, userID, scope, key).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodePersonalSettings(encoded)
}

func savePersonalModelSettings(userID, scope, workspaceID string, patch map[string]any) error {
	if strings.TrimSpace(userID) == "" {
		return errors.New("user is required")
	}
	if err := validatePersonalModelPatch(patch); err != nil {
		return err
	}
	key, err := personalSettingsKey(scope, workspaceID)
	if err != nil {
		return err
	}
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	current, err := readPersonalModelSettingsTx(tx, userID, scope, key)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	applySettingsPatch(current, patch)
	if len(current) == 0 {
		_, err = tx.Exec(`DELETE FROM user_model_settings WHERE user_id=? AND scope=? AND workspace_id=?`, userID, scope, key)
	} else {
		encoded, encodeErr := json.Marshal(current)
		if encodeErr != nil {
			_ = tx.Rollback()
			return encodeErr
		}
		_, err = tx.Exec(`INSERT INTO user_model_settings(user_id,scope,workspace_id,settings_json,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(user_id,scope,workspace_id) DO UPDATE SET settings_json=excluded.settings_json, updated_at=excluded.updated_at`, userID, scope, key, encoded, time.Now().UTC().Format(time.RFC3339Nano))
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func readPersonalModelSettingsTx(tx *sql.Tx, userID, scope, workspaceID string) (map[string]any, error) {
	var encoded string
	err := tx.QueryRow(`SELECT settings_json FROM user_model_settings WHERE user_id=? AND scope=? AND workspace_id=?`, userID, scope, workspaceID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodePersonalSettings(encoded)
}

func decodePersonalSettings(encoded string) (map[string]any, error) {
	var settings map[string]any
	if err := json.Unmarshal([]byte(encoded), &settings); err != nil {
		return nil, err
	}
	if settings == nil {
		return map[string]any{}, nil
	}
	return settings, nil
}

func deletePersonalModelSettings(userID string) error {
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DELETE FROM user_model_settings WHERE user_id=?`, userID)
	return err
}

func modelStartupSettings(effective map[string]any) ModelStartupSettings {
	settings := ModelStartupSettings{}
	settings.Provider, _ = effective["defaultProvider"].(string)
	settings.Model, _ = effective["defaultModel"].(string)
	settings.ThinkingLevel, _ = effective["defaultThinkingLevel"].(string)
	return settings
}
