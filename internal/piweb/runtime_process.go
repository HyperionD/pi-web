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
	stdout io.ReadCloser
	stderr io.ReadCloser
	events chan PiEvent

	writeReady chan struct{}
	stdinOnce  sync.Once
	pipesOnce  sync.Once
	poisonOnce sync.Once
	finishOnce sync.Once
	mu         sync.Mutex
	pending    map[string]chan runtimeResponse
	poisonErr  error
	running    bool
	closed     bool
	lastUse    time.Time
	activity   map[string]int
	seq        atomic.Uint64
	done       chan struct{}

	stdoutDone chan struct{}
	stderrDone chan struct{}
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
		cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr, events: make(chan PiEvent, 64), pending: map[string]chan runtimeResponse{},
		running: true, lastUse: time.Now(), activity: map[string]int{}, done: make(chan struct{}), stdoutDone: make(chan struct{}),
		stderrDone: make(chan struct{}),
	}
	go runtime.readStdout(stdout)
	go runtime.readStderr(stderr)
	go runtime.waitForProcess()
	return runtime, nil
}

func (runtime *processPiRuntime) readStdout(stdout io.Reader) {
	defer close(runtime.stdoutDone)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if err := runtime.handleLine(scanner.Bytes()); err != nil {
			runtime.protocolFailure(err)
			terminateCommandProcessGroup(runtime.cmd)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		runtime.protocolFailure(fmt.Errorf("pi stdout scanner: %w", err))
		terminateCommandProcessGroup(runtime.cmd)
		return
	}
	// EOF before the process lifecycle has been joined is a protocol failure,
	// not a successful idle state: wake every RPC waiter and stop the process.
	runtime.protocolFailure(errors.New("pi stdout closed unexpectedly"))
	terminateCommandProcessGroup(runtime.cmd)
}

func (runtime *processPiRuntime) readStderr(stderr io.Reader) {
	defer close(runtime.stderrDone)
	_, _ = io.Copy(io.Discard, stderr)
}

