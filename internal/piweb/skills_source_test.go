package piweb

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizeRepositoryRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:obra/superpowers.git": "github.com/obra/superpowers",
		"https://gitlab.com/acme/tools.git":   "gitlab.com/acme/tools",
		"ssh://git@gitea.example/acme/tools":  "gitea.example/acme/tools",
	}
	for remote, want := range cases {
		if got := normalizeRepositoryRemote(remote); got != want {
			t.Errorf("normalizeRepositoryRemote(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestClassifySkillSourceUsesLocalFallback(t *testing.T) {
	root := t.TempDir()
	source := classifySkillSource(context.Background(), PiSkillCommand{Scope: "project"}, filepath.Join(root, ".pi", "skills", "demo", "SKILL.md"), root)
	if source.Kind != "local" || source.ID != "local:project" || source.Label != "项目自定义技能" {
		t.Fatalf("unexpected source: %#v", source)
	}
}

func TestClassifySkillSourceUsesPackageMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"@acme/skills"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(root, "skills", "demo", "SKILL.md")
	source := classifySkillSource(context.Background(), PiSkillCommand{
		Scope: "user", Origin: "package", BaseDir: root,
	}, skillPath, t.TempDir())
	if source.Kind != "package" || source.ID != "npm:@acme/skills" || source.Label != "@acme/skills" {
		t.Fatalf("unexpected source: %#v", source)
	}
}

func TestClassifySkillSourceUsesGitRemoteOutsideWorkspace(t *testing.T) {
	repository := t.TempDir()
	workspace := t.TempDir()
	runSourceGit(t, repository, "init")
	runSourceGit(t, repository, "config", "remote.origin.url", "git@github.com:acme/skills.git")
	skillPath := filepath.Join(repository, "SKILL.md")
	source := classifySkillSource(context.Background(), PiSkillCommand{Scope: "user"}, skillPath, workspace)
	if source.Kind != "repository" || source.ID != "github.com/acme/skills" {
		t.Fatalf("unexpected source: %#v", source)
	}
}

func runSourceGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
