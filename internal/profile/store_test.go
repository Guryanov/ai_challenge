package profile

import (
	"os"
	"path/filepath"
	"testing"
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

func TestFileStoreSaveLoad(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewFileStore(tmp)

	p := Profile{Name: "Разработчик", Description: "Опытный Go-разработчик"}
	profileID := GenerateProfileID(p.Name)
	if err := store.Save(profileID, p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, err := store.Load(profileID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded.Name != "Разработчик" {
		t.Fatalf("unexpected name: %q", loaded.Name)
	}
	if loaded.Description != "Опытный Go-разработчик" {
		t.Fatalf("unexpected description: %q", loaded.Description)
	}
	if loaded.ID == "" {
		t.Fatal("expected generated id")
	}
}

func TestFileStoreList(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewFileStore(tmp)

	if err := store.Save("profile_a", Profile{Name: "Алиса", Description: "Аналитик"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := store.Save("profile_b", Profile{Name: "Боб", Description: "Разработчик"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	profiles, err := store.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].Name != "Алиса" || profiles[1].Name != "Боб" {
		t.Fatalf("unexpected order: %+v", profiles)
	}
}

func TestFileStoreDelete(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewFileStore(tmp)

	if err := store.Save("to_delete", Profile{Name: "Temp"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := store.Delete("to_delete"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	profiles, _ := store.List()
	if len(profiles) != 0 {
		t.Fatalf("expected 0 profiles, got %d", len(profiles))
	}
}

func TestValidateProfileID(t *testing.T) {
	valid := []string{"my-profile", "my profile", "profile_123", "Профиль 1"}
	for _, id := range valid {
		if err := ValidateProfileID(id); err != nil {
			t.Errorf("expected %q to be valid: %v", id, err)
		}
	}

	invalid := []string{"", "../etc", "a/b", "a\\b", "a:b", "a..b"}
	for _, id := range invalid {
		if err := ValidateProfileID(id); err == nil {
			t.Errorf("expected %q to be invalid", id)
		}
	}
}

func TestGenerateProfileID(t *testing.T) {
	id1 := GenerateProfileID("My Profile")
	id2 := GenerateProfileID("My Profile")
	if id1 == id2 {
		t.Fatal("generated ids should be unique")
	}
	if ValidateProfileID(id1) != nil {
		t.Fatalf("generated id %q should be valid", id1)
	}
}

func TestProfileFileSanitization(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewFileStore(tmp)

	id := "test profile"
	if err := store.Save(id, Profile{Name: "Test"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := filepath.Join(tmp, "test_profile.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected file %q to exist: %v", expected, err)
	}
}
