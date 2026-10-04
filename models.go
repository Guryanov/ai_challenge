package main

import (
	"ai-chat/internal/history"
	"ai-chat/internal/knowledge"
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
	RAGEnabled      *bool             `json:"rag_enabled"`
	RAGStrategy     string            `json:"rag_strategy"`
	RAGTopK         int               `json:"rag_top_k"`
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

// digestResponse — ответ с ежедневной сводкой.
type digestResponse struct {
	Content     string `json:"content,omitempty"`
	GeneratedAt string `json:"generated_at,omitempty"`
	Error       string `json:"error,omitempty"`
}

// kbFile — загруженный файл базы знаний.
type kbFile struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	SizeBytes int    `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	Indexed   bool   `json:"indexed"`
	UpdatedAt string `json:"updated_at"`
}

// kbFilesResponse — список файлов базы знаний.
type kbFilesResponse struct {
	Files []kbFile `json:"files"`
}

// kbIndexRequest — запрос индексации файлов проекта.
type kbIndexRequest struct {
	ProjectID string `json:"project_id"`
	Strategy  string `json:"strategy"`
}

// kbIndexResponse — результаты индексации.
type kbIndexResponse struct {
	Runs []knowledge.IndexRun `json:"runs"`
}

// kbSearchRequest — запрос поиска по индексу.
type kbSearchRequest struct {
	ProjectID string `json:"project_id"`
	Strategy  string `json:"strategy"`
	Query     string `json:"query"`
	K         int    `json:"k"`
}

// kbSearchResult — результат поиска.
type kbSearchResult struct {
	Path    string  `json:"path"`
	Name    string  `json:"name"`
	Section string  `json:"section"`
	ChunkID string  `json:"chunk_id"`
	Score   float64 `json:"score"`
	Text    string  `json:"text"`
}

// kbSearchResponse — результаты поиска.
type kbSearchResponse struct {
	Results []kbSearchResult `json:"results"`
}

// kbQueryRequest — создание тест-запроса.
type kbQueryRequest struct {
	ProjectID       string `json:"project_id"`
	Text            string `json:"text"`
	ExpectedPath    string `json:"expected_path"`
	ExpectedSection string `json:"expected_section"`
}

// kbQueriesResponse — список тест-запросов.
type kbQueriesResponse struct {
	Queries []knowledge.Query `json:"queries"`
}

// kbGenerateRequest — авто-генерация тест-запросов через LLM.
type kbGenerateRequest struct {
	ProjectID string `json:"project_id"`
	Strategy  string `json:"strategy"`
	N         int    `json:"n"`
}

// kbBenchmarkRequest — запуск оценки стратегий.
type kbBenchmarkRequest struct {
	ProjectID string `json:"project_id"`
	K         int    `json:"k"`
}

// kbBenchmarkResponse — результаты оценки стратегий.
type kbBenchmarkResponse struct {
	Runs []knowledge.BenchmarkRun `json:"runs"`
}
