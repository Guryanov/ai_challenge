package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// OpenAIClient вызывает OpenAI-совместимый POST {endpoint} (обычно /v1/embeddings).
type OpenAIClient struct {
	endpoint string
	apiKey   string
	model    string
	batch    int
	client   *http.Client
}

// NewOpenAIClient создаёт клиент эмбеддингов OpenAI-совместимого API.
func NewOpenAIClient(endpoint, apiKey, model string, batch int, timeout time.Duration) *OpenAIClient {
	if batch <= 0 {
		batch = 32
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &OpenAIClient{
		endpoint: strings.TrimSpace(endpoint),
		apiKey:   apiKey,
		model:    model,
		batch:    batch,
		client:   &http.Client{Timeout: timeout},
	}
}

// NewClient выбирает реализацию Client по формату API.
// format: "openai" (по умолчанию) или "ollama".
func NewClient(format, url, apiKey, model string, batch int, timeout time.Duration) Client {
	if strings.EqualFold(strings.TrimSpace(format), "ollama") {
		return NewOllamaClient(url, model, batch, timeout)
	}
	return NewOpenAIClient(url, apiKey, model, batch, timeout)
}

type openAIEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed возвращает нормализованные эмбеддинги для texts.
func (c *OpenAIClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	result := make([][]float32, 0, len(texts))
	dim := 0

	for start := 0; start < len(texts); start += c.batch {
		end := start + c.batch
		if end > len(texts) {
			end = len(texts)
		}

		batch, err := c.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}

		for _, vec := range batch {
			if dim == 0 {
				dim = len(vec)
			}
			if len(vec) != dim {
				return nil, fmt.Errorf("сервис эмбеддингов вернул векторы разной размерности: %d и %d", dim, len(vec))
			}
			result = append(result, normalize(vec))
		}
	}

	return result, nil
}

func (c *OpenAIClient) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload, err := json.Marshal(openAIEmbedRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации запроса эмбеддингов: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса эмбеддингов: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("сервис эмбеддингов недоступен по %s: %w", c.endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа сервиса эмбеддингов: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("модель эмбеддингов %q не найдена по адресу %s", c.model, c.endpoint)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("сервис эмбеддингов вернул статус %d: %s", resp.StatusCode, extractOpenAIError(body))
	}

	var parsed openAIEmbedResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("ошибка разбора ответа сервиса эмбеддингов: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("сервис эмбеддингов: %s", parsed.Error.Message)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("сервис вернул %d эмбеддингов вместо %d", len(parsed.Data), len(texts))
	}

	sort.SliceStable(parsed.Data, func(i, j int) bool {
		return parsed.Data[i].Index < parsed.Data[j].Index
	})

	out := make([][]float32, len(parsed.Data))
	for i, item := range parsed.Data {
		out[i] = item.Embedding
	}
	return out, nil
}

func extractOpenAIError(body []byte) string {
	var parsed openAIEmbedResponse
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error != nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return strings.TrimSpace(string(body))
}
