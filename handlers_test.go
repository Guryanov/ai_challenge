package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-chat/internal/mcp"
)

func TestHandleMCPStatus(t *testing.T) {
	registry := &mcp.Registry{}
	// Registry internals are unexported, but we can verify the endpoint shape
	// by using the public constructor with an empty config.
	registry = mcp.NewRegistry(t.Context(), mcp.Config{})
	defer registry.CloseAll()

	req := httptest.NewRequest(http.MethodGet, "/api/mcp/status", nil)
	rec := httptest.NewRecorder()

	handleMCPStatus(registry)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp struct {
		Servers []mcp.ServerStatus `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Servers == nil {
		t.Fatal("expected servers slice, got nil")
	}
}
