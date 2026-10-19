package main

import (
	"context"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"ai-chat/internal/agent"
	"ai-chat/internal/embed"
	"ai-chat/internal/history"
	"ai-chat/internal/knowledge"
	"ai-chat/internal/mcp"
	"ai-chat/internal/pipeline"
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

	// Конвейер Sentry → Jira → фикс → PR.
	if err := validatePipeline(cfg.Pipeline); err != nil {
		log.Fatalf("Ошибка конфигурации конвейера: %v", err)
	}

	pipelineStore, err := pipeline.NewFileStore(filepath.Join("data", "pipeline", "state.json"))
	if err != nil {
		log.Fatalf("Ошибка создания хранилища состояния конвейера: %v", err)
	}

	orchestrator := pipeline.NewOrchestrator(
		cfg.Pipeline,
		mcpRegistry,
		mcpCatalog{registry: mcpRegistry},
		llmAgent,
		newKBRetriever(kbService),
		pipelineStore,
	)

	repoIndexer := pipeline.NewIndexer(kbService, cfg.Pipeline.Root, cfg.Pipeline.ProjectID, cfg.Pipeline.IndexExtensions, cfg.Pipeline.IndexMaxFileKB)
	if cfg.Pipeline.IndexOnStart {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			if _, err := repoIndexer.Index(ctx); err != nil {
				log.Printf("[pipeline] индексация кода при старте не удалась: %v", err)
			}
		}()
	}

	pipelineScheduler := pipeline.NewScheduler(orchestrator, cfg.Pipeline.At, cfg.Pipeline.Enabled)
	pipelineScheduler.Start()
	defer pipelineScheduler.Stop()
	log.Printf("Конвейер: включён=%t, расписание=%s, проект RAG=%s", cfg.Pipeline.Enabled, cfg.Pipeline.At, cfg.Pipeline.ProjectID)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.HandleFunc("GET /api/mcp/status", handleMCPStatus(mcpRegistry))
	mux.HandleFunc("POST /api/mcp/{name}/disconnect", handleMCPDisconnect(mcpRegistry))
	mux.HandleFunc("POST /api/mcp/{name}/connect", handleMCPConnect("mcp.yaml", mcpRegistry))
	mux.HandleFunc("POST /api/chat", handleChat(llmAgent))
	mux.HandleFunc("POST /api/chat/clear", handleClearHistory(llmAgent))
	mux.HandleFunc("POST /api/session/facts", handleSessionFacts(llmAgent))
	mux.HandleFunc("POST /api/session/branches", handleSessionBranches(llmAgent))
	mux.HandleFunc("POST /api/pipeline/run", handlePipelineRun(orchestrator))
	mux.HandleFunc("GET /api/pipeline/status", handlePipelineStatus(orchestrator))

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

// mcpCatalog адаптирует реестр MCP под pipeline.ToolCatalog.
type mcpCatalog struct {
	registry *mcp.Registry
}

// Catalog перечисляет инструменты подключённых MCP-серверов.
func (c mcpCatalog) Catalog() []pipeline.ServerTool {
	if c.registry == nil {
		return nil
	}

	statuses := c.registry.Status()
	out := make([]pipeline.ServerTool, 0, len(statuses))
	for _, s := range statuses {
		tools := make([]string, 0, len(s.Tools))
		for _, t := range s.Tools {
			tools = append(tools, t.Name)
		}
		out = append(out, pipeline.ServerTool{Server: s.Name, Tools: tools})
	}
	return out
}
