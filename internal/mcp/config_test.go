package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "mcp.yaml")

	data := `
servers:
  filesystem:
    command: uvx
    args:
      - "-y"
      - "@modelcontextprotocol/server-filesystem"
  deepwiki:
    type: sse
    url: https://mcp.deepwiki.com/mcp
    headers:
      Accept: text/event-stream
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fs, ok := cfg.Servers["filesystem"]
	if !ok {
		t.Fatal("expected filesystem server")
	}
	if fs.Type != "stdio" {
		t.Fatalf("expected default type stdio, got %q", fs.Type)
	}
	if fs.Timeout == 0 {
		t.Fatal("expected default timeout")
	}

	dw, ok := cfg.Servers["deepwiki"]
	if !ok {
		t.Fatal("expected deepwiki server")
	}
	if dw.Type != "sse" {
		t.Fatalf("expected type sse, got %q", dw.Type)
	}
	if dw.URL != "https://mcp.deepwiki.com/mcp" {
		t.Fatalf("unexpected url: %q", dw.URL)
	}
	if dw.Headers["Accept"] != "text/event-stream" {
		t.Fatalf("unexpected headers: %v", dw.Headers)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("expected empty config, got %d servers", len(cfg.Servers))
	}
}
