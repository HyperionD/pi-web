package piweb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

const databaseAdminPasswordKey = "adminPasswordHash"

func databaseSetting(key string) (string, bool, error) {
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		return "", false, err
	}
	defer db.Close()
	var value string
	err = db.QueryRow(`SELECT value FROM app_settings WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}
func saveDatabaseSettings(values map[string]string) error {
	db, err := openWebDB(DefaultWebDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for key, value := range values {
		if _, err = tx.Exec(`INSERT INTO app_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
func databaseAdminPasswordConfigured() (bool, error) {
	_, ok, err := databaseSetting(databaseAdminPasswordKey)
	return ok, err
}
func setupDatabaseAdminPassword(password string) error {
	if err := validateAdminPassword(password); err != nil {
		return err
	}
	configured, err := databaseAdminPasswordConfigured()
	if err != nil {
		return err
	}
	if configured {
		return ErrAdminPasswordAlreadyConfigured
	}
	hash, err := hashAdminPassword(password)
	if err != nil {
		return err
	}
	return saveDatabaseSettings(map[string]string{databaseAdminPasswordKey: hash})
}
func verifyDatabaseAdminPassword(password string) error {
	encoded, ok, err := databaseSetting(databaseAdminPasswordKey)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAdminPasswordRequired
	}
	if !verifyAdminPasswordHash(password, encoded) {
		return ErrAdminPasswordInvalid
	}
	return nil
}
func databaseAppConfig() (AppConfigResponse, error) {
	brand, _, err := databaseSetting("brand")
	if err != nil {
		return AppConfigResponse{}, err
	}
	upload, _, err := databaseSetting("maxUploadBytes")
	if err != nil {
		return AppConfigResponse{}, err
	}
	config := map[string]any{"version": float64(1), "brand": DefaultAppBrand, "workspace": map[string]any{}}
	if strings.TrimSpace(brand) != "" {
		config["brand"] = brand
	}
	if upload != "" {
		var value int64
		if err := json.Unmarshal([]byte(upload), &value); err == nil {
			config["workspace"].(map[string]any)["maxUploadBytes"] = value
		}
	}
	return AppConfigResponse{Config: config, Path: DefaultWebDBPath()}, nil
}
func saveDatabaseAppConfig(patch map[string]any) (AppConfigResponse, error) {
	values := map[string]string{}
	if brand, ok := patch["brand"].(string); ok {
		values["brand"] = strings.TrimSpace(brand)
	}
	if workspace, ok := patch["workspace"].(map[string]any); ok {
		if value, exists := workspace["maxUploadBytes"]; exists {
			normalized, err := normalizeMaxUploadBytes(value)
			if err != nil {
				return AppConfigResponse{}, err
			}
			encoded, _ := json.Marshal(normalized)
			values["maxUploadBytes"] = string(encoded)
		}
	}
	if err := saveDatabaseSettings(values); err != nil {
		return AppConfigResponse{}, err
	}
	return databaseAppConfig()
}
