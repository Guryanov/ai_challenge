// Package mcp реализует обёртку над MCP SDK для stdio- и SSE-серверов.
package mcp

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ServerConfig описывает один MCP-сервер.
type ServerConfig struct {
	Type     string            `yaml:"type,omitempty"`
	URL      string            `yaml:"url,omitempty"`
	Command  string            `yaml:"command,omitempty"`
	Args     []string          `yaml:"args,omitempty"`
	Env      map[string]string `yaml:"env,omitempty"`
	Headers  map[string]string `yaml:"headers,omitempty"`
	Disabled bool              `yaml:"disabled,omitempty"`
	Timeout  time.Duration     `yaml:"timeout,omitempty"`
}

// Config — корневая структура mcp.yaml.
type Config struct {
	Servers map[string]ServerConfig `yaml:"servers,omitempty"`
}

// LoadConfig загружает конфигурацию MCP-серверов из файла.
// Если файл не существует, возвращается пустая конфигурация без ошибки.
func LoadConfig(path string) (Config, error) {
	var cfg Config

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("не удалось прочитать %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("не удалось распарсить %s: %w", path, err)
	}

	if cfg.Servers == nil {
		cfg.Servers = make(map[string]ServerConfig)
	}

	for name, s := range cfg.Servers {
		if s.Type == "" {
			s.Type = "stdio"
		}
		if s.Timeout == 0 {
			s.Timeout = 30 * time.Second
		}
		cfg.Servers[name] = s
	}

	return cfg, nil
}
