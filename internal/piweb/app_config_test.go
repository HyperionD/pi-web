package piweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestLoadAppConfigValidatesStoredBrand(t *testing.T) {
	cases := []struct {
		name      string
		brand     any
		wantBrand string
		wantError bool
	}{
		{name: "trims", brand: "  Custom  ", wantBrand: "Custom"},
		{name: "blank resets", brand: " \t\n ", wantBrand: DefaultAppBrand},
		{name: "null resets", brand: nil, wantBrand: DefaultAppBrand},
		{name: "wrong type", brand: 42, wantError: true},
		{name: "control character", brand: "bad\x00brand", wantError: true},
		{name: "too long", brand: strings.Repeat("a", 121), wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
			path := filepath.Join(root, "pi-web-config.json")
			data, err := json.Marshal(map[string]any{"brand": tc.brand})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}

			response, err := LoadAppConfig()
			if tc.wantError {
				if err == nil {
					t.Fatal("expected invalid stored brand error")
				}
				return
			}
			if err != nil || response.Config["brand"] != tc.wantBrand {
				t.Fatalf("response=%+v err=%v", response, err)
			}
		})
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
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
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

func TestSaveAppConfigSerializesConcurrentReadMergeWrites(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))

	const writers = 64
	start := make(chan struct{})
	errorsCh := make(chan error, writers)
	var group sync.WaitGroup
	group.Add(writers)
	for index := 0; index < writers; index++ {
		index := index
		go func() {
			defer group.Done()
			<-start
			_, err := SaveAppConfig(map[string]any{
				fmt.Sprintf("concurrent-%d", index): fmt.Sprintf("value-%d", index),
			})
			errorsCh <- err
		}()
	}
	close(start)
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent save: %v", err)
		}
	}

	response, err := LoadAppConfig()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < writers; index++ {
		key := fmt.Sprintf("concurrent-%d", index)
		if response.Config[key] != fmt.Sprintf("value-%d", index) {
			t.Fatalf("lost concurrent update for %q: %#v", key, response.Config[key])
		}
	}
}

func TestSaveAppConfigCorrectsExistingFilePermissions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))

	path, err := AppConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"brand":"Existing"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := before.Mode().Perm(); mode == 0o600 {
		t.Fatalf("test setup unexpectedly created private file: %o", mode)
	}

	if _, err := SaveAppConfig(map[string]any{"brand": "Updated"}); err != nil {
		t.Fatal(err)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file permissions=%o", mode)
	}
}

func TestSaveAppConfigCorrectsExistingDirectoryPermissions(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "agent", "sessions")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", sessionDir)
	configDir := filepath.Dir(filepath.Join(root, "agent", "pi-web-config.json"))
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	path, err := SaveAppConfig(map[string]any{"brand": "Custom"})
	if err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path.Path))
	if err != nil {
		t.Fatal(err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("directory permissions=%o", mode)
	}
	fileInfo, err := os.Stat(path.Path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file permissions=%o", mode)
	}
}

func TestAppConfigPathReturnsErrorWhenDefaultSessionDirectoryEmpty(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "")
	t.Setenv("HOME", "")
	if _, err := AppConfigPath(); err == nil {
		t.Fatal("expected empty default session directory error")
	}
}

func TestAppConfigPathReturnsErrorForEmptySessionDirectory(t *testing.T) {
	if _, err := appConfigPath(""); err == nil {
		t.Fatal("expected empty session directory error")
	}
}
