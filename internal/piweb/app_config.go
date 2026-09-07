package piweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const DefaultAppBrand = "pi.dev / web"

const (
	MinMaxUploadBytes     = 1 << 20
	DefaultMaxUploadBytes = 100 << 20
	MaxMaxUploadBytes     = 2 << 30
)

var ErrAppConfigValidation = errors.New("app config validation error")
var appConfigMu sync.Mutex

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
	appConfigMu.Lock()
	defer appConfigMu.Unlock()

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

// LoadMaxUploadBytes returns the effective workspace upload limit for the current request.
func LoadMaxUploadBytes() (int64, error) {
	response, err := LoadAppConfig()
	if err != nil {
		return 0, err
	}
	workspace, ok := response.Config["workspace"].(map[string]any)
	if !ok {
		return int64(DefaultMaxUploadBytes), nil
	}
	value, ok := workspace["maxUploadBytes"]
	if !ok || value == nil {
		return int64(DefaultMaxUploadBytes), nil
	}
	number, err := normalizeMaxUploadBytes(value)
	if err != nil {
		return 0, err
	}
	return int64(number), nil
}

func SaveAppConfig(patch map[string]any) (AppConfigResponse, error) {
	appConfigMu.Lock()
	defer appConfigMu.Unlock()

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
	public := cloneAppConfigValue(stored).(map[string]any)
	delete(public, adminPasswordHashKey)
	effective := defaultAppConfig()
	mergeAppConfig(effective, public)
	return AppConfigResponse{Config: effective, Path: path}
}

func defaultAppConfig() map[string]any {
	return map[string]any{
		"version": float64(1),
		"brand":   DefaultAppBrand,
		"workspace": map[string]any{
			"maxUploadBytes": float64(DefaultMaxUploadBytes),
		},
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
	if value, ok := stored["brand"]; ok {
		brand, err := normalizedAppBrand(value)
		if err != nil {
			return err
		}
		if brand == "" {
			delete(stored, "brand")
		} else {
			stored["brand"] = brand
		}
	}

	value, ok := stored["workspace"]
	if !ok {
		return nil
	}
	workspace, err := normalizeStoredWorkspaceConfig(value)
	if err != nil {
		return err
	}
	if workspace == nil {
		delete(stored, "workspace")
	} else {
		stored["workspace"] = workspace
	}
	return nil
}

func normalizeStoredWorkspaceConfig(value any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	workspace, ok := value.(map[string]any)
	if !ok {
		return nil, appConfigValidationError("app config workspace must be an object or null")
	}
	normalized := make(map[string]any, len(workspace))
	for key, nested := range workspace {
		if key == "allowedRoots" {
			roots, err := normalizeAllowedRoots(nested)
			if err != nil {
				return nil, err
			}
			normalized[key] = roots
			continue
		}
		if key == "maxUploadBytes" {
			if nested == nil {
				continue
			}
			maxUploadBytes, err := normalizeMaxUploadBytes(nested)
			if err != nil {
				return nil, err
			}
			normalized[key] = maxUploadBytes
			continue
		}
		normalized[key] = cloneAppConfigValue(nested)
	}
	return normalized, nil
}

func normalizeAllowedRoots(value any) ([]any, error) {
	var values []any
	switch roots := value.(type) {
	case []any:
		values = roots
	case []string:
		values = make([]any, len(roots))
		for index, root := range roots {
			values[index] = root
		}
	default:
		return nil, appConfigValidationError("app config workspace.allowedRoots must be an array of strings")
	}

	normalized := make([]any, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		root, ok := value.(string)
		if !ok {
			return nil, appConfigValidationError(fmt.Sprintf("app config workspace.allowedRoots[%d] must be a string", index))
		}
		root = strings.TrimSpace(root)
		if root == "" {
			return nil, appConfigValidationError(fmt.Sprintf("app config workspace.allowedRoots[%d] must not be empty", index))
		}
		expanded, err := expandAppConfigPath(root)
		if err != nil {
			return nil, appConfigValidationError(fmt.Sprintf("app config workspace.allowedRoots[%d] %q: %v", index, root, err))
		}
		expanded = filepath.Clean(expanded)
		absolute, err := filepath.Abs(expanded)
		if err != nil {
			return nil, appConfigValidationf("app config workspace.allowedRoots[%d] %q: make absolute: %v", index, root, err)
		}
		realPath, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, appConfigValidationf("app config workspace.allowedRoots[%d] %q: resolve path: %v", index, root, err)
		}
		realPath = filepath.Clean(realPath)
		info, err := os.Stat(realPath)
		if err != nil {
			return nil, appConfigValidationf("app config workspace.allowedRoots[%d] %q: stat path: %v", index, root, err)
		}
		if !info.IsDir() {
			return nil, appConfigValidationf("app config workspace.allowedRoots[%d] %q: path is not a directory", index, root)
		}
		if _, ok := seen[realPath]; ok {
			continue
		}
		seen[realPath] = struct{}{}
		normalized = append(normalized, realPath)
	}
	return normalized, nil
}

