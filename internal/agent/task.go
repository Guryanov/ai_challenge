package agent

import (
	"fmt"
	"strings"

	"ai-chat/internal/history"
	"ai-chat/internal/memory"
)

// Стадии конечного автомата задачи.
const (
	TaskStagePlanning     = "planning"
	TaskStageExecution    = "execution"
	TaskStageVerification = "verification"
	TaskStageCompletion   = "completion"
)

// Статусы задачи.
const (
	TaskStatusPending  = "pending"
	TaskStatusApproved = "approved"
	TaskStatusRejected = "rejected"
)

// taskAction — действия пользователя над текущим этапом.
const (
	TaskActionApprove = "approve"
	TaskActionReject  = "reject"
)

// taskStageResult — результат генерации этапа.
type taskStageResult struct {
	content string
	stage   string
}

// advanceTaskStage переводит задачу на следующий этап после утверждения.
func advanceTaskStage(stage string) string {
	switch stage {
	case TaskStagePlanning:
		return TaskStageExecution
	case TaskStageExecution:
		return TaskStageVerification
	case TaskStageVerification:
		return TaskStageCompletion
	default:
		return TaskStageCompletion
	}
}

// resetTaskState сбрасывает состояние задачи для нового запроса.
func resetTaskState(session *history.Session, originalRequest string) {
	session.TaskStage = TaskStagePlanning
	session.TaskStatus = TaskStatusPending
	session.TaskContext = history.TaskContext{OriginalRequest: originalRequest}
}

// clearTaskState очищает состояние задачи (используется в режиме chat).
func clearTaskState(session *history.Session) {
	session.TaskStage = ""
	session.TaskStatus = ""
	session.TaskContext = history.TaskContext{}
}

// applyTaskAction применяет действие пользователя (утверждение/отклонение).
func applyTaskAction(session *history.Session, action, rejectionReason string) error {
	switch action {
	case TaskActionApprove:
		if session.TaskStatus != TaskStatusPending {
			return fmt.Errorf("текущий этап не ожидает утверждения")
		}
		if session.TaskStage == TaskStageCompletion {
			session.TaskStatus = TaskStatusApproved
		} else {
			session.TaskStage = advanceTaskStage(session.TaskStage)
			session.TaskStatus = TaskStatusPending
			session.TaskContext.RejectionReason = ""
		}
	case TaskActionReject:
		if session.TaskStatus != TaskStatusPending {
			return fmt.Errorf("текущий этап не ожидает отклонения")
		}
		session.TaskStatus = TaskStatusRejected
		session.TaskContext.RejectionReason = strings.TrimSpace(rejectionReason)
	default:
		return fmt.Errorf("неизвестное действие: %s", action)
	}
	return nil
}

// buildTaskStagePrompt формирует промпт для текущего этапа задачи.
func buildTaskStagePrompt(session history.Session, projectContext, userProfileContext string, invariants []memory.Invariant) string {
	ctx := session.TaskContext
	stage := session.TaskStage
	rejected := session.TaskStatus == TaskStatusRejected

	var b strings.Builder

	b.WriteString(fmt.Sprintf("Текущий этап: %s.\n\n", stageName(stage)))

	if ctx.OriginalRequest != "" {
		b.WriteString(fmt.Sprintf("Исходный запрос пользователя:\n%s\n\n", ctx.OriginalRequest))
	}

	if stage != TaskStagePlanning && ctx.Plan != "" {
		b.WriteString(fmt.Sprintf("Утверждённый план:\n%s\n\n", ctx.Plan))
	}
	if stage == TaskStageVerification || stage == TaskStageCompletion {
		if ctx.ExecutionResult != "" {
			b.WriteString(fmt.Sprintf("Результат выполнения:\n%s\n\n", ctx.ExecutionResult))
		}
	}
	if stage == TaskStageCompletion && ctx.VerificationReport != "" {
		b.WriteString(fmt.Sprintf("Отчёт проверки:\n%s\n\n", ctx.VerificationReport))
	}

	if projectContext != "" {
		b.WriteString(fmt.Sprintf("Контекст проекта:\n%s\n\n", projectContext))
	}
	if userProfileContext != "" {
		b.WriteString(fmt.Sprintf("%s\n\n", userProfileContext))
	}

	if rejected && ctx.RejectionReason != "" {
		b.WriteString(fmt.Sprintf("Предыдущая попытка была отклонена по причине:\n%s\n\n", ctx.RejectionReason))
	}

	switch stage {
	case TaskStagePlanning:
		b.WriteString("Составь подробный план решения задачи. Перечисли шаги и укажи, какие технологии/паттерны использовать. " +
			"Перед каждым шагом проверь его на соответствие инвариантам проекта. Ответь одним сплошным текстом.")
	case TaskStageExecution:
		b.WriteString("Выполни утверждённый план. Предоставь конкретный результат: код, текст, инструкции или другое решение в зависимости от задачи. " +
			"Учитывай инварианты проекта; если решение нарушает hard-инвариант, откажись от него и объясни причину.")
	case TaskStageVerification:
		verificationBlock := formatInvariantVerificationBlock(invariants)
		if verificationBlock != "" {
			b.WriteString(verificationBlock)
		} else {
			b.WriteString("Проверь результат выполнения на соответствие правилам, технологиям и принципам проекта, а также профилю пользователя. " +
				"Если есть нарушения, опиши их в начале ответа как 'Проблема: ...'. Если всё корректно, напиши 'Проверка пройдена' и краткое резюме.")
		}
	case TaskStageCompletion:
		b.WriteString("Подготовь итоговое резюме выполненной задачи. Кратко опиши, что было сделано, и укажи ключевые результаты.")
	}

	return strings.TrimSpace(b.String())
}

