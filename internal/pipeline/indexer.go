package pipeline

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"ai-chat/internal/knowledge"
)

// KBIndexer — интерфейс сервиса базы знаний для индексации кода.
type KBIndexer interface {
	UploadFile(ctx context.Context, projectID, filePath, content string) (knowledge.File, error)
	Index(ctx context.Context, projectID, strategy string) ([]knowledge.IndexRun, error)
}

// IndexStats — результат индексации кода проекта.
type IndexStats struct {
	Uploaded int `json:"uploaded"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

// skipDirs — служебные каталоги, которые не индексируются.
var skipDirs = map[string]bool{
	".git":          true,
	"data":          true,
	"history":       true,
	"vendor":        true,
	"node_modules":  true,
	".kilo":         true,
	".venv-litellm": true,
}

// skipFiles — файлы с секретами/локальные, которые не индексируются.
var skipFiles = map[string]bool{
	"mcp.yaml":           true,
	"litellm-keys.local": true,
}

// isSecretFile сообщает, что файл не должен попадать в базу знаний.
func isSecretFile(name string) bool {
	if skipFiles[name] {
		return true
	}
	return strings.HasPrefix(name, ".env")
}

// Indexer индексирует локальный код проекта в базу знаний.
type Indexer struct {
	service    KBIndexer
	root       string
	projectID  string
	extensions map[string]bool
	maxBytes   int64
}

// NewIndexer создаёт индексатор кода проекта.
func NewIndexer(service KBIndexer, root, projectID string, extensions []string, maxFileKB int) *Indexer {
	exts := make(map[string]bool, len(extensions))
	for _, e := range extensions {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		exts[e] = true
	}

	var maxBytes int64
	if maxFileKB > 0 {
		maxBytes = int64(maxFileKB) * 1024
	}

	return &Indexer{
		service:    service,
		root:       root,
		projectID:  projectID,
		extensions: exts,
		maxBytes:   maxBytes,
	}
}

// Index обходит локальный чекаут, заливает подходящие файлы в базу знаний
// и индексирует их. Повторный вызов идемпотентен: неизменённые чанки не
// пересчитываются (кэш эмбеддингов по хэшу).
func (ix *Indexer) Index(ctx context.Context) (IndexStats, error) {
	var stats IndexStats
	if ix.service == nil {
		return stats, fmt.Errorf("сервис базы знаний недоступен")
	}
	if strings.TrimSpace(ix.projectID) == "" {
		return stats, fmt.Errorf("project_id для индексации кода не задан")
	}

	root := ix.root
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if isSecretFile(d.Name()) {
			stats.Skipped++
			return nil
		}
		if !ix.extensions[strings.ToLower(filepath.Ext(path))] {
			stats.Skipped++
			return nil
		}

		info, err := d.Info()
		if err != nil {
			stats.Skipped++
			return nil
		}
		if ix.maxBytes > 0 && info.Size() > ix.maxBytes {
			stats.Skipped++
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			stats.Failed++
			return nil
		}
		if !isTextFile(data) {
			stats.Skipped++
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		if _, err := ix.service.UploadFile(ctx, ix.projectID, rel, string(data)); err != nil {
			log.Printf("[pipeline] индекс: не удалось залить %s: %v", rel, err)
			stats.Failed++
			return nil
		}
		stats.Uploaded++
		return nil
	})
	if walkErr != nil {
		return stats, fmt.Errorf("обход каталога %q: %w", root, walkErr)
	}

	if stats.Uploaded == 0 {
		log.Printf("[pipeline] индекс: подходящих файлов не найдено (root=%s)", root)
		return stats, nil
	}

	if _, err := ix.service.Index(ctx, ix.projectID, "structure"); err != nil {
		return stats, fmt.Errorf("индексация кода: %w", err)
	}

	log.Printf("[pipeline] индекс: проект %s, файлов %d, пропущено %d, ошибок %d",
		ix.projectID, stats.Uploaded, stats.Skipped, stats.Failed)
	return stats, nil
}

// isTextFile грубо определяет, что файл — корректный UTF-8 текст без NUL.
func isTextFile(data []byte) bool {
	if len(data) == 0 || !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}
