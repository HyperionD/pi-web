package piweb

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

func CurrentPiContextUsage(ctx context.Context, sessionFile string, cwd string) (*ContextUsage, error) {
	if strings.TrimSpace(sessionFile) == "" {
		return nil, fmt.Errorf("session file is required")
	}
	executable, err := exec.LookPath("pi")
	if err != nil {
		return nil, fmt.Errorf("pi executable not found: %w", err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	command := exec.CommandContext(queryCtx, executable, "--mode", "rpc", "--session", sessionFile, "--no-extensions")
	if cwd != "" {
		command.Dir = cwd
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	_, writeErr := fmt.Fprintln(stdin, `{"id":"context","type":"get_session_stats"}`)
	closeErr := stdin.Close()
	usage, readErr := readContextUsageResponse(stdout)
	waitErr := command.Wait()
	if err := firstPiRPCError(queryCtx, writeErr, closeErr, readErr, waitErr); err != nil {
		return nil, err
	}
	return usage, nil
}

func CurrentPiSetModel(ctx context.Context, sessionFile string, cwd string, provider string, modelID string) error {
	if strings.TrimSpace(sessionFile) == "" {
		return fmt.Errorf("session file is required")
	}
	if strings.TrimSpace(provider) == "" || strings.TrimSpace(modelID) == "" {
		return fmt.Errorf("provider and model are required")
	}
	executable, err := exec.LookPath("pi")
	if err != nil {
		return fmt.Errorf("pi executable not found: %w", err)
	}
	commandJSON, err := json.Marshal(map[string]string{"id": "model", "type": "set_model", "provider": provider, "modelId": modelID})
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	command := exec.CommandContext(queryCtx, executable, "--mode", "rpc", "--session", sessionFile, "--no-extensions")
	if cwd != "" {
		command.Dir = cwd
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	_, writeErr := fmt.Fprintln(stdin, string(commandJSON))
	closeErr := stdin.Close()
	setErr := readSetModelResponse(stdout)
	waitErr := command.Wait()
	if err := firstPiRPCError(queryCtx, writeErr, closeErr, setErr, waitErr); err != nil {
		return err
	}
	return nil
}

func readSetModelResponse(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var response struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Command string `json:"command"`
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(scanner.Text()), &response); err != nil {
			continue
		}
		if response.ID != "model" || response.Type != "response" || response.Command != "set_model" {
			continue
		}
		if !response.Success {
			if response.Error == "" {
				response.Error = "pi set_model failed"
			}
			return fmt.Errorf("%s", response.Error)
		}
		return nil
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("pi set_model returned no response")
}

func readContextUsageResponse(reader io.Reader) (*ContextUsage, error) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		usage, matched, err := parseSessionStatsRPCLine(scanner.Text())
		if err != nil {
			return nil, err
		}
		if matched {
			return usage, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("pi get_session_stats returned no response")
}

func firstPiRPCError(ctx context.Context, errors ...error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("pi RPC timed out: %w", ctx.Err())
	}
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func CurrentPiModel(ctx context.Context, cwd string) (string, error) {
	status, err := CurrentPiModelStatus(ctx, cwd)
	return status.Model, err
}

func CurrentPiModelStatus(ctx context.Context, cwd string) (RuntimeStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pi", "--mode", "rpc", "--no-session")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "PI_SKIP_VERSION_CHECK=1")
	configureCommandProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return RuntimeStatus{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RuntimeStatus{}, err
	}
	stderr, _ := cmd.StderrPipe()
	var stderrBuf bytes.Buffer
	if stderr != nil {
		go func() { _, _ = stderrBuf.ReadFrom(stderr) }()
	}
	if err := cmd.Start(); err != nil {
		return RuntimeStatus{}, err
	}
	defer func() {
		terminateCommandProcessGroup(cmd)
		_ = cmd.Wait()
	}()

	if _, err := io.WriteString(stdin, `{"id":"state","type":"get_state"}`+"\n"); err != nil {
		return RuntimeStatus{}, err
	}
	_ = stdin.Close()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		status, matched, err := parseStateModelStatusRPCLine(scanner.Text())
		if err != nil {
			return RuntimeStatus{}, err
		}
		if matched {
			return status, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return RuntimeStatus{}, err
	}
	if ctx.Err() != nil {
		return RuntimeStatus{}, ctx.Err()
	}
	if output := strings.TrimSpace(stderrBuf.String()); output != "" {
		return RuntimeStatus{}, fmt.Errorf("pi get_state failed: %s", output)
	}
	return RuntimeStatus{}, fmt.Errorf("pi get_state returned no response")
}

func parseStateModelRPCLine(line string) (string, bool, error) {
	status, matched, err := parseStateModelStatusRPCLine(line)
	return status.Model, matched, err
}

func parseSessionStatsRPCLine(line string) (*ContextUsage, bool, error) {
	var response struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			ContextUsage *ContextUsage `json:"contextUsage"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		return nil, false, nil
	}
	if response.ID != "context" || response.Type != "response" || response.Command != "get_session_stats" {
		return nil, false, nil
	}
	if !response.Success {
		if response.Error == "" {
			response.Error = "pi get_session_stats failed"
		}
		return nil, true, fmt.Errorf("%s", response.Error)
	}
	return response.Data.ContextUsage, true, nil
}

func parseStateModelStatusRPCLine(line string) (RuntimeStatus, bool, error) {
	var response struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			Model *struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Provider string `json:"provider"`
			} `json:"model"`
			ThinkingLevel string `json:"thinkingLevel"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		return RuntimeStatus{}, false, nil
	}
	if response.ID != "state" || response.Type != "response" || response.Command != "get_state" {
		return RuntimeStatus{}, false, nil
	}
	if !response.Success {
		if response.Error == "" {
			response.Error = "pi get_state failed"
		}
		return RuntimeStatus{}, true, fmt.Errorf("%s", response.Error)
	}
	if response.Data.Model == nil {
		return RuntimeStatus{}, true, nil
	}
	model := strings.TrimSpace(response.Data.Model.Name)
	if model == "" {
		model = strings.TrimSpace(response.Data.Model.ID)
	}
	if model == "" {
		model = strings.TrimSpace(response.Data.Model.Provider)
	}
	return RuntimeStatus{
		Model:         model,
		ModelProvider: strings.TrimSpace(response.Data.Model.Provider),
		ThinkingLevel: strings.TrimSpace(response.Data.ThinkingLevel),
	}, true, nil
}
