package pipeline

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
)

// mcpTools резолвит логические инструменты в конкретные имена MCP-серверов
// и выполняет их вызовы.
type mcpTools struct {
	caller ToolCaller
	cat    ToolCatalog
}

func newMCPTools(caller ToolCaller, cat ToolCatalog) *mcpTools {
	return &mcpTools{caller: caller, cat: cat}
}

// resolve находит полное имя инструмента "<server>_<tool>" по списку кандидатов.
func (m *mcpTools) resolve(server string, candidates ...string) (string, error) {
	if m.cat == nil {
		return "", fmt.Errorf("каталог MCP-инструментов недоступен")
	}

	for _, st := range m.cat.Catalog() {
		if st.Server != server {
			continue
		}
		for _, cand := range candidates {
			for _, name := range st.Tools {
				if name == cand {
					return server + "_" + name, nil
				}
			}
		}
	}

	return "", fmt.Errorf("MCP-инструмент не найден: сервер %q, кандидаты %v", server, candidates)
}

// call выполняет инструмент и возвращает текстовый результат.
func (m *mcpTools) call(name string, args map[string]any) (string, error) {
	if m.caller == nil {
		return "", fmt.Errorf("MCP-реестр недоступен")
	}

	payload, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("не удалось сериализовать аргументы %s: %w", name, err)
	}

	log.Printf("[pipeline] MCP %s args=%s", name, truncate(string(payload), 400))

	raw, err := m.caller.Execute(name, json.RawMessage(payload))
	if err != nil {
		return "", fmt.Errorf("вызов инструмента %s: %w", name, err)
	}
	return raw, nil
}

// --- Sentry ---

func (m *mcpTools) searchIssues(server, query, org, project string) ([]SentryIssue, error) {
	name, err := m.resolve(server, "search_issues", "list_issues")
	if err != nil {
		return nil, err
	}

	args := map[string]any{"query": query}
	if org != "" {
		args["organizationSlug"] = org
	}
	if project != "" {
		args["projectSlug"] = project
	}

	raw, err := m.call(name, args)
	if err != nil {
		return nil, err
	}
	return parseIssues(raw)
}

// --- Jira ---

func (m *mcpTools) createJiraIssue(server, projectKey, issueType, summary, description string) (string, error) {
	name, err := m.resolve(server, "jira_create_issue", "create_issue")
	if err != nil {
		return "", err
	}

	raw, err := m.call(name, map[string]any{
		"project_key": projectKey,
		"summary":     summary,
		"description": description,
		"issue_type":  issueType,
	})
	if err != nil {
		return "", err
	}

	key := parseJiraKey(raw)
	if key == "" {
		return "", fmt.Errorf("не удалось извлечь ключ Jira из ответа: %s", truncate(raw, 400))
	}
	return key, nil
}

// --- GitHub ---

func (m *mcpTools) createBranch(server, owner, repo, branch, from string) error {
	name, err := m.resolve(server, "create_branch")
	if err != nil {
		return err
	}

	raw, err := m.call(name, map[string]any{
		"owner":       owner,
		"repo":        repo,
		"branch":      branch,
		"from_branch": from,
	})
	if err != nil {
		return err
	}
	if isErrorResult(raw) {
		return fmt.Errorf("создание ветки: %s", truncate(raw, 300))
	}
	return nil
}

// getFileContent читает файл репозитория и возвращает его текст и blob sha.
func (m *mcpTools) getFileContent(server, owner, repo, path, ref string) (content, sha string, err error) {
	name, err := m.resolve(server, "get_file_contents")
	if err != nil {
		return "", "", err
	}

	args := map[string]any{"owner": owner, "repo": repo, "path": path}
	if ref != "" {
		args["ref"] = ref
	}

	raw, err := m.call(name, args)
	if err != nil {
		return "", "", err
	}
	return parseFileContent(raw)
}

func (m *mcpTools) createOrUpdateFile(server, owner, repo, path, content, message, branch, sha string) error {
	name, err := m.resolve(server, "create_or_update_file")
	if err != nil {
		return err
	}

	args := map[string]any{
		"owner":   owner,
		"repo":    repo,
		"path":    path,
		"content": content,
		"message": message,
		"branch":  branch,
	}
	if sha != "" {
		args["sha"] = sha
	}

	raw, err := m.call(name, args)
	if err != nil {
		return err
	}
	if isErrorResult(raw) {
		return fmt.Errorf("запись файла %s: %s", path, truncate(raw, 300))
	}
	return nil
}

func (m *mcpTools) createPullRequest(server, owner, repo, title, head, base, body string) (string, error) {
	name, err := m.resolve(server, "create_pull_request")
	if err != nil {
		return "", err
	}

	raw, err := m.call(name, map[string]any{
		"owner": owner,
		"repo":  repo,
		"title": title,
		"head":  head,
		"base":  base,
		"body":  body,
	})
	if err != nil {
		return "", err
	}
	if isErrorResult(raw) {
		return "", fmt.Errorf("создание PR: %s", truncate(raw, 300))
	}
	return parsePullRequestURL(raw), nil
}

// --- Разбор ответов ---

var (
	jiraKeyPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)
	prURLPattern   = regexp.MustCompile(`https?://[^\s")']+/pull/\d+`)
)

// issueJSON — подмножество полей issue Sentry, которое нам нужно.
type issueJSON struct {
	ID        string `json:"id"`
	ShortID   string `json:"shortId"`
	Title     string `json:"title"`
	Culprit   string `json:"culprit"`
	Level     string `json:"level"`
	Count     int    `json:"count"`
	FirstSeen string `json:"firstSeen"`
	LastSeen  string `json:"lastSeen"`
	Permalink string `json:"permalink"`
	Metadata  struct {
		Type     string `json:"type"`
		Value    string `json:"value"`
		Filename string `json:"filename"`
		Function string `json:"function"`
	} `json:"metadata"`
}

