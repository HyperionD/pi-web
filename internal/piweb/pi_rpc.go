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

const piRPCTimeout = 8 * time.Second

func runPiRPC(ctx context.Context, cwd string, extraArgs []string, request RPCCommand) (RPCResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, piRPCTimeout)
	defer cancel()

	args := []string{"--mode", "rpc", "--no-session"}
	args = append(args, extraArgs...)
	command := exec.CommandContext(ctx, "pi", args...)
	command.Dir = cwd
	command.Env = append(os.Environ(), "PI_SKIP_VERSION_CHECK=1")
	configureCommandProcessGroup(command)

	stdin, err := command.StdinPipe()
	if err != nil {
		return RPCResponse{}, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return RPCResponse{}, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return RPCResponse{}, err
	}
	var stderrBuffer bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderrBuffer, stderr)
	}()

	if err := command.Start(); err != nil {
		return RPCResponse{}, err
	}
	defer func() {
		terminateCommandProcessGroup(command)
		_ = command.Wait()
		<-stderrDone
	}()

	if request.ID == "" {
		request.ID = "pi-web-rpc"
	}
	line, err := marshalRPCCommand(request)
	if err != nil {
		return RPCResponse{}, err
	}
	if _, err := fmt.Fprintln(stdin, string(line)); err != nil {
		return RPCResponse{}, err
	}
	if err := stdin.Close(); err != nil {
		return RPCResponse{}, err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var response RPCResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			continue
		}
		if response.ID == request.ID && response.Type == "response" {
			return response, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return RPCResponse{}, err
	}
	if ctx.Err() != nil {
		return RPCResponse{}, ctx.Err()
	}
	if output := strings.TrimSpace(stderrBuffer.String()); output != "" {
		return RPCResponse{}, fmt.Errorf("pi RPC failed: %s", output)
	}
	return RPCResponse{}, errors.New("pi RPC returned no response")
}