func expandAppConfigPath(path string) (string, error) {
	prefix := "~" + string(filepath.Separator)
	if path != "~" && !strings.HasPrefix(path, prefix) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, prefix)), nil
}

func applyAppConfigPatch(stored, patch map[string]any) error {
	for key, value := range patch {
		if key == adminPasswordHashKey {
			return appConfigValidationError("admin password hash cannot be changed through app config")
		}
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
		if key == "workspace" {
			if value == nil {
				delete(stored, key)
				continue
			}
			workspace, err := normalizeStoredWorkspaceConfig(value)
			if err != nil {
				return err
			}
			if existing, ok := stored[key]; ok && existing != nil {
				existingWorkspace, err := normalizeStoredWorkspaceConfig(existing)
				if err != nil {
					return err
				}
				for nestedKey, nestedValue := range workspace {
					existingWorkspace[nestedKey] = nestedValue
				}
				if rawWorkspace, ok := value.(map[string]any); ok {
					if rawMaxUploadBytes, exists := rawWorkspace["maxUploadBytes"]; exists && rawMaxUploadBytes == nil {
						delete(existingWorkspace, "maxUploadBytes")
					}
				}
				stored[key] = existingWorkspace
			} else {
				stored[key] = workspace
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

func normalizeMaxUploadBytes(value any) (float64, error) {
	var number float64
	switch value := value.(type) {
	case int:
		number = float64(value)
	case int8:
		number = float64(value)
	case int16:
		number = float64(value)
	case int32:
		number = float64(value)
	case int64:
		number = float64(value)
	case uint:
		number = float64(value)
	case uint8:
		number = float64(value)
	case uint16:
		number = float64(value)
	case uint32:
		number = float64(value)
	case uint64:
		number = float64(value)
	case float32:
		number = float64(value)
	case float64:
		number = value
	default:
		return 0, appConfigValidationError("app config workspace.maxUploadBytes must be an integer")
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number ||
		number < MinMaxUploadBytes || number > MaxMaxUploadBytes {
		return 0, appConfigValidationf(
			"app config workspace.maxUploadBytes must be an integer between %d and %d",
			MinMaxUploadBytes, MaxMaxUploadBytes,
		)
	}
	return number, nil
}

func normalizedAppBrand(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	brand, ok := value.(string)
	if !ok {
		return "", appConfigValidationError("app config brand must be a string or null")
	}
	brand = strings.TrimSpace(brand)
	if utf8.RuneCountInString(brand) > 120 {
		return "", appConfigValidationError("app config brand must be at most 120 characters")
	}
	for _, r := range brand {
		if unicode.IsControl(r) {
			return "", appConfigValidationError("app config brand must not contain control characters")
		}
	}
	return brand, nil
}

func appConfigValidationError(message string) error {
	return fmt.Errorf("%w: %s", ErrAppConfigValidation, message)
}

func appConfigValidationf(format string, args ...any) error {
	return appConfigValidationError(fmt.Sprintf(format, args...))
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
		if key == "workspace" {
			workspace, ok := value.(map[string]any)
			if ok {
				effectiveWorkspace, _ := dst[key].(map[string]any)
				mergeAppConfig(effectiveWorkspace, workspace)
				dst[key] = effectiveWorkspace
				continue
			}
		}
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
