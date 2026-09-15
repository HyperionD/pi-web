package piweb

import (
	"encoding/json"
	"fmt"
	"strings"
)

var runEvents = map[string]bool{
	EventAssistantSegmentStarted: true, EventAssistantSegmentDelta: true,
	EventAssistantSegmentCommit: true, EventToolStarted: true,
	EventToolArgsDelta: true, EventToolOutput: true, EventToolFinished: true,
	EventRunFinished: true, EventRunFailed: true, EventRunCancelled: true,
	EventRunInterrupted: true,
}

func ReduceSessionEvent(view ProjectionView, event StoredSessionEvent) (ProjectionChange, error) {
	if err := validateProjectionEvent(view.Stream, event); err != nil {
		return ProjectionChange{}, err
	}
	switch event.Type {
	case EventUserMessageCreated:
		return reduceUserMessage(view, event)
	case EventRunStarted:
		change := ProjectionChange{Stream: view.Stream}
		change.Stream.Status = SessionStatusRunning
		change.Stream.CurrentRunID = event.RunID
		return change, nil
	case EventAssistantSegmentStarted, EventAssistantSegmentDelta, EventAssistantSegmentCommit:
		return reduceAssistantSegment(view, event)
	case EventToolStarted, EventToolArgsDelta, EventToolOutput, EventToolFinished:
		return reduceToolEvent(view, event)
	case EventRunFinished, EventRunFailed, EventRunCancelled, EventRunInterrupted:
		return reduceTerminalRun(view, event)
	case EventSessionCreated, EventSessionRenamed, EventSessionDeleted, EventSessionModelChanged,
		EventContextUsageUpdated, EventRuntimeModelUpdated:
		return reduceSessionMetadata(view, event)
	case EventCompactionStarted:
		change := ProjectionChange{Stream: view.Stream}
		change.Stream.Status = SessionStatusCompacting
		return change, nil
	case EventCompactionCommitted:
		change := ProjectionChange{Stream: view.Stream}
		change.Stream.Status = SessionStatusIdle
		return change, nil
	case EventCompactionFailed:
		change := ProjectionChange{Stream: view.Stream}
		change.Stream.Status = SessionStatusError
		return change, nil
	case EventRunCancelRequested, EventSteerAccepted:
		return ProjectionChange{Stream: view.Stream}, nil
	case EventSessionRebased, EventWorkspaceFilesChanged:
		return ProjectionChange{Stream: view.Stream, RequireRebase: event.Type == EventSessionRebased}, nil
	default:
		return ProjectionChange{}, ErrUnknownSessionEvent
	}
}

func reduceSessionMetadata(view ProjectionView, event StoredSessionEvent) (ProjectionChange, error) {
	metadata := view.Metadata
	if metadata.SessionID == "" {
		metadata.SessionID = event.SessionID
	}
	var payload struct {
		WorkspaceID   string          `json:"workspaceId"`
		Title         string          `json:"title"`
		Provider      string          `json:"provider"`
		Model         string          `json:"model"`
		ThinkingLevel string          `json:"thinkingLevel"`
		ContextUsage  json.RawMessage `json:"contextUsage"`
	}
	if err := decodeEventPayload(event, &payload); err != nil {
		return ProjectionChange{}, err
	}
	if event.Type == EventSessionRenamed && strings.TrimSpace(payload.Title) == "" {
		return ProjectionChange{}, ErrInvalidSessionEvent
	}
	if payload.WorkspaceID != "" {
		metadata.WorkspaceID = payload.WorkspaceID
	}
	if payload.Title != "" {
		metadata.Title = payload.Title
	}
	if payload.Provider != "" {
		metadata.Provider = payload.Provider
	}
	if payload.Model != "" {
		metadata.Model = payload.Model
	}
	if payload.ThinkingLevel != "" {
		metadata.ThinkingLevel = payload.ThinkingLevel
	}
	if event.Type == EventSessionDeleted {
		metadata.Deleted = true
	}
	if event.Type == EventContextUsageUpdated && len(payload.ContextUsage) > 0 {
		metadata.ContextUsage = append(json.RawMessage(nil), payload.ContextUsage...)
	}
	metadata.UpdatedSeq = event.Seq
	return ProjectionChange{Stream: view.Stream, Metadata: &metadata}, nil
}

