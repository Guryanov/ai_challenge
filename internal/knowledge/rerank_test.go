package knowledge

import (
	"context"
	"math"
	"testing"
)

func TestThresholdRerankerFilters(t *testing.T) {
	candidates := []ScoredChunk{{Score: 0.9}, {Score: 0.4}, {Score: 0.2}}

	got, err := ThresholdReranker{Min: 0.35}.Rerank("q", candidates)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2", len(got))
	}
	if got[0].Score != 0.9 || got[1].Score != 0.4 {
		t.Fatalf("order not preserved: %+v", got)
	}
}

func TestThresholdRerankerNegativeDisables(t *testing.T) {
	candidates := []ScoredChunk{{Score: 0.9}, {Score: -0.5}}
	got, err := ThresholdReranker{Min: -1}.Rerank("q", candidates)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2 (filter disabled)", len(got))
	}
}

func TestNewRerankerMode(t *testing.T) {
	if _, ok := NewReranker("none", 0.9).(NoopReranker); !ok {
		t.Fatal("mode none should use NoopReranker")
	}
	if _, ok := NewReranker("threshold", 0.9).(ThresholdReranker); !ok {
		t.Fatal("mode threshold should use ThresholdReranker")
	}
	if r, ok := NewReranker("", 0.9).(ThresholdReranker); !ok || r.Min != 0.9 {
		t.Fatal("empty mode should default to ThresholdReranker with given threshold")
	}
}

func TestNormalizeVec(t *testing.T) {
	got := normalizeVec([]float32{3, 4})
	if math.Abs(float64(got[0])-0.6) > 1e-6 || math.Abs(float64(got[1])-0.8) > 1e-6 {
		t.Fatalf("normalizeVec([3 4]) = %v, want [0.6 0.8]", got)
	}

	// Идемпотентность: уже единичный вектор возвращается как есть.
	again := normalizeVec(got)
	if &again[0] != &got[0] {
		t.Fatal("normalizeVec should return the same slice for a unit vector")
	}

	zero := normalizeVec([]float32{0, 0})
	if len(zero) != 2 || zero[0] != 0 || zero[1] != 0 {
		t.Fatalf("normalizeVec([0 0]) = %v, want unchanged", zero)
	}
}

func TestServiceRetrieveThresholdFilters(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, &fakeEmbedder{})

	mustUpload(t, svc, "p", "alpha.txt", "alpha alpha")
	mustUpload(t, svc, "p", "beta.txt", "beta beta")
	if _, err := svc.Index(ctx, "p", "fixed"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	threshold := 0.5
	got, err := svc.Retrieve(ctx, "p", RetrieveOptions{
		Strategy: "fixed", Query: "alpha", TopK: 5, Candidates: 20, Threshold: &threshold,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(got) != 1 || got[0].Path != "alpha.txt" {
		t.Fatalf("threshold should keep only alpha.txt, got %+v", got)
	}

	// Режим none игнорирует порог.
	gotAll, err := svc.Retrieve(ctx, "p", RetrieveOptions{
		Strategy: "fixed", Query: "alpha", TopK: 5, Mode: "none",
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(gotAll) < 2 {
		t.Fatalf("mode none should return both chunks, got %d", len(gotAll))
	}
}

func TestServiceRetrieveTopKTruncates(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, &fakeEmbedder{})

	mustUpload(t, svc, "p", "alpha1.txt", "alpha one")
	mustUpload(t, svc, "p", "alpha2.txt", "alpha two")
	if _, err := svc.Index(ctx, "p", "fixed"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	got, err := svc.Retrieve(ctx, "p", RetrieveOptions{
		Strategy: "fixed", Query: "alpha", TopK: 1, Candidates: 20, Mode: "none",
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1 (top-K truncation)", len(got))
	}
}
