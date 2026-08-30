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

func CurrentPiContextUsage(ctx context.Context, sessionFile string, cwd string) (usage *ContextUsage, resultErr error) {
	if strings.TrimSpace(sessionFile) == "" {
		return nil, fmt.Errorf("session file is required")
	}
	executable, err := exec.LookPath("pi")
	if err != nil {
		return nil, fmt.Errorf("pi executable not found: %w", err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	snapshotFile, cleanupSnapshot, err := createSessionSnapshot(queryCtx, sessionFile)
	if err != nil {
		return nil, fmt.Errorf("create session snapshot: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, cleanupSnapshot()) }()
	command := exec.CommandContext(queryCtx, executable, "--mode", "rpc", "--session", snapshotFile, "--no-extensions")
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
	var readErr error
	usage, readErr = readContextUsageResponse(stdout)
	waitErr := command.Wait()
	if err := firstPiRPCError(queryCtx, writeErr, closeErr, readErr, waitErr); err != nil {
		return nil, err
	}
	return usage, nil
}

func createSessionSnapshot(ctx context.Context, sessionFile string) (string, func() error, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	source, err := os.Open(sessionFile)
	if err != nil {
		return "", nil, err
	}
	stat, err := source.Stat()
	if err != nil {
		return "", nil, errors.Join(err, source.Close())
	}
	snapshot, err := os.CreateTemp("", "pi-web-session-*.jsonl")
	if err != nil {
		return "", nil, errors.Join(err, source.Close())
	}
	_, copyErr := io.CopyN(snapshot, contextReader{ctx: ctx, reader: source}, stat.Size())
	sourceCloseErr := source.Close()
	trimErr := truncateIncompleteJSONLTail(ctx, snapshot, stat.Size())
	snapshotCloseErr := snapshot.Close()
	if err := errors.Join(copyErr, sourceCloseErr, trimErr, snapshotCloseErr); err != nil {
		return "", nil, errors.Join(err, os.Remove(snapshot.Name()))
	}
	return snapshot.Name(), func() error { return os.Remove(snapshot.Name()) }, nil
}

func truncateIncompleteJSONLTail(ctx context.Context, file *os.File, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if size == 0 {
		return nil
	}
	var finalByte [1]byte
	if _, err := file.ReadAt(finalByte[:], size-1); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if finalByte[0] == '\n' {
		return nil
	}
	lineStart, err := finalJSONLLineStart(ctx, file, size)
	if err != nil {
		return err
	}
	complete, err := isCompleteJSONSection(ctx, file, lineStart, size-lineStart)
	if err != nil {
		return err
	}
	if complete {
		return nil
	}
	return file.Truncate(lineStart)
}

func finalJSONLLineStart(ctx context.Context, file *os.File, size int64) (int64, error) {
	const chunkSize int64 = 64 * 1024
	for end := size; end > 0; {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		start := max(int64(0), end-chunkSize)
		chunk := make([]byte, end-start)
		n, err := file.ReadAt(chunk, start)
		if err != nil && err != io.EOF {
			return 0, err
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if newline := bytes.LastIndexByte(chunk[:n], '\n'); newline >= 0 {
			return start + int64(newline) + 1, nil
		}
		end = start
	}
	return 0, nil
}

func isCompleteJSONSection(ctx context.Context, file *os.File, start int64, size int64) (bool, error) {
	section := io.NewSectionReader(file, start, size)
	decoder := json.NewDecoder(contextReader{ctx: ctx, reader: section})
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		var syntaxError *json.SyntaxError
		if errors.As(err, &syntaxError) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	if err := decoder.Decode(&value); errors.Is(err, io.EOF) {
		return true, nil
	} else if err != nil {
		var syntaxError *json.SyntaxError
		if errors.As(err, &syntaxError) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, nil
		}
		return false, err
	}
	return false, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, reader.ctx.Err()
	default:
		return reader.reader.Read(buffer)
	}
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
