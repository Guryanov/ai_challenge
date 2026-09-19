package main

import (
	"ai-chat/internal/history"
	"ai-chat/internal/memory"
)

// chatRequest — входящий HTTP-запрос от фронтенда.
type chatRequest struct {
	Message         string            `json:"message"`
	ResponseFormat  string            `json:"response_format"`
	Role            string            `json:"role"`
	SessionID       string            `json:"session_id"`
	ProjectID       string            `json:"project_id"`
	ProfileID       string            `json:"profile_id"`
	ContextStrategy string            `json:"context_strategy"`
	Facts           map[string]string `json:"facts"`
	BranchAction    string            `json:"branch_action"`
	WorkflowMode    string            `json:"workflow_mode"`
	Temperature     *float64          `json:"temperature"`
	MaxTokens       *int              `json:"max_tokens"`
	StopSequence    string            `json:"stop_sequence"`
}

// chatResponse — HTTP-ответ фронтенду.
type chatResponse struct {
	User               string              `json:"user"`
	Response           string              `json:"response"`
	FinishReason       string              `json:"finish_reason,omitempty"`
	DurationMs         int64               `json:"duration_ms,omitempty"`
	PromptTokens       int                 `json:"prompt_tokens,omitempty"`
	CompletionTokens   int                 `json:"completion_tokens,omitempty"`
	TotalTokens        int                 `json:"total_tokens,omitempty"`
	SessionTotalTokens int                 `json:"session_total_tokens,omitempty"`
	Compressed         bool                `json:"compressed,omitempty"`
	ActiveBranch       string              `json:"active_branch,omitempty"`
	Branches           []string            `json:"branches,omitempty"`
	Facts              map[string]string   `json:"facts,omitempty"`
	ProjectID          string              `json:"project_id,omitempty"`
	ProfileID          string              `json:"profile_id,omitempty"`
	TaskStage          string              `json:"task_stage,omitempty"`
	TaskStatus         string              `json:"task_status,omitempty"`
	TaskContext        history.TaskContext `json:"task_context,omitempty"`
	Error              string              `json:"error,omitempty"`
}

// invariantsRequest — запрос на сохранение инвариантов проекта.
type invariantsRequest struct {
	ProjectID  string             `json:"project_id"`
	Invariants []memory.Invariant `json:"invariants"`
}

// invariantsResponse — ответ со списком инвариантов проекта.
type invariantsResponse struct {
	Invariants []memory.Invariant `json:"invariants"`
}
