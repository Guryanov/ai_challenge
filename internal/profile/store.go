// Package profile реализует хранение профилей пользователей.
package profile

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Profile — профиль пользователя, участвующий в запросах к LLM.
type Profile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store описывает хранилище профилей пользователей.
type Store interface {
	Load(profileID string) (Profile, error)
	Save(profileID string, p Profile) error
	Delete(profileID string) error
	List() ([]Profile, error)
}

// FileStore сохраняет каждый профиль в отдельном JSON-файле.
type FileStore struct {
	baseDir string
	mu      sync.Mutex
	locks   map[string]*sync.Mutex
}

// NewFileStore создаёт файловое хранилище профилей в указанном каталоге.
func NewFileStore(baseDir string) (*FileStore, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("не удалось создать каталог профилей %q: %w", baseDir, err)
	}

	return &FileStore{
		baseDir: baseDir,
		locks:   make(map[string]*sync.Mutex),
	}, nil
}

// Load возвращает сохранённый профиль.
func (s *FileStore) Load(profileID string) (Profile, error) {
	if err := ValidateProfileID(profileID); err != nil {
		return Profile{}, err
	}

	path := s.profileFile(profileID)

	s.lock(profileID).Lock()
	defer s.unlock(profileID)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, nil
		}
		return Profile{}, fmt.Errorf("не удалось прочитать профиль %q: %w", profileID, err)
	}

	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return Profile{}, fmt.Errorf("не удалось распарсить профиль %q: %w", profileID, err)
	}

	return p, nil
}

// Save атомарно сохраняет профиль.
func (s *FileStore) Save(profileID string, p Profile) error {
	if err := ValidateProfileID(profileID); err != nil {
		return err
	}
	if p.Name == "" {
		return fmt.Errorf("имя профиля не может быть пустым")
	}

	now := time.Now().UTC()
	if p.ID == "" {
		p.ID = profileID
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("не удалось сериализовать профиль %q: %w", profileID, err)
	}

	path := s.profileFile(profileID)

	s.lock(profileID).Lock()
	defer s.unlock(profileID)

	tmp, err := os.CreateTemp(s.baseDir, ".profile-*.tmp")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл для профиля %q: %w", profileID, err)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось записать профиль %q: %w", profileID, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось закрыть временный файл профиля %q: %w", profileID, err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("не удалось сохранить профиль %q: %w", profileID, err)
	}

	return nil
}

// Delete удаляет файл профиля.
func (s *FileStore) Delete(profileID string) error {
	if err := ValidateProfileID(profileID); err != nil {
		return err
	}

	path := s.profileFile(profileID)

	s.lock(profileID).Lock()
	defer s.unlock(profileID)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("не удалось удалить профиль %q: %w", profileID, err)
	}

	return nil
}

// List возвращает список всех сохранённых профилей, отсортированный по имени.
func (s *FileStore) List() ([]Profile, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return []Profile{}, fmt.Errorf("не удалось прочитать каталог профилей: %w", err)
	}

	profiles := []Profile{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		profileID := strings.TrimSuffix(entry.Name(), ".json")
		// filename может содержать escape-последовательности.
		profileID, err := url.PathUnescape(profileID)
		if err != nil {
			continue
		}
		profileID = strings.ReplaceAll(profileID, "_", " ")

		p, err := s.Load(profileID)
		if err != nil {
			continue
		}
		if p.ID == "" {
			continue
		}
		profiles = append(profiles, p)
	}

	sort.Slice(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i].Name) < strings.ToLower(profiles[j].Name)
	})

	return profiles, nil
}

// ValidateProfileID проверяет, что идентификатор профиля безопасен.
func ValidateProfileID(profileID string) error {
	if strings.TrimSpace(profileID) == "" {
		return fmt.Errorf("profile_id не может быть пустым")
	}
	if strings.Contains(profileID, "..") {
		return fmt.Errorf("profile_id не может содержать '..'")
	}
	valid := regexp.MustCompile(`^[\p{L}\p{N}\s\-_]+$`)
	if !valid.MatchString(profileID) {
		return fmt.Errorf("profile_id содержит недопустимые символы")
	}
	return nil
}

// GenerateProfileID создаёт безопасный идентификатор профиля на основе имени.
func GenerateProfileID(name string) string {
	s := strings.TrimSpace(name)
	s = strings.ToLower(s)
	// Заменяем пробелы и спецсимволы на подчёркивания.
	re := regexp.MustCompile(`[^\p{L}\p{N}]+`)
	s = re.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = "profile"
	}
	return fmt.Sprintf("%s_%d", s, time.Now().UnixNano())
}

// profileFile возвращает безопасный путь к файлу профиля.
func (s *FileStore) profileFile(profileID string) string {
	filename := sanitizeProfileID(profileID) + ".json"
	return filepath.Join(s.baseDir, filename)
}

// sanitizeProfileID превращает profile_id в безопасное имя файла.
func sanitizeProfileID(profileID string) string {
	s := strings.TrimSpace(profileID)
	s = strings.ReplaceAll(s, " ", "_")
	s = url.PathEscape(s)
	return s
}

// lock возвращает мьютекс для указанного профиля, создавая его при необходимости.
func (s *FileStore) lock(profileID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.locks[profileID] == nil {
		s.locks[profileID] = &sync.Mutex{}
	}
	return s.locks[profileID]
}

// unlock освобождает мьютекс профиля.
func (s *FileStore) unlock(profileID string) {
	s.lock(profileID).Unlock()
}
