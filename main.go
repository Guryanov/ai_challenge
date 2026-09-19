package main

import (
	"log"
	"net/http"

	"ai-chat/internal/agent"
	"ai-chat/internal/history"
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

	llmAgent := agent.NewSimpleAgent(cfg.Agent, httpClient).
		WithHistory(historyStore).
		WithMemory(memoryStore).
		WithProfile(profileStore)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.HandleFunc("POST /api/chat", handleChat(llmAgent))
	mux.HandleFunc("POST /api/chat/clear", handleClearHistory(llmAgent))
	mux.HandleFunc("POST /api/session/facts", handleSessionFacts(llmAgent))
	mux.HandleFunc("POST /api/session/branches", handleSessionBranches(llmAgent))
	mux.HandleFunc("GET /api/memory", handleGetMemory(memoryStore))
	mux.HandleFunc("POST /api/memory", handleUpsertMemory(memoryStore, llmAgent))
	mux.HandleFunc("POST /api/memory/delete", handleDeleteMemory(memoryStore))
	mux.HandleFunc("GET /api/profiles", handleListProfiles(profileStore))
	mux.HandleFunc("POST /api/profiles", handleUpsertProfile(profileStore))
	mux.HandleFunc("POST /api/profiles/delete", handleDeleteProfile(profileStore))
	mux.HandleFunc("POST /api/task/approve", handleTaskApprove(llmAgent))
	mux.HandleFunc("POST /api/task/reject", handleTaskReject(llmAgent))
	mux.HandleFunc("POST /api/task/cancel", handleTaskCancel(llmAgent))

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
