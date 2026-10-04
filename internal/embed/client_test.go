package embed

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaClientEmbed(t *testing.T) {
	var gotInputs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotInputs += len(req.Input)

		embeddings := make([][]float32, len(req.Input))
		for i := range req.Input {
			embeddings[i] = []float32{3, 4}
		}
		_ = json.NewEncoder(w).Encode(embedResponse{Embeddings: embeddings})
	}))
	defer srv.Close()

	client := NewOllamaClient(srv.URL, "test-model", 2, 5*time.Second)
	vectors, err := client.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 3 {
		t.Fatalf("got %d vectors, want 3", len(vectors))
	}
	if gotInputs != 3 {
		t.Fatalf("server saw %d inputs, want 3", gotInputs)
	}
	for _, v := range vectors {
		if math.Abs(float64(v[0]*v[0]+v[1]*v[1])-1) > 1e-6 {
			t.Errorf("vector not normalized: %v", v)
		}
	}
}

func TestOllamaClientNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewOllamaClient(srv.URL, "missing", 4, 5*time.Second)
	_, err := client.Embed(context.Background(), []string{"a"})
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestOllamaClientUnreachable(t *testing.T) {
	client := NewOllamaClient("http://127.0.0.1:1", "m", 1, time.Second)
	_, err := client.Embed(context.Background(), []string{"a"})
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestOllamaClientEmpty(t *testing.T) {
	client := NewOllamaClient("http://127.0.0.1:1", "m", 1, time.Second)
	vectors, err := client.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed(nil): %v", err)
	}
	if len(vectors) != 0 {
		t.Fatalf("got %d vectors, want 0", len(vectors))
	}
}

func TestOpenAIClientEmbedAndOrdering(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req openAIEmbedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model

		// Возвращаем элементы в обратном порядке, чтобы проверить сортировку по index.
		_ = json.NewEncoder(w).Encode(openAIEmbedResponse{
			Data: []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Index: 1, Embedding: []float32{0, 1}},
				{Index: 0, Embedding: []float32{3, 4}},
			},
		})
	}))
	defer srv.Close()

	client := NewOpenAIClient(srv.URL, "secret", "SMLab/bge-m3", 8, 5*time.Second)
	vectors, err := client.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotModel != "SMLab/bge-m3" {
		t.Errorf("model = %q", gotModel)
	}
	if len(vectors) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vectors))
	}
	// Первым должен быть vector с index=0, нормализованный [3,4] -> [0.6,0.8].
	if math.Abs(float64(vectors[0][0])-0.6) > 1e-6 || math.Abs(float64(vectors[0][1])-0.8) > 1e-6 {
		t.Fatalf("first vector = %v, want [0.6 0.8]", vectors[0])
	}
}

func TestOpenAIClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model not available"}}`))
	}))
	defer srv.Close()

	client := NewOpenAIClient(srv.URL, "", "bad", 4, 5*time.Second)
	_, err := client.Embed(context.Background(), []string{"a"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewClientSelectsProvider(t *testing.T) {
	if _, ok := NewClient("ollama", "http://localhost:11434", "", "m", 1, time.Second).(*OllamaClient); !ok {
		t.Fatal("expected OllamaClient")
	}
	if _, ok := NewClient("openai", "http://localhost/v1/embeddings", "k", "m", 1, time.Second).(*OpenAIClient); !ok {
		t.Fatal("expected OpenAIClient")
	}
}
