package agent

import (
	"context"
	"strings"
	"testing"
)

// recordingRetriever фиксирует project_id и опции RAG-поиска.
type recordingRetriever struct {
	projectID string
	opts      RetrieveOptions
	chunks    []RetrievedChunk
}

func (r *recordingRetriever) Retrieve(_ context.Context, projectID string, opts RetrieveOptions) ([]RetrievedChunk, error) {
	r.projectID = projectID
	r.opts = opts
	return r.chunks, nil
}

func TestTicketPrependsGreetingAndUsesQuestion(t *testing.T) {
	client := &capturingClient{}
	cfg := Config{APIFormat: "openai", Model: "test"}
	a := NewSimpleAgent(cfg, client)

	resp, err := a.Run(AgentRequest{
		SessionID: "s-ticket",
		Message:   "",
		Ticket: &Ticket{
			ID:       "42",
			UserName: "Иван",
			Project:  "group/repo",
			Question: "Почему падает сервис X?",
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.HasPrefix(resp.Content, "Добрый день уважаемый Иван.\n\n") {
		t.Fatalf("response does not start with greeting: %q", resp.Content)
	}
	if len(client.payloads) == 0 {
		t.Fatal("no LLM payload captured")
	}
	if !strings.Contains(string(client.payloads[0]), "Почему падает сервис X?") {
		t.Fatalf("payload does not contain ticket question:\n%s", client.payloads[0])
	}
}

func TestTicketUsesProjectForRAG(t *testing.T) {
	client := &capturingClient{}
	retriever := &recordingRetriever{chunks: []RetrievedChunk{
		{Path: "docs/x.md", Text: "Фрагмент про сервис X"},
	}}
	cfg := Config{APIFormat: "openai", Model: "test", RAGEnabled: true, RAGStrategy: "structure", RAGTopK: 3}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	if _, err := a.Run(AgentRequest{
		SessionID: "s-ticket-rag",
		Ticket: &Ticket{
			UserName: "Пётр",
			Project:  "group/repo",
			Question: "Как работает X?",
		},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if retriever.projectID != "group/repo" {
		t.Fatalf("RAG project_id = %q, want group/repo", retriever.projectID)
	}
	if retriever.opts.Query != "Как работает X?" {
		t.Fatalf("RAG query = %q, want ticket question", retriever.opts.Query)
	}
	if !strings.Contains(string(client.payloads[0]), "Фрагмент про сервис X") {
		t.Fatalf("payload does not contain retrieved chunk:\n%s", client.payloads[0])
	}
}

func TestTicketDoesNotRefuseWhenRAGEmpty(t *testing.T) {
	client := &capturingClient{}
	retriever := &recordingRetriever{}
	cfg := Config{APIFormat: "openai", Model: "test", RAGEnabled: true, RAGStrategy: "structure", RAGTopK: 3}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	resp, err := a.Run(AgentRequest{
		SessionID: "s-ticket-empty-rag",
		Ticket: &Ticket{
			UserName: "Анна",
			Project:  "group/repo",
			Question: "Что с сервисом X?",
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(client.payloads) == 0 {
		t.Fatal("LLM was not called: hard RAG refusal applied in ticket mode")
	}
	if strings.Contains(resp.Content, "Не знаю.") {
		t.Fatalf("unexpected deterministic refusal: %q", resp.Content)
	}
	if !strings.HasPrefix(resp.Content, "Добрый день уважаемый Анна.\n\n") {
		t.Fatalf("response does not start with greeting: %q", resp.Content)
	}
}

func TestTicketUsesGitLabToolsInChat(t *testing.T) {
	client := &mockSequenceClient{
		responses: [][]byte{
			toolCallResponse("gitlab_read_file", `{"file_path":"README.md"}`),
			textResponse("Ответ по проекту из GitLab"),
		},
	}
	tools := &mockToolRegistry{results: map[string]string{
		"gitlab_read_file": "содержимое файла",
	}}
	cfg := Config{APIFormat: "openai", Model: "test"}
	a := NewSimpleAgent(cfg, client).WithTools(tools)

	resp, err := a.Run(AgentRequest{
		SessionID:    "s-ticket-tools",
		WorkflowMode: WorkflowModeChat,
		Ticket: &Ticket{
			UserName: "Сергей",
			Project:  "group/repo",
			Question: "Что в README?",
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(tools.calls) != 1 || tools.calls[0].Name != "gitlab_read_file" {
		t.Fatalf("expected 1 gitlab tool call, got %+v", tools.calls)
	}
	if !strings.HasPrefix(resp.Content, "Добрый день уважаемый Сергей.\n\n") {
		t.Fatalf("response does not start with greeting: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "Ответ по проекту из GitLab") {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
}

func TestTicketWorkflowModeNotSupported(t *testing.T) {
	client := &capturingClient{}
	cfg := Config{APIFormat: "openai", Model: "test"}
	a := NewSimpleAgent(cfg, client)

	resp, err := a.Run(AgentRequest{
		SessionID:    "s-ticket-workflow",
		WorkflowMode: WorkflowModeWorkflow,
		Ticket: &Ticket{
			UserName: "Олег",
			Project:  "group/repo",
			Question: "Вопрос",
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if resp.Content != ticketWorkflowNotice {
		t.Fatalf("content = %q, want workflow notice", resp.Content)
	}
	if len(client.payloads) != 0 {
		t.Fatalf("LLM must not be called in workflow ticket mode, got %d calls", len(client.payloads))
	}
}

func TestTicketValidation(t *testing.T) {
	client := &capturingClient{}
	cfg := Config{APIFormat: "openai", Model: "test"}
	a := NewSimpleAgent(cfg, client)

	if _, err := a.Run(AgentRequest{
		SessionID: "s-ticket-invalid",
		Ticket:    &Ticket{UserName: "", Project: "group/repo", Question: "Вопрос"},
	}); err == nil {
		t.Fatal("expected error for empty user_name")
	}

	if _, err := a.Run(AgentRequest{
		SessionID: "s-ticket-invalid2",
		Ticket:    &Ticket{UserName: "Иван", Project: "group/repo", Question: "  "},
	}); err == nil {
		t.Fatal("expected error for empty question")
	}
}
