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
	calls    int
	chunks   []RetrievedChunk
	err      error
	lastOpts RetrieveOptions
}

func (f *fakeRetriever) Retrieve(_ context.Context, _ string, opts RetrieveOptions) ([]RetrievedChunk, error) {
	f.calls++
	f.lastOpts = opts
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

func TestRunRefusesWhenNoRelevantChunks(t *testing.T) {
	client := &capturingClient{}
	retriever := &fakeRetriever{}
	cfg := Config{APIFormat: "openai", Model: "test", RAGEnabled: true, RAGThreshold: 0.35, RAGMode: "threshold"}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	resp, err := a.Run(AgentRequest{
		Message:    "вопрос",
		ProjectID:  "p",
		SessionID:  "s-empty",
		RAGEnabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.payloads) != 0 {
		t.Fatalf("LLM must not be called on low relevance, got %d calls", len(client.payloads))
	}
	if !strings.Contains(resp.Content, "Не знаю") {
		t.Fatalf("expected refusal message, got %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "Уточните") {
		t.Fatalf("expected clarification request, got %q", resp.Content)
	}
	if resp.RAGUsed || len(resp.Sources) != 0 {
		t.Fatalf("refusal must not carry sources: used=%v sources=%d", resp.RAGUsed, len(resp.Sources))
	}
}

func TestRunAddsSourcesAndCitations(t *testing.T) {
	client := &capturingClient{}
	retriever := &fakeRetriever{chunks: []RetrievedChunk{
		{Path: "docs/guide.md", Name: "guide.md", Section: "Введение", ChunkID: "abc123", Score: 0.82, Text: "Уникальный факт из базы знаний"},
	}}
	cfg := Config{APIFormat: "openai", Model: "test", RAGEnabled: true, RAGStrategy: "structure", RAGTopK: 3, RAGMode: "threshold"}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	resp, err := a.Run(AgentRequest{
		Message:    "что такое X?",
		ProjectID:  "p",
		SessionID:  "s-src",
		RAGEnabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.RAGUsed {
		t.Fatal("expected RAGUsed=true")
	}
	if len(resp.Sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(resp.Sources))
	}
	src := resp.Sources[0]
	if src.Path != "docs/guide.md" || src.Section != "Введение" || src.ChunkID != "abc123" {
		t.Fatalf("unexpected source: %+v", src)
	}
	if src.Quote != "Уникальный факт из базы знаний" {
		t.Fatalf("unexpected quote: %q", src.Quote)
	}
	// Чистый ответ не должен содержать блок источников — он добавляется на HTTP-слое.
	if strings.Contains(resp.Content, "Источники:") {
		t.Fatalf("agent content must stay clean, got %q", resp.Content)
	}
}

func TestRunWorkflowDoesNotRefuse(t *testing.T) {
	client := &capturingClient{}
	retriever := &fakeRetriever{}
	cfg := Config{APIFormat: "openai", Model: "test", RAGEnabled: true, RAGThreshold: 0.35, RAGMode: "threshold"}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	resp, err := a.Run(AgentRequest{
		Message:      "вопрос",
		ProjectID:    "p",
		SessionID:    "s-wf",
		RAGEnabled:   boolPtr(true),
		WorkflowMode: WorkflowModeWorkflow,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.payloads) == 0 {
		t.Fatal("workflow must still call LLM, refusal is chat-only")
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
}

func TestRunRetrievalErrorDoesNotRefuse(t *testing.T) {
	client := &capturingClient{}
	retriever := &fakeRetriever{err: errors.New("embedder недоступен")}
	cfg := Config{APIFormat: "openai", Model: "test", RAGEnabled: true}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	resp, err := a.Run(AgentRequest{
		Message:    "вопрос",
		ProjectID:  "p",
		SessionID:  "s-err",
		RAGEnabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.payloads) == 0 {
		t.Fatal("retrieval error must fall back to LLM, not refusal")
	}
	if resp.Content == ragRefusalMessage {
		t.Fatal("retrieval error must not produce refusal")
	}
}

func TestFormatSourcesBlock(t *testing.T) {
	if got := FormatSourcesBlock(nil); got != "" {
		t.Fatalf("empty sources must produce empty block, got %q", got)
	}

	block := FormatSourcesBlock([]SourceRef{
		{Path: "docs/a.md", Section: "S1", ChunkID: "c1", Quote: "первая цитата"},
		{Path: "b.md", ChunkID: "c2", Quote: strings.Repeat("я", 400)},
	})
	for _, want := range []string{"Источники:", "docs/a.md", "S1", "chunk_id: c1", "первая цитата", "b.md", "chunk_id: c2", "…"} {
		if !strings.Contains(block, want) {
			t.Errorf("expected block to contain %q, got:\n%s", want, block)
		}
	}
	if strings.Contains(block, strings.Repeat("я", 400)) {
		t.Error("long quote must be truncated")
	}
}

func TestRunPassesRetrieveOptions(t *testing.T) {
	client := &capturingClient{}
	retriever := &fakeRetriever{chunks: []RetrievedChunk{{Path: "a.md", Text: "текст"}}}
	cfg := Config{
		APIFormat: "openai", Model: "test", RAGEnabled: true,
		RAGStrategy: "fixed", RAGTopK: 7, RAGCandidates: 30, RAGThreshold: 0.5, RAGMode: "threshold",
	}
	a := NewSimpleAgent(cfg, client).WithKnowledge(retriever)

	threshold := 0.9
	if _, err := a.Run(AgentRequest{
		Message:       "запрос",
		ProjectID:     "p",
		SessionID:     "s-opts",
		RAGEnabled:    boolPtr(true),
		RAGStrategy:   "structure",
		RAGTopK:       2,
		RAGCandidates: 11,
		RAGThreshold:  &threshold,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	opts := retriever.lastOpts
	if opts.Strategy != "structure" || opts.TopK != 2 || opts.Candidates != 11 || opts.Query != "запрос" {
		t.Fatalf("unexpected options: %+v", opts)
	}
	if opts.Mode != "threshold" {
		t.Fatalf("mode = %q, want threshold", opts.Mode)
	}
	if opts.Threshold == nil || *opts.Threshold != 0.9 {
		t.Fatalf("threshold = %v, want 0.9", opts.Threshold)
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
