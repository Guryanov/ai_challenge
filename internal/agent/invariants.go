package agent

import (
	"fmt"
	"strings"

	"ai-chat/internal/memory"
)

// formatInvariantsContext формирует системное сообщение с активными инвариантами проекта.
// Если активных инвариантов нет, возвращает пустую строку.
func formatInvariantsContext(invariants []memory.Invariant) string {
	if len(invariants) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Инварианты проекта (жёсткие правила, которые нельзя нарушать):\n\n")
	for i, inv := range invariants {
		b.WriteString(fmt.Sprintf("%d. [%s] [%s] %s", i+1, invariantCategoryName(inv.Category), inv.Severity, inv.Title))
		if strings.TrimSpace(inv.Description) != "" {
			b.WriteString(fmt.Sprintf("\n   %s", strings.TrimSpace(inv.Description)))
		}
		b.WriteString("\n")
	}

	b.WriteString("\nПеред каждым техническим, архитектурным или бизнес-решением явно проверь его на соответствие инвариантам. ")
	b.WriteString("Если предлагаемое решение нарушает hard-инвариант, откажись от него и объясни, какой именно инвариант нарушается. ")
	b.WriteString("Если решение нарушает soft-инвариант, укажи на это предупреждением, но решение может быть допустимо при согласовании.")

	return strings.TrimSpace(b.String())
}

// formatInvariantVerificationBlock формирует блок инструкций для этапа verification workflow.
func formatInvariantVerificationBlock(invariants []memory.Invariant) string {
	if len(invariants) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Проверь результат выполнения на соответствие следующим инвариантам проекта:\n")
	for _, inv := range invariants {
		b.WriteString(fmt.Sprintf("- [%s] %s (%s): %s\n", inv.Severity, inv.Title, invariantCategoryName(inv.Category), inv.Description))
	}
	b.WriteString("\nДля каждого инварианта укажи, нарушен ли он. ")
	b.WriteString("Если найдено нарушение hard-инварианта, начни ответ со строки \"Отказ:\" и опиши нарушение. ")
	b.WriteString("Если нарушений hard-инвариантов нет, напиши \"Проверка пройдена\" и краткое резюме по soft-инвариантам.")

	return strings.TrimSpace(b.String())
}

func invariantCategoryName(category string) string {
	switch category {
	case "architecture":
		return "архитектура"
	case "technical_decision":
		return "техническое решение"
	case "stack":
		return "стек"
	case "business_rule":
		return "бизнес-правило"
	default:
		return category
	}
}
