package pipeline

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-chat/internal/agent"
)

// Имена MCP-серверов, ожидаемые в mcp.yaml.
const (
	sentryServer = "sentry"
	jiraServer   = "jira"
	githubServer = "github"
)

// maxFixContextFiles — сколько полных файлов подмешивать в промпт фикса.
const maxFixContextFiles = 5

// repoFile — содержимое файла репозитория.
type repoFile struct {
	Path    string
	Content string
}

// Orchestrator выполняет один проход конвейера: Sentry → Jira → ветка → фикс → PR.
type Orchestrator struct {
	cfg       Config
	tools     *mcpTools
	llm       LLM
	retriever retriever
	store     Store

	runMu   sync.Mutex
	lastMu  sync.Mutex
	last    RunResult
	lastSet bool
}

// NewOrchestrator создаёт оркестратор конвейера.
func NewOrchestrator(cfg Config, caller ToolCaller, cat ToolCatalog, llm LLM, ret retriever, store Store) *Orchestrator {
	return &Orchestrator{
		cfg:       cfg,
		tools:     newMCPTools(caller, cat),
		llm:       llm,
		retriever: ret,
		store:     store,
	}
}

// Config возвращает конфигурацию конвейера.
func (o *Orchestrator) Config() Config { return o.cfg }

// LastRun возвращает результат последнего запуска.
func (o *Orchestrator) LastRun() (RunResult, bool) {
	o.lastMu.Lock()
	defer o.lastMu.Unlock()
	return o.last, o.lastSet
}

// Records возвращает все обработанные issue.
func (o *Orchestrator) Records() []Record {
	if o.store == nil {
		return nil
	}
	return o.store.List()
}

// RunOnce выполняет один проход конвейера. При dryRun мутирующие вызовы
// (Jira/GitHub) не выполняются, состояние не сохраняется.
func (o *Orchestrator) RunOnce(ctx context.Context, dryRun bool) (RunResult, error) {
	o.runMu.Lock()
	defer o.runMu.Unlock()

	res := RunResult{StartedAt: nowString(), DryRun: dryRun}

	issues, err := o.tools.searchIssues(sentryServer, o.cfg.SentryQuery, o.cfg.SentryOrg, o.cfg.SentryProject)
	if err != nil {
		res.Error = err.Error()
		res.FinishedAt = nowString()
		o.setLast(res)
		return res, err
	}
	res.Fetched = len(issues)

	// Обрабатываем сначала самые свежие issue.
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].LastSeen > issues[j].LastSeen })

	for _, issue := range issues {
		if res.Processed >= o.cfg.MaxIssuesPerRun {
			break
		}
		if rec, ok := o.store.Get(issue.ID); ok && rec.isDone() {
			continue
		}
		res.Processed++
		res.Items = append(res.Items, o.processIssue(ctx, issue, dryRun))
	}

	res.FinishedAt = nowString()
	o.setLast(res)
	return res, nil
}

func (o *Orchestrator) setLast(res RunResult) {
	o.lastMu.Lock()
	o.last, o.lastSet = res, true
	o.lastMu.Unlock()
}

