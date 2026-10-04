package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type capturingClient struct {
	payloads [][]byte
}

func (c *capturingClient) Call(payload []byte) ([]byte, error) {
	c.payloads = append(c.payloads, append([]byte(nil), payload...))
	return json.Marshal(map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]string{
					"content": "ok",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     1,
			"completion_tokens": 1,
			"total_tokens":      2,
		},
	})
}

type fakeRetriever struct {
	calls  int
	chunks []RetrievedChunk
	err    error
}

func (f *fakeRetriever) Retrieve(_ context.Context, _, _, _ string, _ int) ([]RetrievedChunk, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.chunks, nil
}

func boolPtr(v bool) *bool { return &v }

func TestRunInjectsKnowledgeContext(t *testing.T) {
	client := &capturingClient{}
	retriever := &fakeRetriever{chunks: []RetrievedChunk{
		{Path: "docs/guide.md", Section: "Введение", Text: "Уникальный факт из базы знаний"},
	}}
	cfg := Config{APIFormat: "openai", Model: "test", RAGEnabled: true, RAGStrategy: "structure", RAGTopK: 3}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	if _, err := a.Run(AgentRequest{
		Message:    "что такое X?",
		ProjectID:  "p",
		SessionID:  "s1",
		RAGEnabled: boolPtr(true),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if retriever.calls != 1 {
		t.Fatalf("retriever called %d times, want 1", retriever.calls)
	}
	if len(client.payloads) == 0 {
		t.Fatal("no LLM payload captured")
	}
	payload := string(client.payloads[0])
	if !strings.Contains(payload, "Уникальный факт из базы знаний") {
		t.Fatalf("payload does not contain retrieved chunk:\n%s", payload)
	}
	if !strings.Contains(payload, "docs/guide.md") {
		t.Fatalf("payload does not contain chunk source:\n%s", payload)
	}
}

func TestRunSkipsKnowledgeWhenDisabled(t *testing.T) {
	client := &capturingClient{}
	retriever := &fakeRetriever{chunks: []RetrievedChunk{{Path: "x", Text: "секретный-фрагмент"}}}
	cfg := Config{APIFormat: "openai", Model: "test", RAGEnabled: true}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	if _, err := a.Run(AgentRequest{
		Message:    "привет",
		ProjectID:  "p",
		SessionID:  "s2",
		RAGEnabled: boolPtr(false),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if retriever.calls != 0 {
		t.Fatalf("retriever called %d times, want 0", retriever.calls)
	}
	if strings.Contains(string(client.payloads[0]), "секретный-фрагмент") {
		t.Fatal("payload must not contain chunks when RAG is disabled")
	}
}

func TestRunSkipsKnowledgeWithoutProject(t *testing.T) {
	client := &capturingClient{}
	retriever := &fakeRetriever{chunks: []RetrievedChunk{{Path: "x", Text: "фрагмент"}}}
	cfg := Config{APIFormat: "openai", Model: "test", RAGEnabled: true}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	if _, err := a.Run(AgentRequest{Message: "привет", SessionID: "s3"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if retriever.calls != 0 {
		t.Fatalf("retriever called %d times, want 0", retriever.calls)
	}
}

func TestRunRetrievalErrorDoesNotFailChat(t *testing.T) {
	client := &capturingClient{}
	retriever := &fakeRetriever{err: errors.New("embedder недоступен")}
	cfg := Config{APIFormat: "openai", Model: "test", RAGEnabled: true}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	if _, err := a.Run(AgentRequest{
		Message:    "вопрос",
		ProjectID:  "p",
		SessionID:  "s4",
		RAGEnabled: boolPtr(true),
	}); err != nil {
		t.Fatalf("Run should not fail when retrieval fails: %v", err)
	}
	if retriever.calls != 1 {
		t.Fatalf("retriever called %d times, want 1", retriever.calls)
	}
}

func TestFormatKnowledgeContext(t *testing.T) {
	if got := formatKnowledgeContext(nil); got != "" {
		t.Fatalf("expected empty context, got %q", got)
	}

	ctx := formatKnowledgeContext([]RetrievedChunk{
		{Path: "a.md", Section: "S1", Text: "текст один"},
		{Path: "b.md", Text: "текст два"},
	})
	for _, want := range []string{"базы знаний", "a.md", "S1", "текст один", "b.md", "текст два"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("expected context to contain %q, got:\n%s", want, ctx)
		}
	}
}
