package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ai-chat/internal/history"
)

const summarySystemPrompt = `Сделай краткое, но информативное summary диалога.
Сохрани ключевые факты, контекст, имена, даты, договорённости и намерения пользователя.
Ответь одним коротким текстом без приветствий и лишних комментариев.`

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
	// Загружаем историю текущей сессии.
	session, err := a.loadSession(req.SessionID)
	if err != nil {
		return AgentResponse{}, err
	}

	compress := req.CompressHistory || session.Compressed

	// При необходимости сжимаем старую историю в summary.
	if compress {
		if _, err := a.compressSession(&session); err != nil {
			return AgentResponse{}, err
		}
	}

	payload, err := buildPayload(a.config, req, session.Messages)
	if err != nil {
		return AgentResponse{}, err
	}

	mainStart := time.Now()
	body, err := a.client.Call(payload)
	if err != nil {
		return AgentResponse{}, err
	}

	resp := extractResponse(a.config.APIFormat, req.ResponseFormat, body)
	resp.Duration = time.Since(mainStart)
	resp.Compressed = session.Compressed

	// Сохраняем новое сообщение пользователя и ответ ассистента.
	sessionTotal, err := a.saveHistory(req.SessionID, &session, req.Message, resp, compress)
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

func (a *SimpleAgent) loadSession(sessionID string) (history.Session, error) {
	if a.history == nil || sessionID == "" {
		return history.Session{}, nil
	}
	return a.history.LoadSession(sessionID)
}

func (a *SimpleAgent) saveHistory(sessionID string, session *history.Session, userMessage string, resp AgentResponse, compress bool) (int, error) {
	if a.history == nil || sessionID == "" {
		return resp.TotalTokens, nil
	}

	if compress {
		session.Compressed = true
	}
	session.TotalTokens += resp.TotalTokens

	session.Messages = append(session.Messages,
		history.Message{Role: "user", Content: userMessage},
		history.Message{
			Role:             "assistant",
			Content:          resp.Content,
			PromptTokens:     resp.PromptTokens,
			CompletionTokens: resp.CompletionTokens,
			TotalTokens:      resp.TotalTokens,
		},
	)

	if err := a.history.SaveSession(sessionID, *session); err != nil {
		return 0, err
	}

	return session.TotalTokens, nil
}

// compressSession заменяет сообщения, кроме двух последних, на summary.
// Возвращает токены, потраченные на генерацию summary.
func (a *SimpleAgent) compressSession(session *history.Session) (AgentResponse, error) {
	if len(session.Messages) <= 2 {
		return AgentResponse{}, nil
	}

	context := session.Messages[:len(session.Messages)-2]

	// Если вся старая история уже представлена summary, пересчитывать нечего.
	hasRaw := false
	for _, m := range context {
		if !m.IsSummary {
			hasRaw = true
			break
		}
	}
	if !hasRaw {
		return AgentResponse{}, nil
	}

	summaryText, summaryResp, err := a.summarize(context)
	if err != nil {
		return AgentResponse{}, fmt.Errorf("ошибка генерации summary: %w", err)
	}

	summaryMsg := history.Message{
		Role:             "system",
		Content:          "Контекст предыдущего диалога:\n" + summaryText,
		IsSummary:        true,
		PromptTokens:     summaryResp.PromptTokens,
		CompletionTokens: summaryResp.CompletionTokens,
		TotalTokens:      summaryResp.TotalTokens,
	}

	lastTwo := session.Messages[len(session.Messages)-2:]
	session.Messages = append([]history.Message{summaryMsg}, lastTwo...)
	session.Compressed = true
	session.TotalTokens += summaryResp.TotalTokens

	return summaryResp, nil
}

// summarize отправляет старые сообщения в LLM и возвращает их summary.
func (a *SimpleAgent) summarize(messages []history.Message) (string, AgentResponse, error) {
	payload, err := buildSummaryPayload(a.config, messages)
	if err != nil {
		return "", AgentResponse{}, fmt.Errorf("ошибка построения запроса summary: %w", err)
	}

	body, err := a.client.Call(payload)
	if err != nil {
		return "", AgentResponse{}, err
	}

	resp := extractResponse(a.config.APIFormat, "text", body)
	return strings.TrimSpace(resp.Content), resp, nil
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
