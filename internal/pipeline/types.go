// Package pipeline реализует узко-задачный конвейер:
// Sentry → Jira → ветка bugfix/JIRA-### → фикс кода по RAG → PR.
package pipeline

import (
	"context"
	"encoding/json"

	"ai-chat/internal/agent"
)

// ToolCaller выполняет MCP-инструмент по полному имени "<server>_<tool>".
type ToolCaller interface {
	Execute(name string, args json.RawMessage) (string, error)
}

// LLM выполняет одиночный вызов модели без инструментов и истории диалога.
type LLM interface {
	Complete(req agent.AgentRequest) (agent.AgentResponse, error)
}

// ServerTool — доступные инструменты одного MCP-сервера (без префикса сервера).
type ServerTool struct {
	Server string
	Tools  []string
}

// ToolCatalog перечисляет инструменты подключённых MCP-серверов.
type ToolCatalog interface {
	Catalog() []ServerTool
}

// Config — настройки конвейера.
type Config struct {
	Enabled         bool
	At              string
	SentryQuery     string
	SentryOrg       string
	SentryProject   string
	RepoOwner       string
	RepoName        string
	BaseBranch      string
	Root            string
	ProjectID       string
	IndexOnStart    bool
	IndexExtensions []string
	IndexMaxFileKB  int
	JiraProjectKey  string
	JiraIssueType   string
	MaxIssuesPerRun int
	MaxFilesPerFix  int
}

// SentryIssue — критический issue из Sentry.
type SentryIssue struct {
	ID         string
	Title      string
	Culprit    string
	Level      string
	Count      int
	FirstSeen  string
	LastSeen   string
	Permalink  string
	StackTrace string
	Raw        string
}

// searchQuery формирует строку для RAG-поиска по месту падения.
func (i SentryIssue) searchQuery() string {
	parts := make([]string, 0, 4)
	for _, s := range []string{i.Title, i.Culprit, i.StackTrace} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	query := ""
	for _, p := range parts {
		if query != "" {
			query += "\n"
		}
		query += p
	}
	return query
}

// FileEdit — новая (полная) версия файла.
type FileEdit struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FixPlan — результат генерации фикса.
type FixPlan struct {
	Files   []FileEdit `json:"files"`
	Summary string     `json:"summary"`
}

// Record — состояние обработки одного issue.
type Record struct {
	IssueID   string `json:"issue_id"`
	Title     string `json:"title"`
	JiraKey   string `json:"jira_key,omitempty"`
	Branch    string `json:"branch,omitempty"`
	PRURL     string `json:"pr_url,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// Статусы записи.
const (
	StatusJiraCreated   = "jira_created"
	StatusBranchCreated = "branch_created"
	StatusPRCreated     = "pr_created"
	StatusNeedsManual   = "needs_manual_fix"
	StatusFailed        = "failed"
	StatusDryRun        = "dry_run"
)

// isDone сообщает, что issue обработан окончательно и не требует повтора.
func (r Record) isDone() bool {
	return r.Status == StatusPRCreated || r.Status == StatusNeedsManual
}

// RunResult — итог одного запуска конвейера.
type RunResult struct {
	StartedAt  string   `json:"started_at"`
	FinishedAt string   `json:"finished_at"`
	DryRun     bool     `json:"dry_run"`
	Fetched    int      `json:"fetched"`
	Processed  int      `json:"processed"`
	Items      []Record `json:"items"`
	Error      string   `json:"error,omitempty"`
}

// retriever — сокращённый интерфейс RAG-поиска (совпадает с agent.KnowledgeRetriever).
type retriever interface {
	Retrieve(ctx context.Context, projectID string, opts agent.RetrieveOptions) ([]agent.RetrievedChunk, error)
}
