package piweb

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceSkillPatchRequiresAdminForGlobalSkill(t *testing.T) {
	server, admin := accountServer(t)
	server.config.EnablePiExecution = true
	root := t.TempDir()
	workspace, err := server.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(t.TempDir(), "SKILL.md")
	writeSkillFixtureFile(t, globalPath)
	installSkillPiFixture(t, globalPath, "user")
	member := addAccount(t, server, admin, "skill-member", workspace.ID)

	response := accountRequest(server, http.MethodPatch, "/api/workspaces/"+workspace.ID+"/skills", `{"path":"`+globalPath+`","enabled":false}`, member)
	if response.Code != http.StatusForbidden {
		t.Fatalf("member global patch status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWorkspaceSkillPatchAllowsMemberProjectSkill(t *testing.T) {
	server, admin := accountServer(t)
	server.config.EnablePiExecution = true
	root := t.TempDir()
	workspace, err := server.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(root, ".pi", "skills", "demo", "SKILL.md")
	writeSkillFixtureFile(t, projectPath)
	installSkillPiFixture(t, projectPath, "project")
	member := addAccount(t, server, admin, "skill-project-member", workspace.ID)

	response := accountRequest(server, http.MethodPatch, "/api/workspaces/"+workspace.ID+"/skills", `{"path":"`+projectPath+`","enabled":false}`, member)
	if response.Code != http.StatusOK {
		t.Fatalf("member project patch status=%d body=%s", response.Code, response.Body.String())
	}
}

func writeSkillFixtureFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: demo\ndescription: Demo\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
