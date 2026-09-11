package agent

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
}
