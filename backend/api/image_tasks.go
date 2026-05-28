package api

import (
	"context"
	"time"

	"chatgpt2api/internal/accounts"
	"chatgpt2api/internal/imagehistory"
)

type imageTaskStatus string

const (
	imageTaskStatusQueued          imageTaskStatus = "queued"
	imageTaskStatusRunning         imageTaskStatus = "running"
	imageTaskStatusSucceeded       imageTaskStatus = "succeeded"
	imageTaskStatusFailed          imageTaskStatus = "failed"
	imageTaskStatusCancelRequested imageTaskStatus = "cancel_requested"
	imageTaskStatusCancelled       imageTaskStatus = "cancelled"
	imageTaskStatusExpired         imageTaskStatus = "expired"
)

type imageTaskWaitingReason string

const (
	imageTaskWaitingReasonNone                  imageTaskWaitingReason = ""
	imageTaskWaitingReasonGlobalConcurrency     imageTaskWaitingReason = "global_concurrency"
	imageTaskWaitingReasonPaidAccountBusy       imageTaskWaitingReason = "paid_account_busy"
	imageTaskWaitingReasonCompatibleAccountBusy imageTaskWaitingReason = "compatible_account_busy"
	imageTaskWaitingReasonSourceAccountBusy     imageTaskWaitingReason = "source_account_busy"
	imageTaskWaitingReasonRetryBackoff          imageTaskWaitingReason = "retry_backoff"
)

type imageTaskSourceImagePayload struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Name    string `json:"name"`
	DataURL string `json:"dataUrl,omitempty"`
	URL     string `json:"url,omitempty"`
}

type imageTaskReferenceImagePayload struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	DataURL string `json:"dataUrl,omitempty"`
	URL     string `json:"url,omitempty"`
}

type imageTaskSourceReferencePayload struct {
	OriginalFileID     string `json:"original_file_id"`
	OriginalGenID      string `json:"original_gen_id"`
	ConversationID     string `json:"conversation_id,omitempty"`
	ParentMessageID    string `json:"parent_message_id,omitempty"`
	ResponseID         string `json:"response_id,omitempty"`
	ResponseProviderID string `json:"response_provider_id,omitempty"`
	SourceAccountID    string `json:"source_account_id"`
}

type imageTaskContextReferencePayload struct {
	ConversationID     string `json:"conversation_id,omitempty"`
	ParentMessageID    string `json:"parent_message_id,omitempty"`
	ResponseID         string `json:"response_id,omitempty"`
	ResponseProviderID string `json:"response_provider_id,omitempty"`
	SourceAccountID    string `json:"source_account_id"`
}

type createImageTaskRequest struct {
	UserID              string                              `json:"-"`
	Username            string                              `json:"-"`
	TaskID              string                              `json:"taskId,omitempty"`
	ConversationID      string                              `json:"conversationId"`
	TurnID              string                              `json:"turnId"`
	Source              string                              `json:"source,omitempty"`
	Mode                string                              `json:"mode"`
	Prompt              string                              `json:"prompt"`
	Model               string                              `json:"model"`
	Count               int                                 `json:"count"`
	Size                string                              `json:"size,omitempty"`
	ResolutionAccess    string                              `json:"resolutionAccess,omitempty"`
	Quality             string                              `json:"quality,omitempty"`
	Background          string                              `json:"background,omitempty"`
	ResponseFormat      string                              `json:"responseFormat,omitempty"`
	RetryImageIndex     *int                                `json:"retryImageIndex,omitempty"`
	PrivatePhotoMode    bool                                `json:"privatePhotoMode,omitempty"`
	CommonSystemHint    string                              `json:"commonSystemHint,omitempty"`
	PrivateSystemHint   string                              `json:"privateSystemHint,omitempty"`
	SourceImages        []imageTaskSourceImagePayload       `json:"sourceImages,omitempty"`
	ReferenceImages     []imageTaskReferenceImagePayload    `json:"referenceImages,omitempty"`
	SourceReference     *imageTaskSourceReferencePayload    `json:"sourceReference,omitempty"`
	ContextReference    *imageTaskContextReferencePayload   `json:"contextReference,omitempty"`
	ConversationContext string                              `json:"conversationContext,omitempty"`
	Policy              *accounts.ImageAccountRoutingPolicy `json:"policy,omitempty"`
	SystemHint          string                              `json:"systemHint,omitempty"`
	RequestBaseURL      string                              `json:"-"`
}

type imageTaskBlocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

type imageTaskSourceSnapshot struct {
	Workspace int `json:"workspace"`
	Compat    int `json:"compat"`
}

type imageTaskFinalStatusSnapshot struct {
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Expired   int `json:"expired"`
}

type imageTaskView struct {
	ID              string                 `json:"id"`
	UserID          string                 `json:"userId,omitempty"`
	ConversationID  string                 `json:"conversationId"`
	TurnID          string                 `json:"turnId"`
	Mode            string                 `json:"mode"`
	Status          imageTaskStatus        `json:"status"`
	CreatedAt       string                 `json:"createdAt"`
	StartedAt       string                 `json:"startedAt,omitempty"`
	FinishedAt      string                 `json:"finishedAt,omitempty"`
	Count           int                    `json:"count"`
	RetryImageIndex *int                   `json:"retryImageIndex,omitempty"`
	QueuePosition   int                    `json:"queuePosition,omitempty"`
	WaitingReason   imageTaskWaitingReason `json:"waitingReason,omitempty"`
	Blockers        []imageTaskBlocker     `json:"blockers,omitempty"`
	Images          []imagehistory.Image   `json:"images"`
	Error           string                 `json:"error,omitempty"`
	CancelRequested bool                   `json:"cancelRequested,omitempty"`
}

type imageTaskSnapshot struct {
	Running          int                          `json:"running"`
	MaxRunning       int                          `json:"maxRunning"`
	Queued           int                          `json:"queued"`
	Total            int                          `json:"total"`
	ActiveSources    imageTaskSourceSnapshot      `json:"activeSources"`
	FinalStatuses    imageTaskFinalStatusSnapshot `json:"finalStatuses"`
	RetentionSeconds int                          `json:"retentionSeconds"`
}

type imageTaskEvent struct {
	Type     string             `json:"type"`
	TaskID   string             `json:"taskId,omitempty"`
	Task     *imageTaskView     `json:"task,omitempty"`
	Snapshot *imageTaskSnapshot `json:"snapshot,omitempty"`
}

type imageTaskRequirement struct {
	NeedPaid        bool
	SourceAccountID string
	PolicySnapshot  *accounts.ImageAccountRoutingPolicy
}

type imageTaskSourceImage struct {
	ID      string
	Role    string
	Name    string
	DataURL string
	URL     string
}

type imageTaskSourceReference struct {
	OriginalFileID     string
	OriginalGenID      string
	ConversationID     string
	ParentMessageID    string
	ResponseID         string
	ResponseProviderID string
	SourceAccountID    string
}

type imageTaskContextReference struct {
	ConversationID     string
	ParentMessageID    string
	ResponseID         string
	ResponseProviderID string
	SourceAccountID    string
}

type imageTaskUnit struct {
	Index             int
	Status            imageTaskStatus
	StartedAt         time.Time
	FinishedAt        time.Time
	Error             string
	DeferredCount     int
	NextAttemptAt     time.Time
	Cancel            context.CancelFunc
	Attempted         map[string]struct{}
	ExternalStart     int
	ExternalStartInit bool
}

type imageTask struct {
	ID                  string
	UserID              string
	Username            string
	ConversationID      string
	TurnID              string
	Source              string
	Mode                string
	Prompt              string
	Model               string
	Count               int
	RetryImageIndex     *int
	Size                string
	ResolutionAccess    string
	Quality             string
	Background          string
	ResponseFormat      string
	PrivatePhotoMode    bool
	CommonSystemHint    string
	PrivateSystemHint   string
	SystemHint          string
	ConversationContext string
	RequestBaseURL      string
	SourceImages        []imageTaskSourceImage
	ReferenceImages     []imageTaskSourceImage
	SourceReference     *imageTaskSourceReference
	ContextReference    *imageTaskContextReference
	Requirement         imageTaskRequirement
	CreatedAt           time.Time
	StartedAt           time.Time
	FinishedAt          time.Time
	Status              imageTaskStatus
	WaitingReason       imageTaskWaitingReason
	Blockers            []imageTaskBlocker
	Images              []imagehistory.Image
	Error               string
	Units               []imageTaskUnit
	ActiveUnits         int
	CancelRequested     bool
}
