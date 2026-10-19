package pipeline

import (
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTrip(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	if _, ok := store.Get("1"); ok {
		t.Fatal("expected empty store")
	}

	if err := store.Save(Record{IssueID: "1", Title: "t", Status: StatusJiraCreated, JiraKey: "P-1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rec, ok := store.Get("1")
	if !ok {
		t.Fatal("record not found")
	}
	if rec.JiraKey != "P-1" || rec.Status != StatusJiraCreated {
		t.Fatalf("record = %+v", rec)
	}
	if rec.UpdatedAt == "" {
		t.Fatal("UpdatedAt must be set")
	}

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
}
