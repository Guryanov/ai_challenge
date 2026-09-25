package agent

import (
	"encoding/json"

	"ai-chat/internal/history"
)

// extractResponse разбирает ответ внешнего API в зависимости от формата.
func extractResponse(apiFormat, responseFormat string, body []byte) LLMResponse {
	if apiFormat == "openai" {
		return extractOpenAIResponse(responseFormat, body)
	}
	return LLMResponse{AgentResponse: AgentResponse{Content: extractGenericResponse(body)}}
}

// LLMResponse расширяет AgentResponse возможностью запросить вызов инструментов.
type LLMResponse struct {
	AgentResponse
	ToolCalls []history.ToolCall
}

func extractOpenAIResponse(responseFormat string, body []byte) LLMResponse {
	var data struct {
		Choices []struct {
			Message struct {
				Content   string           `json:"content"`
				ToolCalls []openAIToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	resp := LLMResponse{}

	if err := json.Unmarshal(body, &data); err != nil {
		resp.Content = string(body)
		return resp
	}

	if data.Error != nil {
		resp.Content = "Ошибка API: " + data.Error.Message
		return resp
	}

	if data.Usage != nil {
		resp.PromptTokens = data.Usage.PromptTokens
		resp.CompletionTokens = data.Usage.CompletionTokens
		resp.TotalTokens = data.Usage.TotalTokens
	}

	if len(data.Choices) > 0 {
		message := data.Choices[0].Message
		content := message.Content
		if responseFormat == "json" {
			content = prettyPrintJSON(content)
		}
		resp.Content = content
		resp.FinishReason = data.Choices[0].FinishReason
		resp.ToolCalls = toHistoryToolCalls(message.ToolCalls)
		return resp
	}

	resp.Content = string(body)
	return resp
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func toHistoryToolCalls(calls []openAIToolCall) []history.ToolCall {
	if len(calls) == 0 {
		return nil
	}

	result := make([]history.ToolCall, 0, len(calls))
	for _, c := range calls {
		result = append(result, history.ToolCall{
			ID:   c.ID,
			Type: c.Type,
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      c.Function.Name,
				Arguments: c.Function.Arguments,
			},
		})
	}
	return result
}

func extractGenericResponse(body []byte) string {
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return string(body)
	}

	if response, ok := data["response"].(string); ok {
		return response
	}
	if text, ok := data["text"].(string); ok {
		return text
	}
	if message, ok := data["message"].(string); ok {
		return message
	}
	if jsonField, ok := data["json"].(map[string]interface{}); ok {
		formatted, err := json.MarshalIndent(jsonField, "", "  ")
		if err == nil {
			return string(formatted)
		}
	}

	formatted, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return string(body)
	}
	return string(formatted)
}

func prettyPrintJSON(s string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	formatted, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(formatted)
}
