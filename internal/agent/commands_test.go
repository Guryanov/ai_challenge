package agent

import (
	"errors"
	"strings"
	"testing"

	"ai-chat/internal/history"
)

// failingClient падает при любом вызове — используется, чтобы проверить,
// что команды обрабатываются без обращения к LLM.
type failingClient struct{ calls int }

func (c *failingClient) Call([]byte) ([]byte, error) {
	c.calls++
	return nil, errors.New("LLM не должен вызываться для команд")
}

func newCommandTestStore(t *testing.T) *history.FileStore {
	t.Helper()
	store, err := history.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return store
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		message string
		name    string
		args    string
		ok      bool
	}{
		{"/help golang/go", "help", "golang/go", true},
		{"/HELP golang/go", "help", "golang/go", true},
		{"/help", "help", "", true},
		{"/exit", "exit", "", true},
		{"  /exit  ", "exit", "", true},
		{"/help a/b extra ignored", "help", "a/b", true},
		{"/", "", "", true},
		{"обычное сообщение", "", "", false},
		{"  /not a command prefix", "not", "a", true},
	}

	for _, tt := range tests {
		name, args, ok := parseCommand(tt.message)
		if ok != tt.ok || name != tt.name || args != tt.args {
			t.Errorf("parseCommand(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.message, name, args, ok, tt.name, tt.args, tt.ok)
		}
	}
}

func TestHelpCommandSetsProjectWithoutLLM(t *testing.T) {
	store := newCommandTestStore(t)
	client := &failingClient{}
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client).WithHistory(store)

	resp, err := a.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.calls != 0 {
		t.Fatalf("LLM был вызван %d раз, ожидалось 0", client.calls)
	}
	if resp.ActiveGitHubProject != "golang/go" {
		t.Fatalf("ActiveGitHubProject = %q, want %q", resp.ActiveGitHubProject, "golang/go")
	}
	if !strings.Contains(resp.Content, "golang/go") {
		t.Fatalf("ответ не содержит имя проекта: %q", resp.Content)
	}

	session, err := store.LoadSession("s1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if session.GitHubProject != "golang/go" {
		t.Fatalf("session.GitHubProject = %q, want %q", session.GitHubProject, "golang/go")
	}
}

func TestHelpCommandUsageAndInvalidArg(t *testing.T) {
	for _, message := range []string{"/help", "/help bad", "/help owner/", "/help /repo"} {
		store := newCommandTestStore(t)
		client := &failingClient{}
		a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client).WithHistory(store)

		resp, err := a.Run(AgentRequest{Message: message, SessionID: "s1"})
		if err != nil {
			t.Fatalf("Run(%q): %v", message, err)
		}
		if client.calls != 0 {
			t.Fatalf("Run(%q): LLM был вызван %d раз, ожидалось 0", message, client.calls)
		}
		if resp.ActiveGitHubProject != "" {
			t.Fatalf("Run(%q): проект не должен устанавливаться, получено %q", message, resp.ActiveGitHubProject)
		}
		if !strings.Contains(resp.Content, "Использование: /help") {
			t.Fatalf("Run(%q): нет подсказки по использованию: %q", message, resp.Content)
		}
	}
}

func TestExitCommandClearsProject(t *testing.T) {
	store := newCommandTestStore(t)
	client := &failingClient{}
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client).WithHistory(store)

	if _, err := a.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"}); err != nil {
		t.Fatalf("help Run: %v", err)
	}

	resp, err := a.Run(AgentRequest{Message: "/exit", SessionID: "s1"})
	if err != nil {
		t.Fatalf("exit Run: %v", err)
	}
	if resp.ActiveGitHubProject != "" {
		t.Fatalf("ActiveGitHubProject после /exit = %q, ожидалось пусто", resp.ActiveGitHubProject)
	}

	session, err := store.LoadSession("s1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if session.GitHubProject != "" {
		t.Fatalf("session.GitHubProject после /exit = %q, ожидалось пусто", session.GitHubProject)
	}

	// Повторный /exit без активного проекта — просто подсказка.
	resp, err = a.Run(AgentRequest{Message: "/exit", SessionID: "s1"})
	if err != nil {
		t.Fatalf("second exit Run: %v", err)
	}
	if !strings.Contains(resp.Content, "не активен") {
		t.Fatalf("нет сообщения об отсутствии режима: %q", resp.Content)
	}
}

func TestUnknownCommandListsAvailable(t *testing.T) {
	client := &failingClient{}
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client)

	resp, err := a.Run(AgentRequest{Message: "/foo bar", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.calls != 0 {
		t.Fatalf("LLM был вызван %d раз, ожидалось 0", client.calls)
	}
	if !strings.Contains(resp.Content, "/help") || !strings.Contains(resp.Content, "/exit") {
		t.Fatalf("нет списка доступных команд: %q", resp.Content)
	}
}

func TestCommandNotSavedToHistory(t *testing.T) {
	store := newCommandTestStore(t)
	client := &failingClient{}
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client).WithHistory(store)

	if _, err := a.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	session, err := store.LoadSession("s1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if msgs := session.Branches[session.ActiveBranch].Messages; len(msgs) != 0 {
		t.Fatalf("команда попала в историю: %d сообщений", len(msgs))
	}
}