func (runtime *processPiRuntime) handleLine(raw []byte) error {
	var envelope struct {
		ID         string          `json:"id"`
		Type       string          `json:"type"`
		Command    string          `json:"command"`
		Success    bool            `json:"success"`
		Data       json.RawMessage `json:"data"`
		Error      string          `json:"error"`
		ToolCallID string          `json:"toolCallId"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("invalid pi RPC output: %w", err)
	}
	if envelope.Type == "" {
		return errors.New("invalid pi RPC output: missing event type")
	}
	runtime.touchEvent(envelope.Type, envelope.ToolCallID)
	if envelope.Type == "response" {
		response := RPCResponse{ID: envelope.ID, Type: envelope.Type, Command: envelope.Command, Success: envelope.Success, Data: envelope.Data, Error: envelope.Error}
		if runtime.deliverResponse(response) {
			return nil
		}
	}
	copyRaw := append(json.RawMessage(nil), raw...)
	runtime.enqueueEvent(PiEvent{Type: envelope.Type, Raw: copyRaw})
	return nil
}

func (runtime *processPiRuntime) enqueueEvent(event PiEvent) {
	select {
	case runtime.events <- event:
		return
	default:
	}
	// Never evict an older event to make room: that silently changes the
	// protocol stream and can lose agent_end. Closing the runtime makes the
	// overflow observable to waiters and the event consumer as a terminal error.
	runtime.poison(fmt.Errorf("pi event channel is full while reporting %s", event.Type))
}

func (runtime *processPiRuntime) deliverResponse(response RPCResponse) bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	waiter := runtime.pending[response.ID]
	if waiter == nil {
		return false
	}
	delete(runtime.pending, response.ID)
	waiter <- runtimeResponse{response: response}
	return true
}

func (runtime *processPiRuntime) protocolFailure(err error) {
	runtime.poison(err)
}

func (runtime *processPiRuntime) poison(err error) {
	runtime.poisonOnce.Do(func() {
		runtime.mu.Lock()
		runtime.closed = true
		runtime.poisonErr = err
		pending := runtime.pending
		runtime.pending = map[string]chan runtimeResponse{}
		runtime.mu.Unlock()
		for _, waiter := range pending {
			waiter <- runtimeResponse{err: err}
		}
		runtime.closeStdin()
		if runtime.cmd != nil {
			terminateCommandProcessGroup(runtime.cmd)
		}
	})
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
	if _, exists := runtime.pending[command.ID]; exists {
		runtime.mu.Unlock()
		return RPCResponse{}, fmt.Errorf("duplicate RPC request ID %q", command.ID)
	}
	runtime.pending[command.ID] = waiter
	runtime.lastUse = time.Now()
	runtime.mu.Unlock()

	if writeErr := runtime.writeContext(ctx, append(line, '\n')); writeErr != nil {
		runtime.removePending(command.ID)
		if ctx.Err() != nil {
			runtime.poison(ctx.Err())
		} else {
			runtime.poison(writeErr)
		}
		return RPCResponse{}, writeErr
	}
	for {
		select {
		case result := <-waiter:
			return result.response, result.err
		default:
		}
		select {
		case result := <-waiter:
			return result.response, result.err
		case <-ctx.Done():
			runtime.removePending(command.ID)
			return RPCResponse{}, ctx.Err()
		case <-runtime.done:
			select {
			case result := <-waiter:
				return result.response, result.err
			default:
			}
			return RPCResponse{}, errors.New("pi process exited")
		}
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

func (runtime *processPiRuntime) writeContext(ctx context.Context, data []byte) error {
	if runtime.stdin == nil {
		return errors.New("runtime stdin is unavailable")
	}
	runtime.mu.Lock()
	if runtime.writeReady == nil {
		runtime.writeReady = make(chan struct{}, 1)
		runtime.writeReady <- struct{}{}
	}
	ready := runtime.writeReady
	runtime.mu.Unlock()
	select {
	case <-ready:
	case <-ctx.Done():
		runtime.closeStdin()
		return ctx.Err()
	}
	defer func() { ready <- struct{}{} }()
	writeDone := make(chan error, 1)
	go func() {
		_, err := runtime.stdin.Write(data)
		writeDone <- err
	}()
	select {
	case err := <-writeDone:
		return err
	case <-ctx.Done():
		runtime.closeStdin()
		return ctx.Err()
	}
}

func (runtime *processPiRuntime) closeStdin() {
	runtime.stdinOnce.Do(func() {
		if runtime.stdin != nil {
			_ = runtime.stdin.Close()
		}
	})
}

func (runtime *processPiRuntime) closePipes() {
	runtime.pipesOnce.Do(func() {
		if runtime.stdout != nil {
			_ = runtime.stdout.Close()
		}
		if runtime.stderr != nil {
			_ = runtime.stderr.Close()
		}
	})
}

func (runtime *processPiRuntime) waitForProcess() {
	err := runtime.cmd.Wait()
	runtime.closePipes()
	<-runtime.stdoutDone
	<-runtime.stderrDone
	if err == nil {
		err = errors.New("pi process exited")
	}
	runtime.finish(fmt.Errorf("pi process exited: %w", err))
}

func (runtime *processPiRuntime) finish(err error) {
	runtime.finishOnce.Do(func() {
		runtime.mu.Lock()
		runtime.running = false
		runtime.closed = true
		pending := runtime.pending
		runtime.pending = map[string]chan runtimeResponse{}
		runtime.mu.Unlock()
		for _, waiter := range pending {
			waiter <- runtimeResponse{err: err}
		}
		if runtime.done != nil {
			close(runtime.done)
		}
		if runtime.events != nil {
			close(runtime.events)
		}
	})
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
	return len(runtime.pending) > 0 || len(runtime.activity) > 0
}
func (runtime *processPiRuntime) LastUsedAt() time.Time {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.lastUse
}
func (runtime *processPiRuntime) touchEvent(eventType, toolCallID string) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.lastUse = time.Now()
	if runtime.activity == nil {
		runtime.activity = map[string]int{}
	}
	key := eventActivityKey(eventType, toolCallID)
	if key == "" {
		return
	}
	switch eventType {
	case "agent_start", "turn_start", "tool_execution_start":
		runtime.activity[key]++
	case "agent_end", "turn_end", "tool_execution_end":
		if runtime.activity[key] > 0 {
			runtime.activity[key]--
			if runtime.activity[key] == 0 {
				delete(runtime.activity, key)
			}
		}
	}
}

func (runtime *processPiRuntime) Close(ctx context.Context) error {
	runtime.mu.Lock()
	if runtime.closed && !runtime.running {
		runtime.mu.Unlock()
		return nil
	}
	runtime.closed = true
	runtime.mu.Unlock()
	runtime.closeStdin()
	if runtime.cmd == nil {
		runtime.finish(errors.New("runtime closed"))
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
	select {
	case <-runtime.done:
		return nil
	case <-ctx.Done():
		terminateCommandProcessGroup(runtime.cmd)
		forceTerminateCommandProcessGroup(runtime.cmd)
		runtime.closePipes()
		cleanupTimer := time.NewTimer(250 * time.Millisecond)
		defer cleanupTimer.Stop()
		select {
		case <-runtime.done:
		case <-cleanupTimer.C:
			// The reaper owns done; returning here is bounded without claiming
			// that a stubborn descendant or reader has already been cleaned up.
		}
		return ctx.Err()
	}
}
