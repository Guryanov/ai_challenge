package pipeline

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseIssuesWrapper(t *testing.T) {
	raw := "Вот результат:\n```json\n{\"issues\":[{\"id\":\"1\",\"title\":\"panic\",\"culprit\":\"a.go\",\"level\":\"fatal\"}]}\n```"
	issues, err := parseIssues(raw)
	if err != nil {
		t.Fatalf("parseIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].ID != "1" || issues[0].Title != "panic" {
		t.Fatalf("unexpected issues: %+v", issues)
	}
}

func TestParseIssuesArray(t *testing.T) {
	raw := `[{"id":"a","title":"t1"},{"id":"b","title":"t2"}]`
	issues, err := parseIssues(raw)
	if err != nil {
		t.Fatalf("parseIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("len = %d, want 2", len(issues))
	}
}

func TestParseJiraKey(t *testing.T) {
	for _, raw := range []string{
		`{"key":"PROJ-42"}`,
		"Создан тикет PROJ-42 успешно",
	} {
		if got := parseJiraKey(raw); got != "PROJ-42" {
			t.Fatalf("parseJiraKey(%q) = %q", raw, got)
		}
	}
	if got := parseJiraKey("no key"); got != "" {
		t.Fatalf("expected empty key, got %q", got)
	}
}

func TestParseFileContentBase64(t *testing.T) {
	content := "package main\n\nfunc main() {}\n"
	raw := `{"content":"` + base64.StdEncoding.EncodeToString([]byte(content)) + `","sha":"deadbeef"}`
	got, sha, err := parseFileContent(raw)
	if err != nil {
		t.Fatalf("parseFileContent: %v", err)
	}
	if got != content {
		t.Fatalf("content = %q, want %q", got, content)
	}
	if sha != "deadbeef" {
		t.Fatalf("sha = %q", sha)
	}
}

func TestParsePullRequestURL(t *testing.T) {
	raw := `{"html_url":"https://github.com/o/r/pull/9"}`
	if got := parsePullRequestURL(raw); got != "https://github.com/o/r/pull/9" {
		t.Fatalf("pr url = %q", got)
	}
	if got := parsePullRequestURL("created https://github.com/o/r/pull/9 ok"); got != "https://github.com/o/r/pull/9" {
		t.Fatalf("pr url (text) = %q", got)
	}
}

func TestExtractJSONFence(t *testing.T) {
	raw := "some text\n```json\n{\"a\":1}\n```\ntail"
	obj := extractJSON(raw)
	if string(obj) != `{"a":1}` {
		t.Fatalf("extractJSON = %q", string(obj))
	}
}

func TestParseFixPlanFiltersEmpty(t *testing.T) {
	raw := `{"files":[{"path":"a.go","content":"x"},{"path":"","content":"y"}],"summary":"s"}`
	plan, err := parseFixPlan(raw)
	if err != nil {
		t.Fatalf("parseFixPlan: %v", err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Path != "a.go" {
		t.Fatalf("plan = %+v", plan)
	}
	if !strings.Contains(plan.Summary, "s") {
		t.Fatalf("summary = %q", plan.Summary)
	}
}
