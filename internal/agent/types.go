package agent

import (
	"context"
	"time"

	"ai-chat/internal/history"
)

// ContextStrategy — стратегия управления контекстом диалога.
type ContextStrategy string

const (
	StrategyFull          ContextStrategy = "full"
	StrategySummary       ContextStrategy = "summary"
	StrategySlidingWindow ContextStrategy = "sliding_window"
	StrategyFacts         ContextStrategy = "facts"
	StrategyBranching     ContextStrategy = "branching"
)

// Режимы обработки запроса.
const (
	WorkflowModeChat     = "chat"
	WorkflowModeWorkflow = "workflow"
)

// AgentRequest — запрос к агенту.
type AgentRequest struct {
	Message         string
	ResponseFormat  string
	Role            string
	SessionID       string
	ProjectID       string
	ProfileID       string
	ContextStrategy ContextStrategy
	Facts           map[string]string
	BranchAction    string // create:<name> | switch:<name>
	TaskAction      string // "approve" | "reject" | ""
	RejectionReason string
	WorkflowMode    string // "chat" | "workflow"
	Temperature     *float64
	MaxTokens       *int
	StopSequence    string
	Context         context.Context
	RAGEnabled      *bool    // nil — использовать значение по умолчанию из конфигурации
	RAGStrategy     string   // "fixed" | "structure"; пусто — значение по умолчанию
	RAGTopK         int      // <= 0 — значение по умолчанию
	RAGCandidates   int      // кандидатов до фильтра; <= 0 — значение по умолчанию
	RAGThreshold    *float64 // порог cosine; nil — значение по умолчанию
}

// RetrieveOptions — параметры RAG-поиска, передаваемые ретриверу.
type RetrieveOptions struct {
	Strategy   string
	Query      string
	Candidates int
	TopK       int
	Mode       string
	Threshold  *float64
}

// AgentResponse — ответ агента.
type AgentResponse struct {
	Content            string
	FinishReason       string
	PromptTokens       int
	CompletionTokens   int
	TotalTokens        int
	SessionTotalTokens int
	Compressed         bool
	ActiveBranch       string
	Branches           []string
	Facts              map[string]string
	ProjectID          string
	ProfileID          string
	TaskStage          string
	TaskStatus         string
	TaskContext        history.TaskContext
	Duration           time.Duration
}

// RetrievedChunk — фрагмент, найденный в базе знаний проекта.
type RetrievedChunk struct {
	Path    string
	Name    string
	Section string
	Score   float64
	Text    string
}

// KnowledgeRetriever ищет релевантные фрагменты базы знаний проекта.
type KnowledgeRetriever interface {
	Retrieve(ctx context.Context, projectID string, opts RetrieveOptions) ([]RetrievedChunk, error)
}

// Agent — интерфейс LLM-агента.
// Реализации могут добавлять историю, инструменты и другие возможности.
type Agent interface {
	Run(req AgentRequest) (AgentResponse, error)
	Manage(req AgentRequest) (AgentResponse, error)
	ClearHistory(sessionID string) error
}
