package chunk

import (
	"strings"
	"testing"
)

func TestFixedChunkerBasic(t *testing.T) {
	tests := []struct {
		name      string
		size      int
		overlap   int
		content   string
		wantCount int
	}{
		{"empty", 100, 0, "", 0},
		{"whitespace only", 100, 0, "   \n\t ", 0},
		{"short", 100, 0, "hello world", 1},
		{"exact fit", 5, 0, "abcde", 1},
		{"forced split", 5, 0, strings.Repeat("x", 12), 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewFixedChunker(tc.size, tc.overlap)
			chunks := c.Chunk(File{Path: "f.txt", Name: "f.txt", Content: tc.content})
			if len(chunks) != tc.wantCount {
				t.Fatalf("got %d chunks, want %d", len(chunks), tc.wantCount)
			}
			for _, ch := range chunks {
				if ch.Strategy != StrategyFixed {
					t.Errorf("strategy = %q", ch.Strategy)
				}
				if ch.CharCount > tc.size && tc.size > 0 {
					t.Errorf("chunk of %d runes exceeds size %d", ch.CharCount, tc.size)
				}
				if ch.Section != "f.txt" {
					t.Errorf("section = %q, want file name", ch.Section)
				}
			}
		})
	}
}

func TestFixedChunkerOverlapAndProgress(t *testing.T) {
	content := "word " + strings.Repeat("a", 50) + " end"
	c := NewFixedChunker(20, 5)
	chunks := c.Chunk(File{Path: "f", Name: "f", Content: content})
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	// Индексы должны строго возрастать и быть уникальными.
	seen := map[int]bool{}
	for _, ch := range chunks {
		if seen[ch.Index] {
			t.Fatalf("duplicate index %d", ch.Index)
		}
		seen[ch.Index] = true
		if ch.ChunkID == "" {
			t.Fatal("empty chunk id")
		}
	}
}

func TestFixedChunkerNoInfiniteLoop(t *testing.T) {
	c := NewFixedChunker(10, 3)
	chunks := c.Chunk(File{Path: "f", Name: "f", Content: strings.Repeat("z", 1000)})
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
}

func TestStructureChunkerHeadings(t *testing.T) {
	content := strings.Join([]string{
		"# A",
		"intro",
		"## B",
		"b content here",
		"### C",
		"c content here",
		"## D",
		"d content here",
	}, "\n")

	c := NewStructureChunker(1000, 0, 0)
	chunks := c.Chunk(File{Path: "doc.md", Name: "doc.md", Content: content})

	wantSections := []string{"A", "A > B", "A > B > C", "A > D"}
	if len(chunks) != len(wantSections) {
		t.Fatalf("got %d chunks, want %d: %+v", len(chunks), len(wantSections), chunks)
	}
	for i, want := range wantSections {
		if chunks[i].Section != want {
			t.Errorf("chunk %d section = %q, want %q", i, chunks[i].Section, want)
		}
		if chunks[i].Strategy != StrategyStructure {
			t.Errorf("chunk %d strategy = %q", i, chunks[i].Strategy)
		}
	}
}

func TestStructureChunkerIgnoresCodeFence(t *testing.T) {
	content := strings.Join([]string{
		"# Title",
		"```",
		"# not a heading",
		"```",
		"body text",
	}, "\n")

	c := NewStructureChunker(1000, 0, 0)
	chunks := c.Chunk(File{Path: "doc.md", Name: "doc.md", Content: content})
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1: %+v", len(chunks), chunks)
	}
	if chunks[0].Section != "Title" {
		t.Errorf("section = %q", chunks[0].Section)
	}
	if !strings.Contains(chunks[0].Text, "# not a heading") {
		t.Errorf("code fence content lost: %q", chunks[0].Text)
	}
}

func TestStructureChunkerParagraphFallback(t *testing.T) {
	content := "first paragraph\n\nsecond paragraph\n\nthird paragraph"
	c := NewStructureChunker(1000, 0, 0)
	chunks := c.Chunk(File{Path: "notes.txt", Name: "notes.txt", Content: content})
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3: %+v", len(chunks), chunks)
	}
	for _, ch := range chunks {
		if ch.Section != "notes.txt" {
			t.Errorf("section = %q, want file name", ch.Section)
		}
	}
}

func TestStructureChunkerMergesSmallSections(t *testing.T) {
	content := "# Overview\nThis parent section has enough characters to stay separate.\n\n## S1\naa\n\n## S2\nbb\n"
	c := NewStructureChunker(1000, 10, 0)
	chunks := c.Chunk(File{Path: "doc.md", Name: "doc.md", Content: content})
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 (small siblings merged): %+v", len(chunks), chunks)
	}
	if chunks[0].Section != "Overview" || chunks[1].Section != "Overview" {
		t.Fatalf("unexpected sections: %q, %q", chunks[0].Section, chunks[1].Section)
	}
}

func TestStructureChunkerSplitsOversizedSection(t *testing.T) {
	big := strings.Repeat("lorem ipsum ", 20)
	content := "# Big\n" + big
	c := NewStructureChunker(50, 0, 0)
	chunks := c.Chunk(File{Path: "doc.md", Name: "doc.md", Content: content})
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}
	for _, ch := range chunks {
		if ch.Section != "Big" {
			t.Errorf("section = %q, want Big", ch.Section)
		}
		if ch.Strategy != StrategyStructure {
			t.Errorf("strategy = %q", ch.Strategy)
		}
	}
}

func TestMakeChunkIDStable(t *testing.T) {
	a := MakeChunkID("fixed", "a.txt", 0, "hello")
	b := MakeChunkID("fixed", "a.txt", 0, "hello")
	c := MakeChunkID("fixed", "a.txt", 1, "hello")
	if a != b {
		t.Fatal("chunk id should be stable")
	}
	if a == c {
		t.Fatal("chunk id should depend on index")
	}
}
