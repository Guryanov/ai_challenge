package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed static/index.html
var indexHTML string

const (
	defaultExternalAPI = "https://httpbin.org/anything"
	defaultModel       = "SMLab-Test/Kimi-K2.7-Code"
	defaultTimeout     = 60 * time.Second
)

type config struct {
	ExternalAPI        string
	APIKey             string
	AuthType           string
	APIFormat          string
	SystemPrompt       string
	AssistantPrompt    string
	UserPromptTemplate string
	Timeout            time.Duration
}

type chatRequest struct {
	Message        string   `json:"message"`
	ResponseFormat string   `json:"response_format"`
	Temperature    *float64 `json:"temperature"`
	MaxTokens      *int     `json:"max_tokens"`
	StopSequence   string   `json:"stop_sequence"`
}

type chatResponse struct {
	User         string `json:"user"`
	Response     string `json:"response"`
	FinishReason string `json:"finish_reason,omitempty"`
	Error        string `json:"error,omitempty"`
}

type extractionResult struct {
	Content      string
	FinishReason string
}

func loadConfig() config {
	cfg := config{
		ExternalAPI:        os.Getenv("EXTERNAL_API_URL"),
		APIKey:             os.Getenv("API_KEY"),
		AuthType:           strings.ToLower(os.Getenv("AUTH_TYPE")),
		APIFormat:          strings.ToLower(os.Getenv("API_FORMAT")),
		SystemPrompt:       os.Getenv("SYSTEM_PROMPT"),
		AssistantPrompt:    os.Getenv("ASSISTANT_PROMPT"),
		UserPromptTemplate: os.Getenv("USER_PROMPT_TEMPLATE"),
		Timeout:            defaultTimeout,
	}

	if cfg.ExternalAPI == "" {
		cfg.ExternalAPI = defaultExternalAPI
	}

	// Если задан API_KEY, но не указаны AUTH_TYPE и API_FORMAT,
	// используем OpenAI-совместимый формат с авторизацией Bearer.
	if cfg.APIKey != "" {
		if cfg.AuthType == "" {
			cfg.AuthType = "bearer"
		}
		if cfg.APIFormat == "" {
			cfg.APIFormat = "openai"
		}
	} else {
		if cfg.AuthType == "" {
			cfg.AuthType = "none"
		}
		if cfg.APIFormat == "" {
			cfg.APIFormat = "generic"
		}
	}

	return cfg
}

func main() {
	cfg := loadConfig()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.HandleFunc("POST /api/chat", handleChat(cfg))

	addr := ":" + port
	log.Printf("Сервер запущен на http://localhost%s", addr)
	log.Printf("Внешний API: %s", cfg.ExternalAPI)
	log.Printf("Формат API: %s, авторизация: %s", cfg.APIFormat, cfg.AuthType)
	if cfg.APIFormat == "openai" {
		logPrompts(cfg)
	}
	if err := http.ListenAndServe(addr, loggingMiddleware(mux)); err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.statusCode, time.Since(start))
	})
}

func logPrompts(cfg config) {
	parts := []string{fmt.Sprintf("model=%s", defaultModel)}
	if cfg.SystemPrompt != "" {
		parts = append(parts, "system_prompt=set")
	}
	if cfg.AssistantPrompt != "" {
		parts = append(parts, "assistant_prompt=set")
	}
	if cfg.UserPromptTemplate != "" {
		parts = append(parts, "user_prompt_template=set")
	}
	log.Printf("Промпты: %s", strings.Join(parts, ", "))
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(indexHTML)); err != nil {
		log.Printf("Ошибка записи ответа: %v", err)
	}
}

