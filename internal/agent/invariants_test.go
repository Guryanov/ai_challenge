package agent

import (
	"strings"
	"testing"

	"ai-chat/internal/memory"
)

func TestFormatInvariantsContext(t *testing.T) {
	invariants := []memory.Invariant{
		{Title: "PostgreSQL", Description: "Только PostgreSQL", Category: "stack", Severity: "hard", Active: true},
		{Title: "Микросервисы", Category: "architecture", Severity: "hard", Active: true},
	}

	ctx := formatInvariantsContext(invariants)
	if ctx == "" {
		t.Fatal("expected non-empty context")
	}

	expectedParts := []string{
		"Инварианты проекта",
		"[стек] [hard] PostgreSQL",
		"[архитектура] [hard] Микросервисы",
		"откажись от него",
	}
	for _, part := range expectedParts {
		if !strings.Contains(ctx, part) {
			t.Errorf("expected context to contain %q, got:\n%s", part, ctx)
		}
	}
}

func TestFormatInvariantsContextEmpty(t *testing.T) {
	ctx := formatInvariantsContext(nil)
	if ctx != "" {
		t.Fatalf("expected empty context, got %q", ctx)
	}

	ctx = formatInvariantsContext([]memory.Invariant{})
	if ctx != "" {
		t.Fatalf("expected empty context, got %q", ctx)
	}
}

func TestFormatInvariantVerificationBlock(t *testing.T) {
	invariants := []memory.Invariant{
		{Title: "No MongoDB", Description: "Запрещено использовать MongoDB", Category: "stack", Severity: "hard", Active: true},
	}

	block := formatInvariantVerificationBlock(invariants)
	if block == "" {
		t.Fatal("expected non-empty block")
	}

	if !strings.Contains(block, "Проверь результат выполнения") {
		t.Errorf("expected verification header, got:\n%s", block)
	}
	if !strings.Contains(block, "Отказ:") {
		t.Errorf("expected refusal marker, got:\n%s", block)
	}
	if !strings.Contains(block, "No MongoDB") {
		t.Errorf("expected invariant title, got:\n%s", block)
	}
}

func TestFormatInvariantVerificationBlockEmpty(t *testing.T) {
	block := formatInvariantVerificationBlock(nil)
	if block != "" {
		t.Fatalf("expected empty block, got %q", block)
	}
}

func TestInvariantCategoryName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"architecture", "архитектура"},
		{"technical_decision", "техническое решение"},
		{"stack", "стек"},
		{"business_rule", "бизнес-правило"},
		{"unknown", "unknown"},
	}
	for _, tc := range cases {
		got := invariantCategoryName(tc.input)
		if got != tc.expected {
			t.Errorf("invariantCategoryName(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
