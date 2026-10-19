package agent

import (
	"strings"
	"testing"
)

func TestCompleteSingleShotWithRAG(t *testing.T) {
	client := &capturingClient{}
	ret := &fakeRetriever{chunks: []RetrievedChunk{{Path: "main.go", Text: "уникальный_фрагмент_xyz"}}}
	cfg := Config{
		APIFormat:     "openai",
		Model:         "test",
		RAGEnabled:    true,
		RAGStrategy:   "structure",
		RAGTopK:       3,
		RAGCandidates: 5,
	}
	a := NewSimpleAgent(cfg, client).WithKnowledge(ret)

	resp, err := a.Complete(AgentRequest{Message: "почему падает", ProjectID: "repo"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if len(client.payloads) != 1 {
		t.Fatalf("LLM calls = %d, want 1", len(client.payloads))
	}
	payload := string(client.payloads[0])
	if !strings.Contains(payload, "уникальный_фрагмент_xyz") {
		t.Fatalf("payload does not contain RAG chunk:\n%s", payload)
	}
	if strings.Contains(payload, `"tools"`) {
		t.Fatalf("Complete must not send tools:\n%s", payload)
	}
	if !resp.RAGUsed || len(resp.Sources) != 1 {
		t.Fatalf("RAGUsed=%t sources=%d", resp.RAGUsed, len(resp.Sources))
	}
}

func TestCompleteRejectsEmptyMessage(t *testing.T) {
	client := &capturingClient{}
	a := NewSimpleAgent(Config{APIFormat: "openai", Model: "test"}, client)

	if _, err := a.Complete(AgentRequest{Message: "  "}); err == nil {
		t.Fatal("expected error for empty message")
	}
	if len(client.payloads) != 0 {
		t.Fatalf("LLM must not be called, got %d calls", len(client.payloads))
	}
}
