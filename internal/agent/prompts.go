package agent

import (
	"fmt"
	"strings"
	"unicode/utf8"

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

// formatKnowledgeContext форматирует найденные в базе знаний фрагменты
// в системное сообщение с указанием источника.
func formatKnowledgeContext(chunks []RetrievedChunk) string {
	if len(chunks) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Релевантные фрагменты из базы знаний проекта. ")
	b.WriteString("Используй их для ответа; проверяемый список источников и цитат формируется автоматически, поэтому не придумывай источники сам. ")
	b.WriteString("Если ответа во фрагментах нет, скажи об этом и не выдумывай.\n")
	for i, c := range chunks {
		b.WriteString(fmt.Sprintf("\n[%d] %s", i+1, c.Path))
		if c.Section != "" {
			b.WriteString(" — " + c.Section)
		}
		if c.ChunkID != "" {
			b.WriteString(" (chunk_id: " + c.ChunkID + ")")
		}
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(c.Text))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// sourceQuoteLimit — максимальная длина цитаты в рунах.
const sourceQuoteLimit = 300

// quoteExcerpt возвращает дословный фрагмент текста, усечённый до лимита.
func quoteExcerpt(text string) string {
	trimmed := strings.TrimSpace(text)
	if utf8.RuneCountInString(trimmed) <= sourceQuoteLimit {
		return trimmed
	}
	return strings.TrimSpace(string([]rune(trimmed)[:sourceQuoteLimit])) + "…"
}

// FormatSourcesBlock формирует детерминированный блок источников и цитат,
// который дописывается к текстовому ответу. Пустой список даёт пустую строку.
func FormatSourcesBlock(sources []SourceRef) string {
	if len(sources) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n---\nИсточники:\n")
	for i, s := range sources {
		source := s.Path
		if source == "" {
			source = s.Name
		}
		b.WriteString(fmt.Sprintf("%d. %s", i+1, source))
		if s.Section != "" {
			b.WriteString(" — " + s.Section)
		}
		if s.ChunkID != "" {
			b.WriteString(" (chunk_id: " + s.ChunkID + ")")
		}
		b.WriteString("\n")
		if q := quoteExcerpt(s.Quote); q != "" {
			b.WriteString("   «" + q + "»\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// toSourceRefs преобразует найденные чанки в источники с цитатами.
func toSourceRefs(chunks []RetrievedChunk) []SourceRef {
	if len(chunks) == 0 {
		return nil
	}
	sources := make([]SourceRef, 0, len(chunks))
	for _, c := range chunks {
		sources = append(sources, SourceRef{
			Path:    c.Path,
			Name:    c.Name,
			Section: c.Section,
			ChunkID: c.ChunkID,
			Score:   c.Score,
			Quote:   quoteExcerpt(c.Text),
		})
	}
	return sources
}

func buildMessages(cfg Config, req AgentRequest, history []history.Message, userProfileContext, projectContext, invariantContext, workflowContext, knowledgeContext string) []map[string]any {
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

	if knowledgeContext != "" {
		messages = append(messages, map[string]any{"role": "system", "content": knowledgeContext})
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
