package digest

import (
	"errors"
	"testing"
	"time"

	"ai-chat/internal/agent"
)

// fakeAgent — мок агента для тестирования генерации сводки.
type fakeAgent struct {
	calls    []agent.AgentRequest
	response agent.AgentResponse
	err      error
}

func (f *fakeAgent) Run(req agent.AgentRequest) (agent.AgentResponse, error) {
	f.calls = append(f.calls, req)
	return f.response, f.err
}

func TestGenerate_SavesContent(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	runner := &fakeAgent{
		response: agent.AgentResponse{Content: "Top movie: Test Movie"},
	}

	if err := generate(runner, store, "prompt"); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(runner.calls))
	}
	req := runner.calls[0]
	if req.Message != "prompt" {
		t.Errorf("message mismatch: got %q, want %q", req.Message, "prompt")
	}
	if req.WorkflowMode != agent.WorkflowModeChat {
		t.Errorf("workflow mode mismatch: got %q, want %q", req.WorkflowMode, agent.WorkflowModeChat)
	}
	if req.SessionID != "" {
		t.Errorf("expected empty session_id, got %q", req.SessionID)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Content != "Top movie: Test Movie" {
		t.Errorf("content mismatch: got %q", loaded.Content)
	}
	if loaded.GeneratedAt.IsZero() {
		t.Errorf("expected generated_at to be set")
	}
}

func TestGenerate_SavesError(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	runner := &fakeAgent{
		err: errors.New("llm unavailable"),
	}

	if err := generate(runner, store, "prompt"); err == nil {
		t.Fatalf("expected error from generate")
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Error != "llm unavailable" {
		t.Errorf("error mismatch: got %q, want %q", loaded.Error, "llm unavailable")
	}
	if loaded.Content != "" {
		t.Errorf("expected empty content on error, got %q", loaded.Content)
	}
}

func TestTimeUntil(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 9, 27, 10, 30, 0, 0, loc)

	d, err := timeUntil("09:00", now)
	if err != nil {
		t.Fatalf("timeUntil failed: %v", err)
	}
	expected := 22*time.Hour + 30*time.Minute
	if d != expected {
		t.Errorf("duration mismatch: got %v, want %v", d, expected)
	}

	d, err = timeUntil("11:00", now)
	if err != nil {
		t.Fatalf("timeUntil failed: %v", err)
	}
	expected = 30 * time.Minute
	if d != expected {
		t.Errorf("duration mismatch: got %v, want %v", d, expected)
	}
}

func TestTimeUntil_InvalidFormat(t *testing.T) {
	if _, err := timeUntil("not-a-time", time.Now()); err == nil {
		t.Fatalf("expected error for invalid time format")
	}
}
