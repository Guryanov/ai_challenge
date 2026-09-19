package agent

import "time"

// ContextStrategy — стратегия управления контекстом диалога.
type ContextStrategy string

const (
	StrategyFull          ContextStrategy = "full"
	StrategySummary       ContextStrategy = "summary"
	StrategySlidingWindow ContextStrategy = "sliding_window"
	StrategyFacts         ContextStrategy = "facts"
	StrategyBranching     ContextStrategy = "branching"
)

// AgentRequest — запрос к агенту.
type AgentRequest struct {
	Message         string
	ResponseFormat  string
	Role            string
	SessionID       string
	ProjectID       string
	ContextStrategy ContextStrategy
	Facts           map[string]string
	BranchAction    string // create:<name> | switch:<name>
	Temperature     *float64
	MaxTokens       *int
	StopSequence    string
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
	Duration           time.Duration
}

// Agent — интерфейс LLM-агента.
// Реализации могут добавлять историю, инструменты и другие возможности.
type Agent interface {
	Run(req AgentRequest) (AgentResponse, error)
	Manage(req AgentRequest) (AgentResponse, error)
	ClearHistory(sessionID string) error
}
