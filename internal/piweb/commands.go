package piweb

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
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
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pi", "--mode", "rpc", "--no-session")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "PI_SKIP_VERSION_CHECK=1")
	configureCommandProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, _ := cmd.StderrPipe()
	var stderrBuf bytes.Buffer
	stderrDone := make(chan struct{})
	if stderr != nil {
		go func() {
			defer close(stderrDone)
			_, _ = stderrBuf.ReadFrom(stderr)
		}()
	} else {
		close(stderrDone)
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		terminateCommandProcessGroup(cmd)
		_ = cmd.Wait()
	}()

	if _, err := io.WriteString(stdin, `{"id":"commands","type":"get_commands"}`+"\n"); err != nil {
		return nil, err
	}
	_ = stdin.Close()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		commands, matched, err := parseCommandsRPCLine(scanner.Text())
		if err != nil {
			return nil, err
		}
		if matched {
			return commands, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	<-stderrDone
	if output := strings.TrimSpace(stderrBuf.String()); output != "" {
		return nil, fmt.Errorf("pi get_commands failed: %s", output)
	}
	return nil, errors.New("pi get_commands returned no response")
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
