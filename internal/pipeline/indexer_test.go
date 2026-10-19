package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ai-chat/internal/knowledge"
)

type fakeIndexer struct {
	uploaded map[string]string
	indexed  bool
}

func (f *fakeIndexer) UploadFile(_ context.Context, _, path, content string) (knowledge.File, error) {
	if f.uploaded == nil {
		f.uploaded = map[string]string{}
	}
	f.uploaded[path] = content
	return knowledge.File{Path: path}, nil
}

func (f *fakeIndexer) Index(_ context.Context, projectID, strategy string) ([]knowledge.IndexRun, error) {
	f.indexed = true
	return []knowledge.IndexRun{{ProjectID: projectID, Strategy: strategy}}, nil
}

func TestIndexerFiltersFiles(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, data []byte) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	write("main.go", []byte("package main\n"))
	write("README.md", []byte("# Readme\n"))
	write("image.bin", []byte{0, 1, 2, 3})
	write("bad.go", []byte("package bad\x00\x01"))
	write(filepath.Join("data", "skip.go"), []byte("package data\n"))
	write("mcp.yaml", []byte("servers: {}\n"))
	write(".env.secrets", []byte("API_KEY=secret\n"))

	svc := &fakeIndexer{}
	ix := NewIndexer(svc, root, "repo", []string{".go", ".md"}, 256)

	stats, err := ix.Index(context.Background())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	if _, ok := svc.uploaded["main.go"]; !ok {
		t.Fatalf("main.go not uploaded: %+v", svc.uploaded)
	}
	if _, ok := svc.uploaded["README.md"]; !ok {
		t.Fatalf("README.md not uploaded: %+v", svc.uploaded)
	}
	if _, ok := svc.uploaded["image.bin"]; ok {
		t.Fatal("image.bin must be skipped")
	}
	if _, ok := svc.uploaded["bad.go"]; ok {
		t.Fatal("non-UTF8 bad.go must be skipped")
	}
	if _, ok := svc.uploaded[filepath.ToSlash(filepath.Join("data", "skip.go"))]; ok {
		t.Fatal("data/ dir must be skipped")
	}
	if _, ok := svc.uploaded["mcp.yaml"]; ok {
		t.Fatal("mcp.yaml (secrets) must be skipped")
	}
	if _, ok := svc.uploaded[".env.secrets"]; ok {
		t.Fatal(".env.secrets must be skipped")
	}
	if stats.Uploaded != 2 {
		t.Fatalf("uploaded = %d, want 2", stats.Uploaded)
	}
	if !svc.indexed {
		t.Fatal("Index must be called")
	}
}
