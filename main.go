package main

import (
	"context"
	"log"
	"net/http"

	"ai-chat/internal/agent"
	"ai-chat/internal/digest"
	"ai-chat/internal/embed"
	"ai-chat/internal/history"
	"ai-chat/internal/knowledge"
	"ai-chat/internal/mcp"
	"ai-chat/internal/memory"
	"ai-chat/internal/profile"
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

	memoryStore, err := memory.NewFileStore("memory")
	if err != nil {
		log.Fatalf("Ошибка создания хранилища памяти: %v", err)
	}

	profileStore, err := profile.NewFileStore("profiles")
	if err != nil {
		log.Fatalf("Ошибка создания хранилища профилей: %v", err)
	}

	mcpRegistry := mcp.NewRegistry(context.Background(), cfg.MCP)
	defer func() {
		if err := mcpRegistry.CloseAll(); err != nil {
			log.Printf("Ошибка закрытия MCP-серверов: %v", err)
		}
	}()

	llmAgent := agent.NewSimpleAgent(cfg.Agent, httpClient).
		WithHistory(historyStore).
		WithMemory(memoryStore).
		WithProfile(profileStore).
		WithTools(mcpRegistry)

	digestStore, err := digest.NewFileStore("digest")
	if err != nil {
		log.Fatalf("Ошибка создания хранилища сводки: %v", err)
	}

	digestScheduler := digest.NewScheduler(llmAgent, digestStore, cfg.DigestPrompt, cfg.DigestAt, cfg.DigestEnabled)
	digestScheduler.Start()
	defer digestScheduler.Stop()

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
	mux.HandleFunc("GET /api/digest", handleDailyDigest(digestStore))
	mux.HandleFunc("GET /api/mcp/status", handleMCPStatus(mcpRegistry))
	mux.HandleFunc("POST /api/mcp/{name}/disconnect", handleMCPDisconnect(mcpRegistry))
	mux.HandleFunc("POST /api/mcp/{name}/connect", handleMCPConnect("mcp.yaml", mcpRegistry))
	mux.HandleFunc("POST /api/chat", handleChat(llmAgent))
	mux.HandleFunc("POST /api/chat/clear", handleClearHistory(llmAgent))
	mux.HandleFunc("POST /api/session/facts", handleSessionFacts(llmAgent))
	mux.HandleFunc("POST /api/session/branches", handleSessionBranches(llmAgent))
	mux.HandleFunc("GET /api/memory", handleGetMemory(memoryStore))
	mux.HandleFunc("POST /api/memory", handleUpsertMemory(memoryStore, llmAgent))
	mux.HandleFunc("POST /api/memory/delete", handleDeleteMemory(memoryStore))
	mux.HandleFunc("GET /api/invariants", handleListInvariants(memoryStore))
	mux.HandleFunc("POST /api/invariants", handleSaveInvariants(memoryStore))
	mux.HandleFunc("POST /api/invariants/delete", handleDeleteInvariants(memoryStore))
	mux.HandleFunc("GET /api/profiles", handleListProfiles(profileStore))
	mux.HandleFunc("POST /api/profiles", handleUpsertProfile(profileStore))
	mux.HandleFunc("POST /api/profiles/delete", handleDeleteProfile(profileStore))
	mux.HandleFunc("POST /api/task/approve", handleTaskApprove(llmAgent))
	mux.HandleFunc("POST /api/task/reject", handleTaskReject(llmAgent))
	mux.HandleFunc("POST /api/task/cancel", handleTaskCancel(llmAgent))

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
