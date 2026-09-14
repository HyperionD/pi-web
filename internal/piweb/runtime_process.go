package piweb

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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

	writeReady      chan struct{}
	stdinOnce       sync.Once
	pipesOnce       sync.Once
	poisonOnce      sync.Once
	stopOnce        sync.Once
	finishOnce      sync.Once
	mu              sync.Mutex
	pending         map[string]chan runtimeResponse
	quarantined     map[string]struct{}
	poisonErr       error
	failure         error
	stderrTail      []byte
	stderrTruncated bool
	running         bool
	closed          bool
	lastUse         time.Time
	activity        map[string]int
	seq             atomic.Uint64
	done            chan struct{}

	stdoutDone chan struct{}
	stderrDone chan struct{}
	stop       chan struct{}
}

type runtimeResponse struct {
	response RPCResponse
	err      error
}

const runtimeRequestQuarantineLimit = 1024

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
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stdout = stdoutWriter
	stderr, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stdoutWriter.Close()
		return nil, err
	}
	cmd.Stderr = stderrWriter
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stdoutWriter.Close()
		_ = stderr.Close()
		_ = stderrWriter.Close()
		return nil, err
	}
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	runtime := &processPiRuntime{
		cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr, events: make(chan PiEvent, 64),
		running: true, lastUse: time.Now(), activity: map[string]int{}, pending: map[string]chan runtimeResponse{}, quarantined: map[string]struct{}{}, done: make(chan struct{}), stdoutDone: make(chan struct{}),
		stderrDone: make(chan struct{}), stop: make(chan struct{}),
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
	buffer := make([]byte, 4096)
	for {
		read, err := stderr.Read(buffer)
		if read > 0 {
			runtime.mu.Lock()
			runtime.stderrTail = append(runtime.stderrTail, buffer[:read]...)
			if len(runtime.stderrTail) > 16*1024 {
				runtime.stderrTail = runtime.stderrTail[len(runtime.stderrTail)-16*1024:]
				runtime.stderrTruncated = true
			}
			runtime.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
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
	case <-runtime.stop:
	}
}

func (runtime *processPiRuntime) deliverResponse(response RPCResponse) bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	waiter := runtime.pending[response.ID]
	if waiter == nil {
		if _, quarantined := runtime.quarantined[response.ID]; quarantined {
			delete(runtime.quarantined, response.ID)
			return true
		}
		return false
	}
	delete(runtime.pending, response.ID)
	waiter <- runtimeResponse{response: response}
	return true
}

func (runtime *processPiRuntime) protocolFailure(err error) {
	runtime.poison(err)
}

func (runtime *processPiRuntime) Failure() error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.failure != nil {
		return runtime.failure
	}
	if runtime.poisonErr != nil {
		return runtime.poisonErr
	}
	return nil
}

func (runtime *processPiRuntime) poison(err error) {
	runtime.poisonOnce.Do(func() {
		runtime.mu.Lock()
		runtime.closed = true
		runtime.poisonErr = err
		runtime.failure = err
		diagnostic := runtime.failureWithStderrLocked()
		pending := runtime.pending
		runtime.pending = map[string]chan runtimeResponse{}
		runtime.mu.Unlock()
		runtime.stopEvents()
		for _, waiter := range pending {
			waiter <- runtimeResponse{err: diagnostic}
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
	if _, quarantined := runtime.quarantined[command.ID]; quarantined {
		runtime.mu.Unlock()
		return RPCResponse{}, fmt.Errorf("RPC request ID %q is quarantined", command.ID)
	}
	runtime.pending[command.ID] = waiter
	runtime.lastUse = time.Now()
	runtime.mu.Unlock()

	if writeErr := runtime.writeContext(ctx, append(line, '\n')); writeErr != nil {
		runtime.quarantinePending(command.ID)
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
			runtime.quarantinePending(command.ID)
			return RPCResponse{}, ctx.Err()
		case <-runtime.done:
			select {
			case result := <-waiter:
				return result.response, result.err
			default:
			}
			if failure := runtime.Failure(); failure != nil {
				return RPCResponse{}, failure
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

func (runtime *processPiRuntime) quarantinePending(id string) {
	runtime.mu.Lock()
	if runtime.quarantined == nil {
		runtime.quarantined = map[string]struct{}{}
	}
	delete(runtime.pending, id)
	if _, exists := runtime.quarantined[id]; exists {
		runtime.mu.Unlock()
		return
	}
	if len(runtime.quarantined) >= runtimeRequestQuarantineLimit {
		runtime.mu.Unlock()
		runtime.poison(errors.New("RPC request quarantine limit exceeded"))
		return
	}
	runtime.quarantined[id] = struct{}{}
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
	stdoutFinished := waitForPipe(runtime.stdoutDone, 100*time.Millisecond)
	stderrFinished := waitForPipe(runtime.stderrDone, 100*time.Millisecond)
	if !stdoutFinished || !stderrFinished {
		// A reader can be waiting for event-channel capacity after the process
		// exits. Stop that wait before joining the reader goroutines.
		runtime.stopEvents()
		runtime.closePipes()
	}
	<-runtime.stdoutDone
	<-runtime.stderrDone
	if err == nil {
		err = errors.New("pi process exited")
	}
	runtime.finish(fmt.Errorf("pi process exited: %w", err))
}

func waitForPipe(done <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (runtime *processPiRuntime) finish(err error) {
	runtime.finishOnce.Do(func() {
		runtime.mu.Lock()
		diagnostic := runtime.failure
		if diagnostic == nil {
			diagnostic = err
		} else if err != nil && diagnostic.Error() != err.Error() {
			diagnostic = fmt.Errorf("%w: process wait: %v", diagnostic, err)
		}
		runtime.failure = runtime.withStderrLocked(diagnostic)
		finalFailure := runtime.failure
		runtime.running = false
		runtime.closed = true
		pending := runtime.pending
		runtime.pending = map[string]chan runtimeResponse{}
		runtime.mu.Unlock()
		runtime.stopEvents()
		for _, waiter := range pending {
			waiter <- runtimeResponse{err: finalFailure}
		}
		if runtime.done != nil {
			close(runtime.done)
		}
		if runtime.events != nil {
			close(runtime.events)
		}
	})
}

func (runtime *processPiRuntime) failureWithStderrLocked() error {
	return runtime.withStderrLocked(runtime.failure)
}

func (runtime *processPiRuntime) withStderrLocked(err error) error {
	if err == nil || len(runtime.stderrTail) == 0 {
		return err
	}
	stderr := RedactSecrets(string(runtime.stderrTail))
	if runtime.stderrTruncated {
		stderr = "[REDACTED: stderr tail truncated]"
	}
	return fmt.Errorf("%w: stderr: %s", err, stderr)
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

func (runtime *processPiRuntime) stopEvents() {
	runtime.stopOnce.Do(func() {
		if runtime.stop != nil {
			close(runtime.stop)
		}
	})
}

func (runtime *processPiRuntime) Close(ctx context.Context) error {
	runtime.mu.Lock()
	if runtime.closed && !runtime.running {
		runtime.mu.Unlock()
		return nil
	}
	runtime.closed = true
	runtime.mu.Unlock()
	runtime.stopEvents()
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
