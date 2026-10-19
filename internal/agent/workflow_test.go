package agent

import (
	"testing"

	"ai-chat/internal/history"
)

func TestEffectiveWorkflowModeDefault(t *testing.T) {
	mode := effectiveWorkflowMode(history.Session{})
	if mode != WorkflowModeChat {
		t.Fatalf("expected default mode %q, got %q", WorkflowModeChat, mode)
	}
}

func TestEffectiveWorkflowModeFromSession(t *testing.T) {
	mode := effectiveWorkflowMode(history.Session{WorkflowMode: WorkflowModeWorkflow})
	if mode != WorkflowModeWorkflow {
		t.Fatalf("expected mode %q, got %q", WorkflowModeWorkflow, mode)
	}
}

func TestRunChatModeSkipsTaskState(t *testing.T) {
	tmp := t.TempDir()
	store, err := history.NewFileStore(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client := &mockAPIClient{response: "Прямой ответ"}
	cfg := Config{APIFormat: "openai", Model: "test"}
	agent := NewSimpleAgent(cfg, client).WithHistory(store)

	resp, err := agent.Run(AgentRequest{
		Message:      "Привет",
		SessionID:    "sess-chat",
		WorkflowMode: WorkflowModeChat,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Прямой ответ" {
		t.Fatalf("unexpected response content: %q", resp.Content)
	}
	if resp.TaskStage != "" {
		t.Fatalf("expected no task stage in chat mode, got %q", resp.TaskStage)
	}
	if resp.TaskStatus != "" {
		t.Fatalf("expected no task status in chat mode, got %q", resp.TaskStatus)
	}

	session, err := store.LoadSession("sess-chat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.WorkflowMode != WorkflowModeChat {
		t.Fatalf("expected session workflow mode %q, got %q", WorkflowModeChat, session.WorkflowMode)
	}
	if session.TaskStage != "" {
		t.Fatalf("expected no task stage stored in chat mode, got %q", session.TaskStage)
	}
}

func TestRunWorkflowModeCreatesTaskState(t *testing.T) {
	tmp := t.TempDir()
	store, err := history.NewFileStore(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client := &mockAPIClient{response: "План"}
	cfg := Config{APIFormat: "openai", Model: "test"}
	agent := NewSimpleAgent(cfg, client).WithHistory(store)

	resp, err := agent.Run(AgentRequest{
		Message:      "Сделай авторизацию",
		SessionID:    "sess-workflow",
		WorkflowMode: WorkflowModeWorkflow,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.TaskStage != TaskStagePlanning {
		t.Fatalf("expected stage %q, got %q", TaskStagePlanning, resp.TaskStage)
	}
	if resp.TaskStatus != TaskStatusPending {
		t.Fatalf("expected status %q, got %q", TaskStatusPending, resp.TaskStatus)
	}

	session, err := store.LoadSession("sess-workflow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.TaskStage != TaskStagePlanning {
		t.Fatalf("expected stored stage %q, got %q", TaskStagePlanning, session.TaskStage)
	}
	if session.TaskContext.OriginalRequest != "Сделай авторизацию" {
		t.Fatalf("expected original request stored, got %q", session.TaskContext.OriginalRequest)
	}
}

func TestRunSwitchFromWorkflowToChatClearsTask(t *testing.T) {
	tmp := t.TempDir()
	store, err := history.NewFileStore(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client := &mockAPIClient{response: "Ответ"}
	cfg := Config{APIFormat: "openai", Model: "test"}
	agent := NewSimpleAgent(cfg, client).WithHistory(store)

	// Сначала запускаем задачу в режиме workflow.
	if _, err := agent.Run(AgentRequest{
		Message:      "Сделай авторизацию",
		SessionID:    "sess-switch",
		WorkflowMode: WorkflowModeWorkflow,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Переключаемся в режим chat — состояние задачи должно очиститься.
	resp, err := agent.Run(AgentRequest{
		Message:      "Привет",
		SessionID:    "sess-switch",
		WorkflowMode: WorkflowModeChat,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.TaskStage != "" {
		t.Fatalf("expected task cleared after switching to chat, got stage %q", resp.TaskStage)
	}

	session, err := store.LoadSession("sess-switch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.TaskStage != "" {
		t.Fatalf("expected no task stage after switch, got %q", session.TaskStage)
	}
}
