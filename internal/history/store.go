// Package history реализует хранение истории сообщений диалога.
package history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Message — одно сообщение в истории диалога.
type Message struct {
	Role             string    `json:"role"`
	Content          string    `json:"content"`
	IsSummary        bool      `json:"is_summary,omitempty"`
	PromptTokens     int       `json:"prompt_tokens,omitempty"`
	CompletionTokens int       `json:"completion_tokens,omitempty"`
	TotalTokens      int       `json:"total_tokens,omitempty"`
	Timestamp        time.Time `json:"timestamp,omitempty"`
}

// Session — состояние диалога одной сессии.
type Session struct {
	Compressed  bool      `json:"compressed"`
	TotalTokens int       `json:"total_tokens"`
	Messages    []Message `json:"messages"`
}

// Store описывает хранилище истории сообщений.
type Store interface {
	LoadSession(sessionID string) (Session, error)
	SaveSession(sessionID string, session Session) error
	Delete(sessionID string) error
}

// FileStore сохраняет историю каждой сессии в отдельном JSON-файле.
type FileStore struct {
	baseDir string
	mu      sync.Mutex
	locks   map[string]*sync.Mutex
}

// NewFileStore создаёт файловое хранилище истории в указанном каталоге.
func NewFileStore(baseDir string) (*FileStore, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("не удалось создать каталог истории %q: %w", baseDir, err)
	}

	return &FileStore{
		baseDir: baseDir,
		locks:   make(map[string]*sync.Mutex),
	}, nil
}

// LoadSession возвращает сохранённую сессию.
// Поддерживает старый формат (JSON-массив сообщений) и новый формат (объект Session).
func (s *FileStore) LoadSession(sessionID string) (Session, error) {
	path := s.sessionFile(sessionID)

	s.lock(sessionID).Lock()
	defer s.unlock(sessionID)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Session{}, nil
		}
		return Session{}, fmt.Errorf("не удалось прочитать историю сессии %q: %w", sessionID, err)
	}

	// Старый формат — просто массив сообщений.
	if len(data) > 0 && bytes.TrimSpace(data)[0] == '[' {
		var messages []Message
		if err := json.Unmarshal(data, &messages); err != nil {
			return Session{}, fmt.Errorf("не удалось распарсить историю сессии %q: %w", sessionID, err)
		}
		return Session{Messages: messages}, nil
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, fmt.Errorf("не удалось распарсить историю сессии %q: %w", sessionID, err)
	}

	return session, nil
}

// SaveSession атомарно сохраняет сессию.
func (s *FileStore) SaveSession(sessionID string, session Session) error {
	now := time.Now().UTC()
	for i := range session.Messages {
		if session.Messages[i].Timestamp.IsZero() {
			session.Messages[i].Timestamp = now
		}
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("не удалось сериализовать историю сессии %q: %w", sessionID, err)
	}

	path := s.sessionFile(sessionID)

	s.lock(sessionID).Lock()
	defer s.unlock(sessionID)

	tmp, err := os.CreateTemp(s.baseDir, ".history-*.tmp")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл для истории %q: %w", sessionID, err)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось записать историю сессии %q: %w", sessionID, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось закрыть временный файл истории %q: %w", sessionID, err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось сохранить историю сессии %q: %w", sessionID, err)
	}

	return nil
}

// Delete удаляет файл истории сессии.
func (s *FileStore) Delete(sessionID string) error {
	path := s.sessionFile(sessionID)

	s.lock(sessionID).Lock()
	defer s.unlock(sessionID)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("не удалось удалить историю сессии %q: %w", sessionID, err)
	}

	return nil
}

// sessionFile возвращает безопасный путь к файлу истории сессии.
func (s *FileStore) sessionFile(sessionID string) string {
	// url.PathEscape защищает от спецсимволов и path traversal.
	filename := url.PathEscape(sessionID) + ".json"
	return filepath.Join(s.baseDir, filename)
}

// lock возвращает мьютекс для указанной сессии, создавая его при необходимости.
func (s *FileStore) lock(sessionID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.locks[sessionID] == nil {
		s.locks[sessionID] = &sync.Mutex{}
	}
	return s.locks[sessionID]
}

// unlock освобождает мьютекс сессии.
func (s *FileStore) unlock(sessionID string) {
	s.lock(sessionID).Unlock()
}