// processIssue выполняет шаги конвейера для одного issue, возобновляя
// частично завершённую работу по сохранённой записи.
func (o *Orchestrator) processIssue(ctx context.Context, issue SentryIssue, dryRun bool) Record {
	rec, ok := o.store.Get(issue.ID)
	if !ok {
		rec = Record{IssueID: issue.ID, Status: StatusFailed}
	}
	if rec.Title == "" {
		rec.Title = issue.Title
	}

	// 1. Текст тикета и создание Jira.
	var ticketSummary string
	if rec.JiraKey == "" {
		resp, err := o.llm.Complete(agent.AgentRequest{
			Message:        buildTicketPrompt(issue),
			ResponseFormat: "json",
			Context:        ctx,
		})
		if err != nil {
			return o.fail(rec, "генерация тикета: "+err.Error())
		}
		var description string
		ticketSummary, description = parseTicketText(resp.Content, issue)

		if dryRun {
			rec.JiraKey = "<dry-run>"
			rec.Branch = "bugfix/<dry-run>"
			rec.Status = StatusDryRun
			rec.Error = "dry-run: тикет, ветка и PR не создаются"
			return rec
		}

		key, err := o.tools.createJiraIssue(jiraServer, o.cfg.JiraProjectKey, o.cfg.JiraIssueType, ticketSummary, description)
		if err != nil {
			return o.fail(rec, "создание Jira: "+err.Error())
		}
		rec.JiraKey = key
		rec.Status = StatusJiraCreated
		o.save(rec)
		log.Printf("[pipeline] issue %s: создан Jira-тикет %s", issue.ID, key)
	}

	// 2. Ветка bugfix/JIRA-###.
	branch := rec.Branch
	if branch == "" {
		branch = "bugfix/" + rec.JiraKey
		if err := o.tools.createBranch(githubServer, o.cfg.RepoOwner, o.cfg.RepoName, branch, o.cfg.BaseBranch); err != nil {
			return o.fail(rec, "создание ветки: "+err.Error())
		}
		rec.Branch = branch
		rec.Status = StatusBranchCreated
		o.save(rec)
		log.Printf("[pipeline] issue %s: создана ветка %s", issue.ID, branch)
	}

	// 3. Фикс кода по RAG.
	plan, err := o.buildFix(ctx, issue)
	if err != nil {
		return o.manual(rec, "генерация фикса: "+err.Error())
	}
	if len(plan.Files) == 0 {
		return o.manual(rec, "модель не предложила изменений")
	}

	// 4. Применение правок в ветке.
	applied := 0
	var applyErr string
	for _, f := range plan.Files {
		if applied >= o.cfg.MaxFilesPerFix {
			break
		}
		_, sha, _ := o.tools.getFileContent(githubServer, o.cfg.RepoOwner, o.cfg.RepoName, f.Path, branch)
		message := fmt.Sprintf("%s: автофикс Sentry issue %s", rec.JiraKey, issue.ID)
		if err := o.tools.createOrUpdateFile(githubServer, o.cfg.RepoOwner, o.cfg.RepoName, f.Path, f.Content, message, branch, sha); err != nil {
			applyErr = err.Error()
			continue
		}
		applied++
	}
	if applied == 0 {
		msg := "не удалось применить правки"
		if applyErr != "" {
			msg += ": " + applyErr
		}
		return o.manual(rec, msg)
	}
	log.Printf("[pipeline] issue %s: применено файлов %d", issue.ID, applied)

	// 5. Pull request в базовую ветку.
	title := rec.JiraKey + ": " + ticketSummary
	if strings.TrimSpace(ticketSummary) == "" {
		title = rec.JiraKey + ": автофикс Sentry issue " + issue.ID
	}
	prURL, err := o.tools.createPullRequest(githubServer, o.cfg.RepoOwner, o.cfg.RepoName, title, branch, o.cfg.BaseBranch, buildPRBody(issue, rec.JiraKey, plan))
	if err != nil {
		return o.fail(rec, "создание PR: "+err.Error())
	}
	rec.PRURL = prURL
	rec.Status = StatusPRCreated
	o.save(rec)
	log.Printf("[pipeline] issue %s: создан PR %s", issue.ID, prURL)
	return rec
}

// buildFix находит релевантные файлы через RAG и просит LLM предложить правки.
func (o *Orchestrator) buildFix(ctx context.Context, issue SentryIssue) (FixPlan, error) {
	var files []repoFile
	if o.retriever != nil && o.cfg.ProjectID != "" {
		chunks, err := o.retriever.Retrieve(ctx, o.cfg.ProjectID, agent.RetrieveOptions{
			Strategy:   "structure",
			Query:      issue.searchQuery(),
			Candidates: 20,
			TopK:       8,
		})
		if err != nil {
			log.Printf("[pipeline] RAG-поиск не удался: %v", err)
		} else {
			files = o.fetchFiles(ctx, uniquePaths(chunks))
		}
	}

	resp, err := o.llm.Complete(agent.AgentRequest{
		Message:        buildFixPrompt(issue, files),
		ResponseFormat: "json",
		Context:        ctx,
	})
	if err != nil {
		return FixPlan{}, err
	}
	return parseFixPlan(resp.Content)
}

// fetchFiles читает полные файлы (из базовой ветки) для найденных путей.
func (o *Orchestrator) fetchFiles(ctx context.Context, paths []string) []repoFile {
	files := make([]repoFile, 0, len(paths))
	for _, p := range paths {
		content, _, err := o.tools.getFileContent(githubServer, o.cfg.RepoOwner, o.cfg.RepoName, p, o.cfg.BaseBranch)
		if err != nil {
			log.Printf("[pipeline] не удалось прочитать файл %s: %v", p, err)
			continue
		}
		files = append(files, repoFile{Path: p, Content: content})
	}
	return files
}

// uniquePaths возвращает уникальные пути чанков в порядке появления.
func uniquePaths(chunks []agent.RetrievedChunk) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if c.Path == "" || seen[c.Path] {
			continue
		}
		seen[c.Path] = true
		out = append(out, c.Path)
		if len(out) >= maxFixContextFiles {
			break
		}
	}
	return out
}

func (o *Orchestrator) fail(rec Record, msg string) Record {
	rec.Status = StatusFailed
	rec.Error = msg
	o.save(rec)
	log.Printf("[pipeline] issue %s: ошибка: %s", rec.IssueID, msg)
	return rec
}

func (o *Orchestrator) manual(rec Record, msg string) Record {
	rec.Status = StatusNeedsManual
	rec.Error = msg
	o.save(rec)
	log.Printf("[pipeline] issue %s: требуется ручной фикс: %s", rec.IssueID, msg)
	return rec
}

func (o *Orchestrator) save(rec Record) {
	if o.store == nil {
		return
	}
	if err := o.store.Save(rec); err != nil {
		log.Printf("[pipeline] не удалось сохранить запись issue %s: %v", rec.IssueID, err)
	}
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}