func validateProjectionEvent(stream SessionStreamState, event StoredSessionEvent) error {
	if event.Seq <= 0 || event.SessionID == "" || event.Type == "" || event.SourceID == "" {
		return ErrInvalidSessionEvent
	}
	if event.Type == EventRunStarted {
		if event.RunID == "" || stream.CurrentRunID != "" {
			return ErrStaleSessionRun
		}
		return nil
	}
	if runEvents[event.Type] {
		if event.RunID == "" || stream.CurrentRunID != event.RunID {
			return ErrStaleSessionRun
		}
		if event.SourceOrdinal == nil || *event.SourceOrdinal <= 0 {
			return ErrInvalidSessionEvent
		}
	}
	if requiresEntityID(event.Type) && !validEntityID(event.EntityID, event.RunID) {
		return ErrInvalidEntityID
	}
	return nil
}

func requiresEntityID(eventType string) bool {
	return eventType == EventAssistantSegmentStarted || eventType == EventAssistantSegmentDelta ||
		eventType == EventAssistantSegmentCommit || eventType == EventToolStarted ||
		eventType == EventToolArgsDelta || eventType == EventToolOutput || eventType == EventToolFinished
}

func validEntityID(entityID, runID string) bool {
	if entityID == "" || !strings.Contains(entityID, ":") {
		return false
	}
	if runID == "" {
		return true
	}
	return strings.Contains(entityID, ":"+runID+":")
}

func reduceUserMessage(view ProjectionView, event StoredSessionEvent) (ProjectionChange, error) {
	var payload struct {
		Text        string             `json:"text"`
		Attachments []PromptAttachment `json:"attachments"`
	}
	if err := decodeEventPayload(event, &payload); err != nil {
		return ProjectionChange{}, err
	}
	item := ProjectedSessionItem{
		ItemID: "user:" + event.SourceID, Ordinal: nextOrdinal(view), Kind: "user", Origin: "runtime",
		Text: payload.Text, Attachments: payload.Attachments, CreatedSeq: event.Seq, UpdatedSeq: event.Seq,
	}
	return ProjectionChange{Stream: view.Stream, Upsert: &item}, nil
}

func reduceAssistantSegment(view ProjectionView, event StoredSessionEvent) (ProjectionChange, error) {
	var payload struct {
		Kind  string `json:"kind"`
		Delta string `json:"delta"`
		Text  string `json:"text"`
	}
	if err := decodeEventPayload(event, &payload); err != nil {
		return ProjectionChange{}, err
	}
	item, exists := view.Items[event.EntityID]
	if !exists {
		item = ProjectedSessionItem{ItemID: event.EntityID, Ordinal: nextOrdinal(view), Kind: payload.Kind, Origin: "runtime", RunID: event.RunID, EntityID: event.EntityID, CreatedSeq: event.Seq}
	}
	if payload.Kind != "" {
		item.Kind = payload.Kind
	}
	if event.Type == EventAssistantSegmentStarted {
		item.Status = ItemStatusStreaming
	} else if event.Type == EventAssistantSegmentDelta {
		item.Text += payload.Delta
	} else {
		if payload.Text != "" {
			item.Text = payload.Text
		}
		item.Status = ItemStatusCommitted
	}
	item.UpdatedSeq = event.Seq
	return ProjectionChange{Stream: view.Stream, Upsert: &item}, nil
}

func reduceToolEvent(view ProjectionView, event StoredSessionEvent) (ProjectionChange, error) {
	var payload map[string]any
	if err := decodeEventPayload(event, &payload); err != nil {
		return ProjectionChange{}, err
	}
	item, exists := view.Items[event.EntityID]
	if event.Type == EventToolStarted && !exists {
		item = ProjectedSessionItem{ItemID: event.EntityID, Ordinal: nextOrdinal(view), Kind: "tool", Origin: "runtime", RunID: event.RunID, EntityID: event.EntityID, Status: ToolStatusRunning, CreatedSeq: event.Seq}
		eventString(&item.ToolCallID, payload, "toolCallId")
		eventString(&item.Tool, payload, "tool")
		eventString(&item.Args, payload, "args")
	} else if !exists {
		return ProjectionChange{}, fmt.Errorf("tool entity %s not found", event.EntityID)
	}
	switch event.Type {
	case EventToolArgsDelta:
		item.Args += stringValue(payload, "delta")
	case EventToolOutput:
		if stringValue(payload, "mode") == "replace" {
			item.Body = stringValue(payload, "chunk")
		} else {
			item.Body += stringValue(payload, "chunk")
		}
		syncContentMetadata(&item, event.Seq)
	case EventToolFinished:
		if isTerminalTool(item.Status) && !sameToolFinish(item, payload) {
			return ProjectionChange{}, ErrConflictingToolFinish
		}
		item.Body = stringValue(payload, "body")
		item.Status = stringOrDefault(stringValue(payload, "status"), ToolStatusOK)
		item.ResultMeta = stringValue(payload, "resultMeta")
		item.DurationMs = intValue(payload, "durationMs")
		syncContentMetadata(&item, event.Seq)
	}
	item.UpdatedSeq = event.Seq
	return ProjectionChange{Stream: view.Stream, Upsert: &item}, nil
}

