package agent

import "time"

// AgentRequest — запрос к агенту.
type AgentRequest struct {
	Message         string
	ResponseFormat  string
	Role            string
	SessionID       string
	CompressHistory bool
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
	Duration           time.Duration
}

// Agent — интерфейс LLM-агента.
// Реализации могут добавлять историю, инструменты и другие возможности.
type Agent interface {
	Run(req AgentRequest) (AgentResponse, error)
	ClearHistory(sessionID string) error
}
