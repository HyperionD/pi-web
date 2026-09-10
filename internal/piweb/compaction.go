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
	manager, err := r.runtimeManager(store)
	if err != nil {
		return CompactionResult{}, err
	}
	_, eventCancel := context.WithCancel(parent)
	commandCtx, commandCancel := context.WithTimeout(parent, 5*time.Minute)
	run := &activePiRun{
		cancel: eventCancel, commandCancel: commandCancel, commandContext: commandCtx,
		compacting: true, done: make(chan struct{}), eventsDone: make(chan struct{}),
		runtimeReady: make(chan struct{}), settled: make(chan struct{}),
	}
	if err := r.claimRun(sessionID, run); err != nil {
		eventCancel()
		commandCancel()
		return CompactionResult{}, err
	}
	runtime, err := manager.Ensure(parent, sessionID)
	if err != nil {
		r.forgetRun(sessionID, run)
		eventCancel()
		commandCancel()
		return CompactionResult{}, err
	}
	if !run.attachRuntime(runtime) {
		_ = manager.Retire(context.Background(), sessionID, runtime)
		r.forgetRun(sessionID, run)
		eventCancel()
		commandCancel()
		return CompactionResult{}, errSessionBusy
	}
	defer func() {
		eventCancel()
		commandCancel()
		r.finishRun(sessionID, run)
	}()
	broker.Publish(sessionID, "session.status", map[string]string{"status": "compacting"})
	response, err := run.sendRPCContext(commandCtx, RPCCommand{Type: "compact"})
	if err == nil && !response.Success {
		err = errors.New(response.Error)
		if response.Error == "" {
			err = errors.New("pi compact failed")
		}
	}
	if run.isCancelling() {
		if err == nil {
			err = context.Canceled
		}
		return CompactionResult{}, err
	}
	if err != nil {
		if commandCtx.Err() != nil {
			run.beginCancel()
			r.cancelRunRuntime(sessionID, run, manager)
			return CompactionResult{}, err
		}
		broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
		broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})
		return CompactionResult{}, err
	}
	var result CompactionResult
	if err := json.Unmarshal(response.Data, &result); err != nil {
		err = fmt.Errorf("decode pi compact response: %w", err)
		if run.isCancelling() {
			return CompactionResult{}, err
		}
		broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
		broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})
		return CompactionResult{}, err
	}
	if run.isCancelling() {
		return CompactionResult{}, context.Canceled
	}
	message := Message{Kind: "pi", Text: fmt.Sprintf("context summarized · %d tokens before compaction", result.TokensBefore)}
	if err := store.AppendMessage(sessionID, message); err != nil {
		if run.isCancelling() {
			return CompactionResult{}, err
		}
		broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
		broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})
		return CompactionResult{}, err
	}
	if run.isCancelling() {
		return CompactionResult{}, context.Canceled
	}
	broker.Publish(sessionID, "session.message", message)
	broker.Publish(sessionID, "session.status", map[string]string{
		"status":     "idle",
		"finishedAt": time.Now().UTC().Format(time.RFC3339),
	})
	return result, nil
}

func abortAndCloseRuntime(runtime PiRuntime) {
	abortCtx, abortCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, _ = runtime.Send(abortCtx, RPCCommand{Type: "abort"})
	abortCancel()
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = runtime.Close(closeCtx)
	closeCancel()
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