func reduceTerminalRun(view ProjectionView, event StoredSessionEvent) (ProjectionChange, error) {
	if event.Type == EventRunFinished && hasRunningTool(view) {
		return ProjectionChange{}, ErrActiveToolAtRunFinish
	}
	status, toolStatus := terminalStatuses(event.Type)
	change := ProjectionChange{Stream: view.Stream, Terminalize: []ProjectedSessionItem{}}
	change.Stream.Status, change.Stream.CurrentRunID = status, ""
	for _, itemID := range view.Order {
		item := view.Items[itemID]
		if item.RunID != event.RunID || item.Status == "" || isTerminalItem(item.Status) {
			continue
		}
		if item.Kind == "tool" {
			item.Status = toolStatus
		} else {
			item.Status = ItemStatusCommitted
		}
		item.UpdatedSeq = event.Seq
		change.Terminalize = append(change.Terminalize, item)
	}
	if event.Type == EventRunFailed {
		errorItem, err := projectRunFailureError(view, event)
		if err != nil {
			return ProjectionChange{}, err
		}
		change.Upsert = errorItem
	}
	return change, nil
}

// projectRunFailureError converts a run failure payload into a durable transcript
// item so the message survives snapshot resyncs, rebases and session reloads.
func projectRunFailureError(view ProjectionView, event StoredSessionEvent) (*ProjectedSessionItem, error) {
	var payload struct {
		Error string `json:"error"`
	}
	if err := decodeEventPayload(event, &payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.Error) == "" {
		return nil, nil
	}
	item := ProjectedSessionItem{
		ItemID: "error:" + event.RunID, Ordinal: nextOrdinal(view), Kind: "error", Origin: "runtime",
		RunID: event.RunID, Text: payload.Error, CreatedSeq: event.Seq, UpdatedSeq: event.Seq,
	}
	return &item, nil
}

func terminalStatuses(eventType string) (string, string) {
	switch eventType {
	case EventRunCancelled:
		return SessionStatusCancelled, ToolStatusCancelled
	case EventRunInterrupted:
		return SessionStatusInterrupted, ToolStatusInterrupted
	case EventRunFailed:
		return SessionStatusError, ToolStatusErr
	default:
		return SessionStatusIdle, ToolStatusOK
	}
}

func syncContentMetadata(item *ProjectedSessionItem, seq int64) {
	if item.Body == "" {
		item.ContentRef, item.ContentVersion, item.ContentLength = "", 0, 0
		return
	}
	item.ContentRef = item.ItemID
	item.ContentVersion = seq
	item.ContentLength = int64(len(item.Body))
}

func hasRunningTool(view ProjectionView) bool {
	for _, item := range view.Items {
		if item.Kind == "tool" && item.Status == ToolStatusRunning {
			return true
		}
	}
	return false
}

func isTerminalItem(status string) bool {
	return status == ToolStatusOK || status == ToolStatusErr || status == ToolStatusCancelled ||
		status == ToolStatusInterrupted || status == ItemStatusCommitted
}

func isTerminalTool(status string) bool {
	return status == ToolStatusOK || status == ToolStatusErr || status == ToolStatusCancelled || status == ToolStatusInterrupted
}

func sameToolFinish(item ProjectedSessionItem, payload map[string]any) bool {
	return item.Body == stringValue(payload, "body") && item.Status == stringOrDefault(stringValue(payload, "status"), ToolStatusOK)
}

func decodeEventPayload(event StoredSessionEvent, target any) error {
	if len(event.Payload) == 0 || string(event.Payload) == "null" {
		return nil
	}
	if err := json.Unmarshal(event.Payload, target); err != nil {
		return fmt.Errorf("decode %s payload: %w", event.Type, err)
	}
	return nil
}

func nextOrdinal(view ProjectionView) int64 {
	max := int64(0)
	for _, item := range view.Items {
		if item.Ordinal > max {
			max = item.Ordinal
		}
	}
	return max + 1
}

func eventString(target *string, payload map[string]any, key string) {
	if value := stringValue(payload, key); value != "" {
		*target = value
	}
}

func stringValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func stringOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func intValue(payload map[string]any, key string) int {
	value, _ := payload[key].(float64)
	return int(value)
}