func handleChat(cfg config) http.HandlerFunc {
	client := &http.Client{Timeout: cfg.Timeout}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		if req.Message == "" {
			writeError(w, http.StatusBadRequest, "Сообщение не может быть пустым")
			return
		}

		payload, err := buildPayload(cfg, req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Ошибка формирования запроса")
			return
		}

		externalReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, cfg.ExternalAPI, bytes.NewReader(payload))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Ошибка создания запроса к внешнему API")
			return
		}

		externalReq.Header.Set("Content-Type", "application/json")
		externalReq.Header.Set("Accept", "application/json")
		setAuthHeader(externalReq, cfg)

		log.Printf("Запрос к внешнему API: %s, message=%q, response_format=%s, temperature=%s, max_tokens=%s, stop=%q",
			cfg.ExternalAPI,
			truncate(req.Message, 80),
			req.ResponseFormat,
			formatFloatPtr(req.Temperature),
			formatMaxTokens(req.MaxTokens),
			req.StopSequence,
		)

		apiStart := time.Now()
		externalResp, err := client.Do(externalReq)
		if err != nil {
			log.Printf("Ошибка вызова внешнего API: %v", err)
			writeError(w, http.StatusBadGateway, fmt.Sprintf("Ошибка вызова внешнего API: %v", err))
			return
		}
		defer externalResp.Body.Close()

		respBody, err := io.ReadAll(externalResp.Body)
		if err != nil {
			writeError(w, http.StatusBadGateway, "Ошибка чтения ответа внешнего API")
			return
		}

		if externalResp.StatusCode < 200 || externalResp.StatusCode >= 300 {
			log.Printf("Внешний API вернул статус %d: %s", externalResp.StatusCode, truncate(string(respBody), 200))
			writeError(w, http.StatusBadGateway, fmt.Sprintf("Внешний API вернул статус %d: %s", externalResp.StatusCode, string(respBody)))
			return
		}

		result := extractResponse(cfg.APIFormat, req.ResponseFormat, respBody)
		log.Printf("Ответ внешнего API: status=%d, duration=%s, finish_reason=%q, content_length=%d",
			externalResp.StatusCode,
			time.Since(apiStart),
			result.FinishReason,
			len(result.Content),
		)

		resp := chatResponse{
			User:         req.Message,
			Response:     result.Content,
			FinishReason: result.FinishReason,
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("Ошибка кодирования ответа: %v", err)
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func formatMaxTokens(v *int) string {
	if v == nil {
		return "not set"
	}
	return fmt.Sprintf("%d", *v)
}

func formatFloatPtr(v *float64) string {
	if v == nil {
		return "not set"
	}
	return fmt.Sprintf("%g", *v)
}

type openaiPayload struct {
	Model          string              `json:"model"`
	Messages       []map[string]string `json:"messages"`
	ResponseFormat *responseFormat     `json:"response_format,omitempty"`
	Temperature    *float64            `json:"temperature,omitempty"`
	MaxTokens      *int                `json:"max_tokens,omitempty"`
	Stop           string              `json:"stop,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

func buildPayload(cfg config, req chatRequest) ([]byte, error) {
	switch cfg.APIFormat {
	case "openai":
		var rf *responseFormat
		if req.ResponseFormat == "json" {
			rf = &responseFormat{Type: "json_object"}
		}

		return json.Marshal(openaiPayload{
			Model:          defaultModel,
			Messages:       buildMessages(cfg, req.Message),
			ResponseFormat: rf,
			Temperature:    req.Temperature,
			MaxTokens:      req.MaxTokens,
			Stop:           req.StopSequence,
		})
	default:
		userContent := req.Message
		if cfg.UserPromptTemplate != "" {
			userContent = strings.ReplaceAll(cfg.UserPromptTemplate, "{message}", req.Message)
		}
		if cfg.SystemPrompt != "" {
			userContent = cfg.SystemPrompt + "\n\n" + userContent
		}
		if cfg.AssistantPrompt != "" {
			userContent = userContent + "\n\n" + cfg.AssistantPrompt
		}
		return json.Marshal(map[string]string{"message": userContent})
	}
}

func buildMessages(cfg config, message string) []map[string]string {
	var messages []map[string]string

	if cfg.SystemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": cfg.SystemPrompt})
	}

	userContent := message
	if cfg.UserPromptTemplate != "" {
		userContent = strings.ReplaceAll(cfg.UserPromptTemplate, "{message}", message)
	}
	messages = append(messages, map[string]string{"role": "user", "content": userContent})

	if cfg.AssistantPrompt != "" {
		messages = append(messages, map[string]string{"role": "assistant", "content": cfg.AssistantPrompt})
	}

	return messages
}

func setAuthHeader(req *http.Request, cfg config) {
	if cfg.APIKey == "" {
		return
	}

	switch cfg.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	case "x-api-key":
		req.Header.Set("X-API-Key", cfg.APIKey)
	case "api-key":
		req.Header.Set("Api-Key", cfg.APIKey)
	}
}

func extractResponse(apiFormat, responseFormat string, body []byte) extractionResult {
	if apiFormat == "openai" {
		return extractOpenAIResponse(responseFormat, body)
	}
	return extractionResult{Content: extractGenericResponse(body)}
}

func extractOpenAIResponse(responseFormat string, body []byte) extractionResult {
	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return extractionResult{Content: string(body)}
	}

	if data.Error != nil {
		return extractionResult{Content: "Ошибка API: " + data.Error.Message}
	}

	if len(data.Choices) > 0 {
		content := data.Choices[0].Message.Content
		if responseFormat == "json" {
			content = prettyPrintJSON(content)
		}
		return extractionResult{
			Content:      content,
			FinishReason: data.Choices[0].FinishReason,
		}
	}

	return extractionResult{Content: string(body)}
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

func writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(chatResponse{Error: message}); err != nil {
		log.Printf("Ошибка кодирования ошибки: %v", err)
	}
}
