package piweb

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type processPiRuntime struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan PiEvent

	writeMu    sync.Mutex
	mu         sync.Mutex
	pending    map[string]chan runtimeResponse
	running    bool
	active     bool
	closed     bool
	lastUse    time.Time
	seq        atomic.Uint64
	done       chan struct{}
	stdoutDone chan struct{}
}

type runtimeResponse struct {
	response RPCResponse
	err      error
}

func newPiRuntime(ctx context.Context, sessionFile, cwd string) (PiRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command("pi", piRPCArgs(sessionFile)...)
	cmd.Dir = cwd
	configureCommandProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	runtime := &processPiRuntime{
		cmd: cmd, stdin: stdin, events: make(chan PiEvent, 64), pending: map[string]chan runtimeResponse{},
		running: true, lastUse: time.Now(), done: make(chan struct{}), stdoutDone: make(chan struct{}),
	}
	go runtime.readStdout(stdout)
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go runtime.waitForProcess()
	return runtime, nil
}

func (runtime *processPiRuntime) readStdout(stdout io.Reader) {
	defer close(runtime.stdoutDone)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		runtime.handleLine(scanner.Bytes())
	}
}

func (runtime *processPiRuntime) handleLine(raw []byte) {
	var envelope struct {
		ID      string          `json:"id"`
		Type    string          `json:"type"`
		Command string          `json:"command"`
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Type == "" {
		return
	}
	runtime.touchEvent(envelope.Type)
	if envelope.Type == "response" {
		response := RPCResponse{ID: envelope.ID, Type: envelope.Type, Command: envelope.Command, Success: envelope.Success, Data: envelope.Data, Error: envelope.Error}
		if runtime.deliverResponse(response) {
			return
		}
	}
	copyRaw := append(json.RawMessage(nil), raw...)
	select {
	case runtime.events <- PiEvent{Type: envelope.Type, Raw: copyRaw}:
	case <-runtime.done:
	}
}

func (runtime *processPiRuntime) deliverResponse(response RPCResponse) bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	id := response.ID
	waiter := runtime.pending[id]
	if waiter == nil && id == "" && len(runtime.pending) == 1 {
		for pendingID, candidate := range runtime.pending {
			id, waiter = pendingID, candidate
		}
	}
	if waiter == nil {
		return false
	}
	delete(runtime.pending, id)
	select {
	case waiter <- runtimeResponse{response: response}:
	default:
	}
	return true
}

func (runtime *processPiRuntime) Send(ctx context.Context, command RPCCommand) (RPCResponse, error) {
	if command.ID == "" {
		command.ID = fmt.Sprintf("piweb-%d", runtime.seq.Add(1))
	}
	line, err := marshalRPCCommand(command)
	if err != nil {
		return RPCResponse{}, err
	}
	waiter := make(chan runtimeResponse, 1)
	runtime.mu.Lock()
	if !runtime.running || runtime.closed {
		runtime.mu.Unlock()
		return RPCResponse{}, errors.New("runtime is not running")
	}
	runtime.pending[command.ID] = waiter
	runtime.lastUse = time.Now()
	runtime.mu.Unlock()

	runtime.writeMu.Lock()
	_, writeErr := runtime.stdin.Write(append(line, '\n'))
	runtime.writeMu.Unlock()
	if writeErr != nil {
		runtime.removePending(command.ID)
		return RPCResponse{}, writeErr
	}
	if command.Type != "compact" {
		runtime.removePending(command.ID)
		return RPCResponse{ID: command.ID, Type: "response", Command: command.Type, Success: true}, nil
	}
	select {
	case result := <-waiter:
		return result.response, result.err
	case <-ctx.Done():
		runtime.removePending(command.ID)
		return RPCResponse{}, ctx.Err()
	case <-runtime.done:
		return RPCResponse{}, errors.New("pi process exited")
	}
}

func marshalRPCCommand(command RPCCommand) ([]byte, error) {
	fields := map[string]json.RawMessage{}
	if len(command.Payload) > 0 && string(command.Payload) != "null" {
		if err := json.Unmarshal(command.Payload, &fields); err != nil {
			return nil, err
		}
	}
	if command.Type != "" {
		fields["type"], _ = json.Marshal(command.Type)
	}
	if command.ID != "" {
		fields["id"], _ = json.Marshal(command.ID)
	}
	return json.Marshal(fields)
}

func (runtime *processPiRuntime) removePending(id string) {
	runtime.mu.Lock()
	delete(runtime.pending, id)
	runtime.mu.Unlock()
}

func (runtime *processPiRuntime) waitForProcess() {
	err := runtime.cmd.Wait()
	<-runtime.stdoutDone
	runtime.mu.Lock()
	runtime.running = false
	runtime.active = false
	runtime.closed = true
	pending := runtime.pending
	runtime.pending = map[string]chan runtimeResponse{}
	runtime.mu.Unlock()
	for _, waiter := range pending {
		select {
		case waiter <- runtimeResponse{err: fmt.Errorf("pi process exited: %w", err)}:
		default:
		}
	}
	close(runtime.done)
	close(runtime.events)
}

func (runtime *processPiRuntime) Events() <-chan PiEvent { return runtime.events }
func (runtime *processPiRuntime) IsRunning() bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.running && !runtime.closed
}
func (runtime *processPiRuntime) Active() bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.active || len(runtime.pending) > 0
}
func (runtime *processPiRuntime) LastUsedAt() time.Time {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.lastUse
}
func (runtime *processPiRuntime) touchEvent(eventType string) {
	runtime.mu.Lock()
	runtime.lastUse = time.Now()
	switch eventType {
	case "agent_start", "turn_start", "tool_execution_start":
		runtime.active = true
	case "agent_end", "turn_end":
		runtime.active = false
	}
	runtime.mu.Unlock()
}

func (runtime *processPiRuntime) Close(ctx context.Context) error {
	runtime.mu.Lock()
	if runtime.closed && !runtime.running {
		runtime.mu.Unlock()
		return nil
	}
	runtime.closed = true
	runtime.mu.Unlock()
	_ = runtime.stdin.Close()
	select {
	case <-runtime.done:
		return nil
	case <-ctx.Done():
		terminateCommandProcessGroup(runtime.cmd)
		forceTerminateCommandProcessGroup(runtime.cmd)
		select {
		case <-runtime.done:
			return ctx.Err()
		default:
			return ctx.Err()
		}
	}
}
