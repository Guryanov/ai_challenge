package agent

import (
	"strings"

	"ai-chat/internal/history"
)

func buildSystemPrompt(cfg Config, role string) string {
	var parts []string
	if cfg.OrchestratorInstructions != "" {
		parts = append(parts, cfg.OrchestratorInstructions)
	}
	if cfg.SystemPrompt != "" {
		parts = append(parts, cfg.SystemPrompt)
	}
	if role != "" && cfg.Roles[role] != "" {
		parts = append(parts, cfg.Roles[role])
	}
	return strings.Join(parts, "\n\n")
}

func buildMessages(cfg Config, req AgentRequest, history []history.Message, projectContext string) []map[string]string {
	var messages []map[string]string

	systemContent := buildSystemPrompt(cfg, req.Role)
	if systemContent != "" {
		messages = append(messages, map[string]string{"role": "system", "content": systemContent})
	}

	if projectContext != "" {
		messages = append(messages, map[string]string{"role": "system", "content": projectContext})
	}

	// Добавляем историю предыдущих сообщений.
	for _, msg := range history {
		messages = append(messages, map[string]string{"role": msg.Role, "content": msg.Content})
	}

	// Текущее сообщение пользователя.
	userContent := req.Message
	if cfg.UserPromptTemplate != "" {
		userContent = strings.ReplaceAll(cfg.UserPromptTemplate, "{message}", req.Message)
	}
	messages = append(messages, map[string]string{"role": "user", "content": userContent})

	if cfg.AssistantPrompt != "" {
		messages = append(messages, map[string]string{"role": "assistant", "content": cfg.AssistantPrompt})
	}

	return messages
}
