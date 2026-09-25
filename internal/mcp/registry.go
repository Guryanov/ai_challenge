package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"ai-chat/internal/agent"
	"github.com/mark3labs/mcp-go/mcp"
)

// ToolStatus описывает один инструмент MCP-сервера для отображения в UI.
type ToolStatus struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ServerStatus описывает состояние одного MCP-сервера.
type ServerStatus struct {
	Name      string       `json:"name"`
	Connected bool         `json:"connected"`
	Disabled  bool         `json:"disabled"`
	Error     string       `json:"error,omitempty"`
	Tools     []ToolStatus `json:"tools"`
}

// Registry реализует agent.ToolRegistry поверх нескольких MCP-серверов.
type Registry struct {
	clients  map[string]*serverClient
	statuses map[string]*ServerStatus
	mu       sync.RWMutex
}

// NewRegistry создаёт реестр и запускает все включённые MCP-серверы из конфигурации.
func NewRegistry(ctx context.Context, cfg Config) *Registry {
	r := &Registry{
		clients:  make(map[string]*serverClient),
		statuses: make(map[string]*ServerStatus),
	}

	for name, sc := range cfg.Servers {
		if sc.Disabled {
			r.statuses[name] = &ServerStatus{
				Name:      name,
				Connected: false,
				Disabled:  true,
				Tools:     []ToolStatus{},
			}
			continue
		}

		var client *serverClient
		var err error
		switch sc.Type {
		case "sse":
			client, err = newSseServerClient(ctx, name, sc)
		case "http":
			client, err = newHttpServerClient(ctx, name, sc)
		case "stdio":
			client, err = newStdioServerClient(ctx, name, sc)
		default:
			err = fmt.Errorf("неподдерживаемый тип транспорта %q", sc.Type)
		}

		if err != nil {
			log.Printf("[mcp] предупреждение: сервер %s недоступен: %v", name, err)
			r.statuses[name] = &ServerStatus{
				Name:      name,
				Connected: false,
				Error:     err.Error(),
				Tools:     []ToolStatus{},
			}
			continue
		}

		r.clients[name] = client
		status := &ServerStatus{
			Name:      name,
			Connected: true,
			Tools:     []ToolStatus{},
		}
		status.Tools = r.fetchTools(ctx, name, client)
		r.statuses[name] = status
		log.Printf("[mcp] сервер %s подключён", name)
	}

	return r
}

// Definitions возвращает OpenAI-совместимые определения инструментов от всех серверов.
// Имена инструментов префиксируются именем сервера: "<server>_<tool>".
func (r *Registry) Definitions() []agent.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ctx := context.Background()
	defs := make([]agent.ToolDefinition, 0)

	for name, client := range r.clients {
		tools, err := client.listTools(ctx)
		if err != nil {
			log.Printf("[mcp] предупреждение: не удалось получить список инструментов от сервера %s: %v", name, err)
			continue
		}

		for _, t := range tools {
			def := agent.ToolDefinition{
				Name:        prefixedToolName(name, t.Name),
				Description: t.Description,
			}

			if t.RawInputSchema != nil {
				var schema map[string]any
				if err := json.Unmarshal(t.RawInputSchema, &schema); err == nil {
					def.Parameters = schema
				}
			} else {
				def.Parameters = toolInputSchemaToMap(t.InputSchema)
			}

			defs = append(defs, def)
		}
	}

	return defs
}

// Execute вызывает инструмент на соответствующем MCP-сервере.
// Имя инструмента должно быть в формате "<server>_<tool>".
func (r *Registry) Execute(name string, args json.RawMessage) (string, error) {
	serverName, toolName, err := parsePrefixedToolName(name)
	if err != nil {
		return "", err
	}

	r.mu.RLock()
	client, ok := r.clients[serverName]
	r.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("MCP-сервер %s не найден", serverName)
	}

	var arguments map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return "", fmt.Errorf("неверные аргументы инструмента %s: %w", name, err)
		}
	}

	ctx := context.Background()
	result, err := client.callTool(ctx, toolName, arguments)
	if err != nil {
		return fmt.Sprintf("Ошибка вызова инструмента %s: %v", name, err), nil
	}

	return formatCallToolResult(result), nil
}

// Status возвращает актуальный статус всех MCP-серверов и их инструментов.
func (r *Registry) Status() []ServerStatus {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	result := make([]ServerStatus, 0, len(r.statuses))

	for name, status := range r.statuses {
		updated := *status

		if client, ok := r.clients[name]; ok && client != nil {
			updated.Tools = r.fetchTools(ctx, name, client)
		}

		result = append(result, updated)
	}

	return result
}

// fetchTools загружает и преобразует список инструментов сервера.
func (r *Registry) fetchTools(ctx context.Context, name string, client *serverClient) []ToolStatus {
	tools, err := client.listTools(ctx)
	if err != nil {
		log.Printf("[mcp] предупреждение: не удалось получить список инструментов от сервера %s: %v", name, err)
		return []ToolStatus{}
	}

	result := make([]ToolStatus, 0, len(tools))
	for _, t := range tools {
		ts := ToolStatus{
			Name:        t.Name,
			Description: t.Description,
		}

		if t.RawInputSchema != nil {
			var schema map[string]any
			if err := json.Unmarshal(t.RawInputSchema, &schema); err == nil {
				ts.Parameters = schema
			}
		} else {
			ts.Parameters = toolInputSchemaToMap(t.InputSchema)
		}

		result = append(result, ts)
	}

	return result
}

// CloseAll завершает все запущенные MCP-серверы.
func (r *Registry) CloseAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for name, client := range r.clients {
		if err := client.close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("ошибка закрытия MCP-сервера %s: %w", name, err)
		}
	}
	r.clients = make(map[string]*serverClient)
	return firstErr
}

// prefixedToolName формирует имя инструмента с префиксом сервера.
func prefixedToolName(server, tool string) string {
	return server + "_" + tool
}

// parsePrefixedToolName разбирает имя вида "<server>_<tool>".
func parsePrefixedToolName(name string) (server, tool string, err error) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("неверный формат имени инструмента %q: ожидается <server>_<tool>", name)
	}
	return parts[0], parts[1], nil
}

// toolInputSchemaToMap преобразует ToolInputSchema в map для OpenAI-совместимого payload.
func toolInputSchemaToMap(schema mcp.ToolInputSchema) map[string]any {
	return map[string]any{
		"type":       schema.Type,
		"properties": schema.Properties,
		"required":   schema.Required,
	}
}

// formatCallToolResult превращает результат вызова инструмента в строку.
func formatCallToolResult(result *mcp.CallToolResult) string {
	if result.IsError {
		var parts []string
		for _, c := range result.Content {
			parts = append(parts, contentToString(c))
		}
		return "Ошибка инструмента: " + strings.Join(parts, "\n")
	}

	var parts []string
	for _, c := range result.Content {
		parts = append(parts, contentToString(c))
	}
	return strings.Join(parts, "\n")
}

// contentToString извлекает текст из любого Content.
func contentToString(c mcp.Content) string {
	switch v := c.(type) {
	case mcp.TextContent:
		return v.Text
	case *mcp.TextContent:
		if v == nil {
			return ""
		}
		return v.Text
	case mcp.ImageContent:
		return fmt.Sprintf("[image/%s: %d bytes]", v.MIMEType, len(v.Data))
	case *mcp.ImageContent:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("[image/%s: %d bytes]", v.MIMEType, len(v.Data))
	default:
		data, err := json.Marshal(c)
		if err != nil {
			return fmt.Sprintf("[неподдерживаемый контент: %T]", c)
		}
		return string(data)
	}
}
