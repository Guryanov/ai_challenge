package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildTicketPrompt формирует запрос к LLM для текста Jira-тикета.
func buildTicketPrompt(issue SentryIssue) string {
	var b strings.Builder
	b.WriteString("Ты пишешь задачу в Jira на основе критического падения (краша) из Sentry.\n\n")
	b.WriteString(formatIssue(issue))
	b.WriteString("\n\nВерни строго JSON без markdown-обёртки:\n")
	b.WriteString(`{"summary": "краткий заголовок до 120 символов", "description": "описание на русском: что упало, где, вероятная причина, ссылка на Sentry"}`)
	return b.String()
}

// buildFixPrompt формирует запрос к LLM для генерации правок кода.
func buildFixPrompt(issue SentryIssue, files []repoFile) string {
	var b strings.Builder
	b.WriteString("Ты — Go-инженер. Нужно исправить баг по критическому падению из Sentry. ")
	b.WriteString("Ниже — описание issue и текущее содержимое файлов проекта, вероятно связанных с падением.\n\n")
	b.WriteString(formatIssue(issue))
	b.WriteString("\n\nФайлы проекта:\n")
	if len(files) == 0 {
		b.WriteString("(файлы не найдены — верни пустой список files)\n")
	}
	for _, f := range files {
		b.WriteString(fmt.Sprintf("\n--- FILE: %s ---\n%s\n--- END FILE ---\n", f.Path, f.Content))
	}
	b.WriteString("\nВерни строго JSON без markdown-обёртки, заменив файлы ЦЕЛИКОМ (полный новый текст файла):\n")
	b.WriteString(`{"files": [{"path": "путь/как в репозитории", "content": "полный новый текст файла"}], "summary": "кратко: причина и что изменено"}`)
	b.WriteString("\n\nПравила: меняй только необходимые файлы; сохраняй стиль и форматирование; не добавляй файлы, которых нет в списке выше; ")
	b.WriteString("если уверенного исправления нет — верни пустой массив files.")
	return b.String()
}

// buildPRBody формирует тело pull request.
func buildPRBody(issue SentryIssue, jiraKey string, plan FixPlan) string {
	var b strings.Builder
	b.WriteString("Автоматически создано конвейером Sentry → Jira.\n\n")
	b.WriteString(fmt.Sprintf("Jira: %s\n", jiraKey))
	if issue.ID != "" {
		b.WriteString(fmt.Sprintf("Sentry issue: %s\n", issue.ID))
	}
	if issue.Permalink != "" {
		b.WriteString(fmt.Sprintf("Sentry: %s\n", issue.Permalink))
	}
	if issue.Title != "" {
		b.WriteString(fmt.Sprintf("\nПричина (issue): %s\n", issue.Title))
	}
	if plan.Summary != "" {
		b.WriteString(fmt.Sprintf("\nИсправление: %s\n", plan.Summary))
	}
	if len(plan.Files) > 0 {
		paths := make([]string, 0, len(plan.Files))
		for _, f := range plan.Files {
			paths = append(paths, "- "+f.Path)
		}
		b.WriteString("\nИзменённые файлы:\n" + strings.Join(paths, "\n") + "\n")
	}
	b.WriteString("\n> Требуется ревью человеком. PR не мержится автоматически.")
	return b.String()
}

// formatIssue форматирует данные Sentry issue для промпта.
func formatIssue(issue SentryIssue) string {
	var b strings.Builder
	b.WriteString("Sentry issue:\n")
	appendField(&b, "id", issue.ID)
	appendField(&b, "title", issue.Title)
	appendField(&b, "culprit", issue.Culprit)
	appendField(&b, "level", issue.Level)
	if issue.Count > 0 {
		b.WriteString(fmt.Sprintf("- events: %d\n", issue.Count))
	}
	appendField(&b, "firstSeen", issue.FirstSeen)
	appendField(&b, "lastSeen", issue.LastSeen)
	appendField(&b, "permalink", issue.Permalink)
	if issue.StackTrace != "" {
		appendField(&b, "details", issue.StackTrace)
	}
	if issue.Raw != "" {
		b.WriteString("\nСырые данные issue (JSON):\n")
		b.WriteString(truncate(issue.Raw, 4000))
	}
	return strings.TrimSpace(b.String())
}

func appendField(b *strings.Builder, name, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString(fmt.Sprintf("- %s: %s\n", name, value))
}

// parseTicketText разбирает JSON-ответ LLM с текстом тикета.
func parseTicketText(content string, issue SentryIssue) (summary, description string) {
	obj := extractJSON(content)
	if obj != nil {
		var data struct {
			Summary     string `json:"summary"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(obj, &data); err == nil {
			summary = strings.TrimSpace(data.Summary)
			description = strings.TrimSpace(data.Description)
		}
	}

	if summary == "" {
		summary = strings.TrimSpace(issue.Title)
	}
	if summary == "" {
		summary = "Критическое падение " + issue.ID
	}
	if description == "" {
		description = strings.TrimSpace(content)
	}
	if description == "" {
		description = formatIssue(issue)
	}
	return summary, description
}

// parseFixPlan разбирает JSON-ответ LLM с планом правок.
func parseFixPlan(content string) (FixPlan, error) {
	obj := extractJSON(content)
	if obj == nil {
		return FixPlan{}, fmt.Errorf("в ответе модели не найден JSON")
	}

	var plan FixPlan
	if err := json.Unmarshal(obj, &plan); err != nil {
		return FixPlan{}, fmt.Errorf("не удалось разобрать план правок: %w", err)
	}

	valid := make([]FileEdit, 0, len(plan.Files))
	for _, f := range plan.Files {
		if strings.TrimSpace(f.Path) == "" || f.Content == "" {
			continue
		}
		valid = append(valid, f)
	}
	plan.Files = valid
	return plan, nil
}
