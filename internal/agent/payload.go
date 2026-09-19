package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"ai-chat/internal/history"
	"ai-chat/internal/memory"
)

type openaiPayload struct {
	Model          string              `json:"model"`
	Messages       []map[string]string `json:"messages"`
	ResponseFormat *responseFormat     `json:"response_format,omitempty"`
	Temperature    *float64            `json:"temperature,omitempty"`
	MaxTokens      *int                `json:"max_tokens,omitempty"`
	Stop           string              `json:"stop,omitempty"`
	Args           []string            `json:"args,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

func buildPayload(cfg Config, req AgentRequest, history []history.Message, userProfileContext, projectContext, invariantContext string) ([]byte, error) {
	switch cfg.APIFormat {
	case "openai":
		var rf *responseFormat
		if req.ResponseFormat == "json" {
			rf = &responseFormat{Type: "json_object"}
		}

		return json.Marshal(openaiPayload{
			Model:          cfg.Model,
			Messages:       buildMessages(cfg, req, history, userProfileContext, projectContext, invariantContext),
			ResponseFormat: rf,
			Temperature:    req.Temperature,
			MaxTokens:      req.MaxTokens,
			Stop:           req.StopSequence,
			Args:           []string{"-y", "@orchestrator-agent"},
		})
	default:
		userContent := req.Message
		if cfg.UserPromptTemplate != "" {
			userContent = strings.ReplaceAll(cfg.UserPromptTemplate, "{message}", req.Message)
		}
		systemContent := buildSystemPrompt(cfg, req.Role)
		if systemContent != "" {
			userContent = systemContent + "\n\n" + userContent
		}
		if userProfileContext != "" {
			userContent = userProfileContext + "\n\n" + userContent
		}
		if invariantContext != "" {
			userContent = invariantContext + "\n\n" + userContent
		}
		if projectContext != "" {
			userContent = projectContext + "\n\n" + userContent
		}
		if cfg.AssistantPrompt != "" {
			userContent = userContent + "\n\n" + cfg.AssistantPrompt
		}
		return json.Marshal(map[string]string{"message": userContent})
	}
}

// buildSummaryPayload формирует запрос к LLM для генерации summary старых сообщений.
func buildSummaryPayload(cfg Config, messages []history.Message) ([]byte, error) {
	conversation := formatConversation(messages)

	switch cfg.APIFormat {
	case "openai":
		return json.Marshal(openaiPayload{
			Model: cfg.Model,
			Messages: []map[string]string{
				{"role": "system", "content": summarySystemPrompt},
				{"role": "user", "content": conversation},
			},
			Temperature: ptrFloat64(0.3),
			MaxTokens:   ptrInt(cfg.SummaryMaxTokens),
			Args:        []string{"-y", "@orchestrator-agent"},
		})
	default:
		return json.Marshal(map[string]string{
			"message": summarySystemPrompt + "\n\n" + conversation,
		})
	}
}

// buildMemorySummaryPayload формирует запрос к LLM для генерации summary
// старых записей долгосрочной памяти проекта.
func buildMemorySummaryPayload(cfg Config, entries []memory.Entry) ([]byte, error) {
	var b strings.Builder
	b.WriteString("Сделай краткое, но содержательное summary следующих записей долгосрочной памяти проекта. " +
		"Сохрани ключевые технологии, паттерны, принципы и знания. Ответь одним коротким текстом без приветствий и лишних комментариев.\n\n")
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("[%s] %s: %s\n%s\n\n", e.Type, e.Title, strings.Join(e.Tags, ", "), e.Content))
	}

	switch cfg.APIFormat {
	case "openai":
		return json.Marshal(openaiPayload{
			Model: cfg.Model,
			Messages: []map[string]string{
				{"role": "system", "content": "Ты помощник по обобщению знаний проекта."},
				{"role": "user", "content": strings.TrimSpace(b.String())},
			},
			Temperature: ptrFloat64(0.3),
			MaxTokens:   ptrInt(cfg.SummaryMaxTokens),
			Args:        []string{"-y", "@orchestrator-agent"},
		})
	default:
		return json.Marshal(map[string]string{
			"message": "Ты помощник по обобщению знаний проекта.\n\n" + strings.TrimSpace(b.String()),
		})
	}
}

func ptrFloat64(v float64) *float64 { return &v }
func ptrInt(v int) *int             { return &v }
