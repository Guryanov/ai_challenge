package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-chat/internal/knowledge"
	"ai-chat/internal/mcp"
)

type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if strings.Contains(strings.ToLower(text), "alpha") {
			out[i] = []float32{1, 0, 0}
		} else {
			out[i] = []float32{0, 0, 1}
		}
	}
	return out, nil
}

func newTestKBService(t *testing.T) *knowledge.Service {
	t.Helper()
	store, err := knowledge.NewStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return knowledge.NewService(store, stubEmbedder{}, knowledge.Config{
		ChunkSize: 1000, TopK: 5, EmbedModel: "stub",
	}, nil)
}

func postJSON(t *testing.T, handler http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestKBHandlersFlow(t *testing.T) {
	svc := newTestKBService(t)

	// Загрузка текста.
	rec := postJSON(t, handleKBText(svc), "/api/kb/text",
		`{"project_id":"p","path":"alpha.txt","content":"alpha alpha alpha"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status %d: %s", rec.Code, rec.Body.String())
	}

	// Индексация обеих стратегий.
	rec = postJSON(t, handleKBIndex(svc), "/api/kb/index", `{"project_id":"p"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("index status %d: %s", rec.Code, rec.Body.String())
	}
	var indexResp kbIndexResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &indexResp); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if len(indexResp.Runs) != 2 {
		t.Fatalf("index runs = %d, want 2", len(indexResp.Runs))
	}

	// Поиск.
	rec = postJSON(t, handleKBSearch(svc), "/api/kb/search",
		`{"project_id":"p","strategy":"fixed","query":"alpha","k":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status %d: %s", rec.Code, rec.Body.String())
	}
	var searchResp kbSearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &searchResp); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(searchResp.Results) == 0 || searchResp.Results[0].Path != "alpha.txt" {
		t.Fatalf("unexpected search results: %+v", searchResp.Results)
	}

	// Тест-запрос + бенчмарк.
	rec = postJSON(t, handleKBAddQuery(svc), "/api/kb/queries",
		`{"project_id":"p","text":"alpha","expected_path":"alpha.txt"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("add query status %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(t, handleKBBenchmark(svc), "/api/kb/benchmark", `{"project_id":"p","k":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("benchmark status %d: %s", rec.Code, rec.Body.String())
	}
	var benchResp kbBenchmarkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &benchResp); err != nil {
		t.Fatalf("decode benchmark: %v", err)
	}
	if len(benchResp.Runs) != 2 {
		t.Fatalf("benchmark runs = %d, want 2", len(benchResp.Runs))
	}

	// Метрики.
	req := httptest.NewRequest(http.MethodGet, "/api/kb/metrics?project_id=p", nil)
	rec = httptest.NewRecorder()
	handleKBMetrics(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status %d: %s", rec.Code, rec.Body.String())
	}
	var metrics knowledge.Metrics
	if err := json.Unmarshal(rec.Body.Bytes(), &metrics); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if len(metrics.Structural) != 2 {
		t.Fatalf("structural = %d, want 2", len(metrics.Structural))
	}
}

func TestHandleMCPStatus(t *testing.T) {
	registry := &mcp.Registry{}
	// Registry internals are unexported, but we can verify the endpoint shape
	// by using the public constructor with an empty config.
	registry = mcp.NewRegistry(t.Context(), mcp.Config{})
	defer registry.CloseAll()

	req := httptest.NewRequest(http.MethodGet, "/api/mcp/status", nil)
	rec := httptest.NewRecorder()

	handleMCPStatus(registry)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp struct {
		Servers []mcp.ServerStatus `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Servers == nil {
		t.Fatal("expected servers slice, got nil")
	}
}

func TestHandleMCPDisconnect(t *testing.T) {
	registry := mcp.NewRegistry(t.Context(), mcp.Config{
		Servers: map[string]mcp.ServerConfig{
			"test": {Type: "stdio", Command: "echo", Disabled: true},
		},
	})
	defer registry.CloseAll()

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/test/disconnect", nil)
	req.SetPathValue("name", "test")
	rec := httptest.NewRecorder()

	handleMCPDisconnect(registry)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleMCPConnect(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "mcp.yaml")
	data := `
servers:
  test:
    type: stdio
    command: echo
    disabled: true
`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	registry := mcp.NewRegistry(t.Context(), mcp.Config{})
	defer registry.CloseAll()

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/test/connect", nil)
	req.SetPathValue("name", "test")
	rec := httptest.NewRecorder()

	handleMCPConnect(configPath, registry)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
