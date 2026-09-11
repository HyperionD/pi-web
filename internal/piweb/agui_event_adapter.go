package piweb

import (
	"encoding/json"
	"strconv"
	"strings"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

type aguiDurableState struct {
	state *aguiStreamState
}

func newAguiDurableState(threadID, runID, fallback string) *aguiDurableState {
	return &aguiDurableState{state: newAguiStreamState(threadID, runID, fallback)}
}

func ConsumeDomainEvent(event StoredSessionEvent, stream *aguiEventStream) (bool, error) {
	state := newAguiDurableState("", "", event.SessionID)
	state.Consume(event, stream)
	return false, nil
}

func (s *aguiDurableState) Consume(event StoredSessionEvent, stream *aguiEventStream) (bool, error) {
	s.handle(event, stream)
	return false, nil
}

func (s *aguiDurableState) handle(event StoredSessionEvent, stream *aguiEventStream) {
	switch event.Type {
	case EventAssistantSegmentDelta:
		var payload struct {
			Kind  string `json:"kind"`
			Delta string `json:"delta"`
		}
		_ = json.Unmarshal(event.Payload, &payload)
		if payload.Kind == "think" {
			s.state.emitThinkingDelta(payload.Delta, stream)
		} else {
			s.state.emitTextDelta(payload.Delta, stream)
		}
	case EventAssistantSegmentCommit:
		var payload struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}
		_ = json.Unmarshal(event.Payload, &payload)
		s.closeSegment(payload.Kind, stream)
	case EventToolStarted:
		var payload struct {
			ToolCallID string `json:"toolCallId"`
			Tool       string `json:"tool"`
			Args       string `json:"args"`
		}
		_ = json.Unmarshal(event.Payload, &payload)
		callID := payload.ToolCallID
		if callID == "" {
			callID = strings.TrimPrefix(event.EntityID, "tool:"+event.RunID+":")
		}
		name := payload.Tool
		if name == "" {
			name = "tool"
		}
		s.state.toolIDs[name] = append(s.state.toolIDs[name], callID)
		s.state.toolNames[callID] = name
		stream.write(aguievents.NewToolCallStartEvent(callID, name))
		if strings.TrimSpace(payload.Args) != "" {
			stream.write(aguievents.NewToolCallArgsEvent(callID, payload.Args))
		}
	case EventToolOutput:
		var payload struct {
			Chunk string `json:"chunk"`
			Mode  string `json:"mode"`
		}
		_ = json.Unmarshal(event.Payload, &payload)
		s.emitToolResult(event.EntityID, payload.Chunk, payload.Mode, stream)
	case EventToolFinished:
		var payload struct {
			Body   string `json:"body"`
			Status string `json:"status"`
		}
		_ = json.Unmarshal(event.Payload, &payload)
		s.emitToolResult(event.EntityID, payload.Body, "replace", stream)
		s.emitToolEnd(event.EntityID, payload.Status, stream)
	case EventRunFinished, EventRunFailed, EventRunCancelled, EventRunInterrupted:
		s.closeOpen(stream)
	}
	s.emitCursor(event.Seq, stream)
}

func (s *aguiDurableState) closeSegment(kind string, stream *aguiEventStream) {
	if kind == "think" {
		if s.state.thinkingOpen {
			stream.write(aguievents.NewReasoningMessageEndEvent(s.state.thinkingID))
			stream.write(aguievents.NewReasoningEndEvent(s.state.thinkingID))
			s.state.thinkingOpen = false
		}
		return
	}
	if s.state.textOpen {
		stream.write(aguievents.NewTextMessageEndEvent(s.state.messageID))
		s.state.textOpen = false
	}
}

func (s *aguiDurableState) emitToolResult(entityID, content, mode string, stream *aguiEventStream) {
	if strings.TrimSpace(content) == "" {
		return
	}
	callID := s.toolCallID(entityID)
	if callID == "" {
		return
	}
	if mode != "replace" {
		mode = "append"
	}
	stream.write(aguievents.NewCustomEvent(aguiToolOutputEventName, aguievents.WithValue(map[string]string{
		"toolCallId": callID,
		"mode":       mode,
	})))
	stream.write(aguievents.NewToolCallResultEvent(aguievents.GenerateMessageID(), callID, content))
}

func (s *aguiDurableState) emitToolEnd(entityID, status string, stream *aguiEventStream) {
	callID := s.toolCallID(entityID)
	if callID == "" {
		return
	}
	stream.write(aguievents.NewToolCallEndEvent(callID))
	delete(s.state.toolNames, callID)
}

func (s *aguiDurableState) toolCallID(entityID string) string {
	parts := strings.SplitN(entityID, ":", 3)
	if len(parts) == 3 && parts[0] == "tool" {
		return parts[2]
	}
	return ""
}

func (s *aguiDurableState) emitCursor(seq int64, stream *aguiEventStream) {
	if seq <= 0 {
		return
	}
	stream.write(aguievents.NewCustomEvent(aguiBrokerCursorEventName, aguievents.WithValue(map[string]string{
		"eventId": strconv.FormatInt(seq, 10),
	})))
}

func (s *aguiDurableState) closeOpen(stream *aguiEventStream) {
	s.state.closeOpen(stream)
}
