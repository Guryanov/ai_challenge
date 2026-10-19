package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"ai-chat/internal/agent"
)

// fakeCaller — фейковый MCP-реестр.
type fakeCaller struct {
	results map[string]string
	err     error
	calls   []string
}

func (f *fakeCaller) Execute(name string, _ json.RawMessage) (string, error) {
	f.calls = append(f.calls, name)
	if f.err != nil {
		return "", f.err
	}
	res, ok := f.results[name]
	if !ok {
		return "", fmt.Errorf("нет фейкового ответа для %s", name)
	}
	return res, nil
}

// fakeCatalog — фейковый каталог инструментов.
type fakeCatalog struct{ tools []ServerTool }

func (c fakeCatalog) Catalog() []ServerTool { return c.tools }

// fakeLLM выдаёт заранее заданные ответы по порядку.
type fakeLLM struct {
	responses []string
	idx       int
}

func (l *fakeLLM) Complete(_ agent.AgentRequest) (agent.AgentResponse, error) {
	if l.idx >= len(l.responses) {
		return agent.AgentResponse{}, fmt.Errorf("нет фейкового ответа LLM #%d", l.idx)
	}
	resp := l.responses[l.idx]
	if l.idx < len(l.responses)-1 {
		l.idx++
	}
	return agent.AgentResponse{Content: resp}, nil
}

// fakeRetriever — фейковый RAG-поиск.
type fakeRetriever struct {
	chunks []agent.RetrievedChunk
	err    error
}

func (r fakeRetriever) Retrieve(_ context.Context, _ string, _ agent.RetrieveOptions) ([]agent.RetrievedChunk, error) {
	return r.chunks, r.err
}

func testCatalog() fakeCatalog {
	return fakeCatalog{tools: []ServerTool{
		{Server: "sentry", Tools: []string{"search_issues"}},
		{Server: "jira", Tools: []string{"jira_create_issue"}},
		{Server: "github", Tools: []string{"create_branch", "get_file_contents", "create_or_update_file", "create_pull_request"}},
	}}
}

func testConfig() Config {
	return Config{
		Enabled:         true,
		SentryQuery:     "is:unresolved level:fatal",
		RepoOwner:       "owner",
		RepoName:        "repo",
		BaseBranch:      "main",
		ProjectID:       "repo",
		JiraProjectKey:  "PROJ",
		JiraIssueType:   "Task",
		MaxIssuesPerRun: 1,
		MaxFilesPerFix:  3,
	}
}

