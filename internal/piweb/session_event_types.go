package piweb

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	EventUserMessageCreated      = "user.message.created"
	EventRunStarted              = "run.started"
	EventRunFinished             = "run.finished"
	EventRunFailed               = "run.failed"
	EventRunCancelled            = "run.cancelled"
	EventRunInterrupted          = "run.interrupted"
	EventAssistantSegmentStarted = "assistant.segment.started"
	EventAssistantSegmentDelta   = "assistant.segment.delta"
	EventAssistantSegmentCommit  = "assistant.segment.committed"
	EventToolStarted             = "tool.started"
	EventToolArgsDelta           = "tool.args.delta"
	EventToolOutput              = "tool.output"
	EventToolFinished            = "tool.finished"
	EventSessionRebased          = "session.rebased"
	EventSessionRenamed          = "session.renamed"
	EventWorkspaceFilesChanged   = "workspace.files.changed"
)

const (
	SessionStatusIdle        = "idle"
	SessionStatusRunning     = "running"
	SessionStatusThinking    = "thinking"
	SessionStatusCompacting  = "compacting"
	SessionStatusCancelled   = "cancelled"
	SessionStatusInterrupted = "interrupted"
	SessionStatusError       = "error"
	ToolStatusRunning        = "running"
	ToolStatusOK             = "ok"
	ToolStatusErr            = "err"
	ToolStatusCancelled      = "cancelled"
	ToolStatusInterrupted    = "interrupted"
	ItemStatusStreaming      = "streaming"
	ItemStatusCommitted      = "committed"
)

type SessionEventDraft struct {
	SessionID     string
	RunID         string
	SourceID      string
	SourceOrdinal *int64
	Type          string
	EntityID      string
	Payload       json.RawMessage
	OccurredAt    time.Time
}

type StoredSessionEvent struct {
	Seq int64 `json:"-"`
	SessionEventDraft
}

type SessionStreamState struct {
	SessionID         string
	CurrentSeq        int64
	Status            string
	CurrentRunID      string
	ProjectionVersion int64
	SourceOrdinal     int64
	SourceHash        string
	SourceSeq         int64
}

type ProjectedSessionItem struct {
	ItemID         string             `json:"itemId"`
	Ordinal        int64              `json:"ordinal"`
	Kind           string             `json:"kind"`
	Origin         string             `json:"origin"`
	RunID          string             `json:"runId,omitempty"`
	EntityID       string             `json:"entityId,omitempty"`
	Text           string             `json:"text,omitempty"`
	Tool           string             `json:"tool,omitempty"`
	ToolCallID     string             `json:"toolCallId,omitempty"`
	Args           string             `json:"args,omitempty"`
	Body           string             `json:"body,omitempty"`
	Status         string             `json:"status,omitempty"`
	ResultMeta     string             `json:"resultMeta,omitempty"`
	DurationMs     int                `json:"durationMs,omitempty"`
	Attachments    []PromptAttachment `json:"attachments,omitempty"`
	CreatedSeq     int64              `json:"createdSeq"`
	UpdatedSeq     int64              `json:"updatedSeq"`
	BlockIndex     int                `json:"blockIndex,omitempty"`
	ContentRef     string             `json:"contentRef,omitempty"`
	ContentVersion int64              `json:"contentVersion,omitempty"`
	ContentLength  int64              `json:"contentLength,omitempty"`
}

type ProjectionView struct {
	Stream SessionStreamState
	Order  []string
	Items  map[string]ProjectedSessionItem
}

type ProjectionChange struct {
	Stream        SessionStreamState
	Upsert        *ProjectedSessionItem
	Terminalize   []ProjectedSessionItem
	DeleteItemIDs []string
	RequireRebase bool
}

var (
	ErrUnknownSessionEvent     = errors.New("unknown session event")
	ErrInvalidSessionEvent     = errors.New("invalid session event")
	ErrInvalidEntityID         = errors.New("invalid session entity id")
	ErrStaleSessionRun         = errors.New("stale session run")
	ErrActiveToolAtRunFinish   = errors.New("active tool at run finish")
	ErrConflictingToolFinish   = errors.New("conflicting tool finish")
	ErrCommandHashConflict     = errors.New("command request hash conflict")
	ErrInvalidSourceOrdinal    = errors.New("invalid source ordinal")
	ErrProjectionCursorExpired = errors.New("projection cursor expired")
	ErrInvalidProjectionCursor = errors.New("invalid projection cursor")
	ErrContentVersionConflict  = errors.New("content version conflict")
	ErrInvalidContentRange     = errors.New("invalid content range")
)
