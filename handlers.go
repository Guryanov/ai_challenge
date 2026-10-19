package main

import (
	_ "embed"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"ai-chat/internal/agent"
	"ai-chat/internal/knowledge"
	"ai-chat/internal/mcp"
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

func handleMCPStatus(registry *mcp.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		status := registry.Status()
		resp := map[string]any{
			"servers": status,
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("Ошибка кодирования статуса MCP: %v", err)
		}
	}
}

func handleMCPDisconnect(registry *mcp.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "имя сервера не указано")
			return
		}

		if err := registry.Disconnect(name); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
			log.Printf("Ошибка кодирования ответа отключения MCP: %v", err)
		}
	}
}

func handleMCPConnect(configPath string, registry *mcp.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "имя сервера не указано")
			return
		}

		cfg, err := mcp.LoadConfig(configPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "не удалось прочитать mcp.yaml: "+err.Error())
			return
		}

		sc, ok := cfg.Servers[name]
		if !ok {
			writeError(w, http.StatusNotFound, "сервер не найден в mcp.yaml")
			return
		}

		if err := registry.Connect(r.Context(), name, sc); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
			log.Printf("Ошибка кодирования ответа подключения MCP: %v", err)
		}
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
			Context:         r.Context(),
			RAGEnabled:      req.RAGEnabled,
			RAGStrategy:     req.RAGStrategy,
			RAGTopK:         req.RAGTopK,
			RAGCandidates:   req.RAGCandidates,
			RAGThreshold:    req.RAGThreshold,
		})
		if err != nil {
			log.Printf("Ошибка агента: %v", err)
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}

		resp := chatResponse{
			User:                req.Message,
			Response:            strings.TrimSpace(result.Content) + agent.FormatSourcesBlock(result.Sources),
			FinishReason:        result.FinishReason,
			DurationMs:          result.Duration.Milliseconds(),
			PromptTokens:        result.PromptTokens,
			CompletionTokens:    result.CompletionTokens,
			TotalTokens:         result.TotalTokens,
			SessionTotalTokens:  result.SessionTotalTokens,
			Compressed:          result.Compressed,
			ActiveBranch:        result.ActiveBranch,
			Branches:            result.Branches,
			Facts:               result.Facts,
			ProjectID:           result.ProjectID,
			Sources:             result.Sources,
			RAGUsed:             result.RAGUsed,
			ActiveGitHubProject: result.ActiveGitHubProject,
			ActiveGitBranch:     result.ActiveGitBranch,
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

// writeChatResponse формирует chatResponse из AgentResponse.
func writeChatResponse(w http.ResponseWriter, userMessage string, result agent.AgentResponse) {
	resp := chatResponse{
		User:                userMessage,
		Response:            strings.TrimSpace(result.Content) + agent.FormatSourcesBlock(result.Sources),
		FinishReason:        result.FinishReason,
		DurationMs:          result.Duration.Milliseconds(),
		PromptTokens:        result.PromptTokens,
		CompletionTokens:    result.CompletionTokens,
		TotalTokens:         result.TotalTokens,
		SessionTotalTokens:  result.SessionTotalTokens,
		Compressed:          result.Compressed,
		ActiveBranch:        result.ActiveBranch,
		Branches:            result.Branches,
		Facts:               result.Facts,
		ProjectID:           result.ProjectID,
		Sources:             result.Sources,
		RAGUsed:             result.RAGUsed,
		ActiveGitHubProject: result.ActiveGitHubProject,
		ActiveGitBranch:     result.ActiveGitBranch,
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Ошибка кодирования ответа: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(chatResponse{Error: message}); err != nil {
		log.Printf("Ошибка кодирования ошибки: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("Ошибка кодирования ответа: %v", err)
	}
}

// --- База знаний ---

func kbFiles(files []knowledge.File) []kbFile {
	out := make([]kbFile, 0, len(files))
	for _, f := range files {
		out = append(out, kbFile{
			Path:      f.Path,
			Name:      f.Name,
			SizeBytes: f.SizeBytes,
			SHA256:    f.ContentSHA256,
			Indexed:   f.IndexedAt != "",
			UpdatedAt: f.UpdatedAt,
		})
	}
	return out
}

func handleKBUpload(service *knowledge.Service, maxBytes int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		if maxBytes > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "Не удалось прочитать форму или превышен размер: "+err.Error())
			return
		}
		defer r.MultipartForm.RemoveAll()

		projectID := r.FormValue("project_id")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "project_id обязателен")
			return
		}

		headers := r.MultipartForm.File["files"]
		if len(headers) == 0 {
			writeError(w, http.StatusBadRequest, "не передано ни одного файла")
			return
		}

		for _, header := range headers {
			file, err := header.Open()
			if err != nil {
				writeError(w, http.StatusBadRequest, "не удалось открыть файл: "+err.Error())
				return
			}
			content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
			file.Close()
			if err != nil {
				writeError(w, http.StatusBadRequest, "не удалось прочитать файл: "+err.Error())
				return
			}
			if maxBytes > 0 && int64(len(content)) > maxBytes {
				writeError(w, http.StatusRequestEntityTooLarge, "файл превышает допустимый размер")
				return
			}

			if _, err := service.UploadBytes(r.Context(), projectID, header.Filename, content); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		files, err := service.ListFiles(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, kbFilesResponse{Files: kbFiles(files)})
	}
}

