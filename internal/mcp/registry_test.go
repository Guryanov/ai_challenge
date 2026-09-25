package mcp

import (
	"testing"

	"ai-chat/internal/agent"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestPrefixedToolName(t *testing.T) {
	cases := []struct {
		server, tool, full string
	}{
		{"filesystem", "read_file", "filesystem_read_file"},
		{"my-server", "tool", "my-server_tool"},
	}

	for _, c := range cases {
		full := prefixedToolName(c.server, c.tool)
		if full != c.full {
			t.Fatalf("prefixedToolName(%q, %q) = %q, want %q", c.server, c.tool, full, c.full)
		}

		server, tool, err := parsePrefixedToolName(full)
		if err != nil {
			t.Fatalf("parsePrefixedToolName(%q) error: %v", full, err)
		}
		if server != c.server || tool != c.tool {
			t.Fatalf("parsePrefixedToolName(%q) = (%q, %q), want (%q, %q)", full, server, tool, c.server, c.tool)
		}
	}
}

func TestParsePrefixedToolNameInvalid(t *testing.T) {
	_, _, err := parsePrefixedToolName("toolname")
	if err == nil {
		t.Fatal("expected error for invalid tool name")
	}
}

func TestToolInputSchemaToMap(t *testing.T) {
	schema := mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{
			"city": map[string]any{"type": "string"},
		},
		Required: []string{"city"},
	}

	m := toolInputSchemaToMap(schema)
	if m["type"] != "object" {
		t.Fatalf("unexpected type: %v", m["type"])
	}
}

func TestRegistryDefinitionsReturnsEmptySlice(t *testing.T) {
	r := &Registry{
		clients:  map[string]*serverClient{},
		statuses: map[string]*ServerStatus{},
	}

	defs := r.Definitions()
	if defs == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(defs) != 0 {
		t.Fatalf("expected 0 definitions without servers, got %d", len(defs))
	}
}

func TestRegistryStatusReturnsStoredStatuses(t *testing.T) {
	r := &Registry{
		clients: map[string]*serverClient{},
		statuses: map[string]*ServerStatus{
			"fs": {
				Name:      "fs",
				Connected: true,
				Tools: []ToolStatus{
					{Name: "read_file", Description: "read"},
				},
			},
			"broken": {
				Name:      "broken",
				Connected: false,
				Error:     "exec: not found",
			},
			"disabled": {
				Name:      "disabled",
				Disabled:  true,
				Connected: false,
			},
		},
	}

	statuses := r.Status()
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	byName := make(map[string]ServerStatus)
	for _, s := range statuses {
		byName[s.Name] = s
	}

	if !byName["fs"].Connected || len(byName["fs"].Tools) != 1 {
		t.Fatalf("unexpected fs status: %+v", byName["fs"])
	}
	if byName["broken"].Connected || byName["broken"].Error != "exec: not found" {
		t.Fatalf("unexpected broken status: %+v", byName["broken"])
	}
	if !byName["disabled"].Disabled {
		t.Fatalf("unexpected disabled status: %+v", byName["disabled"])
	}
}

func TestNewRegistryTracksDisabledServer(t *testing.T) {
	cfg := Config{
		Servers: map[string]ServerConfig{
			"disabled": {
				Command:  "echo",
				Disabled: true,
			},
		},
	}

	r := NewRegistry(t.Context(), cfg)
	defer r.CloseAll()

	statuses := r.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if !statuses[0].Disabled {
		t.Fatalf("expected disabled status, got %+v", statuses[0])
	}
}

func TestToOpenAIToolDefinition(t *testing.T) {
	def := agent.ToolDefinition{
		Name:        "fs_read",
		Description: "read file",
		Parameters: map[string]any{
			"type": "object",
		},
	}

	full := prefixedToolName("fs", "read")
	if full != "fs_read" {
		t.Fatalf("unexpected full name: %q", full)
	}

	server, tool, err := parsePrefixedToolName(full)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if server != "fs" || tool != "read" {
		t.Fatalf("unexpected parse result: %q, %q", server, tool)
	}

	_ = def
}