// parseIssues извлекает список issue из текстового результата MCP-инструмента.
func parseIssues(raw string) ([]SentryIssue, error) {
	obj := extractJSON(raw)
	if obj == nil {
		return nil, fmt.Errorf("не удалось найти JSON в ответе Sentry: %s", truncate(raw, 300))
	}

	items := extractIssueArray(obj)
	if items == nil {
		return nil, fmt.Errorf("неожиданный формат ответа Sentry: %s", truncate(string(obj), 300))
	}

	issues := make([]SentryIssue, 0, len(items))
	for _, it := range items {
		var ij issueJSON
		if err := json.Unmarshal(it, &ij); err != nil {
			continue
		}
		id := ij.ID
		if id == "" {
			id = ij.ShortID
		}
		if id == "" {
			continue
		}

		title := ij.Title
		if title == "" {
			title = ij.Metadata.Value
		}
		culprit := ij.Culprit
		if culprit == "" {
			culprit = strings.TrimSpace(ij.Metadata.Filename + " " + ij.Metadata.Function)
		}

		issues = append(issues, SentryIssue{
			ID:         id,
			Title:      title,
			Culprit:    culprit,
			Level:      ij.Level,
			Count:      ij.Count,
			FirstSeen:  ij.FirstSeen,
			LastSeen:   ij.LastSeen,
			Permalink:  ij.Permalink,
			StackTrace: ij.Metadata.Value,
			Raw:        string(it),
		})
	}
	return issues, nil
}

// extractIssueArray достаёт массив issue из JSON-объекта или массива.
func extractIssueArray(obj json.RawMessage) []json.RawMessage {
	var arr []json.RawMessage
	if err := json.Unmarshal(obj, &arr); err == nil {
		return arr
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(obj, &wrapper); err != nil {
		return nil
	}
	for _, key := range []string{"issues", "data", "results", "items"} {
		if raw, ok := wrapper[key]; ok {
			var inner []json.RawMessage
			if err := json.Unmarshal(raw, &inner); err == nil {
				return inner
			}
		}
	}
	return nil
}

// parseJiraKey извлекает ключ Jira (например, PROJ-123) из ответа.
func parseJiraKey(raw string) string {
	return jiraKeyPattern.FindString(raw)
}

// parseFileContent разбирает ответ get_file_contents: текст файла и sha.
func parseFileContent(raw string) (content, sha string, err error) {
	obj := extractJSON(raw)
	if obj == nil {
		// Инструмент мог вернуть текст файла напрямую.
		return raw, "", nil
	}

	var data struct {
		Content  string `json:"content"`
		SHA      string `json:"sha"`
		Text     string `json:"text"`
		Decoded  string `json:"decoded_content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(obj, &data); err != nil {
		return raw, "", nil
	}

	switch {
	case data.Decoded != "":
		content = data.Decoded
	case data.Text != "":
		content = data.Text
	case data.Content != "":
		content = decodeContent(data.Content, data.Encoding)
	default:
		content = raw
	}
	return content, data.SHA, nil
}

// decodeContent декодирует base64, если контент выглядит как base64.
func decodeContent(content, encoding string) string {
	if encoding != "" && !strings.EqualFold(encoding, "base64") {
		return content
	}
	if decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content, "\n", "")); err == nil {
		if str := string(decoded); isMostlyText(str) {
			return str
		}
	}
	return content
}

// isMostlyText грубо проверяет, что строка похожа на текст.
func isMostlyText(s string) bool {
	if s == "" {
		return false
	}
	bad := 0
	for _, r := range s {
		if r == 0 {
			return false
		}
		if r < 0x09 {
			bad++
		}
	}
	return bad*10 < len([]rune(s))
}

// parsePullRequestURL извлекает ссылку на созданный PR.
func parsePullRequestURL(raw string) string {
	if m := prURLPattern.FindString(raw); m != "" {
		return m
	}

	obj := extractJSON(raw)
	if obj == nil {
		return ""
	}
	var data struct {
		HTMLURL string `json:"html_url"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(obj, &data); err == nil {
		if data.HTMLURL != "" {
			return data.HTMLURL
		}
		return data.URL
	}
	return ""
}

// isErrorResult грубо определяет, что текстовый результат инструмента — ошибка.
func isErrorResult(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(lower, "ошибка") {
		return true
	}
	if strings.Contains(lower, "error") && !strings.Contains(lower, `"error":null`) &&
		!strings.Contains(lower, `"errors":[]`) {
		// Не считаем ошибкой обычные ответы; эвристика только для явных маркеров.
		if strings.Contains(lower, "not found") || strings.Contains(lower, "failed") ||
			strings.Contains(lower, "unprocessable") || strings.Contains(lower, "validation failed") {
			return true
		}
	}
	return false
}

// extractJSON находит первый корректный JSON-объект или массив в тексте.
func extractJSON(raw string) json.RawMessage {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}

	// Убираем markdown-обёртку ```json ... ```.
	if idx := strings.Index(s, "```"); idx >= 0 {
		rest := s[idx+3:]
		rest = strings.TrimPrefix(rest, "json")
		if end := strings.Index(rest, "```"); end >= 0 {
			s = strings.TrimSpace(rest[:end])
		} else {
			s = strings.TrimSpace(rest)
		}
	}

	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return nil
	}

	dec := json.NewDecoder(strings.NewReader(s[start:]))
	var v json.RawMessage
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	return v
}

// truncate усекает строку для логов.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
