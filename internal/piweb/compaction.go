package piweb

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

var errSessionBusy = errors.New("session already running")

type CompactionResult struct {
	Summary              string `json:"summary,omitempty"`
	FirstKeptEntryID     string `json:"firstKeptEntryId,omitempty"`
	TokensBefore         int    `json:"tokensBefore"`
	EstimatedTokensAfter int    `json:"estimatedTokensAfter"`
}

func (r *Runner) Compact(parent context.Context, broker *Broker, store *Store, sessionID string) (CompactionResult, error) {
	sessionFile, cwd, ok := store.SessionRuntime(sessionID)
	if !ok {
		return CompactionResult{}, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	run := &activePiRun{cancel: cancel, compacting: true}
	if err := r.claimRun(sessionID, run); err != nil {
		return CompactionResult{}, err
	}
	defer r.forgetRun(sessionID, run)
	broker.Publish(sessionID, "session.status", map[string]string{"status": "compacting"})
	result, err := executePiCompaction(ctx, sessionFile, cwd, run)
	if err != nil {
		broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
		broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})
		return CompactionResult{}, err
	}
	message := Message{Kind: "pi", Text: fmt.Sprintf("context summarized · %d tokens before compaction", result.TokensBefore)}
	if err := store.AppendMessage(sessionID, message); err != nil {
		broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
		broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})
		return CompactionResult{}, err
	}
	broker.Publish(sessionID, "session.message", message)
	broker.Publish(sessionID, "session.status", map[string]string{
		"status":     "idle",
		"finishedAt": time.Now().UTC().Format(time.RFC3339),
	})
	return result, nil
}

func (r *Runner) claimRun(sessionID string, run *activePiRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.running[sessionID]; exists {
		return errSessionBusy
	}
	r.running[sessionID] = run
	return nil
}

func executePiCompaction(ctx context.Context, sessionFile, cwd string, run *activePiRun) (CompactionResult, error) {
	command := exec.CommandContext(ctx, "pi", "--session", sessionFile, "--mode", "rpc")
	command.Dir = cwd
	command.Stderr = io.Discard
	configureCommandProcessGroup(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return CompactionResult{}, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return CompactionResult{}, err
	}
	run.stdin = stdin
	if err := command.Start(); err != nil {
		return CompactionResult{}, err
	}
	terminated := make(chan struct{})
	go terminateOnContext(ctx, command, terminated)
	defer close(terminated)
	writeErr := run.send(map[string]string{"type": "compact"})
	result, readErr := readCompactResponse(stdout)
	closeErr := stdin.Close()
	waitErr := command.Wait()
	return result, firstPiRPCError(ctx, writeErr, closeErr, readErr, waitErr)
}

func terminateOnContext(ctx context.Context, command *exec.Cmd, done <-chan struct{}) {
	select {
	case <-ctx.Done():
		terminateCommandProcessGroup(command)
	case <-done:
	}
}

func readCompactResponse(reader io.Reader) (CompactionResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var response struct {
			Type    string           `json:"type"`
			Command string           `json:"command"`
			Success bool             `json:"success"`
			Error   string           `json:"error"`
			Data    CompactionResult `json:"data"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil || response.Type != "response" || response.Command != "compact" {
			continue
		}
		if !response.Success {
			if strings.TrimSpace(response.Error) == "" {
				response.Error = "pi compact failed"
			}
			return CompactionResult{}, errors.New(response.Error)
		}
		return response.Data, nil
	}
	if err := scanner.Err(); err != nil {
		return CompactionResult{}, err
	}
	return CompactionResult{}, errors.New("pi compact returned no response")
}
