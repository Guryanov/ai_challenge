package main

import (
	"log"
	"net/http"

	"ai-chat/internal/agent"
	"ai-chat/internal/history"
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

	llmAgent := agent.NewSimpleAgent(cfg.Agent, httpClient).WithHistory(historyStore)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.HandleFunc("POST /api/chat", handleChat(llmAgent))
	mux.HandleFunc("POST /api/chat/clear", handleClearHistory(llmAgent))
	mux.HandleFunc("POST /api/session/facts", handleSessionFacts(llmAgent))
	mux.HandleFunc("POST /api/session/branches", handleSessionBranches(llmAgent))

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
