package piweb

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

var ErrEventStoreNotReady = errors.New("session event store operation is not ready")

type SessionEventStore interface {
	StartRun(context.Context, StartRunCommand) (RunReceipt, error)
	Append(context.Context, string, SessionEventDraft) (StoredSessionEvent, error)
	Snapshot(context.Context, string, SessionPageRequest) (SessionStateSnapshot, error)
	EventsAfter(context.Context, string, int64, EventReadLimit) (EventBatch, error)
	ItemContent(context.Context, string, string, int64, int64, int64) ([]byte, int64, error)
	OpenSession(context.Context, SessionSource) (ImportReport, error)
	RecoverInterrupted(context.Context) error
	DeleteSession(context.Context, string) error
	Close(context.Context) error
}

type StartRunCommand struct {
	SessionID       string
	ClientRequestID string
	RequestHash     string
	RunID           string
	DisplayText     string
	Attachments     []PromptAttachment
}

type RunReceipt struct {
	RunID       string `json:"runId"`
	AcceptedSeq int64  `json:"-"`
	Existing    bool   `json:"existing"`
}

type SessionPageRequest struct {
	Limit  int
	Before string
}

type SessionStateSnapshot struct {
	Metadata                SessionMetadataProjection `json:"metadata"`
	Items                   []ProjectedSessionItem    `json:"items"`
	Status                  string                    `json:"status"`
	RunID                   string                    `json:"runId,omitempty"`
	Seq                     int64                     `json:"-"`
	Cursor                  string                    `json:"cursor,omitempty"`
	HasMore                 bool                      `json:"hasMore"`
	Limit                   int                       `json:"limit"`
	ProjectionSchemaVersion int                       `json:"projectionSchemaVersion"`
	ProjectionGeneration    int64                     `json:"-"`
}

type EventReadLimit struct {
	MaxRows  int
	MaxBytes int
}

type EventBatch struct {
	Events        []StoredSessionEvent
	ResetRequired bool
}

type SessionSource struct {
	SessionID string
	Path      string
}

type ImportReport struct {
	Imported bool
	Deferred bool
	Warning  string
}

type RetentionLimits struct {
	ActiveRows  int
	ActiveBytes int
	FinalRows   int
	FinalBytes  int
}

type SQLiteSessionEventStore struct {
	db                  *sql.DB
	hub                 *SessionWakeHub
	writeSlot           chan struct{}
	closeMu             sync.Mutex
	closed              bool
	eventStoreFaultHook func(EventStoreFaultPoint) error
	retention           RetentionLimits
}
