package piweb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunOAuthListHelperFindsPiAIOAuthModuleFromPiCommand(t *testing.T) {
	root := t.TempDir()
	packageRoot := filepath.Join(root, "lib", "node_modules", "@earendil-works", "pi-coding-agent")
	modulePath := filepath.Join(packageRoot, "node_modules", "@earendil-works", "pi-ai", "dist", "providers", "all.js")
	if err := os.MkdirAll(filepath.Dir(modulePath), 0o755); err != nil {
		t.Fatal(err)
	}
	module := `export function builtinProviders() {
		return [{ id: "openai-codex", name: "OpenAI Codex", auth: { oauth: {} } }];
	}`
	if err := os.WriteFile(modulePath, []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}

	cliPath := filepath.Join(packageRoot, "dist", "bundle", "cli.js")
	if err := os.MkdirAll(filepath.Dir(cliPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cliPath, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(root, "bin")
	if err := os.MkdirAll(binPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cliPath, filepath.Join(binPath, "pi")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binPath+string(os.PathListSeparator)+os.Getenv("PATH"))

	providers, err := runOAuthListHelper()
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].ID != "openai-codex" {
		t.Fatalf("unexpected OAuth providers: %+v", providers)
	}
}
