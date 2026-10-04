// Package chunk разбивает текстовые файлы на чанки двумя стратегиями:
// по фиксированному размеру и по структуре (заголовки/разделы).
package chunk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"unicode/utf8"
)

// Стратегии разбиения.
const (
	StrategyFixed     = "fixed"
	StrategyStructure = "structure"
)

// File — входной текстовый файл.
type File struct {
	Path    string
	Name    string
	Content string
}

// Chunk — один фрагмент файла с метаданными.
type Chunk struct {
	Strategy      string
	Path          string
	Name          string
	Section       string
	ChunkID       string
	Index         int
	Text          string
	CharCount     int
	TokenEstimate int
	StartLine     int
	EndLine       int
}

// Chunker — стратегия разбиения.
type Chunker interface {
	Name() string
	Chunk(file File) []Chunk
}

// TokenEstimate оценивает число токенов по эвристике ~4 символа на токен.
func TokenEstimate(text string) int {
	count := utf8.RuneCountInString(text)
	if count == 0 {
		return 0
	}
	return (count + 3) / 4
}

// MakeChunkID строит стабильный идентификатор чанка.
func MakeChunkID(strategy, path string, index int, text string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d|%s", strategy, path, index, text)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// newChunk заполняет производные поля чанка.
func newChunk(strategy string, file File, section string, index int, text string, startLine, endLine int) Chunk {
	return Chunk{
		Strategy:      strategy,
		Path:          file.Path,
		Name:          file.Name,
		Section:       section,
		ChunkID:       MakeChunkID(strategy, file.Path, index, text),
		Index:         index,
		Text:          text,
		CharCount:     utf8.RuneCountInString(text),
		TokenEstimate: TokenEstimate(text),
		StartLine:     startLine,
		EndLine:       endLine,
	}
}
