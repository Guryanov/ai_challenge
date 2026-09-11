package main

import (
	"log"
	"net/http"

	"ai-chat/internal/agent"
)

func main() {
	cfg := loadConfig()

	httpClient := agent.NewHTTPClient(
		cfg.Agent.ExternalAPI,
		cfg.Agent.APIKey,
		cfg.Agent.AuthType,
		cfg.Timeout,
	)
	llmAgent := agent.NewSimpleAgent(cfg.Agent, httpClient)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.HandleFunc("POST /api/chat", handleChat(llmAgent))

	addr := ":" + cfg.Port
	log.Printf("Сервер запущен на http://localhost%s", addr)
	log.Printf("Внешний API: %s", cfg.Agent.ExternalAPI)
	log.Printf("Формат API: %s, авторизация: %s", cfg.Agent.APIFormat, cfg.Agent.AuthType)
	if cfg.Agent.APIFormat == "openai" {
		logPrompts(cfg.Agent)
	}
	if err := http.ListenAndServe(addr, loggingMiddleware(mux)); err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}
