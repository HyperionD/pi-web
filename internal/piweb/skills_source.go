package piweb

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type SkillSource struct {
	Kind  string
	ID    string
	Label string
}

func classifySkillSource(ctx context.Context, command PiSkillCommand, skillPath, workspaceRoot string) SkillSource {
	if source, ok := packageSkillSource(command, skillPath); ok {
		return source
	}
	if source, ok := repositorySkillSource(ctx, skillPath, workspaceRoot); ok {
		return source
	}
	return localSkillSource(command.Scope)
}

func packageSkillSource(command PiSkillCommand, skillPath string) (SkillSource, bool) {
	root := ""
	if command.Origin == "package" {
		root = strings.TrimSpace(command.BaseDir)
	}
	if root == "" {
		root = nodeModuleRoot(skillPath)
	}
	if root == "" {
		return SkillSource{}, false
	}
	name := packageName(root)
	if name == "" {
		name = filepath.Base(root)
	}
	return SkillSource{Kind: "package", ID: "npm:" + name, Label: name}, true
}

func nodeModuleRoot(skillPath string) string {
	for current := filepath.Dir(skillPath); current != filepath.Dir(current); current = filepath.Dir(current) {
		parent := filepath.Base(filepath.Dir(current))
		if parent == "node_modules" {
			return current
		}
		if strings.HasPrefix(parent, "@") && filepath.Base(filepath.Dir(filepath.Dir(current))) == "node_modules" {
			return current
		}
	}
	return ""
}

func packageName(root string) string {
	content, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return ""
	}
	var metadata struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(content, &metadata) != nil {
		return ""
	}
	return strings.TrimSpace(metadata.Name)
}

func repositorySkillSource(ctx context.Context, skillPath, workspaceRoot string) (SkillSource, bool) {
	root, ok := gitOutput(ctx, filepath.Dir(skillPath), "rev-parse", "--show-toplevel")
	if !ok || samePath(root, workspaceRoot) {
		return SkillSource{}, false
	}
	remote, ok := gitOutput(ctx, root, "config", "--get", "remote.origin.url")
	id := normalizeRepositoryRemote(remote)
	if !ok || id == "" {
		return SkillSource{}, false
	}
	return SkillSource{Kind: "repository", ID: id, Label: id}, true
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, bool) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	output, err := command.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(output)), true
}

func samePath(left, right string) bool {
	leftPath, leftErr := filepath.EvalSymlinks(filepath.Clean(left))
	rightPath, rightErr := filepath.EvalSymlinks(filepath.Clean(right))
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return filepath.Clean(leftPath) == filepath.Clean(rightPath)
}

func normalizeRepositoryRemote(remote string) string {
	remote = strings.TrimSpace(strings.TrimSuffix(remote, ".git"))
	if remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(remote, "git@"), ":", 2)
		if len(parts) == 2 {
			return strings.ToLower(parts[0]) + "/" + strings.TrimPrefix(parts[1], "/")
		}
	}
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname()) + "/" + strings.TrimPrefix(parsed.Path, "/")
}

func localSkillSource(scope string) SkillSource {
	switch scope {
	case "project":
		return SkillSource{Kind: "local", ID: "local:project", Label: "项目自定义技能"}
	case "path":
		return SkillSource{Kind: "local", ID: "local:path", Label: "自定义路径技能"}
	default:
		return SkillSource{Kind: "local", ID: "local:user", Label: "全局自定义技能"}
	}
}
