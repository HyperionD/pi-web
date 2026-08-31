package piweb

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAppConfigPathSharesPiAgentParentWithoutUsingPiSettingsName(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "agent", "sessions")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", sessionDir)

	path, err := AppConfigPath()

	if err != nil || path != filepath.Join(filepath.Dir(sessionDir), "pi-web-config.json") {
		t.Fatalf("path=%q err=%v", path, err)
	}
}

func TestLoadAppConfigReturnsDefaultWithoutCreatingFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))

	response, err := LoadAppConfig()

	if err != nil || response.Config["brand"] != DefaultAppBrand {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if response.Config["version"] != float64(1) {
		t.Fatalf("default version=%v", response.Config["version"])
	}
	if response.Path != filepath.Join(root, "pi-web-config.json") {
		t.Fatalf("path=%q", response.Path)
	}
	if _, err := os.Stat(filepath.Join(root, "pi-web-config.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no file creation, got %v", err)
	}
}

func TestAppConfigSaveReturnsEffectiveConfigAndPreservesUnknownKeys(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "agent", "sessions"))

	response, err := SaveAppConfig(map[string]any{"brand": "  Custom  ", "unknown": map[string]any{"enabled": true}})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if response.Config["brand"] != "Custom" || response.Config["version"] != float64(1) {
		t.Fatalf("response=%+v", response)
	}
	unknown, ok := response.Config["unknown"].(map[string]any)
	if !ok || unknown["enabled"] != true {
		t.Fatalf("unknown=%#v", response.Config["unknown"])
	}

	loaded, err := LoadAppConfig()
	if err != nil || loaded.Config["brand"] != "Custom" || loaded.Config["unknown"] == nil {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestAppConfigBrandNilResetsToDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	if _, err := SaveAppConfig(map[string]any{"brand": "Custom"}); err != nil {
		t.Fatal(err)
	}

	response, err := SaveAppConfig(map[string]any{"brand": nil})
	if err != nil || response.Config["brand"] != DefaultAppBrand {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "pi-web-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if _, ok := stored["brand"]; ok {
		t.Fatalf("brand should be reset by deleting explicit key: %#v", stored)
	}
}

func TestAppConfigBlankBrandResetsToDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	if _, err := SaveAppConfig(map[string]any{"brand": "Custom"}); err != nil {
		t.Fatal(err)
	}

	response, err := SaveAppConfig(map[string]any{"brand": " \t\n "})
	if err != nil || response.Config["brand"] != DefaultAppBrand {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestSaveAppConfigRejectsInvalidBrand(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	cases := []struct {
		name  string
		brand any
	}{
		{name: "wrong type", brand: 42},
		{name: "control character", brand: "valid\x00brand"},
		{name: "too long", brand: strings.Repeat("a", 121)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SaveAppConfig(map[string]any{"brand": tc.brand}); err == nil {
				t.Fatal("expected invalid brand error")
			}
		})
	}
	if utf8.RuneCountInString(strings.Repeat("界", 120)) != 120 {
		t.Fatal("test fixture is not 120 runes")
	}
}

func TestLoadAppConfigRejectsMalformedJSONWithPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	path := filepath.Join(root, "pi-web-config.json")
	if err := os.WriteFile(path, []byte(`{"brand":`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadAppConfig()
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err=%v, want path %q", err, path)
	}
}

func TestLoadAppConfigTreatsZeroLengthFileAsDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	path := filepath.Join(root, "pi-web-config.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := LoadAppConfig()
	if err != nil || response.Config["brand"] != DefaultAppBrand {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestSaveAppConfigCreatesPrivateDirectoriesAndFile(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "agent", "sessions")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", sessionDir)

	path, err := AppConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAppConfig(map[string]any{"brand": "Custom"}); err != nil {
		t.Fatal(err)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if mode := dirInfo.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("directory permissions=%o", mode)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file permissions=%o", mode)
	}
}

func TestAppConfigPathReturnsErrorForEmptySessionDirectory(t *testing.T) {
	if _, err := appConfigPath(""); err == nil {
		t.Fatal("expected empty session directory error")
	}
}
