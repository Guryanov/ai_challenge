package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"ai-chat/internal/agent"
	"gopkg.in/yaml.v3"
)

const (
	defaultExternalAPI      = "https://httpbin.org/anything"
	defaultModel            = "SMLab-ext/Kimi-K2.7-Code"
	defaultTimeout          = 60 * time.Second
	defaultSummaryMaxTokens = 500
)

type serverConfig struct {
	Port    string
	Timeout time.Duration
	Agent   agent.Config
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func loadConfig() serverConfig {
	timeout := defaultTimeout
	if v := os.Getenv("TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		} else {
			log.Printf("Предупреждение: не удалось распарсить TIMEOUT=%q, используется значение по умолчанию %s", v, defaultTimeout)
		}
	}

	cfg := serverConfig{
		Port:    os.Getenv("PORT"),
		Timeout: timeout,
		Agent: agent.Config{
			ExternalAPI:              os.Getenv("EXTERNAL_API_URL"),
			APIKey:                   os.Getenv("API_KEY"),
			AuthType:                 strings.ToLower(os.Getenv("AUTH_TYPE")),
			APIFormat:                strings.ToLower(os.Getenv("API_FORMAT")),
			Model:                    getEnvOrDefault("MODEL", defaultModel),
			SystemPrompt:             os.Getenv("SYSTEM_PROMPT"),
			AssistantPrompt:          os.Getenv("ASSISTANT_PROMPT"),
			UserPromptTemplate:       os.Getenv("USER_PROMPT_TEMPLATE"),
			OrchestratorInstructions: loadOrchestratorInstructions(),
			Roles:                    loadRoles(),
			SummaryMaxTokens:           parseIntEnv("SUMMARY_MAX_TOKENS", defaultSummaryMaxTokens),
		},
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.Agent.ExternalAPI == "" {
		cfg.Agent.ExternalAPI = defaultExternalAPI
	}

	// Если задан API_KEY, но не указаны AUTH_TYPE и API_FORMAT,
	// используем OpenAI-совместимый формат с авторизацией Bearer.
	if cfg.Agent.APIKey != "" {
		if cfg.Agent.AuthType == "" {
			cfg.Agent.AuthType = "bearer"
		}
		if cfg.Agent.APIFormat == "" {
			cfg.Agent.APIFormat = "openai"
		}
	} else {
		if cfg.Agent.AuthType == "" {
			cfg.Agent.AuthType = "none"
		}
		if cfg.Agent.APIFormat == "" {
			cfg.Agent.APIFormat = "generic"
		}
	}

	return cfg
}

func parseIntEnv(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		log.Printf("Предупреждение: не удалось распарсить %s, используется значение по умолчанию %d", key, defaultValue)
	}
	return defaultValue
}

func loadOrchestratorInstructions() string {
	data, err := os.ReadFile("orchestrator.yaml")
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Предупреждение: не удалось прочитать orchestrator.yaml: %v", err)
		}
		return ""
	}

	var orchestrator struct {
		Instructions string `yaml:"instructions"`
	}
	if err := yaml.Unmarshal(data, &orchestrator); err != nil {
		log.Printf("Предупреждение: не удалось распарсить orchestrator.yaml: %v", err)
		return ""
	}

	return strings.TrimSpace(orchestrator.Instructions)
}

func loadRoles() map[string]string {
	data, err := os.ReadFile("roles.yaml")
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Предупреждение: не удалось прочитать roles.yaml: %v", err)
		}
		return nil
	}

	var config struct {
		Roles map[string]string `yaml:"roles"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		log.Printf("Предупреждение: не удалось распарсить roles.yaml: %v", err)
		return nil
	}

	return config.Roles
}
