package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestHandleMCPDisconnect(t *testing.T) {
	registry := mcp.NewRegistry(t.Context(), mcp.Config{
		Servers: map[string]mcp.ServerConfig{
			"test": {Type: "stdio", Command: "echo", Disabled: true},
		},
	})
	defer registry.CloseAll()

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/test/disconnect", nil)
	req.SetPathValue("name", "test")
	rec := httptest.NewRecorder()

	handleMCPDisconnect(registry)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleMCPConnect(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "mcp.yaml")
	data := `
servers:
  test:
    type: stdio
    command: echo
    disabled: true
`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	registry := mcp.NewRegistry(t.Context(), mcp.Config{})
	defer registry.CloseAll()

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/test/connect", nil)
	req.SetPathValue("name", "test")
	rec := httptest.NewRecorder()

	handleMCPConnect(configPath, registry)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
