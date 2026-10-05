package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"ai-chat/internal/agent"
	"ai-chat/internal/mcp"
	"gopkg.in/yaml.v3"
)

const (
	defaultExternalAPI      = "https://httpbin.org/anything"
	defaultModel            = "SMLab-ext/Kimi-K2.7-Code"
	defaultTimeout          = 60 * time.Second
	defaultSummaryMaxTokens = 500
	defaultDigestAt         = "09:00"

	defaultIndexDBPath      = "data/index.db"
	defaultOllamaURL        = "http://localhost:11434"
	defaultOllamaEmbedModel = "nomic-embed-text"
	defaultOpenAIEmbedModel = "SMLab/bge-m3"
	defaultEmbedAPIFormat   = "openai"
	defaultEmbedBatch       = 32
	defaultChunkSize        = 1000
	defaultChunkOverlap     = 200
	defaultChunkMinSize     = 200
	defaultSearchTopK       = 5
	defaultMaxUploadMB      = 10
	defaultRAGStrategy      = "structure"
	defaultSearchCandidates = 20
	defaultRerankMode       = "threshold"
	defaultRerankThreshold  = 0.35
)

type knowledgeConfig struct {
	IndexDBPath     string
	EmbedAPIFormat  string
	EmbedAPIURL     string
	EmbedAPIKey     string
	EmbedModel      string
	EmbedBatch      int
	ChunkSize       int
	ChunkOverlap    int
	ChunkMinSize    int
	SearchTopK      int
	Candidates      int
	RerankMode      string
	RerankThreshold float64
	MaxUploadBytes  int64
}

type serverConfig struct {
	Port          string
	Timeout       time.Duration
	Agent         agent.Config
	MCP           mcp.Config
	DigestEnabled bool
	DigestAt      string
	DigestPrompt  string
	Knowledge     knowledgeConfig
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func loadConfig() serverConfig {
	timeout := defaultTimeout
	if v := os.Getenv("TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		} else {
			log.Printf("Предупреждение: не удалось распарсить TIMEOUT=%q, используется значение по умолчанию %s", v, defaultTimeout)
		}
	}

	mcpCfg, err := mcp.LoadConfig("mcp.yaml")
	if err != nil {
		log.Printf("Предупреждение: не удалось загрузить mcp.yaml: %v", err)
	}

	cfg := serverConfig{
		Port:          os.Getenv("PORT"),
		Timeout:       timeout,
		MCP:           mcpCfg,
		DigestEnabled: strings.ToLower(os.Getenv("DAILY_DIGEST_ENABLED")) == "true",
		DigestAt:      getEnvOrDefault("DAILY_DIGEST_AT", defaultDigestAt),
		DigestPrompt:  getEnvOrDefault("DAILY_DIGEST_PROMPT", defaultDigestPrompt()),
		Agent: agent.Config{
			ExternalAPI:              os.Getenv("EXTERNAL_API_URL"),
			APIKey:                   os.Getenv("API_KEY"),
			AuthType:                 strings.ToLower(os.Getenv("AUTH_TYPE")),
			APIFormat:                strings.ToLower(os.Getenv("API_FORMAT")),
			Model:                    getEnvOrDefault("MODEL", defaultModel),
			SystemPrompt:             os.Getenv("SYSTEM_PROMPT"),
			AssistantPrompt:          os.Getenv("ASSISTANT_PROMPT"),
			UserPromptTemplate:       os.Getenv("USER_PROMPT_TEMPLATE"),
			OrchestratorInstructions: loadOrchestratorInstructions(),
			Roles:                    loadRoles(),
			SummaryMaxTokens:         parseIntEnv("SUMMARY_MAX_TOKENS", defaultSummaryMaxTokens),
			RAGEnabled:               parseBoolEnv("RAG_ENABLED", true),
			RAGStrategy:              getEnvOrDefault("RAG_STRATEGY", defaultRAGStrategy),
			RAGTopK:                  parseIntEnv("RAG_TOP_K", defaultSearchTopK),
		},
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.Agent.ExternalAPI == "" {
		cfg.Agent.ExternalAPI = defaultExternalAPI
	}
	if cfg.DigestEnabled {
		if _, err := time.Parse("15:04", cfg.DigestAt); err != nil {
			log.Printf("Предупреждение: неверный формат DAILY_DIGEST_AT=%q, используется значение по умолчанию %s", cfg.DigestAt, defaultDigestAt)
			cfg.DigestAt = defaultDigestAt
		}
	}

	cfg.Knowledge = loadKnowledgeConfig(cfg.Agent)

	// RAG-параметры второго этапа берём из настроек базы знаний.
	cfg.Agent.RAGCandidates = cfg.Knowledge.Candidates
	cfg.Agent.RAGThreshold = cfg.Knowledge.RerankThreshold
	cfg.Agent.RAGMode = cfg.Knowledge.RerankMode

	if cfg.Knowledge.ChunkOverlap >= cfg.Knowledge.ChunkSize {
		log.Printf("Предупреждение: CHUNK_OVERLAP (%d) >= CHUNK_SIZE (%d), используется overlap по умолчанию %d",
			cfg.Knowledge.ChunkOverlap, cfg.Knowledge.ChunkSize, defaultChunkOverlap)
		cfg.Knowledge.ChunkOverlap = defaultChunkOverlap
	}

	// Если задан API_KEY, но не указаны AUTH_TYPE и API_FORMAT,
	// используем OpenAI-совместимый формат с авторизацией Bearer.
	if cfg.Agent.APIKey != "" {
		if cfg.Agent.AuthType == "" {
			cfg.Agent.AuthType = "bearer"
		}
		if cfg.Agent.APIFormat == "" {
			cfg.Agent.APIFormat = "openai"
		}
	} else {
		if cfg.Agent.AuthType == "" {
			cfg.Agent.AuthType = "none"
		}
		if cfg.Agent.APIFormat == "" {
			cfg.Agent.APIFormat = "generic"
		}
	}

	return cfg
}

// loadKnowledgeConfig собирает настройки базы знаний и разрешает параметры эмбеддера.
func loadKnowledgeConfig(agentCfg agent.Config) knowledgeConfig {
	format := strings.ToLower(strings.TrimSpace(getEnvOrDefault("EMBED_API_FORMAT", defaultEmbedAPIFormat)))
	if format != "ollama" {
		format = "openai"
	}

	url := strings.TrimSpace(os.Getenv("EMBED_API_URL"))
	model := strings.TrimSpace(os.Getenv("EMBED_MODEL"))

	if format == "ollama" {
		if url == "" {
			url = getEnvOrDefault("OLLAMA_URL", defaultOllamaURL)
		}
		url = strings.TrimRight(url, "/")
		if model == "" {
			model = getEnvOrDefault("OLLAMA_EMBED_MODEL", defaultOllamaEmbedModel)
		}
	} else {
		if url == "" {
			url = deriveEmbedURL(agentCfg.ExternalAPI)
		}
		if model == "" {
			model = defaultOpenAIEmbedModel
		}
	}

	key := os.Getenv("EMBED_API_KEY")
	if key == "" {
		key = agentCfg.APIKey
	}

	topK := parseIntEnv("SEARCH_TOP_K", defaultSearchTopK)
	candidates := parseIntEnv("SEARCH_CANDIDATES", defaultSearchCandidates)
	if candidates < topK {
		log.Printf("Предупреждение: SEARCH_CANDIDATES (%d) < SEARCH_TOP_K (%d), кандидаты подняты до top-K", candidates, topK)
		candidates = topK
	}

	return knowledgeConfig{
		IndexDBPath:     getEnvOrDefault("INDEX_DB_PATH", defaultIndexDBPath),
		EmbedAPIFormat:  format,
		EmbedAPIURL:     url,
		EmbedAPIKey:     key,
		EmbedModel:      model,
		EmbedBatch:      parseIntEnv("EMBED_BATCH", defaultEmbedBatch),
		ChunkSize:       parseIntEnv("CHUNK_SIZE", defaultChunkSize),
		ChunkOverlap:    parseIntEnv("CHUNK_OVERLAP", defaultChunkOverlap),
		ChunkMinSize:    parseIntEnv("CHUNK_MIN_SIZE", defaultChunkMinSize),
		SearchTopK:      topK,
		Candidates:      candidates,
		RerankMode:      normalizeRerankMode(os.Getenv("RERANK_MODE")),
		RerankThreshold: parseFloatEnv("RERANK_THRESHOLD", defaultRerankThreshold),
		MaxUploadBytes:  int64(parseIntEnv("MAX_UPLOAD_MB", defaultMaxUploadMB)) * 1024 * 1024,
	}
}

// normalizeRerankMode приводит RERANK_MODE к известному значению.
func normalizeRerankMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none":
		return "none"
	case "threshold", "":
		return defaultRerankMode
	default:
		log.Printf("Предупреждение: неизвестный RERANK_MODE=%q, используется %s", raw, defaultRerankMode)
		return defaultRerankMode
	}
}