func TestGitHubProjectInjectsContextAndTools(t *testing.T) {
	store := newCommandTestStore(t)
	client := &capturingClient{}
	tools := &mockToolRegistry{
		definitions: []ToolDefinition{
			{Name: "github_get_file_contents", Description: "Читать файл из репозитория"},
		},
	}
	cfg := Config{APIFormat: "openai", Model: "test"}
	a := NewSimpleAgent(cfg, client).WithHistory(store).WithTools(tools)

	if _, err := a.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"}); err != nil {
		t.Fatalf("help Run: %v", err)
	}
	if _, err := a.Run(AgentRequest{Message: "Как устроен рантайм?", SessionID: "s1"}); err != nil {
		t.Fatalf("question Run: %v", err)
	}

	if len(client.payloads) == 0 {
		t.Fatal("LLM не был вызван для вопроса")
	}
	payload := string(client.payloads[0])
	if !strings.Contains(payload, "Активный проект GitHub: golang/go") {
		t.Fatalf("в payload нет GitHub-контекста: %s", payload)
	}
	if !strings.Contains(payload, "github_get_file_contents") {
		t.Fatalf("в payload нет инструментов github: %s", payload)
	}
}

func TestBranchCommandSetsBranchWithoutLLM(t *testing.T) {
	store := newCommandTestStore(t)
	client := &failingClient{}
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client).WithHistory(store)

	if _, err := a.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"}); err != nil {
		t.Fatalf("help Run: %v", err)
	}

	resp, err := a.Run(AgentRequest{Message: "/branch day30", SessionID: "s1"})
	if err != nil {
		t.Fatalf("branch Run: %v", err)
	}
	if client.calls != 0 {
		t.Fatalf("LLM был вызван %d раз, ожидалось 0", client.calls)
	}
	if resp.ActiveGitBranch != "day30" {
		t.Fatalf("ActiveGitBranch = %q, want %q", resp.ActiveGitBranch, "day30")
	}

	session, err := store.LoadSession("s1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if session.GitBranch != "day30" {
		t.Fatalf("session.GitBranch = %q, want %q", session.GitBranch, "day30")
	}

	// Ветка с «/» поддерживается.
	if _, err := a.Run(AgentRequest{Message: "/branch feature/x", SessionID: "s1"}); err != nil {
		t.Fatalf("feature branch Run: %v", err)
	}
	session, _ = store.LoadSession("s1")
	if session.GitBranch != "feature/x" {
		t.Fatalf("session.GitBranch = %q, want %q", session.GitBranch, "feature/x")
	}
}

func TestBranchCommandHints(t *testing.T) {
	client := &failingClient{}

	// Без проекта.
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client)
	resp, err := a.Run(AgentRequest{Message: "/branch day30", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Content, "/help") {
		t.Fatalf("нет подсказки про /help: %q", resp.Content)
	}

	// Проект есть, аргумент пуст.
	a2 := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client).WithHistory(newCommandTestStore(t))
	if _, err := a2.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"}); err != nil {
		t.Fatalf("help Run: %v", err)
	}
	resp, err = a2.Run(AgentRequest{Message: "/branch", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Content, "Использование: /branch") {
		t.Fatalf("нет подсказки по использованию /branch: %q", resp.Content)
	}
	if client.calls != 0 {
		t.Fatalf("LLM был вызван %d раз, ожидалось 0", client.calls)
	}
}

func TestExitAndHelpResetBranch(t *testing.T) {
	store := newCommandTestStore(t)
	client := &failingClient{}
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client).WithHistory(store)

	if _, err := a.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"}); err != nil {
		t.Fatalf("help Run: %v", err)
	}
	if _, err := a.Run(AgentRequest{Message: "/branch day30", SessionID: "s1"}); err != nil {
		t.Fatalf("branch Run: %v", err)
	}

	// /help повторно сбрасывает ветку.
	if _, err := a.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"}); err != nil {
		t.Fatalf("second help Run: %v", err)
	}
	session, _ := store.LoadSession("s1")
	if session.GitBranch != "" {
		t.Fatalf("после повторного /help ветка = %q, ожидалось пусто", session.GitBranch)
	}

	// /exit очищает и проект, и ветку.
	if _, err := a.Run(AgentRequest{Message: "/branch day30", SessionID: "s1"}); err != nil {
		t.Fatalf("branch Run: %v", err)
	}
	resp, err := a.Run(AgentRequest{Message: "/exit", SessionID: "s1"})
	if err != nil {
		t.Fatalf("exit Run: %v", err)
	}
	if resp.ActiveGitHubProject != "" || resp.ActiveGitBranch != "" {
		t.Fatalf("после /exit project=%q branch=%q, ожидалось пусто", resp.ActiveGitHubProject, resp.ActiveGitBranch)
	}
	session, _ = store.LoadSession("s1")
	if session.GitHubProject != "" || session.GitBranch != "" {
		t.Fatalf("после /exit session project=%q branch=%q", session.GitHubProject, session.GitBranch)
	}
}

