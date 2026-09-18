package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type PiSkillCommand struct {
	Name        string
	Description string
	Path        string
	Scope       string
	Source      string
	Origin      string
	BaseDir     string
}

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
	SourceKind  string `json:"sourceKind"`
	SourceID    string `json:"sourceId"`
	SourceLabel string `json:"sourceLabel"`
}

type SkillDiscoveryResponse struct {
	Skills    []Skill `json:"skills"`
	Available bool    `json:"available"`
	Error     string  `json:"error,omitempty"`
	Notice    string  `json:"notice,omitempty"`
}

type SkillPatchRequest struct {
	Path    string `json:"path"`
	Enabled *bool  `json:"enabled"`
}

func loadWorkspaceSkills(ctx context.Context, cwd string) ([]Skill, error) {
	commands, err := ListPiSkills(ctx, cwd)
	if err != nil {
		return nil, err
	}
	skills := make([]Skill, 0, len(commands))
	classifier := newSkillSourceClassifier(ctx, cwd)
	for _, command := range commands {
		path, err := canonicalSkillPath(command.Path)
		if err != nil {
			continue
		}
		enabled, err := readSkillInvocation(path)
		if err != nil {
			continue
		}
		source := classifier.classify(command, path)
		skills = append(skills, Skill{
			Name: command.Name, Description: command.Description,
			Path: path, Scope: command.Scope, Enabled: enabled,
			SourceKind: source.Kind, SourceID: source.ID, SourceLabel: source.Label,
		})
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Scope != skills[j].Scope {
			return skills[i].Scope < skills[j].Scope
		}
		if skills[i].Name != skills[j].Name {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].Path < skills[j].Path
	})
	return skills, nil
}

func canonicalSkillPath(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(strings.TrimSpace(path)))
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("skill path is not a regular file")
	}
	return filepath.Clean(realPath), nil
}

func ListPiSkills(ctx context.Context, cwd string) ([]PiSkillCommand, error) {
	response, err := runPiRPC(ctx, cwd, []string{"--no-extensions"}, RPCCommand{
		ID: "skills", Type: "get_commands",
	})
	if err != nil {
		return nil, err
	}
	return parsePiSkillCommands(response)
}

func parsePiSkillCommands(response RPCResponse) ([]PiSkillCommand, error) {
	if response.Type != "response" || response.Command != "get_commands" {
		return nil, errors.New("unexpected pi get_commands response")
	}
	if !response.Success {
		if response.Error == "" {
			response.Error = "pi get_commands failed"
		}
		return nil, errors.New(response.Error)
	}

	var data struct {
		Commands []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Source      string `json:"source"`
			SourceInfo  struct {
				Path    string `json:"path"`
				Scope   string `json:"scope"`
				Source  string `json:"source"`
				Origin  string `json:"origin"`
				BaseDir string `json:"baseDir"`
			} `json:"sourceInfo"`
		} `json:"commands"`
	}
	if len(response.Data) > 0 && string(response.Data) != "null" {
		if err := json.Unmarshal(response.Data, &data); err != nil {
			return nil, fmt.Errorf("decode pi skill commands: %w", err)
		}
	}

	skills := make([]PiSkillCommand, 0, len(data.Commands))
	for _, command := range data.Commands {
		if command.Source != "skill" {
			continue
		}
		name := strings.TrimSpace(command.Name)
		description := strings.TrimSpace(command.Description)
		path := strings.TrimSpace(command.SourceInfo.Path)
		scope := skillScope(command.SourceInfo.Scope)
		if name == "" || description == "" || path == "" || scope == "" {
			continue
		}
		skills = append(skills, PiSkillCommand{
			Name: name, Description: description, Path: path, Scope: scope,
			Source: command.SourceInfo.Source, Origin: command.SourceInfo.Origin,
			BaseDir: strings.TrimSpace(command.SourceInfo.BaseDir),
		})
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Scope != skills[j].Scope {
			return skills[i].Scope < skills[j].Scope
		}
		if skills[i].Name != skills[j].Name {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].Path < skills[j].Path
	})
	return skills, nil
}

func skillScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "user":
		return "global"
	case "project":
		return "project"
	case "path":
		return "path"
	case "temporary":
		return "temporary"
	default:
		return ""
	}
}
