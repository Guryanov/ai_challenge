package chunk

import "strings"

// FixedChunker режет текст на чанки фиксированного размера (в символах)
// с перекрытием, стараясь не разрывать слова.
type FixedChunker struct {
	Size    int
	Overlap int
}

// NewFixedChunker создаёт fixed-стратегию с разумными значениями по умолчанию.
func NewFixedChunker(size, overlap int) *FixedChunker {
	if size <= 0 {
		size = 1000
	}
	if overlap < 0 || overlap >= size {
		overlap = 0
	}
	return &FixedChunker{Size: size, Overlap: overlap}
}

// Name возвращает имя стратегии.
func (c *FixedChunker) Name() string { return StrategyFixed }

// Chunk разбивает файл на чанки фиксированного размера.
func (c *FixedChunker) Chunk(file File) []Chunk {
	runes := []rune(file.Content)
	if len(runes) == 0 {
		return nil
	}
	return c.split(StrategyFixed, runes, file, file.Name, 1, 0)
}

// split нарезает runes на чанки фиксированного размера.
// sectionName попадает в метаданные, startLine — номер первой строки текста,
// startIndex — начальный порядковый номер чанка.
func (c *FixedChunker) split(strategy string, runes []rune, file File, sectionName string, startLine, startIndex int) []Chunk {
	if len(runes) == 0 {
		return nil
	}

	lineOf := buildLineIndex(runes)
	var chunks []Chunk
	start := 0
	index := startIndex

	for start < len(runes) {
		end := start + c.Size
		if end > len(runes) {
			end = len(runes)
		}

		cut := end
		if end < len(runes) {
			cut = lastBreak(runes, start, end)
		}
		if cut <= start {
			cut = end
		}

		text := strings.TrimSpace(string(runes[start:cut]))
		if text != "" {
			chunks = append(chunks, newChunk(
				strategy, file, sectionName, index, text,
				startLine+lineOf[start]-1, startLine+lineOf[cut-1]-1,
			))
			index++
		}

		if cut >= len(runes) {
			break
		}

		next := cut - c.Overlap
		if next <= start {
			next = cut
		}
		start = next
	}

	return chunks
}

// lastBreak ищет позицию разреза по границе слова/строки в диапазоне [start, end).
// Возвращает позицию сразу после разделителя или end, если разделителя нет.
func lastBreak(runes []rune, start, end int) int {
	for i := end - 1; i > start; i-- {
		switch runes[i] {
		case '\n':
			return i + 1
		case ' ', '\t':
			return i + 1
		}
	}
	return end
}

// buildLineIndex возвращает для каждого символа номер его строки (1-based).
func buildLineIndex(runes []rune) []int {
	lineOf := make([]int, len(runes))
	line := 1
	for i, r := range runes {
		lineOf[i] = line
		if r == '\n' {
			line++
		}
	}
	return lineOf
}
