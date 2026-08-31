package piweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const DefaultAppBrand = "pi.dev / web"

type AppConfigResponse struct {
	Config map[string]any `json:"config"`
	Path   string         `json:"path"`
}

func AppConfigPath() (string, error) {
	return appConfigPath(DefaultPiSessionDir())
}

func appConfigPath(sessionDir string) (string, error) {
	if sessionDir == "" {
		return "", errors.New("session dir is empty")
	}
	return filepath.Join(filepath.Dir(sessionDir), "pi-web-config.json"), nil
}

func LoadAppConfig() (AppConfigResponse, error) {
	path, err := AppConfigPath()
	if err != nil {
		return AppConfigResponse{}, err
	}
	stored, err := readAppConfig(path)
	if err != nil {
		return AppConfigResponse{}, err
	}
	return appConfigResponse(path, stored), nil
}

func SaveAppConfig(patch map[string]any) (AppConfigResponse, error) {
	path, err := AppConfigPath()
	if err != nil {
		return AppConfigResponse{}, err
	}
	stored, err := readAppConfig(path)
	if err != nil {
		return AppConfigResponse{}, err
	}
	if err := applyAppConfigPatch(stored, patch); err != nil {
		return AppConfigResponse{}, err
	}
	if _, ok := stored["version"]; !ok {
		stored["version"] = float64(1)
	}
	if err := writeAppConfig(path, stored); err != nil {
		return AppConfigResponse{}, err
	}
	return appConfigResponse(path, stored), nil
}

func appConfigResponse(path string, stored map[string]any) AppConfigResponse {
	effective := defaultAppConfig()
	mergeAppConfig(effective, stored)
	return AppConfigResponse{Config: effective, Path: path}
}

func defaultAppConfig() map[string]any {
	return map[string]any{
		"version": float64(1),
		"brand":   DefaultAppBrand,
	}
}

func readAppConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read app config %q: %w", path, err)
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var stored map[string]any
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("read app config %q: %w", path, err)
	}
	if stored == nil {
		return map[string]any{}, nil
	}
	if err := normalizeStoredAppConfig(stored); err != nil {
		return nil, fmt.Errorf("read app config %q: %w", path, err)
	}
	return stored, nil
}

func normalizeStoredAppConfig(stored map[string]any) error {
	value, ok := stored["brand"]
	if !ok {
		return nil
	}
	brand, err := normalizedAppBrand(value)
	if err != nil {
		return err
	}
	if brand == "" {
		delete(stored, "brand")
	} else {
		stored["brand"] = brand
	}
	return nil
}

func applyAppConfigPatch(stored, patch map[string]any) error {
	for key, value := range patch {
		if key == "brand" {
			brand, err := normalizedAppBrand(value)
			if err != nil {
				return err
			}
			if brand == "" {
				delete(stored, key)
			} else {
				stored[key] = brand
			}
			continue
		}
		if value == nil {
			delete(stored, key)
			continue
		}
		stored[key] = cloneAppConfigValue(value)
	}
	return nil
}

func normalizedAppBrand(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	brand, ok := value.(string)
	if !ok {
		return "", errors.New("app config brand must be a string or null")
	}
	brand = strings.TrimSpace(brand)
	if utf8.RuneCountInString(brand) > 120 {
		return "", errors.New("app config brand must be at most 120 characters")
	}
	for _, r := range brand {
		if unicode.IsControl(r) {
			return "", errors.New("app config brand must not contain control characters")
		}
	}
	return brand, nil
}

func writeAppConfig(path string, stored map[string]any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".pi-web-config-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(stored); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func mergeAppConfig(dst, src map[string]any) {
	for key, value := range src {
		dst[key] = cloneAppConfigValue(value)
	}
}

func cloneAppConfigValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(value))
		for key, nested := range value {
			clone[key] = cloneAppConfigValue(nested)
		}
		return clone
	case []any:
		clone := make([]any, len(value))
		for index, nested := range value {
			clone[index] = cloneAppConfigValue(nested)
		}
		return clone
	default:
		return value
	}
}
