package agent

import (
	"encoding/json"
	"strings"

	"ai-chat/internal/history"
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

func buildPayload(cfg Config, req AgentRequest, history []history.Message) ([]byte, error) {
	switch cfg.APIFormat {
	case "openai":
		var rf *responseFormat
		if req.ResponseFormat == "json" {
			rf = &responseFormat{Type: "json_object"}
		}

		return json.Marshal(openaiPayload{
			Model:          cfg.Model,
			Messages:       buildMessages(cfg, req, history),
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
		if cfg.AssistantPrompt != "" {
			userContent = userContent + "\n\n" + cfg.AssistantPrompt
		}
		return json.Marshal(map[string]string{"message": userContent})
	}
}
