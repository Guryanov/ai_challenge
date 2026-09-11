package agent

import (
	"encoding/json"
	"time"
)

// SimpleAgent — базовая реализация LLM-агента.
// Пока выполняет один вызов API, но структура позволяет добавить:
//   - хранение истории (HistoryStore)
//   - инструменты (ToolRegistry)
//   - многошаговое планирование (step loop)
type SimpleAgent struct {
	config  Config
	client  APIClient
	history HistoryStore
	tools   ToolRegistry
}

// NewSimpleAgent создаёт агента с переданным клиентом и конфигурацией.
func NewSimpleAgent(cfg Config, client APIClient) *SimpleAgent {
	return &SimpleAgent{
		config: cfg,
		client: client,
	}
}

// Run выполняет один запрос к API.
// В будущем здесь может быть цикл: observe → think → act.
func (a *SimpleAgent) Run(req AgentRequest) (AgentResponse, error) {
	start := time.Now()

	payload, err := buildPayload(a.config, req)
	if err != nil {
		return AgentResponse{}, err
	}

	body, err := a.client.Call(payload)
	if err != nil {
		return AgentResponse{}, err
	}

	resp := extractResponse(a.config.APIFormat, req.ResponseFormat, body)
	resp.Duration = time.Since(start)

	return resp, nil
}

// WithHistory добавляет хранилище истории.
func (a *SimpleAgent) WithHistory(h HistoryStore) *SimpleAgent {
	a.history = h
	return a
}

// WithTools добавляет реестр инструментов.
func (a *SimpleAgent) WithTools(t ToolRegistry) *SimpleAgent {
	a.tools = t
	return a
}

// HistoryStore — интерфейс для хранения истории сообщений.
// Реализации могут хранить историю в памяти, в БД, в Redis и т.д.
type HistoryStore interface {
	Add(role, content string)
	GetMessages() []map[string]string
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
