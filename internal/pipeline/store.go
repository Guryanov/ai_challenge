package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store — хранилище состояния обработанных issue.
type Store interface {
	Get(issueID string) (Record, bool)
	Save(rec Record) error
	List() []Record
}

// FileStore — файловая реализация Store (один JSON-файл, атомарная запись).
type FileStore struct {
	path string
	mu   sync.Mutex
}

// stateFile — сериализованное состояние.
type stateFile struct {
	Records map[string]Record `json:"records"`
}

// NewFileStore создаёт файловое хранилище состояния по указанному пути.
func NewFileStore(path string) (*FileStore, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("не удалось создать каталог состояния конвейера %q: %w", dir, err)
		}
	}
	return &FileStore{path: path}, nil
}

func (s *FileStore) load() (stateFile, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return stateFile{Records: map[string]Record{}}, nil
		}
		return stateFile{}, fmt.Errorf("не удалось прочитать состояние конвейера: %w", err)
	}

	var st stateFile
	if err := json.Unmarshal(data, &st); err != nil {
		return stateFile{}, fmt.Errorf("не удалось распарсить состояние конвейера: %w", err)
	}
	if st.Records == nil {
		st.Records = map[string]Record{}
	}
	return st, nil
}

// Get возвращает запись по идентификатору issue.
func (s *FileStore) Get(issueID string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.load()
	if err != nil {
		return Record{}, false
	}
	rec, ok := st.Records[issueID]
	return rec, ok
}

// Save создаёт или перезаписывает запись.
func (s *FileStore) Save(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.load()
	if err != nil {
		return err
	}
	rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	st.Records[rec.IssueID] = rec
	return s.persist(st)
}

// List возвращает все записи (сначала недавно обновлённые).
func (s *FileStore) List() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.load()
	if err != nil {
		return nil
	}

	out := make([]Record, 0, len(st.Records))
	for _, rec := range st.Records {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

func (s *FileStore) persist(st stateFile) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("не удалось сериализовать состояние конвейера: %w", err)
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".pipeline-*.tmp")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл состояния: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось записать состояние конвейера: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось закрыть временный файл состояния: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось сохранить состояние конвейера: %w", err)
	}
	return nil
}
