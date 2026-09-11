package agent

import (
	"encoding/json"
	"time"

	"ai-chat/internal/history"
)

// SimpleAgent — базовая реализация LLM-агента.
// Пока выполняет один вызов API, но структура позволяет добавить:
//   - инструменты (ToolRegistry)
//   - многошаговое планирование (step loop)
type SimpleAgent struct {
	config  Config
	client  APIClient
	history history.Store
	tools   ToolRegistry
}

// NewSimpleAgent создаёт агента с переданным клиентом и конфигурацией.
func NewSimpleAgent(cfg Config, client APIClient) *SimpleAgent {
	return &SimpleAgent{
		config: cfg,
		client: client,
	}
}

// WithHistory добавляет хранилище истории.
func (a *SimpleAgent) WithHistory(h history.Store) *SimpleAgent {
	a.history = h
	return a
}

// WithTools добавляет реестр инструментов.
func (a *SimpleAgent) WithTools(t ToolRegistry) *SimpleAgent {
	a.tools = t
	return a
}

// Run выполняет один запрос к API с учётом истории сообщений.
// В будущем здесь может быть цикл: observe → think → act.
func (a *SimpleAgent) Run(req AgentRequest) (AgentResponse, error) {
	start := time.Now()

	// Загружаем историю текущей сессии.
	historyMessages, err := a.loadHistory(req.SessionID)
	if err != nil {
		return AgentResponse{}, err
	}

	payload, err := buildPayload(a.config, req, historyMessages)
	if err != nil {
		return AgentResponse{}, err
	}

	body, err := a.client.Call(payload)
	if err != nil {
		return AgentResponse{}, err
	}

	resp := extractResponse(a.config.APIFormat, req.ResponseFormat, body)
	resp.Duration = time.Since(start)

	// Сохраняем новое сообщение пользователя и ответ ассистента.
	sessionTotal, err := a.saveHistory(req.SessionID, historyMessages, req.Message, resp)
	if err != nil {
		return AgentResponse{}, err
	}
	resp.SessionTotalTokens = sessionTotal

	return resp, nil
}

// ClearHistory очищает историю указанной сессии.
func (a *SimpleAgent) ClearHistory(sessionID string) error {
	if a.history == nil {
		return nil
	}
	return a.history.Delete(sessionID)
}

func (a *SimpleAgent) loadHistory(sessionID string) ([]history.Message, error) {
	if a.history == nil || sessionID == "" {
		return nil, nil
	}
	return a.history.Load(sessionID)
}

func (a *SimpleAgent) saveHistory(sessionID string, prev []history.Message, userMessage string, resp AgentResponse) (int, error) {
	if a.history == nil || sessionID == "" {
		return resp.TotalTokens, nil
	}

	sessionTotal := resp.TotalTokens
	for _, m := range prev {
		sessionTotal += m.TotalTokens
	}

	messages := append(prev,
		history.Message{Role: "user", Content: userMessage},
		history.Message{
			Role:             "assistant",
			Content:          resp.Content,
			PromptTokens:     resp.PromptTokens,
			CompletionTokens: resp.CompletionTokens,
			TotalTokens:      resp.TotalTokens,
		},
	)

	if err := a.history.Save(sessionID, messages); err != nil {
		return 0, err
	}

	return sessionTotal, nil
}

// ToolRegistry — интерфейс для реестра инструментов.
// В будущем здесь будут определения и выполнение инструментов.
type ToolRegistry interface {
	Definitions() []ToolDefinition
	Execute(name string, args json.RawMessage) (string, error)
}

// ToolDefinition — описание инструмента для OpenAI function calling.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
}