// formatWorkflowContext формирует системное сообщение с правилами workflow.
// Возвращает пустую строку, если сессия не находится в workflow.
func formatWorkflowContext(session history.Session) string {
	if session.TaskStage == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("Ты работаешь в режиме многоэтапного workflow. Задача проходит строго 4 этапа:\n" +
		"1. Планирование (planning) — составление плана решения.\n" +
		"2. Выполнение (execution) — реализация утверждённого плана.\n" +
		"3. Проверка (verification) — проверка результата на соответствие правилам, инвариантам и профилю пользователя.\n" +
		"4. Завершение (completion) — итоговое резюме выполненной задачи.\n\n" +
		"Правила:\n" +
		"- Переход с текущего этапа на следующий возможен только после явного утверждения пользователя.\n" +
		"- Не перескакивай через этапы и не выполняй работу следующего этапа раньше времени.\n" +
		"- Если этап отклонён, переделай результат текущего этапа с учётом причины отклонения.\n\n")
	b.WriteString(fmt.Sprintf("Текущий этап: %s (%s, статус: %s).", stageName(session.TaskStage), session.TaskStage, session.TaskStatus))

	return strings.TrimSpace(b.String())
}

// stageName возвращает человекочитаемое название этапа.
func stageName(stage string) string {
	switch stage {
	case TaskStagePlanning:
		return "Планирование"
	case TaskStageExecution:
		return "Выполнение"
	case TaskStageVerification:
		return "Проверка"
	case TaskStageCompletion:
		return "Завершение"
	default:
		return stage
	}
}

// updateTaskContext сохраняет результат текущего этапа.
func updateTaskContext(session *history.Session, content string) {
	switch session.TaskStage {
	case TaskStagePlanning:
		session.TaskContext.Plan = content
	case TaskStageExecution:
		session.TaskContext.ExecutionResult = content
	case TaskStageVerification:
		session.TaskContext.VerificationReport = content
	case TaskStageCompletion:
		session.TaskContext.CompletionSummary = content
	}
}

// formatTaskContextForHistory форматирует состояние задачи для отображения в истории.
func formatTaskContextForHistory(session history.Session) string {
	ctx := session.TaskContext
	var b strings.Builder
	b.WriteString(fmt.Sprintf("**Этап: %s**\n\n", stageName(session.TaskStage)))
	if ctx.Plan != "" {
		b.WriteString(fmt.Sprintf("**План:**\n%s\n\n", ctx.Plan))
	}
	if ctx.ExecutionResult != "" {
		b.WriteString(fmt.Sprintf("**Результат выполнения:**\n%s\n\n", ctx.ExecutionResult))
	}
	if ctx.VerificationReport != "" {
		b.WriteString(fmt.Sprintf("**Проверка:**\n%s\n\n", ctx.VerificationReport))
	}
	if ctx.CompletionSummary != "" {
		b.WriteString(fmt.Sprintf("**Итог:**\n%s\n\n", ctx.CompletionSummary))
	}
	return strings.TrimSpace(b.String())
}
