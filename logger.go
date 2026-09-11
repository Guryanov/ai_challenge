package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"ai-chat/internal/agent"
)

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.statusCode, time.Since(start))
	})
}

func logPrompts(cfg agent.Config) {
	parts := []string{fmt.Sprintf("model=%s", cfg.Model)}
	if cfg.SystemPrompt != "" {
		parts = append(parts, "system_prompt=set")
	}
	if cfg.OrchestratorInstructions != "" {
		parts = append(parts, "orchestrator=set")
	}
	if len(cfg.Roles) > 0 {
		parts = append(parts, fmt.Sprintf("roles=%d", len(cfg.Roles)))
	}
	if cfg.AssistantPrompt != "" {
		parts = append(parts, "assistant_prompt=set")
	}
	if cfg.UserPromptTemplate != "" {
		parts = append(parts, "user_prompt_template=set")
	}
	log.Printf("Промпты: %s", strings.Join(parts, ", "))
}
