package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

const fallbackChoiceSystemPrompt = `Pi Web UI fallback choice protocol:
- You are running inside Pi Web UI.
- When you need the user to choose between options, or the user asks you to ask a choice question,
  output a fenced json block with top-level type "piweb_choice".
- Stop after emitting the fallback block and wait for the user's follow-up.
- When the user later sends:
  选项回复：
  id: <id>
  value: <value>
  continue using that id/value as the selected answer.
- Required schema:
  {
    "type":"piweb_choice",
    "id":"stable-choice-id",
    "question":"Question text",
    "options":[{"label":"Option A","value":"A","description":"What A means"}],
    "allowCustom":false
  }
- Keep id stable, short, and unique. Use at most 8 options. Use inert plain text only.`

type Runner struct {
	mu      sync.Mutex
	running map[string]*activePiRun
	manager *RuntimeManager
}

type activePiRun struct {
	cancel     context.CancelFunc
	stdin      io.WriteCloser
	runtime    PiRuntime
	compacting bool
	mu         sync.Mutex
}

func NewRunner() *Runner {
	return &Runner{running: map[string]*activePiRun{}}
}

func NewRunnerWithRuntimeManager(manager *RuntimeManager) *Runner {
	runner := NewRunner()
	runner.manager = manager
	return runner
}

func (r *Runner) StartPiPrompt(
	parent context.Context,
	broker *Broker,
	store *Store,
	sessionID string,
	text string,
	images []PromptAttachment,
	displayText string,
) error {
	manager, err := r.runtimeManager(store)
	if err != nil {
		return err
	}
	runtime, err := manager.Ensure(parent, sessionID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	activeRun := &activePiRun{cancel: cancel, runtime: runtime}
	if err := r.claimRun(sessionID, activeRun); err != nil {
		cancel()
		return err
	}

	user := Message{Kind: "user", Text: displayText, Attachments: images}
	if err := store.AppendMessage(sessionID, user); err != nil {
		r.forgetRun(sessionID, activeRun)
		cancel()
		return err
	}
	broker.Publish(sessionID, "session.message", user)
	broker.Publish(sessionID, "session.status", map[string]string{"status": "running"})
	go r.consumeRuntimeEvents(ctx, broker, store, sessionID, runtime, activeRun)
	if err := activeRun.sendContext(ctx, rpcPromptCommand(text, images, "")); err != nil {
		r.forgetRun(sessionID, activeRun)
		cancel()
		broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
		broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})
		return err
	}
	return nil
}

func (r *Runner) runtimeManager(store *Store) (*RuntimeManager, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.manager == nil {
		r.manager = NewRuntimeManager(store)
	}
	return r.manager, nil
}

func (r *Runner) consumeRuntimeEvents(ctx context.Context, broker *Broker, store *Store, sessionID string, runtime PiRuntime, run *activePiRun) {
	state := &jsonStreamState{}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-runtime.Events():
			if !ok {
				r.finishPrompt(sessionID, run, broker, errors.New("pi runtime exited"))
				return
			}
			line := string(event.Raw)
			if !handlePiJSONEvent(line, broker, store, sessionID, state) && !isPiTerminalNotification(line) {
				broker.Publish(sessionID, "tool.output", map[string]string{"tool": "pi", "chunk": line})
			}
			if event.Type == "agent_end" {
				r.finishPrompt(sessionID, run, broker, nil)
				return
			}
		}
	}
}

func (r *Runner) finishPrompt(sessionID string, run *activePiRun, broker *Broker, runErr error) {
	r.forgetRun(sessionID, run)
	run.cancel()
	if runErr != nil {
		broker.Publish(sessionID, "error", map[string]string{"error": runErr.Error()})
	}
	broker.Publish(sessionID, "session.status", map[string]string{
		"status": "idle", "finishedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

func (r *Runner) Steer(sessionID string, text string, images []PromptAttachment) error {
	r.mu.Lock()
	run, ok := r.running[sessionID]
	r.mu.Unlock()
	if !ok || run.compacting {
		return errors.New("session is not running")
	}
	return run.sendContext(context.Background(), rpcPromptCommand(text, images, "steer"))
}

func (r *Runner) IsRunning(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[sessionID]
	return ok
}

func (r *Runner) IsCompacting(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.running[sessionID]
	return ok && run.compacting
}

func (r *Runner) forgetRun(sessionID string, run *activePiRun) {
	r.mu.Lock()
	if r.running[sessionID] == run {
		delete(r.running, sessionID)
	}
	r.mu.Unlock()
}

func piRPCArgs(sessionFile string) []string {
	return []string{
		"--session", sessionFile,
		"--mode", "rpc",
		"--append-system-prompt", fallbackChoiceSystemPrompt,
	}
}

type rpcPrompt struct {
	Type              string     `json:"type"`
	Message           string     `json:"message"`
	Images            []rpcImage `json:"images,omitempty"`
	StreamingBehavior string     `json:"streamingBehavior,omitempty"`
}

type rpcImage struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MIMEType string `json:"mimeType"`
}

func rpcPromptCommand(text string, images []PromptAttachment, streamingBehavior string) rpcPrompt {
	return rpcPrompt{Type: "prompt", Message: text, Images: rpcImages(images), StreamingBehavior: streamingBehavior}
}

func rpcImages(images []PromptAttachment) []rpcImage {
	result := make([]rpcImage, 0, len(images))
	for _, image := range images {
		data := strings.TrimSpace(image.DataURL)
		if data == "" {
			continue
		}
		if comma := strings.Index(data, ","); comma >= 0 {
			data = data[comma+1:]
		}
		mimeType := image.MIMEType
		if mimeType == "" {
			mimeType = "image/png"
		}
		result = append(result, rpcImage{Type: "image", Data: data, MIMEType: mimeType})
	}
	return result
}

func (r *activePiRun) send(command any) error {
	return r.sendContext(context.Background(), command)
}

func (r *activePiRun) sendContext(ctx context.Context, command any) error {
	if r.runtime != nil {
		payload, err := json.Marshal(command)
		if err != nil {
			return err
		}
		typeName := ""
		if value, ok := command.(rpcPrompt); ok {
			typeName = value.Type
		}
		response, err := r.runtime.Send(ctx, RPCCommand{Type: typeName, Payload: payload})
		if err != nil {
			return err
		}
		if !response.Success {
			if response.Error == "" {
				response.Error = "pi RPC command failed"
			}
			return errors.New(response.Error)
		}
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	line, err := json.Marshal(command)
	if err != nil {
		return err
	}
	_, err = r.stdin.Write(append(line, '\n'))
	return err
}

func isPiRPCAgentEnd(line string) bool {
	var event struct {
		Type string `json:"type"`
	}
	return json.Unmarshal([]byte(line), &event) == nil && event.Type == "agent_end"
}

func isPiTerminalNotification(line string) bool {
	return strings.Contains(line, "\x1b]777;notify;")
}

func imageExtension(mimeType string) string {
	subtype := strings.TrimPrefix(strings.ToLower(mimeType), "image/")
	subtype = strings.Split(subtype, ";")[0]
	switch subtype {
	case "jpeg":
		return ".jpg"
	case "png", "gif", "webp", "bmp":
		return "." + subtype
	case "svg+xml":
		return ".svg"
	default:
		return ".png"
	}
}

func (r *Runner) Cancel(sessionID string) bool {
	r.mu.Lock()
	run, ok := r.running[sessionID]
	if ok {
		delete(r.running, sessionID)
	}
	r.mu.Unlock()
	if ok {
		run.cancel()
	}
	return ok
}
