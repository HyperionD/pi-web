package piweb

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePiSkillCommandsFiltersNonSkills(t *testing.T) {
	response := RPCResponse{
		Type: "response", Command: "get_commands", Success: true,
		Data: json.RawMessage(`{"commands":[
		  {"name":"skill:review","description":"Review code","source":"skill","sourceInfo":{"path":"/tmp/review/SKILL.md","scope":"project","source":"local","origin":"top-level","baseDir":"/tmp/project/.pi"}},
		  {"name":"format","description":"Format","source":"extension","sourceInfo":{"path":"/tmp/format.ts","scope":"project"}},
		  {"name":"skill:global","description":"Global","source":"skill","sourceInfo":{"path":"/home/u/.pi/agent/skills/global/SKILL.md","scope":"user","source":"local","origin":"top-level"}}
		]}`),
	}

	skills, err := parsePiSkillCommands(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2", len(skills))
	}
	if skills[0].Scope != "global" || skills[1].Scope != "project" {
		t.Fatalf("unexpected scopes: %#v", skills)
	}
	if skills[1].Source != "local" || skills[1].Origin != "top-level" || skills[1].BaseDir != "/tmp/project/.pi" {
		t.Fatalf("unexpected source info: %#v", skills[1])
	}
}

func TestListPiSkillsUsesNoExtensions(t *testing.T) {
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	script := filepath.Join(binDir, "pi")
	scriptBody := `#!/bin/sh
printf '%s\n' "$*" > "$PI_WEB_TEST_PI_ARGS"
printf '%s\n' '{"id":"skills","type":"response","command":"get_commands","success":true,"data":{"commands":[]}}'
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PI_WEB_TEST_PI_ARGS", argsFile)

	if _, err := ListPiSkills(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--mode", "rpc", "--no-session", "--no-extensions"} {
		if !strings.Contains(string(args), expected) {
			t.Fatalf("Pi args %q do not contain %q", args, expected)
		}
	}
	if strings.Contains(string(args), "--approve") {
		t.Fatalf("Pi args unexpectedly approve project: %q", args)
	}
}
