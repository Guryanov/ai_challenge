package main

import (
	"testing"

	"ai-chat/internal/agent"
)

func TestDeriveEmbedURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://api.ai.localcorp.net/v1/chat/completions", "https://api.ai.localcorp.net/v1/embeddings"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/embeddings"},
		{"https://example.com/v1/embeddings", "https://example.com/v1/embeddings"},
		{"http://localhost:8000/embeddings", "http://localhost:8000/embeddings"},
	}
	for _, tc := range tests {
		if got := deriveEmbedURL(tc.in); got != tc.want {
			t.Errorf("deriveEmbedURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadKnowledgeConfigOpenAIDefault(t *testing.T) {
	for _, key := range []string{"EMBED_API_FORMAT", "EMBED_API_URL", "EMBED_MODEL", "EMBED_API_KEY", "OLLAMA_URL", "OLLAMA_EMBED_MODEL"} {
		t.Setenv(key, "")
	}

	cfg := loadKnowledgeConfig(agent.Config{
		ExternalAPI: "https://api.ai.localcorp.net/v1/chat/completions",
		APIKey:      "sk-test",
	})

	if cfg.EmbedAPIFormat != "openai" {
		t.Errorf("format = %q", cfg.EmbedAPIFormat)
	}
	if cfg.EmbedAPIURL != "https://api.ai.localcorp.net/v1/embeddings" {
		t.Errorf("url = %q", cfg.EmbedAPIURL)
	}
	if cfg.EmbedModel != defaultOpenAIEmbedModel {
		t.Errorf("model = %q", cfg.EmbedModel)
	}
	if cfg.EmbedAPIKey != "sk-test" {
		t.Errorf("key = %q", cfg.EmbedAPIKey)
	}
}

func TestParseFloatEnv(t *testing.T) {
	t.Setenv("TEST_FLOAT", "0.25")
	if got := parseFloatEnv("TEST_FLOAT", 1); got != 0.25 {
		t.Fatalf("parseFloatEnv = %v, want 0.25", got)
	}
	t.Setenv("TEST_FLOAT", "bad")
	if got := parseFloatEnv("TEST_FLOAT", 0.35); got != 0.35 {
		t.Fatalf("parseFloatEnv fallback = %v, want 0.35", got)
	}
}

func TestNormalizeRerankMode(t *testing.T) {
	cases := map[string]string{
		"NONE":      "none",
		"none":      "none",
		"":          "threshold",
		"threshold": "threshold",
		"bogus":     "threshold",
	}
	for in, want := range cases {
		if got := normalizeRerankMode(in); got != want {
			t.Errorf("normalizeRerankMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadKnowledgeConfigCandidatesClamp(t *testing.T) {
	for _, key := range []string{"EMBED_API_FORMAT", "EMBED_API_URL", "EMBED_MODEL", "EMBED_API_KEY", "OLLAMA_URL", "OLLAMA_EMBED_MODEL"} {
		t.Setenv(key, "")
	}
	t.Setenv("SEARCH_TOP_K", "10")
	t.Setenv("SEARCH_CANDIDATES", "3")

	cfg := loadKnowledgeConfig(agent.Config{ExternalAPI: "https://api.ai.localcorp.net/v1/chat/completions"})

	if cfg.Candidates != 10 {
		t.Errorf("candidates = %d, want clamp to top-K 10", cfg.Candidates)
	}
	if cfg.RerankMode != defaultRerankMode {
		t.Errorf("rerank mode = %q, want %q", cfg.RerankMode, defaultRerankMode)
	}
	if cfg.RerankThreshold != defaultRerankThreshold {
		t.Errorf("rerank threshold = %v, want %v", cfg.RerankThreshold, defaultRerankThreshold)
	}
}

func TestLoadKnowledgeConfigOllama(t *testing.T) {
	for _, key := range []string{"EMBED_API_URL", "EMBED_MODEL", "EMBED_API_KEY", "OLLAMA_EMBED_MODEL"} {
		t.Setenv(key, "")
	}
	t.Setenv("EMBED_API_FORMAT", "ollama")
	t.Setenv("OLLAMA_URL", "http://localhost:11434/")

	cfg := loadKnowledgeConfig(agent.Config{ExternalAPI: "https://api.ai.localcorp.net/v1/chat/completions"})

	if cfg.EmbedAPIFormat != "ollama" {
		t.Errorf("format = %q", cfg.EmbedAPIFormat)
	}
	if cfg.EmbedAPIURL != "http://localhost:11434" {
		t.Errorf("url = %q", cfg.EmbedAPIURL)
	}
	if cfg.EmbedModel != defaultOllamaEmbedModel {
		t.Errorf("model = %q", cfg.EmbedModel)
	}
}
