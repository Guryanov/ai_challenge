package agent

import (
	"ai-chat/internal/memory"
)

// Config — настройки агента.
type Config struct {
	Model                    string
	ExternalAPI              string
	APIKey                   string
	AuthType                 string
	APIFormat                string
	SystemPrompt             string
	AssistantPrompt          string
	UserPromptTemplate       string
	OrchestratorInstructions string
	Roles                    map[string]string
	SummaryMaxTokens         int
	MemoryStore              memory.Store
}
