package memory

import (
	"strings"
	"testing"
)

func TestValidateInvariant(t *testing.T) {
	valid := Invariant{
		Title:       "Только PostgreSQL",
		Description: "Основное хранилище — PostgreSQL",
		Category:    "stack",
		Severity:    "hard",
		Active:      true,
	}
	if err := ValidateInvariant(valid); err != nil {
		t.Fatalf("expected valid invariant, got error: %v", err)
	}

	cases := []struct {
		name      string
		invariant Invariant
		wantErr   string
	}{
		{
			name:      "empty title",
			invariant: Invariant{Title: "  ", Category: "stack", Severity: "hard"},
			wantErr:   "заголовок",
		},
		{
			name:      "invalid category",
			invariant: Invariant{Title: "X", Category: "unknown", Severity: "hard"},
			wantErr:   "категория",
		},
		{
			name:      "invalid severity",
			invariant: Invariant{Title: "X", Category: "stack", Severity: "critical"},
			wantErr:   "жёсткость",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateInvariant(tc.invariant)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error to contain %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestLoadInvariantsEmpty(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewFileStore(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invariants, err := LoadInvariants(store, "proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(invariants) != 0 {
		t.Fatalf("expected no invariants, got %d", len(invariants))
	}
}

func TestSaveAndLoadInvariants(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewFileStore(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invariants := []Invariant{
		{Title: "PostgreSQL", Category: "stack", Severity: "hard", Active: true},
		{Title: "Микросервисы", Category: "architecture", Severity: "hard", Active: true},
		{Title: "Устаревшее правило", Category: "business_rule", Severity: "soft", Active: false},
	}

	if err := SaveInvariants(store, "my project", invariants); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, err := LoadInvariants(store, "my project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 active invariants, got %d", len(loaded))
	}
	if loaded[0].Title != "PostgreSQL" || loaded[1].Title != "Микросервисы" {
		t.Fatalf("unexpected active invariants: %+v", loaded)
	}

	// Проверяем, что запись появилась в памяти проекта.
	mem, err := store.Load("my project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, e := range mem.LongTerm {
		if e.Type == InvariantsEntryType {
			found = true
			if e.Title == "" {
				t.Fatal("expected non-empty title for invariants entry")
			}
		}
	}
	if !found {
		t.Fatal("expected invariants_list entry in long_term memory")
	}
}

func TestSaveInvariantsReplacesExisting(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewFileStore(tmp)

	if err := SaveInvariants(store, "proj", []Invariant{
		{Title: "A", Category: "stack", Severity: "hard", Active: true},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := SaveInvariants(store, "proj", []Invariant{
		{Title: "B", Category: "architecture", Severity: "soft", Active: true},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mem, _ := store.Load("proj")
	count := 0
	for _, e := range mem.LongTerm {
		if e.Type == InvariantsEntryType {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one invariants entry, got %d", count)
	}

	loaded, _ := LoadInvariants(store, "proj")
	if len(loaded) != 1 || loaded[0].Title != "B" {
		t.Fatalf("unexpected loaded invariants: %+v", loaded)
	}
}

func TestDeleteInvariants(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewFileStore(tmp)

	if err := SaveInvariants(store, "proj", []Invariant{
		{Title: "A", Category: "stack", Severity: "hard", Active: true},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := DeleteInvariants(store, "proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, _ := LoadInvariants(store, "proj")
	if len(loaded) != 0 {
		t.Fatalf("expected no invariants after delete, got %d", len(loaded))
	}
}
