package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"ai-chat/internal/agent"
	"ai-chat/internal/knowledge"
)

const queryGenPrompt = "Составь ровно один короткий вопрос на русском языке, ответ на который содержится в приведённом тексте. Верни только текст вопроса, без пояснений и кавычек.\n\nТекст:\n"

// newQueryGenerator создаёт генератор тест-запросов поверх chat-LLM.
func newQueryGenerator(cfg agent.Config, client agent.APIClient) knowledge.QueryGenerator {
	return func(ctx context.Context, chunkText string) (string, error) {
		if client == nil {
			return "", fmt.Errorf("LLM-клиент недоступен")
		}

		text := chunkText
		if utf8.RuneCountInString(text) > 2000 {
			text = string([]rune(text)[:2000])
		}
		prompt := queryGenPrompt + text

		var (
			payload []byte
			err     error
		)
		if cfg.APIFormat == "openai" {
			payload, err = json.Marshal(map[string]any{
				"model": cfg.Model,
				"messages": []map[string]string{
					{"role": "system", "content": "Ты генерируешь тестовые вопросы для оценки качества поиска."},
					{"role": "user", "content": prompt},
				},
				"temperature": 0.3,
				"max_tokens":  120,
			})
		} else {
			payload, err = json.Marshal(map[string]string{"message": prompt})
		}
		if err != nil {
			return "", err
		}

		body, err := client.Call(payload)
		if err != nil {
			return "", err
		}
		return extractGeneratedQuestion(body), nil
	}
}

// kbRetriever адаптирует базу знаний под agent.KnowledgeRetriever для RAG в чате.
type kbRetriever struct {
	service *knowledge.Service
}

// newKBRetriever создаёт retrieval-обёртку над сервисом базы знаний.
func newKBRetriever(service *knowledge.Service) kbRetriever {
	return kbRetriever{service: service}
}

// Retrieve ищет top-K чанков проекта для использования в ответе агента.
func (r kbRetriever) Retrieve(ctx context.Context, projectID, strategy, query string, k int) ([]agent.RetrievedChunk, error) {
	scored, err := r.service.Search(ctx, projectID, strategy, query, k)
	if err != nil {
		return nil, err
	}
	chunks := make([]agent.RetrievedChunk, 0, len(scored))
	for _, s := range scored {
		chunks = append(chunks, agent.RetrievedChunk{
			Path:    s.Path,
			Name:    s.Name,
			Section: s.Section,
			Score:   s.Score,
			Text:    s.Text,
		})
	}
	return chunks, nil
}

// extractGeneratedQuestion достаёт текст вопроса из ответа chat-LLM.
func extractGeneratedQuestion(body []byte) string {
	var openai struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &openai); err == nil && len(openai.Choices) > 0 {
		return strings.TrimSpace(openai.Choices[0].Message.Content)
	}

	var generic struct {
		Message  string `json:"message"`
		Response string `json:"response"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(body, &generic); err == nil {
		for _, v := range []string{generic.Response, generic.Content, generic.Message} {
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}
