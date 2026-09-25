package agent

import (
	"strings"

	"ai-chat/internal/history"
	"ai-chat/internal/profile"
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

func formatUserProfileContext(p profile.Profile) string {
	if strings.TrimSpace(p.Description) == "" {
		return ""
	}
	return "Профиль пользователя:\n" + strings.TrimSpace(p.Description)
}

func buildMessages(cfg Config, req AgentRequest, history []history.Message, userProfileContext, projectContext, invariantContext, workflowContext string) []map[string]any {
	var messages []map[string]any

	systemContent := buildSystemPrompt(cfg, req.Role)
	if systemContent != "" {
		messages = append(messages, map[string]any{"role": "system", "content": systemContent})
	}

	if userProfileContext != "" {
		messages = append(messages, map[string]any{"role": "system", "content": userProfileContext})
	}

	if invariantContext != "" {
		messages = append(messages, map[string]any{"role": "system", "content": invariantContext})
	}

	if projectContext != "" {
		messages = append(messages, map[string]any{"role": "system", "content": projectContext})
	}

	if workflowContext != "" {
		messages = append(messages, map[string]any{"role": "system", "content": workflowContext})
	}

	// Добавляем историю предыдущих сообщений.
	for _, msg := range history {
		messages = append(messages, messageToOpenAI(msg))
	}

	// Текущее сообщение пользователя.
	userContent := req.Message
	if cfg.UserPromptTemplate != "" {
		userContent = strings.ReplaceAll(cfg.UserPromptTemplate, "{message}", req.Message)
	}
	messages = append(messages, map[string]any{"role": "user", "content": userContent})

	if cfg.AssistantPrompt != "" {
		messages = append(messages, map[string]any{"role": "assistant", "content": cfg.AssistantPrompt})
	}

	return messages
}

// messageToOpenAI преобразует историческое сообщение в OpenAI-совместимый формат.
func messageToOpenAI(msg history.Message) map[string]any {
	m := map[string]any{
		"role":    msg.Role,
		"content": msg.Content,
	}

	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		m["tool_calls"] = toolCallsToOpenAI(msg.ToolCalls)
		if msg.Content == "" {
			m["content"] = nil
		}
	}

	if msg.Role == "tool" && msg.ToolCallID != "" {
		m["tool_call_id"] = msg.ToolCallID
	}

	return m
}

// toolCallsToOpenAI преобразует исторические ToolCall в OpenAI-формат.
func toolCallsToOpenAI(calls []history.ToolCall) []map[string]any {
	result := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		result = append(result, map[string]any{
			"id":   c.ID,
			"type": c.Type,
			"function": map[string]any{
				"name":      c.Function.Name,
				"arguments": c.Function.Arguments,
			},
		})
	}
	return result
}
