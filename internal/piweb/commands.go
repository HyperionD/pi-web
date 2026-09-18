package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type SlashCommand struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Path        string `json:"path,omitempty"`
}

func ListRuntimeCommands(ctx context.Context, runtime PiRuntime) ([]SlashCommand, error) {
	response, err := runtime.Send(ctx, RPCCommand{Type: "get_commands"})
	if err != nil {
		return nil, err
	}
	return parseCommandsRPCResponse(response)
}

func ListPiCommands(ctx context.Context, cwd string) ([]SlashCommand, error) {
	response, err := runPiRPC(ctx, cwd, nil, RPCCommand{ID: "commands", Type: "get_commands"})
	if err != nil {
		return nil, err
	}
	return parseCommandsRPCResponse(response)
}

type commandsRPCData struct {
	Commands []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Source      string `json:"source"`
		Location    string `json:"location"`
		Path        string `json:"path"`
		SourceInfo  struct {
			Path  string `json:"path"`
			Scope string `json:"scope"`
		} `json:"sourceInfo"`
	} `json:"commands"`
}

func parseCommandsRPCResponse(response RPCResponse) ([]SlashCommand, error) {
	if response.Type != "response" || response.Command != "get_commands" {
		return nil, errors.New("unexpected pi get_commands response")
	}
	if !response.Success {
		if response.Error == "" {
			response.Error = "pi get_commands failed"
		}
		return nil, errors.New(response.Error)
	}
	var data commandsRPCData
	if len(response.Data) > 0 && string(response.Data) != "null" {
		if err := json.Unmarshal(response.Data, &data); err != nil {
			return nil, fmt.Errorf("decode pi get_commands data: %w", err)
		}
	}
	commands := make([]SlashCommand, 0, len(data.Commands))
	for _, item := range data.Commands {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		scope := item.SourceInfo.Scope
		if scope == "" {
			scope = item.Location
		}
		path := item.SourceInfo.Path
		if path == "" {
			path = item.Path
		}
		commands = append(commands, SlashCommand{
			Name: name, Command: "/" + name, Description: strings.TrimSpace(item.Description),
			Source: item.Source, Scope: displayCommandScope(scope), Path: path,
		})
	}
	return commands, nil
}

func parseCommandsRPCLine(line string) ([]SlashCommand, bool, error) {
	var response RPCResponse
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		return nil, false, nil
	}
	if response.ID != "commands" || response.Type != "response" || response.Command != "get_commands" {
		return nil, false, nil
	}
	commands, err := parseCommandsRPCResponse(response)
	return commands, true, err
}

func displayCommandScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "user":
		return "global"
	case "project":
		return "project"
	case "temporary":
		return "temporary"
	case "path":
		return "path"
	default:
		return scope
	}
}
