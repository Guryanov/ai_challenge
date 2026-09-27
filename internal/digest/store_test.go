package digest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load on empty store failed: %v", err)
	}
	if loaded.Content != "" || !loaded.GeneratedAt.IsZero() {
		t.Fatalf("expected empty digest, got %+v", loaded)
	}

	d := Digest{
		Content:     "Today's top movie is Example.",
		GeneratedAt: time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC),
	}
	if err := store.Save(d); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err = store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Content != d.Content {
		t.Errorf("content mismatch: got %q, want %q", loaded.Content, d.Content)
	}
	if !loaded.GeneratedAt.Equal(d.GeneratedAt) {
		t.Errorf("generated_at mismatch: got %v, want %v", loaded.GeneratedAt, d.GeneratedAt)
	}

	path := filepath.Join(dir, "digest.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("digest file not created: %v", err)
	}
}

func TestFileStore_SaveError(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	// Блокируем возможность записи, создав файл с правами только на чтение.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod failed: %v", err)
	}
	defer os.Chmod(dir, 0o755)

	if err := store.Save(Digest{Content: "test"}); err == nil {
		t.Fatalf("expected error saving to read-only dir")
	}
}

func TestFileStore_LoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	path := filepath.Join(dir, "digest.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if _, err := store.Load(); err == nil {
		t.Fatalf("expected error loading invalid JSON")
	}
}
