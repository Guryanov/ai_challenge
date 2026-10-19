package agent

import (
	"fmt"
	"strings"

	"ai-chat/internal/history"
)

const (
	summarySystemPrompt = `Сделай краткое, но информативное summary диалога.
Сохрани ключевые факты, контекст, имена, даты, договорённости и намерения пользователя.
Ответь одним коротким текстом без приветствий и лишних комментариев.`

	slidingWindowSize = 5
)

// contextResult — результат применения стратегии контекста.
type contextResult struct {
	messages      []history.Message
	summaryTokens int
}

// applyContextStrategy подготавливает историю для отправки в LLM.
func (a *SimpleAgent) applyContextStrategy(session *history.Session, strategy ContextStrategy) (contextResult, error) {
	branch := session.Branches[session.ActiveBranch]

	switch strategy {
	case StrategySummary:
		return a.applySummary(branch.Messages)
	case StrategySlidingWindow:
		return applySlidingWindow(branch.Messages), nil
	case StrategyFacts, StrategyBranching, StrategyFull:
		return contextResult{messages: branch.Messages}, nil
	default:
		return contextResult{messages: branch.Messages}, nil
	}
}

// applySummary сохраняет 2 последних сообщения и заменяет остальные на summary.
func (a *SimpleAgent) applySummary(messages []history.Message) (contextResult, error) {
	if len(messages) <= 2 {
		return contextResult{messages: messages}, nil
	}

	context := messages[:len(messages)-2]

	hasRaw := false
	for _, m := range context {
		if !m.IsSummary {
			hasRaw = true
			break
		}
	}
	if !hasRaw {
		return contextResult{messages: messages}, nil
	}

	summaryText, summaryResp, err := a.summarize(context)
	if err != nil {
		return contextResult{}, fmt.Errorf("ошибка генерации summary: %w", err)
	}

	summaryMsg := history.Message{
		Role:             "system",
		Content:          "Контекст предыдущего диалога:\n" + summaryText,
		IsSummary:        true,
		PromptTokens:     summaryResp.PromptTokens,
		CompletionTokens: summaryResp.CompletionTokens,
		TotalTokens:      summaryResp.TotalTokens,
	}

	return contextResult{
		messages:      append([]history.Message{summaryMsg}, messages[len(messages)-2:]...),
		summaryTokens: summaryResp.TotalTokens,
	}, nil
}

// applySlidingWindow оставляет только последние N обычных сообщений,
// исключая summary-сообщения, чтобы не передавать устаревший контекст.
func applySlidingWindow(messages []history.Message) contextResult {
	var raw []history.Message
	for _, m := range messages {
		if !m.IsSummary {
			raw = append(raw, m)
		}
	}
	if len(raw) <= slidingWindowSize {
		return contextResult{messages: raw}
	}
	return contextResult{messages: raw[len(raw)-slidingWindowSize:]}
}

// formatConversation форматирует сообщения для summary.
func formatConversation(messages []history.Message) string {
	var b strings.Builder
	for _, m := range messages {
		role := m.Role
		if m.IsSummary {
			role = "summary"
		}
		b.WriteString(fmt.Sprintf("%s: %s\n", role, m.Content))
	}
	return strings.TrimSpace(b.String())
}
