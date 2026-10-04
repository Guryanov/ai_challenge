// Package embed предоставляет клиент эмбеддингов через локальный Ollama.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Client — интерфейс генерации эмбеддингов.
type Client interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// OllamaClient вызывает POST {baseURL}/api/embed.
type OllamaClient struct {
	baseURL string
	model   string
	batch   int
	client  *http.Client
}

// NewOllamaClient создаёт клиент Ollama.
func NewOllamaClient(baseURL, model string, batch int, timeout time.Duration) *OllamaClient {
	if batch <= 0 {
		batch = 32
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &OllamaClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		batch:   batch,
		client:  &http.Client{Timeout: timeout},
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error"`
}

// Embed возвращает нормализованные эмбеддинги для texts.
func (c *OllamaClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
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
				return nil, fmt.Errorf("Ollama вернул эмбеддинги разной размерности: %d и %d", dim, len(vec))
			}
			result = append(result, normalize(vec))
		}
	}

	return result, nil
}

func (c *OllamaClient) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload, err := json.Marshal(embedRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации запроса эмбеддингов: %w", err)
	}

	url := c.baseURL + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса эмбеддингов: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Ollama недоступен по %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа Ollama: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("модель эмбеддингов %q не найдена в Ollama, выполните: ollama pull %s", c.model, c.model)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Ollama вернул статус %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed embedResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("ошибка разбора ответа Ollama: %w", err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("Ollama: %s", parsed.Error)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("Ollama вернул %d эмбеддингов вместо %d", len(parsed.Embeddings), len(texts))
	}

	return parsed.Embeddings, nil
}

// normalize приводит вектор к единичной длине (для cosine через скалярное произведение).
func normalize(vec []float32) []float32 {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return vec
	}
	norm := float32(math.Sqrt(sum))
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = v / norm
	}
	return out
}
