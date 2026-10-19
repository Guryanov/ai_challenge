package memory

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// InvariantsEntryType — тип записи в долгосрочной памяти, хранящей YAML-список инвариантов.
const InvariantsEntryType = "invariants_list"

// Invariant описывает жёсткое правило проекта, которое агент не должен нарушать.
type Invariant struct {
	Title       string `yaml:"title" json:"title"`
	Description string `yaml:"description" json:"description"`
	Category    string `yaml:"category" json:"category"`
	Severity    string `yaml:"severity" json:"severity"`
	Active      bool   `yaml:"active" json:"active"`
}

// invariantList — корневой элемент YAML-контента записи инвариантов.
type invariantList struct {
	Invariants []Invariant `yaml:"invariants" json:"invariants"`
}

var validCategories = map[string]bool{
	"architecture":       true,
	"technical_decision": true,
	"stack":              true,
	"business_rule":      true,
}

var validSeverities = map[string]bool{
	"hard": true,
	"soft": true,
}

// ValidateInvariant проверяет корректность одного инварианта.
func ValidateInvariant(inv Invariant) error {
	if strings.TrimSpace(inv.Title) == "" {
		return fmt.Errorf("заголовок инварианта не может быть пустым")
	}
	if !validCategories[inv.Category] {
		return fmt.Errorf("недопустимая категория %q", inv.Category)
	}
	if !validSeverities[inv.Severity] {
		return fmt.Errorf("недопустимая жёсткость %q", inv.Severity)
	}
	return nil
}

// LoadInvariants загружает активные инварианты проекта из хранилища памяти.
func LoadInvariants(store Store, projectID string) ([]Invariant, error) {
	if store == nil || projectID == "" {
		return nil, nil
	}

	mem, err := store.Load(projectID)
	if err != nil {
		return nil, err
	}

	for _, e := range mem.LongTerm {
		if e.Type != InvariantsEntryType {
			continue
		}

		list, err := parseInvariantsYAML(e.Content)
		if err != nil {
			return nil, fmt.Errorf("невалидный YAML инвариантов: %w", err)
		}

		var active []Invariant
		for _, inv := range list.Invariants {
			if inv.Active {
				active = append(active, inv)
			}
		}
		return active, nil
	}

	return nil, nil
}

// SaveInvariants сохраняет список инвариантов проекта как одну запись в долгосрочной памяти.
func SaveInvariants(store Store, projectID string, invariants []Invariant) error {
	if store == nil {
		return fmt.Errorf("хранилище памяти не инициализировано")
	}
	if projectID == "" {
		return fmt.Errorf("project_id не может быть пустым")
	}

	for i, inv := range invariants {
		if err := ValidateInvariant(inv); err != nil {
			return fmt.Errorf("инвариант %d: %w", i+1, err)
		}
	}

	mem, err := store.Load(projectID)
	if err != nil {
		return err
	}

	// Удаляем существующую запись инвариантов, чтобы всегда хранить ровно одну.
	filtered := mem.LongTerm[:0]
	for _, e := range mem.LongTerm {
		if e.Type != InvariantsEntryType {
			filtered = append(filtered, e)
		}
	}
	mem.LongTerm = filtered

	content, err := marshalInvariantsYAML(invariantList{Invariants: invariants})
	if err != nil {
		return fmt.Errorf("ошибка сериализации инвариантов: %w", err)
	}

	entry := Entry{
		Type:    InvariantsEntryType,
		Title:   "Инварианты проекта",
		Content: content,
	}
	if err := UpsertEntry(&mem, entry); err != nil {
		return fmt.Errorf("ошибка сохранения записи инвариантов: %w", err)
	}

	return store.Save(projectID, mem)
}

// DeleteInvariants удаляет запись с инвариантами проекта.
func DeleteInvariants(store Store, projectID string) error {
	if store == nil {
		return fmt.Errorf("хранилище памяти не инициализировано")
	}
	if projectID == "" {
		return fmt.Errorf("project_id не может быть пустым")
	}

	mem, err := store.Load(projectID)
	if err != nil {
		return err
	}

	filtered := mem.LongTerm[:0]
	for _, e := range mem.LongTerm {
		if e.Type != InvariantsEntryType {
			filtered = append(filtered, e)
		}
	}
	mem.LongTerm = filtered

	return store.Save(projectID, mem)
}

func parseInvariantsYAML(content string) (invariantList, error) {
	var list invariantList
	if strings.TrimSpace(content) == "" {
		return list, nil
	}
	if err := yaml.Unmarshal([]byte(content), &list); err != nil {
		return list, err
	}
	return list, nil
}

func marshalInvariantsYAML(list invariantList) (string, error) {
	bytes, err := yaml.Marshal(list)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