// deriveEmbedURL выводит URL эмбеддингов из URL chat/completions.
func deriveEmbedURL(chatURL string) string {
	chatURL = strings.TrimRight(strings.TrimSpace(chatURL), "/")
	if i := strings.LastIndex(chatURL, "/chat/completions"); i >= 0 {
		return chatURL[:i] + "/embeddings"
	}
	if strings.HasSuffix(chatURL, "/v1") {
		return chatURL + "/embeddings"
	}
	return chatURL
}

func defaultDigestPrompt() string {
	return "Ты блогер, который составляет чарт фильмов, собери популярные фильмы за последние сутки и сделай сводку"
}

func parseBoolEnv(key string, defaultValue bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return defaultValue
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Printf("Предупреждение: не удалось распарсить %s=%q, используется значение по умолчанию %t", key, v, defaultValue)
		return defaultValue
	}
}

func parseFloatEnv(key string, defaultValue float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
		log.Printf("Предупреждение: не удалось распарсить %s=%q, используется значение по умолчанию %v", key, v, defaultValue)
	}
	return defaultValue
}

func parseIntEnv(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		log.Printf("Предупреждение: не удалось распарсить %s, используется значение по умолчанию %d", key, defaultValue)
	}
	return defaultValue
}

func loadOrchestratorInstructions() string {
	data, err := os.ReadFile("orchestrator.yaml")
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Предупреждение: не удалось прочитать orchestrator.yaml: %v", err)
		}
		return ""
	}

	var orchestrator struct {
		Instructions string `yaml:"instructions"`
	}
	if err := yaml.Unmarshal(data, &orchestrator); err != nil {
		log.Printf("Предупреждение: не удалось распарсить orchestrator.yaml: %v", err)
		return ""
	}

	return strings.TrimSpace(orchestrator.Instructions)
}

func loadRoles() map[string]string {
	data, err := os.ReadFile("roles.yaml")
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Предупреждение: не удалось прочитать roles.yaml: %v", err)
		}
		return nil
	}

	var config struct {
		Roles map[string]string `yaml:"roles"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		log.Printf("Предупреждение: не удалось распарсить roles.yaml: %v", err)
		return nil
	}

	return config.Roles
}
