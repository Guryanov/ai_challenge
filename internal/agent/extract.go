package agent

import (
	"encoding/json"
)

// extractResponse разбирает ответ внешнего API в зависимости от формата.
func extractResponse(apiFormat, responseFormat string, body []byte) AgentResponse {
	if apiFormat == "openai" {
		return extractOpenAIResponse(responseFormat, body)
	}
	return AgentResponse{Content: extractGenericResponse(body)}
}

func extractOpenAIResponse(responseFormat string, body []byte) AgentResponse {
	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
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

	if err := json.Unmarshal(body, &data); err != nil {
		return AgentResponse{Content: string(body)}
	}

	if data.Error != nil {
		return AgentResponse{Content: "Ошибка API: " + data.Error.Message}
	}

	resp := AgentResponse{}
	if data.Usage != nil {
		resp.PromptTokens = data.Usage.PromptTokens
		resp.CompletionTokens = data.Usage.CompletionTokens
		resp.TotalTokens = data.Usage.TotalTokens
	}

	if len(data.Choices) > 0 {
		content := data.Choices[0].Message.Content
		if responseFormat == "json" {
			content = prettyPrintJSON(content)
		}
		resp.Content = content
		resp.FinishReason = data.Choices[0].FinishReason
		return resp
	}

	return AgentResponse{Content: string(body)}
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
