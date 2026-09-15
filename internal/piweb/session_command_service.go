package piweb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type StartPromptCommand struct {
	SessionID       string
	ClientRequestID string
	DisplayText     string
	Text            string
	Attachments     []PromptAttachment
	StartupSettings []ModelStartupSettings
}

type RuntimePromptStarter interface {
	StartPrompt(ctx context.Context, sessionID, runID, text string, images []PromptAttachment, adapter *RuntimeEventAdapter) error
}

type SessionCommandService struct {
	events   SessionEventStore
	starter  RuntimePromptStarter
	newRunID func() string
	launch   func(task func())
}

func NewSessionCommandService(events SessionEventStore, starter RuntimePromptStarter) *SessionCommandService {
	return &SessionCommandService{
		events: events, starter: starter,
		newRunID: func() string { return "run-" + createSessionID() },
		launch:   func(task func()) { go task() },
	}
}

func PromptRequestHash(command StartPromptCommand) string {
	data, err := json.Marshal(struct {
		Text        string             `json:"text"`
		DisplayText string             `json:"displayText"`
		Attachments []PromptAttachment `json:"attachments"`
	}{command.Text, command.DisplayText, command.Attachments})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (s *SessionCommandService) AppendControlEvent(ctx context.Context, sessionID, requestID, eventType string, value any) (StoredSessionEvent, error) {
	appender, ok := s.events.(SessionControlEventStore)
	if !ok {
		return StoredSessionEvent{}, ErrEventStoreNotReady
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return StoredSessionEvent{}, err
	}
	return appender.AppendControl(ctx, sessionID, "command:"+requestID, eventType, payload)
}

func (s *SessionCommandService) StartPrompt(ctx context.Context, command StartPromptCommand) (RunReceipt, error) {
	return s.StartPromptWith(ctx, command, s.starter)
}

func (s *SessionCommandService) StartPromptWith(ctx context.Context, command StartPromptCommand, starter RuntimePromptStarter) (RunReceipt, error) {
	if command.SessionID == "" || command.ClientRequestID == "" {
		return RunReceipt{}, ErrInvalidSessionEvent
	}
	runID := s.newRunID()
	receipt, err := s.events.StartRun(ctx, StartRunCommand{
		SessionID: command.SessionID, ClientRequestID: command.ClientRequestID,
		RequestHash: PromptRequestHash(command), RunID: runID,
		DisplayText: command.DisplayText, Attachments: command.Attachments,
	})
	if err != nil {
		return RunReceipt{}, err
	}
	if receipt.Existing {
		return receipt, nil
	}
	adapter := NewRuntimeEventAdapter(command.SessionID, receipt.RunID, s.events)
	images := imagePromptAttachments(command.Attachments)
	runtimeContext := context.WithoutCancel(ctx)
	s.launch(func() {
		if startErr := starter.StartPrompt(runtimeContext, command.SessionID, receipt.RunID, command.Text, images, adapter); startErr != nil {
			_ = adapter.Fail(startErr)
		}
	})
	return receipt, nil
}
