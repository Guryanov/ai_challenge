package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"ai-chat/internal/history"
	"ai-chat/internal/memory"
	"ai-chat/internal/profile"
)

// SimpleAgent — базовая реализация LLM-агента.
// Пока выполняет один вызов API, но структура позволяет добавить:
//   - инструменты (ToolRegistry)
//   - многошаговое планирование (step loop)
type SimpleAgent struct {
	config  Config
	client  APIClient
	history history.Store
	memory  memory.Store
	profile profile.Store
	tools   ToolRegistry
}

// NewSimpleAgent создаёт агента с переданным клиентом и конфигурацией.
func NewSimpleAgent(cfg Config, client APIClient) *SimpleAgent {
	return &SimpleAgent{
		config:  cfg,
		client:  client,
		memory:  cfg.MemoryStore,
		profile: cfg.ProfileStore,
	}
}

// WithHistory добавляет хранилище истории.
func (a *SimpleAgent) WithHistory(h history.Store) *SimpleAgent {
	a.history = h
	return a
}

// WithMemory добавляет хранилище памяти проекта.
func (a *SimpleAgent) WithMemory(m memory.Store) *SimpleAgent {
	a.memory = m
	return a
}

// WithProfile добавляет хранилище профилей пользователей.
func (a *SimpleAgent) WithProfile(p profile.Store) *SimpleAgent {
	a.profile = p
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

	if req.TaskAction == "cancel" {
		session.TaskStage = ""
		session.TaskStatus = ""
		session.TaskContext = history.TaskContext{}
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

	// Обновляем режим обработки, если пользователь его явно передал.
	if req.WorkflowMode != "" {
		session.WorkflowMode = req.WorkflowMode
	}
	workflowMode := effectiveWorkflowMode(session)

	// Определяем состояние конечного автомата задачи.
	originalUserMessage := req.Message
	switch {
	case originalUserMessage != "" && workflowMode == WorkflowModeWorkflow:
		// Новый запрос пользователя сбрасывает текущее задание и начинает планирование.
		resetTaskState(&session, originalUserMessage)
	case originalUserMessage != "" && workflowMode == WorkflowModeChat:
		// В режиме chat задачи не создаются, очищаем возможное состояние workflow.
		clearTaskState(&session)
	case req.TaskAction != "":
		if workflowMode != WorkflowModeWorkflow {
			return AgentResponse{}, fmt.Errorf("действия над задачами доступны только в режиме workflow")
		}
		// Утверждение или отклонение текущего этапа.
		if err := applyTaskAction(&session, req.TaskAction, req.RejectionReason); err != nil {
			return AgentResponse{}, err
		}
	case session.TaskStage != "" && session.TaskStatus == TaskStatusPending && workflowMode == WorkflowModeWorkflow:
		// Продолжаем текущий этап (например, после перезагрузки страницы).
	default:
		return AgentResponse{}, fmt.Errorf("сообщение не может быть пустым")
	}

	strategy := a.effectiveStrategy(session)

	ctxResult, err := a.applyContextStrategy(&session, strategy)
	if err != nil {
		return AgentResponse{}, err
	}
	session.TotalTokens += ctxResult.summaryTokens

	// Загружаем профиль пользователя, если задан profile_id.
	var userProfileContext string
	if req.ProfileID != "" && a.profile != nil {
		p, err := a.profile.Load(req.ProfileID)
		if err != nil {
			return AgentResponse{}, err
		}
		userProfileContext = formatUserProfileContext(p)
	}

	// Загружаем память проекта, если задан project_id.
	var projectContext string
	if req.ProjectID != "" && a.memory != nil {
		mem, err := a.memory.Load(req.ProjectID)
		if err != nil {
			return AgentResponse{}, err
		}
		mem, err = a.summarizeLongTermMemoryIfNeeded(req.ProjectID, mem)
		if err != nil {
			return AgentResponse{}, err
		}
		projectContext = formatProjectContext(mem)
	}

	// Добавляем facts в историю как системное сообщение, если используется стратегия facts.
	messages := ctxResult.messages
	if strategy == StrategyFacts && len(session.Facts) > 0 {
		messages = append([]history.Message{{Role: "system", Content: formatFacts(session.Facts)}}, messages...)
	}

	// Формируем сообщение для LLM: либо промпт этапа задачи, либо прямой запрос пользователя.
	llmReq := req
	if workflowMode == WorkflowModeWorkflow {
		llmReq.Message = buildTaskStagePrompt(session, projectContext, userProfileContext)
	} else {
		llmReq.Message = originalUserMessage
	}

	payload, err := buildPayload(a.config, llmReq, messages, userProfileContext, projectContext)
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

	// В режиме workflow сохраняем результат этапа в контексте задачи.
	if workflowMode == WorkflowModeWorkflow {
		updateTaskContext(&session, resp.Content)
		session.TaskStatus = TaskStatusPending
	}

	// Сохраняем новое сообщение пользователя и ответ ассистента.
	sessionTotal, err := a.saveHistory(req.SessionID, &session, originalUserMessage, resp)
	if err != nil {
		return AgentResponse{}, err
	}
	resp.SessionTotalTokens = sessionTotal

	resp.ActiveBranch = session.ActiveBranch
	resp.Branches = branchNames(session.Branches)
	resp.Facts = session.Facts
	resp.ProjectID = req.ProjectID
	resp.ProfileID = req.ProfileID
	resp.TaskStage = session.TaskStage
	resp.TaskStatus = session.TaskStatus
	resp.TaskContext = session.TaskContext

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
	if userMessage != "" {
		branch.Messages = append(branch.Messages, history.Message{Role: "user", Content: userMessage})
	}
	branch.Messages = append(branch.Messages, history.Message{
		Role:             "assistant",
		Content:          resp.Content,
		PromptTokens:     resp.PromptTokens,
		CompletionTokens: resp.CompletionTokens,
		TotalTokens:      resp.TotalTokens,
	})
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

func effectiveWorkflowMode(session history.Session) string {
	if session.WorkflowMode != "" {
		return session.WorkflowMode
	}
	return WorkflowModeChat
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

// formatProjectContext форматирует память проекта в системное сообщение для LLM.
func formatProjectContext(mem memory.Memory) string {
	groups := map[string][]memory.Entry{
		"Принципы":      filterEntriesByType(mem.LongTerm, "principle"),
		"Технологии":    filterEntriesByType(mem.LongTerm, "technology"),
		"Паттерны":      filterEntriesByType(mem.LongTerm, "pattern"),
		"Знания":        filterEntriesByType(mem.LongTerm, "knowledge"),
		"Summary":       filterEntriesByType(mem.LongTerm, "summary"),
		"Текущие задачи": filterEntriesByType(mem.Working, "task"),
		"Сущности":      filterEntriesByType(mem.Working, "entity"),
	}

	var b strings.Builder
	b.WriteString("Контекст проекта:\n")
	hasAny := false
	for title, entries := range groups {
		if len(entries) == 0 {
			continue
		}
		hasAny = true
		b.WriteString("\n")
		b.WriteString(title + ":\n")
		for _, e := range entries {
			line := e.Title
			if e.Content != "" {
				line += ": " + e.Content
			}
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if !hasAny {
		return ""
	}
	return strings.TrimSpace(b.String())
}

func filterEntriesByType(entries []memory.Entry, entryType string) []memory.Entry {
	var result []memory.Entry
	for _, e := range entries {
		if e.Type == entryType {
			result = append(result, e)
		}
	}
	// Стабильная сортировка по времени создания.
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

// SummarizeMemoryIfNeeded проверяет и при необходимости выполняет авто-summary
// долгосрочной памяти проекта.
func (a *SimpleAgent) SummarizeMemoryIfNeeded(projectID string) (memory.Memory, error) {
	if a.memory == nil {
		return memory.Memory{}, nil
	}
	mem, err := a.memory.Load(projectID)
	if err != nil {
		return memory.Memory{}, err
	}
	return a.summarizeLongTermMemoryIfNeeded(projectID, mem)
}

// summarizeLongTermMemoryIfNeeded сворачивает самые старые записи долгосрочной памяти,
// если их количество превышает порог.
func (a *SimpleAgent) summarizeLongTermMemoryIfNeeded(projectID string, mem memory.Memory) (memory.Memory, error) {
	oldest, remaining := memory.OldestLongTerm(mem)
	if len(oldest) == 0 {
		return mem, nil
	}

	payload, err := buildMemorySummaryPayload(a.config, oldest)
	if err != nil {
		return mem, fmt.Errorf("ошибка построения запроса summary памяти: %w", err)
	}

	body, err := a.client.Call(payload)
	if err != nil {
		return mem, fmt.Errorf("ошибка генерации summary памяти: %w", err)
	}

	summaryResp := extractResponse(a.config.APIFormat, "text", body)
	summaryText := strings.TrimSpace(summaryResp.Content)
	if summaryText == "" {
		summaryText = "Summary предыдущих записей долгосрочной памяти."
	}

	summaryEntry := memory.Entry{
		Type:          "summary",
		Title:         fmt.Sprintf("Summary %d записей", len(oldest)),
		Content:       summaryText,
		AutoGenerated: true,
	}

	mem.LongTerm = append(remaining, summaryEntry)

	if err := a.memory.Save(projectID, mem); err != nil {
		return mem, fmt.Errorf("ошибка сохранения памяти после summary: %w", err)
	}

	return mem, nil
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
