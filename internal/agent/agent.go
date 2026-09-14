package agent

import (
	"encoding/json"
	"fmt"
	"strings"
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

// Manage обновляет состояние сессии (facts, ветки, стратегию) без вызова LLM.
func (a *SimpleAgent) Manage(req AgentRequest) (AgentResponse, error) {
	session, err := a.loadSession(req.SessionID)
	if err != nil {
		return AgentResponse{}, err
	}
	a.ensureSession(&session)

	if req.ContextStrategy != "" {
		session.Strategy = string(req.ContextStrategy)
	}

	if req.Facts != nil {
		if session.Facts == nil {
			session.Facts = make(map[string]string)
		}
		for k, v := range req.Facts {
			if v == "" {
				delete(session.Facts, k)
			} else {
				session.Facts[k] = v
			}
		}
	}

	if err := a.handleBranchAction(&session, req.BranchAction); err != nil {
		return AgentResponse{}, err
	}

	if err := a.history.SaveSession(req.SessionID, session); err != nil {
		return AgentResponse{}, err
	}

	return AgentResponse{
		ActiveBranch: session.ActiveBranch,
		Branches:     branchNames(session.Branches),
		Facts:        session.Facts,
	}, nil
}

// Run выполняет один запрос к API с учётом истории сообщений.
// В будущем здесь может быть цикл: observe → think → act.
func (a *SimpleAgent) Run(req AgentRequest) (AgentResponse, error) {
	session, err := a.loadSession(req.SessionID)
	if err != nil {
		return AgentResponse{}, err
	}
	a.ensureSession(&session)

	// Обновляем стратегию, если пользователь её явно передал.
	if req.ContextStrategy != "" {
		session.Strategy = string(req.ContextStrategy)
	}

	// Обновляем facts, если пришли новые.
	if req.Facts != nil {
		if session.Facts == nil {
			session.Facts = make(map[string]string)
		}
		for k, v := range req.Facts {
			if v == "" {
				delete(session.Facts, k)
			} else {
				session.Facts[k] = v
			}
		}
	}

	// Обрабатываем действия с ветками.
	if err := a.handleBranchAction(&session, req.BranchAction); err != nil {
		return AgentResponse{}, err
	}

	strategy := a.effectiveStrategy(session)

	ctxResult, err := a.applyContextStrategy(&session, strategy)
	if err != nil {
		return AgentResponse{}, err
	}
	session.TotalTokens += ctxResult.summaryTokens

	// Для стратегии summary сохраняем сжатую историю в сессии.
	if strategy == StrategySummary {
		branch := session.Branches[session.ActiveBranch]
		branch.Messages = ctxResult.messages
		session.Branches[session.ActiveBranch] = branch
	}

	// Добавляем facts в историю как системное сообщение, если используется стратегия facts.
	messages := ctxResult.messages
	if strategy == StrategyFacts && len(session.Facts) > 0 {
		messages = append([]history.Message{{Role: "system", Content: formatFacts(session.Facts)}}, messages...)
	}

	payload, err := buildPayload(a.config, req, messages)
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
	resp.Compressed = strategy == StrategySummary && len(session.Branches[session.ActiveBranch].Messages) > 2

	// Сохраняем новое сообщение пользователя и ответ ассистента.
	sessionTotal, err := a.saveHistory(req.SessionID, &session, req.Message, resp)
	if err != nil {
		return AgentResponse{}, err
	}
	resp.SessionTotalTokens = sessionTotal

	resp.ActiveBranch = session.ActiveBranch
	resp.Branches = branchNames(session.Branches)
	resp.Facts = session.Facts

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

func (a *SimpleAgent) saveHistory(sessionID string, session *history.Session, userMessage string, resp AgentResponse) (int, error) {
	if a.history == nil || sessionID == "" {
		return resp.TotalTokens, nil
	}

	session.TotalTokens += resp.TotalTokens

	branch := session.Branches[session.ActiveBranch]
	branch.Messages = append(branch.Messages,
		history.Message{Role: "user", Content: userMessage},
		history.Message{
			Role:             "assistant",
			Content:          resp.Content,
			PromptTokens:     resp.PromptTokens,
			CompletionTokens: resp.CompletionTokens,
			TotalTokens:      resp.TotalTokens,
		},
	)
	session.Branches[session.ActiveBranch] = branch

	if err := a.history.SaveSession(sessionID, *session); err != nil {
		return 0, err
	}

	return session.TotalTokens, nil
}

func (a *SimpleAgent) ensureSession(session *history.Session) {
	if session.Branches == nil {
		session.Branches = make(map[string]history.Branch)
	}
	if session.ActiveBranch == "" {
		session.ActiveBranch = "main"
	}
	if _, ok := session.Branches[session.ActiveBranch]; !ok {
		session.Branches[session.ActiveBranch] = history.Branch{}
	}
}

func (a *SimpleAgent) handleBranchAction(session *history.Session, action string) error {
	if action == "" {
		return nil
	}

	parts := strings.SplitN(action, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("неверный формат branch_action: %s", action)
	}

	op, name := parts[0], strings.TrimSpace(parts[1])
	if name == "" {
		return fmt.Errorf("имя ветки не может быть пустым")
	}

	switch op {
	case "create":
		if _, exists := session.Branches[name]; exists {
			return fmt.Errorf("ветка %q уже существует", name)
		}
		active := session.Branches[session.ActiveBranch]
		copied := make([]history.Message, len(active.Messages))
		copy(copied, active.Messages)
		session.Branches[name] = history.Branch{Messages: copied}
		session.ActiveBranch = name
	case "switch":
		if _, exists := session.Branches[name]; !exists {
			return fmt.Errorf("ветка %q не найдена", name)
		}
		session.ActiveBranch = name
	default:
		return fmt.Errorf("неизвестное действие с веткой: %s", op)
	}

	return nil
}

func (a *SimpleAgent) effectiveStrategy(session history.Session) ContextStrategy {
	if session.Strategy != "" {
		return ContextStrategy(session.Strategy)
	}
	return StrategyFull
}

func formatFacts(facts map[string]string) string {
	var b strings.Builder
	b.WriteString("Известные факты:\n")
	for k, v := range facts {
		b.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
	}
	return strings.TrimSpace(b.String())
}

func branchNames(branches map[string]history.Branch) []string {
	names := make([]string, 0, len(branches))
	for name := range branches {
		names = append(names, name)
	}
	return names
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