func newTestOrchestrator(t *testing.T, caller *fakeCaller, llm *fakeLLM, ret fakeRetriever) (*Orchestrator, *FileStore) {
	t.Helper()
	store, err := NewFileStore(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	orch := NewOrchestrator(testConfig(), caller, testCatalog(), llm, ret, store)
	return orch, store
}

func happyCaller() *fakeCaller {
	return &fakeCaller{results: map[string]string{
		"sentry_search_issues":         `{"issues":[{"id":"1","title":"panic in main","culprit":"main.go","level":"fatal","lastSeen":"2026-10-19T07:00:00Z"}]}`,
		"jira_jira_create_issue":       `{"key":"PROJ-42","self":"https://jira/PROJ-42"}`,
		"github_create_branch":         `{"ref":"refs/heads/bugfix/PROJ-42"}`,
		"github_get_file_contents":     `{"content":"cGFja2FnZSBtYWluCg==","sha":"abc123"}`,
		"github_create_or_update_file": `{"content":{"sha":"new"}}`,
		"github_create_pull_request":   `{"html_url":"https://github.com/owner/repo/pull/7"}`,
	}}
}

func TestOrchestratorHappyPath(t *testing.T) {
	caller := happyCaller()
	llm := &fakeLLM{responses: []string{
		`{"summary":"Паника в main","description":"Падает при старте"}`,
		`{"files":[{"path":"main.go","content":"package main\n"}],"summary":"добавлена проверка"}`,
	}}
	ret := fakeRetriever{chunks: []agent.RetrievedChunk{{Path: "main.go", Text: "package main"}}}

	orch, _ := newTestOrchestrator(t, caller, llm, ret)

	res, err := orch.RunOnce(context.Background(), false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if res.Fetched != 1 || res.Processed != 1 {
		t.Fatalf("fetched=%d processed=%d, want 1/1", res.Fetched, res.Processed)
	}

	rec := res.Items[0]
	if rec.Status != StatusPRCreated {
		t.Fatalf("status = %q, want %q (err=%q)", rec.Status, StatusPRCreated, rec.Error)
	}
	if rec.JiraKey != "PROJ-42" {
		t.Fatalf("jira key = %q", rec.JiraKey)
	}
	if rec.Branch != "bugfix/PROJ-42" {
		t.Fatalf("branch = %q", rec.Branch)
	}
	if !strings.Contains(rec.PRURL, "/pull/7") {
		t.Fatalf("pr url = %q", rec.PRURL)
	}

	// Повторный запуск не должен переобрабатывать issue.
	res2, err := orch.RunOnce(context.Background(), false)
	if err != nil {
		t.Fatalf("RunOnce #2: %v", err)
	}
	if res2.Processed != 0 {
		t.Fatalf("second run processed = %d, want 0", res2.Processed)
	}
}

func TestOrchestratorJiraFailureIsRetried(t *testing.T) {
	caller := happyCaller()
	caller.results["jira_jira_create_issue"] = "server error: no key here"
	llm := &fakeLLM{responses: []string{`{"summary":"s","description":"d"}`}}

	orch, _ := newTestOrchestrator(t, caller, llm, fakeRetriever{})

	res, err := orch.RunOnce(context.Background(), false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if res.Items[0].Status != StatusFailed {
		t.Fatalf("status = %q, want failed", res.Items[0].Status)
	}

	// Провал Jira не помечает issue как обработанный — повторяем.
	res2, err := orch.RunOnce(context.Background(), false)
	if err != nil {
		t.Fatalf("RunOnce #2: %v", err)
	}
	if res2.Processed != 1 {
		t.Fatalf("retry processed = %d, want 1", res2.Processed)
	}
}

func TestOrchestratorNoFixNeedsManual(t *testing.T) {
	caller := happyCaller()
	llm := &fakeLLM{responses: []string{
		`{"summary":"s","description":"d"}`,
		`{"files":[],"summary":"не смог"}`, // модель не предложила правок
	}}
	orch, _ := newTestOrchestrator(t, caller, llm, fakeRetriever{})

	res, err := orch.RunOnce(context.Background(), false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	rec := res.Items[0]
	if rec.Status != StatusNeedsManual {
		t.Fatalf("status = %q, want %q", rec.Status, StatusNeedsManual)
	}
	if rec.JiraKey != "PROJ-42" || rec.Branch != "bugfix/PROJ-42" {
		t.Fatalf("record must keep jira/branch: %+v", rec)
	}
	if rec.PRURL != "" {
		t.Fatalf("pr url must be empty, got %q", rec.PRURL)
	}
}

func TestOrchestratorDryRunDoesNotMutate(t *testing.T) {
	caller := happyCaller()
	llm := &fakeLLM{responses: []string{`{"summary":"s","description":"d"}`}}
	orch, _ := newTestOrchestrator(t, caller, llm, fakeRetriever{})

	res, err := orch.RunOnce(context.Background(), true)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !res.DryRun || res.Items[0].Status != StatusDryRun {
		t.Fatalf("dry-run record = %+v", res.Items[0])
	}
	for _, call := range caller.calls {
		if strings.HasPrefix(call, "jira_") || strings.Contains(call, "create_branch") ||
			strings.Contains(call, "create_or_update_file") || strings.Contains(call, "create_pull_request") {
			t.Fatalf("dry-run выполнил мутирующий вызов: %s", call)
		}
	}
}
