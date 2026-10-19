package main

import (
	"context"
	"log"
	"net/http"

	"ai-chat/internal/agent"
	"ai-chat/internal/embed"
	"ai-chat/internal/history"
	"ai-chat/internal/knowledge"
	"ai-chat/internal/mcp"
)

func main() {
	cfg := loadConfig()

	httpClient := agent.NewHTTPClient(
		cfg.Agent.ExternalAPI,
		cfg.Agent.APIKey,
		cfg.Agent.AuthType,
		cfg.Timeout,
	)

	historyStore, err := history.NewFileStore("history")
	if err != nil {
		log.Fatalf("Ошибка создания хранилища истории: %v", err)
	}

	mcpRegistry := mcp.NewRegistry(context.Background(), cfg.MCP)
	defer func() {
		if err := mcpRegistry.CloseAll(); err != nil {
			log.Printf("Ошибка закрытия MCP-серверов: %v", err)
		}
	}()

	llmAgent := agent.NewSimpleAgent(cfg.Agent, httpClient).
		WithHistory(historyStore).
		WithTools(mcpRegistry)

	knowledgeStore, err := knowledge.NewStore(cfg.Knowledge.IndexDBPath)
	if err != nil {
		log.Fatalf("Ошибка открытия индекса базы знаний: %v", err)
	}
	defer func() {
		if err := knowledgeStore.Close(); err != nil {
			log.Printf("Ошибка закрытия базы знаний: %v", err)
		}
	}()

	embedder := embed.NewClient(
		cfg.Knowledge.EmbedAPIFormat,
		cfg.Knowledge.EmbedAPIURL,
		cfg.Knowledge.EmbedAPIKey,
		cfg.Knowledge.EmbedModel,
		cfg.Knowledge.EmbedBatch,
		cfg.Timeout,
	)
	log.Printf("Эмбеддинги: формат %s, модель %s, адрес %s", cfg.Knowledge.EmbedAPIFormat, cfg.Knowledge.EmbedModel, cfg.Knowledge.EmbedAPIURL)

	kbService := knowledge.NewService(knowledgeStore, embedder, knowledge.Config{
		ChunkSize:       cfg.Knowledge.ChunkSize,
		ChunkOverlap:    cfg.Knowledge.ChunkOverlap,
		ChunkMinSize:    cfg.Knowledge.ChunkMinSize,
		TopK:            cfg.Knowledge.SearchTopK,
		Candidates:      cfg.Knowledge.Candidates,
		RerankMode:      cfg.Knowledge.RerankMode,
		RerankThreshold: cfg.Knowledge.RerankThreshold,
		EmbedModel:      cfg.Knowledge.EmbedModel,
	}, newQueryGenerator(cfg.Agent, httpClient))

	// Подключаем базу знаний к агенту для RAG-ответов.
	llmAgent.WithKnowledge(newKBRetriever(kbService))
	log.Printf("RAG: включён=%t, стратегия=%s, кандидаты=%d, top-K=%d, режим=%s, порог=%.2f",
		cfg.Agent.RAGEnabled, cfg.Agent.RAGStrategy, cfg.Agent.RAGCandidates, cfg.Agent.RAGTopK, cfg.Agent.RAGMode, cfg.Agent.RAGThreshold)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.HandleFunc("GET /api/mcp/status", handleMCPStatus(mcpRegistry))
	mux.HandleFunc("POST /api/mcp/{name}/disconnect", handleMCPDisconnect(mcpRegistry))
	mux.HandleFunc("POST /api/mcp/{name}/connect", handleMCPConnect("mcp.yaml", mcpRegistry))
	mux.HandleFunc("POST /api/chat", handleChat(llmAgent))
	mux.HandleFunc("POST /api/chat/clear", handleClearHistory(llmAgent))
	mux.HandleFunc("POST /api/session/facts", handleSessionFacts(llmAgent))
	mux.HandleFunc("POST /api/session/branches", handleSessionBranches(llmAgent))

	mux.HandleFunc("POST /api/kb/files", handleKBUpload(kbService, cfg.Knowledge.MaxUploadBytes))
	mux.HandleFunc("POST /api/kb/text", handleKBText(kbService))
	mux.HandleFunc("GET /api/kb/files", handleKBListFiles(kbService))
	mux.HandleFunc("POST /api/kb/files/delete", handleKBDeleteFile(kbService))
	mux.HandleFunc("POST /api/kb/index", handleKBIndex(kbService))
	mux.HandleFunc("POST /api/kb/search", handleKBSearch(kbService))
	mux.HandleFunc("GET /api/kb/metrics", handleKBMetrics(kbService))
	mux.HandleFunc("GET /api/kb/queries", handleKBListQueries(kbService))
	mux.HandleFunc("POST /api/kb/queries", handleKBAddQuery(kbService))
	mux.HandleFunc("POST /api/kb/queries/delete", handleKBDeleteQuery(kbService))
	mux.HandleFunc("POST /api/kb/queries/generate", handleKBGenerateQueries(kbService))
	mux.HandleFunc("POST /api/kb/benchmark", handleKBBenchmark(kbService))

	addr := ":" + cfg.Port
	log.Printf("Сервер запущен на http://localhost%s", addr)
	log.Printf("Внешний API: %s", cfg.Agent.ExternalAPI)
	log.Printf("Таймаут запросов к API: %s", cfg.Timeout)
	log.Printf("Формат API: %s, авторизация: %s", cfg.Agent.APIFormat, cfg.Agent.AuthType)
	if cfg.Agent.APIFormat == "openai" {
		logPrompts(cfg.Agent)
	}
	if err := http.ListenAndServe(addr, loggingMiddleware(mux)); err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}
