package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type PiSkillCommand struct {
	Name        string
	Description string
	Path        string
	Scope       string
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
				Path  string `json:"path"`
				Scope string `json:"scope"`
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
	default:
		return ""
	}
}
