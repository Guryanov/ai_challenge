package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewFileStoreCreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewFileStore(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected store, got nil")
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("directory should exist: %v", err)
	}
}

func TestFileStoreLoadSave(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewFileStore(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mem := Memory{
		LongTerm: []Entry{
			{ID: "1", Type: "principle", Title: "SOLID", Content: "Принципы ООП"},
		},
		Working: []Entry{
			{ID: "2", Type: "task", Title: "Auth", Content: "JWT + refresh"},
		},
	}

	if err := store.Save("test project", mem); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, err := store.Load("test project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(loaded.LongTerm) != 1 || loaded.LongTerm[0].Title != "SOLID" {
		t.Fatalf("unexpected long_term: %+v", loaded.LongTerm)
	}
	if len(loaded.Working) != 1 || loaded.Working[0].Title != "Auth" {
		t.Fatalf("unexpected working: %+v", loaded.Working)
	}
}

func TestFileStoreLoadMissingProject(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewFileStore(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mem, err := store.Load("missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mem.LongTerm) != 0 || len(mem.Working) != 0 {
		t.Fatalf("expected empty memory, got %+v", mem)
	}
}

func TestValidateProjectID(t *testing.T) {
	valid := []string{"my-project", "my project", "project_123", "Проект 1"}
	for _, id := range valid {
		if err := ValidateProjectID(id); err != nil {
			t.Errorf("expected %q to be valid: %v", id, err)
		}
	}

	invalid := []string{"", "../etc", "a/b", "a\\b", "a:b", "a..b"}
	for _, id := range invalid {
		if err := ValidateProjectID(id); err == nil {
			t.Errorf("expected %q to be invalid", id)
		}
	}
}

func TestUpsertEntryCreatesAndUpdates(t *testing.T) {
	mem := Memory{}

	e1 := Entry{Type: "principle", Title: "SOLID"}
	if err := UpsertEntry(&mem, e1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mem.LongTerm) != 1 {
		t.Fatalf("expected 1 long_term entry, got %d", len(mem.LongTerm))
	}

	id := mem.LongTerm[0].ID
	if id == "" {
		t.Fatal("expected generated id")
	}

	updated := Entry{ID: id, Type: "principle", Title: "SOLID обновлённый", Content: "test"}
	if err := UpsertEntry(&mem, updated); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mem.LongTerm) != 1 {
		t.Fatalf("expected 1 long_term entry after update, got %d", len(mem.LongTerm))
	}
	if mem.LongTerm[0].Title != "SOLID обновлённый" {
		t.Fatalf("expected updated title, got %q", mem.LongTerm[0].Title)
	}
}

func TestUpsertEntryMovesBetweenLevels(t *testing.T) {
	mem := Memory{}

	if err := UpsertEntry(&mem, Entry{Type: "task", Title: "T1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id := mem.Working[0].ID

	if err := UpsertEntry(&mem, Entry{ID: id, Type: "knowledge", Title: "T1 moved"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mem.Working) != 0 {
		t.Fatalf("expected working to be empty, got %d", len(mem.Working))
	}
	if len(mem.LongTerm) != 1 {
		t.Fatalf("expected 1 long_term entry, got %d", len(mem.LongTerm))
	}
}

func TestUpsertEntryUnknownType(t *testing.T) {
	mem := Memory{}
	if err := UpsertEntry(&mem, Entry{Type: "unknown", Title: "x"}); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestDeleteEntry(t *testing.T) {
	mem := Memory{
		LongTerm: []Entry{{ID: "1", Type: "principle", Title: "SOLID"}},
		Working:  []Entry{{ID: "2", Type: "task", Title: "Auth"}},
	}

	if err := DeleteEntry(&mem, "2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mem.Working) != 0 {
		t.Fatalf("expected working to be empty, got %d", len(mem.Working))
	}
	if len(mem.LongTerm) != 1 {
		t.Fatalf("expected long_term to remain, got %d", len(mem.LongTerm))
	}

	if err := DeleteEntry(&mem, "missing"); err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestOldestLongTerm(t *testing.T) {
	now := time.Now().UTC()
	mem := Memory{}
	for i := 0; i < 22; i++ {
		mem.LongTerm = append(mem.LongTerm, Entry{
			ID:        string(rune('a' + i)),
			Type:      "knowledge",
			Title:     "Title",
			CreatedAt: now.Add(time.Duration(i) * time.Hour),
		})
	}

	oldest, remaining := OldestLongTerm(mem)
	if len(oldest) != 2 {
		t.Fatalf("expected 2 oldest entries, got %d", len(oldest))
	}
	if len(remaining) != 20 {
		t.Fatalf("expected 20 remaining entries, got %d", len(remaining))
	}

	if oldest[0].CreatedAt != now {
		t.Fatalf("expected oldest to be %v, got %v", now, oldest[0].CreatedAt)
	}
}

func TestProjectFileSanitization(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewFileStore(tmp)

	id := "test project"
	if err := store.Save(id, Memory{LongTerm: []Entry{{Type: "principle", Title: "x"}}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Пробелы должны быть заменены на подчёркивания и файл не должен содержать /.
	expected := filepath.Join(tmp, "test_project.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected file %q to exist: %v", expected, err)
	}
}
