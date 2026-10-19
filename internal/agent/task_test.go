package agent

import (
	"strings"
	"testing"

	"ai-chat/internal/history"
)

func TestAdvanceTaskStage(t *testing.T) {
	cases := []struct {
		current  string
		expected string
	}{
		{TaskStagePlanning, TaskStageExecution},
		{TaskStageExecution, TaskStageVerification},
		{TaskStageVerification, TaskStageCompletion},
		{TaskStageCompletion, TaskStageCompletion},
	}
	for _, tc := range cases {
		got := advanceTaskStage(tc.current)
		if got != tc.expected {
			t.Errorf("advanceTaskStage(%q) = %q, want %q", tc.current, got, tc.expected)
		}
	}
}

func TestResetTaskState(t *testing.T) {
	session := history.Session{TaskStage: TaskStageExecution, TaskStatus: TaskStatusApproved}
	resetTaskState(&session, "Implement auth")
	if session.TaskStage != TaskStagePlanning {
		t.Fatalf("expected stage planning, got %q", session.TaskStage)
	}
	if session.TaskStatus != TaskStatusPending {
		t.Fatalf("expected status pending, got %q", session.TaskStatus)
	}
	if session.TaskContext.OriginalRequest != "Implement auth" {
		t.Fatalf("expected original request, got %q", session.TaskContext.OriginalRequest)
	}
}

func TestApplyTaskActionApprove(t *testing.T) {
	session := history.Session{TaskStage: TaskStagePlanning, TaskStatus: TaskStatusPending}
	if err := applyTaskAction(&session, TaskActionApprove, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.TaskStage != TaskStageExecution {
		t.Fatalf("expected stage execution, got %q", session.TaskStage)
	}
	if session.TaskStatus != TaskStatusPending {
		t.Fatalf("expected status pending, got %q", session.TaskStatus)
	}
}

func TestApplyTaskActionApproveCompletion(t *testing.T) {
	session := history.Session{TaskStage: TaskStageCompletion, TaskStatus: TaskStatusPending}
	if err := applyTaskAction(&session, TaskActionApprove, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.TaskStage != TaskStageCompletion {
		t.Fatalf("expected stage completion, got %q", session.TaskStage)
	}
	if session.TaskStatus != TaskStatusApproved {
		t.Fatalf("expected status approved, got %q", session.TaskStatus)
	}
}

func TestApplyTaskActionReject(t *testing.T) {
	session := history.Session{TaskStage: TaskStageExecution, TaskStatus: TaskStatusPending}
	if err := applyTaskAction(&session, TaskActionReject, "Need more details"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.TaskStage != TaskStageExecution {
		t.Fatalf("expected stage to stay execution, got %q", session.TaskStage)
	}
	if session.TaskStatus != TaskStatusRejected {
		t.Fatalf("expected status rejected, got %q", session.TaskStatus)
	}
	if session.TaskContext.RejectionReason != "Need more details" {
		t.Fatalf("expected rejection reason, got %q", session.TaskContext.RejectionReason)
	}
}

func TestApplyTaskActionInvalid(t *testing.T) {
	session := history.Session{TaskStage: TaskStagePlanning, TaskStatus: TaskStatusPending}
	if err := applyTaskAction(&session, "unknown", ""); err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestUpdateTaskContext(t *testing.T) {
	session := history.Session{TaskStage: TaskStagePlanning}
	updateTaskContext(&session, "Plan content")
	if session.TaskContext.Plan != "Plan content" {
		t.Fatalf("expected plan content, got %q", session.TaskContext.Plan)
	}

	session.TaskStage = TaskStageExecution
	updateTaskContext(&session, "Execution content")
	if session.TaskContext.ExecutionResult != "Execution content" {
		t.Fatalf("expected execution content, got %q", session.TaskContext.ExecutionResult)
	}
}

func TestFormatWorkflowContext(t *testing.T) {
	session := history.Session{
		TaskStage:  TaskStagePlanning,
		TaskStatus: TaskStatusPending,
	}

	ctx := formatWorkflowContext(session)
	if ctx == "" {
		t.Fatal("expected non-empty workflow context")
	}

	expectedParts := []string{
		"многоэтапного workflow",
		"4 этапа",
		"Планирование",
		"Выполнение",
		"Проверка",
		"Завершение",
		"Переход с текущего этапа на следующий возможен только после явного утверждения пользователя",
		"Текущий этап: Планирование",
		"(planning",
		"pending",
	}
	for _, part := range expectedParts {
		if !strings.Contains(ctx, part) {
			t.Errorf("expected workflow context to contain %q, got:\n%s", part, ctx)
		}
	}
}

func TestFormatWorkflowContextEmptyOutsideWorkflow(t *testing.T) {
	session := history.Session{}
	if ctx := formatWorkflowContext(session); ctx != "" {
		t.Fatalf("expected empty context outside workflow, got %q", ctx)
	}
}