func TestBranchesCommandRoutesToLLM(t *testing.T) {
	store := newCommandTestStore(t)
	client := &capturingClient{}
	tools := &mockToolRegistry{
		definitions: []ToolDefinition{{Name: "github_list_branches", Description: "Список веток"}},
	}
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client).WithHistory(store).WithTools(tools)

	if _, err := a.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"}); err != nil {
		t.Fatalf("help Run: %v", err)
	}
	if _, err := a.Run(AgentRequest{Message: "/branches", SessionID: "s1"}); err != nil {
		t.Fatalf("branches Run: %v", err)
	}

	if len(client.payloads) == 0 {
		t.Fatal("LLM не был вызван для /branches")
	}
	payload := string(client.payloads[0])
	if !strings.Contains(payload, "list_branches") {
		t.Fatalf("в payload нет инструкции про list_branches: %s", payload)
	}
	if !strings.Contains(payload, "github_list_branches") {
		t.Fatalf("в payload нет инструмента github_list_branches: %s", payload)
	}
}

func TestBranchesCommandWithoutProject(t *testing.T) {
	client := &failingClient{}
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client)

	resp, err := a.Run(AgentRequest{Message: "/branches", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Content, "/help") {
		t.Fatalf("нет подсказки про /help: %q", resp.Content)
	}
	if client.calls != 0 {
		t.Fatalf("LLM был вызван %d раз, ожидалось 0", client.calls)
	}
}

func TestDiffCommandHints(t *testing.T) {
	client := &failingClient{}

	// Без проекта.
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client)
	resp, err := a.Run(AgentRequest{Message: "/diff", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Content, "/help") {
		t.Fatalf("нет подсказки про /help: %q", resp.Content)
	}

	// Проект есть, ветки нет, аргумента нет.
	a2 := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client).WithHistory(newCommandTestStore(t))
	if _, err := a2.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"}); err != nil {
		t.Fatalf("help Run: %v", err)
	}
	resp, err = a2.Run(AgentRequest{Message: "/diff", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Content, "Активная ветка не выбрана") {
		t.Fatalf("нет подсказки про /branch: %q", resp.Content)
	}

	// Неверный аргумент.
	resp, err = a2.Run(AgentRequest{Message: "/diff abc", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Content, "Использование: /diff") {
		t.Fatalf("нет подсказки по использованию /diff: %q", resp.Content)
	}
	if client.calls != 0 {
		t.Fatalf("LLM был вызван %d раз, ожидалось 0", client.calls)
	}
}

func TestDiffCommandRoutesToLLM(t *testing.T) {
	store := newCommandTestStore(t)
	client := &capturingClient{}
	tools := &mockToolRegistry{
		definitions: []ToolDefinition{
			{Name: "github_list_pull_requests", Description: "Список PR"},
			{Name: "github_pull_request_read", Description: "Чтение PR"},
		},
	}
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client).WithHistory(store).WithTools(tools)

	if _, err := a.Run(AgentRequest{Message: "/help golang/go", SessionID: "s1"}); err != nil {
		t.Fatalf("help Run: %v", err)
	}

	// По номеру PR.
	if _, err := a.Run(AgentRequest{Message: "/diff 42", SessionID: "s1"}); err != nil {
		t.Fatalf("diff Run: %v", err)
	}
	payload := string(client.payloads[0])
	if !strings.Contains(payload, "#42") {
		t.Fatalf("в payload нет номера PR 42: %s", payload)
	}

	// По активной ветке.
	if _, err := a.Run(AgentRequest{Message: "/branch day30", SessionID: "s1"}); err != nil {
		t.Fatalf("branch Run: %v", err)
	}
	client.payloads = nil
	if _, err := a.Run(AgentRequest{Message: "/diff", SessionID: "s1"}); err != nil {
		t.Fatalf("diff Run: %v", err)
	}
	if len(client.payloads) == 0 {
		t.Fatal("LLM не был вызван для /diff")
	}
	payload = string(client.payloads[0])
	if !strings.Contains(payload, "day30") {
		t.Fatalf("в payload нет активной ветки day30: %s", payload)
	}

	// В историю сохраняется исходная команда, а не синтезированный промпт.
	session, _ := store.LoadSession("s1")
	msgs := session.Branches[session.ActiveBranch].Messages
	var lastUser string
	for _, m := range msgs {
		if m.Role == "user" {
			lastUser = m.Content
		}
	}
	if lastUser != "/diff" {
		t.Fatalf("в историю не сохранена исходная команда: lastUser=%q, msgs=%+v", lastUser, msgs)
	}
}
