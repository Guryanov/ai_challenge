package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"

	"ai-chat/internal/agent"
	"ai-chat/internal/memory"
)

//go:embed static/index.html
var indexHTML string

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(indexHTML)); err != nil {
		log.Printf("Ошибка записи ответа: %v", err)
	}
}

func handleChat(a agent.Agent) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		if req.Message == "" {
			writeError(w, http.StatusBadRequest, "Сообщение не может быть пустым")
			return
		}

		result, err := a.Run(agent.AgentRequest{
			Message:         req.Message,
			ResponseFormat:  req.ResponseFormat,
			Role:            req.Role,
			SessionID:       req.SessionID,
			ProjectID:       req.ProjectID,
			ContextStrategy: agent.ContextStrategy(req.ContextStrategy),
			Facts:           req.Facts,
			BranchAction:    req.BranchAction,
			Temperature:     req.Temperature,
			MaxTokens:       req.MaxTokens,
			StopSequence:    req.StopSequence,
		})
		if err != nil {
			log.Printf("Ошибка агента: %v", err)
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}

		resp := chatResponse{
			User:               req.Message,
			Response:           result.Content,
			FinishReason:       result.FinishReason,
			DurationMs:         result.Duration.Milliseconds(),
			PromptTokens:       result.PromptTokens,
			CompletionTokens:   result.CompletionTokens,
			TotalTokens:        result.TotalTokens,
			SessionTotalTokens: result.SessionTotalTokens,
			Compressed:         result.Compressed,
			ActiveBranch:       result.ActiveBranch,
			Branches:           result.Branches,
			Facts:              result.Facts,
			ProjectID:          result.ProjectID,
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("Ошибка кодирования ответа: %v", err)
		}
	}
}

func handleClearHistory(a agent.Agent) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		if err := a.ClearHistory(req.SessionID); err != nil {
			log.Printf("Ошибка очистки истории: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
			log.Printf("Ошибка кодирования ответа: %v", err)
		}
	}
}

func handleSessionFacts(a agent.Agent) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req struct {
			SessionID       string            `json:"session_id"`
			ContextStrategy string            `json:"context_strategy"`
			Facts           map[string]string `json:"facts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		result, err := a.Manage(agent.AgentRequest{
			SessionID:       req.SessionID,
			ContextStrategy: agent.ContextStrategy(req.ContextStrategy),
			Facts:           req.Facts,
		})
		if err != nil {
			log.Printf("Ошибка управления сессией: %v", err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(chatResponse{
			ActiveBranch: result.ActiveBranch,
			Branches:     result.Branches,
			Facts:        result.Facts,
		}); err != nil {
			log.Printf("Ошибка кодирования ответа: %v", err)
		}
	}
}

func handleSessionBranches(a agent.Agent) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req struct {
			SessionID    string `json:"session_id"`
			BranchAction string `json:"branch_action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		result, err := a.Manage(agent.AgentRequest{
			SessionID:    req.SessionID,
			BranchAction: req.BranchAction,
		})
		if err != nil {
			log.Printf("Ошибка управления веткой: %v", err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(chatResponse{
			ActiveBranch: result.ActiveBranch,
			Branches:     result.Branches,
			Facts:        result.Facts,
		}); err != nil {
			log.Printf("Ошибка кодирования ответа: %v", err)
		}
	}
}

func handleGetMemory(store memory.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		projectID := r.URL.Query().Get("project_id")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "project_id обязателен")
			return
		}

		mem, err := store.Load(projectID)
		if err != nil {
			log.Printf("Ошибка загрузки памяти: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(mem); err != nil {
			log.Printf("Ошибка кодирования ответа: %v", err)
		}
	}
}

// memorySummarizer запускает авто-summary долгосрочной памяти при необходимости.
type memorySummarizer interface {
	SummarizeMemoryIfNeeded(projectID string) (memory.Memory, error)
}

func handleUpsertMemory(store memory.Store, summarizer memorySummarizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req struct {
			ProjectID string       `json:"project_id"`
			Entry     memory.Entry `json:"entry"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		if req.ProjectID == "" {
			writeError(w, http.StatusBadRequest, "project_id обязателен")
			return
		}

		mem, err := store.Load(req.ProjectID)
		if err != nil {
			log.Printf("Ошибка загрузки памяти: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if err := memory.UpsertEntry(&mem, req.Entry); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if err := store.Save(req.ProjectID, mem); err != nil {
			log.Printf("Ошибка сохранения памяти: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// После сохранения проверяем необходимость авто-summary.
		mem, err = summarizer.SummarizeMemoryIfNeeded(req.ProjectID)
		if err != nil {
			log.Printf("Ошибка авто-summary памяти: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(mem); err != nil {
			log.Printf("Ошибка кодирования ответа: %v", err)
		}
	}
}

func handleDeleteMemory(store memory.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req struct {
			ProjectID string `json:"project_id"`
			EntryID   string `json:"entry_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		if req.ProjectID == "" {
			writeError(w, http.StatusBadRequest, "project_id обязателен")
			return
		}
		if req.EntryID == "" {
			writeError(w, http.StatusBadRequest, "entry_id обязателен")
			return
		}

		mem, err := store.Load(req.ProjectID)
		if err != nil {
			log.Printf("Ошибка загрузки памяти: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if err := memory.DeleteEntry(&mem, req.EntryID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if err := store.Save(req.ProjectID, mem); err != nil {
			log.Printf("Ошибка сохранения памяти: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(mem); err != nil {
			log.Printf("Ошибка кодирования ответа: %v", err)
		}
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(chatResponse{Error: message}); err != nil {
		log.Printf("Ошибка кодирования ошибки: %v", err)
	}
}
