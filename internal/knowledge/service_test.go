package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ai-chat/internal/chunk"
)

type fakeEmbedder struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	f.calls += len(texts)
	err := f.err
	f.mu.Unlock()

	if err != nil {
		return nil, err
	}

	out := make([][]float32, len(texts))
	for i, text := range texts {
		switch lower := strings.ToLower(text); {
		case strings.Contains(lower, "alpha"):
			out[i] = []float32{1, 0, 0}
		case strings.Contains(lower, "beta"):
			out[i] = []float32{0, 1, 0}
		default:
			out[i] = []float32{0, 0, 1}
		}
	}
	return out, nil
}

func newTestService(t *testing.T, embedder *fakeEmbedder) (*Service, *Store) {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	svc := NewService(store, embedder, Config{
		ChunkSize:    1000,
		ChunkOverlap: 0,
		ChunkMinSize: 0,
		TopK:         5,
		EmbedModel:   "test-model",
	}, nil)
	return svc, store
}

func TestServiceIndexSearchAndCache(t *testing.T) {
	ctx := context.Background()
	embedder := &fakeEmbedder{}
	svc, _ := newTestService(t, embedder)

	mustUpload(t, svc, "p", "alpha.txt", "alpha alpha alpha")
	mustUpload(t, svc, "p", "beta.txt", "beta beta beta")

	runs, err := svc.Index(ctx, "p", "")
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(runs))
	}

	results, err := svc.Search(ctx, "p", "fixed", "alpha", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Path != "alpha.txt" {
		t.Fatalf("unexpected top result: %+v", results)
	}

	before := embedder.calls
	if _, err := svc.Index(ctx, "p", "fixed"); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if embedder.calls != before {
		t.Fatalf("cache miss: embedder called %d more times", embedder.calls-before)
	}
}

func TestServiceSearchRejectsUnknownStrategy(t *testing.T) {
	svc, _ := newTestService(t, &fakeEmbedder{})
	if _, err := svc.Search(context.Background(), "p", "nope", "q", 3); err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}

func TestServiceIndexFailurePreservesPreviousIndex(t *testing.T) {
	ctx := context.Background()
	embedder := &fakeEmbedder{}
	svc, store := newTestService(t, embedder)

	mustUpload(t, svc, "p", "alpha.txt", "alpha alpha")
	if _, err := svc.Index(ctx, "p", "fixed"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Новый файл заставит сервис обратиться к сломанному эмбеддеру.
	mustUpload(t, svc, "p", "gamma.txt", "gamma gamma")
	failing := &fakeEmbedder{err: context.DeadlineExceeded}
	failingSvc := NewService(store, failing, Config{ChunkSize: 1000, TopK: 5, EmbedModel: "test-model"}, nil)
	if _, err := failingSvc.Index(ctx, "p", "fixed"); err == nil {
		t.Fatal("expected index error")
	}

	chunks, err := store.ListChunks(ctx, "p", "fixed")
	if err != nil {
		t.Fatalf("ListChunks: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("previous index was wiped after failed reindex")
	}
}

func TestServiceBenchmark(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, &fakeEmbedder{})

	mustUpload(t, svc, "p", "alpha.txt", "alpha alpha alpha")
	mustUpload(t, svc, "p", "beta.txt", "beta beta beta")
	if _, err := svc.Index(ctx, "p", ""); err != nil {
		t.Fatalf("Index: %v", err)
	}

	if _, err := svc.AddQuery(ctx, Query{
		ProjectID:    "p",
		Text:         "alpha",
		ExpectedPath: "alpha.txt",
	}); err != nil {
		t.Fatalf("AddQuery: %v", err)
	}

	runs, err := svc.Benchmark(ctx, "p", RetrieveOptions{TopK: 1})
	if err != nil {
		t.Fatalf("Benchmark: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d benchmark runs, want 2", len(runs))
	}
	for _, run := range runs {
		if run.Recall != 1 {
			t.Errorf("strategy %s recall = %v, want 1", run.Strategy, run.Recall)
		}
	}

	metrics, err := svc.GetMetrics(ctx, "p")
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	if len(metrics.Structural) != 2 {
		t.Errorf("structural entries = %d, want 2", len(metrics.Structural))
	}
	if len(metrics.IndexRuns) != 2 {
		t.Errorf("index runs = %d, want 2", len(metrics.IndexRuns))
	}
	if len(metrics.Benchmark) != 2 {
		t.Errorf("benchmark entries = %d, want 2", len(metrics.Benchmark))
	}
}

func TestEvaluate(t *testing.T) {
	results := []QueryResult{
		{Hit: true, FirstRank: 1, Matches: 1},
		{Hit: true, FirstRank: 2, Matches: 2},
		{Hit: false, FirstRank: 0, Matches: 0},
	}
	recall, precision, mrr := Evaluate(results, 5)
	if recall != 2.0/3.0 {
		t.Errorf("recall = %v", recall)
	}
	gotPrecision := 3.0 / 15.0
	if precision != gotPrecision {
		t.Errorf("precision = %v, want %v", precision, gotPrecision)
	}
	gotMRR := (1 + 0.5) / 3.0
	if mrr != gotMRR {
		t.Errorf("mrr = %v, want %v", mrr, gotMRR)
	}
}

func TestSearchTopKOrdering(t *testing.T) {
	chunks := []StoredChunk{
		{Path: "a", Vector: []float32{0, 1}},
		{Path: "b", Vector: []float32{1, 0}},
		{Path: "c", Vector: []float32{0.5, 0.5}},
	}
	top := SearchTopK(chunks, []float32{1, 0}, 2)
	if len(top) != 2 || top[0].Path != "b" {
		t.Fatalf("unexpected top: %+v", top)
	}
}

func TestNormalizePath(t *testing.T) {
	if _, err := normalizePath("  ../etc/passwd "); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if got, err := normalizePath("/docs/a.md"); err != nil || got != "docs/a.md" {
		t.Fatalf("got %q, err %v", got, err)
	}
}

func TestNormalizeStrategy(t *testing.T) {
	if normalizeStrategy("FIXED") != chunk.StrategyFixed {
		t.Fatal("expected fixed")
	}
	if normalizeStrategy("bogus") != "" {
		t.Fatal("expected empty for unknown")
	}
}

func mustUpload(t *testing.T, svc *Service, projectID, path, content string) {
	t.Helper()
	if _, err := svc.UploadFile(context.Background(), projectID, path, content); err != nil {
		t.Fatalf("UploadFile(%s): %v", path, err)
	}
}