func handleKBText(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req struct {
			ProjectID string `json:"project_id"`
			Path      string `json:"path"`
			Content   string `json:"content"`
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
		if req.Path == "" {
			writeError(w, http.StatusBadRequest, "path обязателен")
			return
		}
		if _, err := service.UploadFile(r.Context(), req.ProjectID, req.Path, req.Content); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		files, err := service.ListFiles(r.Context(), req.ProjectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, kbFilesResponse{Files: kbFiles(files)})
	}
}

func handleKBListFiles(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		projectID := r.URL.Query().Get("project_id")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "project_id обязателен")
			return
		}
		files, err := service.ListFiles(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, kbFilesResponse{Files: kbFiles(files)})
	}
}

func handleKBDeleteFile(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req struct {
			ProjectID string `json:"project_id"`
			Path      string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		if err := service.DeleteFile(r.Context(), req.ProjectID, req.Path); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		files, err := service.ListFiles(r.Context(), req.ProjectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, kbFilesResponse{Files: kbFiles(files)})
	}
}

func handleKBIndex(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req kbIndexRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		if req.ProjectID == "" {
			writeError(w, http.StatusBadRequest, "project_id обязателен")
			return
		}

		runs, err := service.Index(r.Context(), req.ProjectID, req.Strategy)
		if err != nil {
			log.Printf("Ошибка индексации базы знаний: %v", err)
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, kbIndexResponse{Runs: runs})
	}
}

func handleKBSearch(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req kbSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		if req.ProjectID == "" {
			writeError(w, http.StatusBadRequest, "project_id обязателен")
			return
		}

		scored, err := service.Retrieve(r.Context(), req.ProjectID, knowledge.RetrieveOptions{
			Strategy:   req.Strategy,
			Query:      req.Query,
			Candidates: req.Candidates,
			TopK:       req.K,
			Mode:       req.Mode,
			Threshold:  req.Threshold,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		results := make([]kbSearchResult, 0, len(scored))
		for _, sc := range scored {
			results = append(results, kbSearchResult{
				Path:    sc.Path,
				Name:    sc.Name,
				Section: sc.Section,
				ChunkID: sc.ChunkID,
				Score:   sc.Score,
				Text:    sc.Text,
			})
		}
		writeJSON(w, http.StatusOK, kbSearchResponse{Results: results})
	}
}

func handleKBMetrics(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		projectID := r.URL.Query().Get("project_id")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "project_id обязателен")
			return
		}
		metrics, err := service.GetMetrics(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, metrics)
	}
}

func handleKBListQueries(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		projectID := r.URL.Query().Get("project_id")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "project_id обязателен")
			return
		}
		queries, err := service.ListQueries(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if queries == nil {
			queries = []knowledge.Query{}
		}
		writeJSON(w, http.StatusOK, kbQueriesResponse{Queries: queries})
	}
}

func handleKBAddQuery(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req kbQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		if _, err := service.AddQuery(r.Context(), knowledge.Query{
			ProjectID:       req.ProjectID,
			Text:            req.Text,
			ExpectedPath:    req.ExpectedPath,
			ExpectedSection: req.ExpectedSection,
			Source:          "manual",
		}); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		queries, err := service.ListQueries(r.Context(), req.ProjectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, kbQueriesResponse{Queries: queries})
	}
}

func handleKBDeleteQuery(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req struct {
			ProjectID string `json:"project_id"`
			ID        int64  `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		if err := service.DeleteQuery(r.Context(), req.ProjectID, req.ID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		queries, err := service.ListQueries(r.Context(), req.ProjectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, kbQueriesResponse{Queries: queries})
	}
}

func handleKBGenerateQueries(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req kbGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		generated, err := service.GenerateQueries(r.Context(), req.ProjectID, req.Strategy, req.N)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}

		queries, err := service.ListQueries(r.Context(), req.ProjectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"generated": generated, "queries": queries})
	}
}

func handleKBBenchmark(service *knowledge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var req kbBenchmarkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Неверный формат запроса")
			return
		}
		defer r.Body.Close()

		runs, err := service.Benchmark(r.Context(), req.ProjectID, knowledge.RetrieveOptions{
			TopK:       req.K,
			Candidates: req.Candidates,
			Mode:       req.Mode,
			Threshold:  req.Threshold,
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, kbBenchmarkResponse{Runs: runs})
	}
}
