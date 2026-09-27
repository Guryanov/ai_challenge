// Package digest реализует хранение ежедневной сводки.
package digest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Digest — ежедневная сводка, сгенерированная агентом.
type Digest struct {
	Content     string    `json:"content"`
	GeneratedAt time.Time `json:"generated_at"`
	Error       string    `json:"error,omitempty"`
}

// Store описывает хранилище сводки.
type Store interface {
	Load() (Digest, error)
	Save(d Digest) error
}

// FileStore сохраняет сводку в JSON-файле.
type FileStore struct {
	baseDir string
	mu      sync.RWMutex
}

// NewFileStore создаёт файловое хранилище сводки.
func NewFileStore(baseDir string) (*FileStore, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("не удалось создать каталог сводки %q: %w", baseDir, err)
	}

	return &FileStore{
		baseDir: baseDir,
	}, nil
}

// Load возвращает сохранённую сводку.
// Если файл не существует, возвращает пустую сводку без ошибки.
func (s *FileStore) Load() (Digest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.digestFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Digest{}, nil
		}
		return Digest{}, fmt.Errorf("не удалось прочитать сводку: %w", err)
	}

	var d Digest
	if err := json.Unmarshal(data, &d); err != nil {
		return Digest{}, fmt.Errorf("не удалось распарсить сводку: %w", err)
	}

	return d, nil
}

// Save атомарно сохраняет сводку.
func (s *FileStore) Save(d Digest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if d.GeneratedAt.IsZero() {
		d.GeneratedAt = time.Now().UTC()
	}

	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("не удалось сериализовать сводку: %w", err)
	}

	path := s.digestFile()
	tmp, err := os.CreateTemp(s.baseDir, ".digest-*.tmp")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл сводки: %w", err)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось записать сводку: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось закрыть временный файл сводки: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось сохранить сводку: %w", err)
	}

	return nil
}

// digestFile возвращает путь к файлу сводки.
func (s *FileStore) digestFile() string {
	return filepath.Join(s.baseDir, "digest.json")
}
