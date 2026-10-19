package mcp

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// serverClient оборачивает mcp-go клиент для одного MCP-сервера.
type serverClient struct {
	name    string
	client  *client.Client
	timeout time.Duration
}

// newStdioServerClient запускает stdio MCP-сервер и выполняет initialize.
func newStdioServerClient(ctx context.Context, name string, cfg ServerConfig) (*serverClient, error) {
	env := buildEnv(cfg.Env)

	c, err := client.NewStdioMCPClient(cfg.Command, env, cfg.Args...)
	if err != nil {
		return nil, fmt.Errorf("не удалось запустить MCP-сервер %s: %w", name, err)
	}

	return initializeClient(ctx, name, c, cfg.Timeout)
}

// newSseServerClient подключается к remote MCP-серверу через SSE и выполняет initialize.
func newSseServerClient(ctx context.Context, name string, cfg ServerConfig) (*serverClient, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("для SSE-сервера %s не указан url", name)
	}

	var opts []transport.ClientOption
	if len(cfg.Headers) > 0 {
		opts = append(opts, transport.WithHeaders(cfg.Headers))
	}

	c, err := client.NewSSEMCPClient(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("не удалось подключиться к SSE MCP-серверу %s: %w", name, err)
	}

	return initializeClient(ctx, name, c, cfg.Timeout)
}

// newHttpServerClient подключается к remote MCP-серверу через Streamable HTTP.
func newHttpServerClient(ctx context.Context, name string, cfg ServerConfig) (*serverClient, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("для HTTP MCP-сервера %s не указан url", name)
	}

	headers := make(map[string]string)
	for k, v := range cfg.Headers {
		headers[k] = v
	}
	// Некоторые серверы (например, DeepWiki) требуют, чтобы POST-запросы принимали
	// и JSON, и SSE-ответы. Если пользователь не переопределил Accept, выставляем
	// оба типа по умолчанию.
	if _, ok := headers["Accept"]; !ok {
		headers["Accept"] = "application/json, text/event-stream"
	}

	var opts []transport.StreamableHTTPCOption
	if len(headers) > 0 {
		opts = append(opts, transport.WithHTTPHeaders(headers))
	}

	c, err := client.NewStreamableHttpClient(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать HTTP MCP-клиент %s: %w", name, err)
	}

	return initializeClient(ctx, name, c, cfg.Timeout)
}

// initializeClient запускает транспорт и выполняет initialize для уже созданного клиента.
func initializeClient(ctx context.Context, name string, c *client.Client, timeout time.Duration) (*serverClient, error) {
	initCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// SSE-транспорт требует явного вызова Start до Initialize.
	if err := c.Start(initCtx); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("не удалось запустить транспорт MCP-сервера %s: %w", name, err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_LEGACY_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "ai-chat-mcp-client",
		Version: "0.1.0",
	}

	if _, err := c.Initialize(initCtx, initReq); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("не удалось инициализировать MCP-сервер %s: %w", name, err)
	}

	return &serverClient{
		name:    name,
		client:  c,
		timeout: timeout,
	}, nil
}

// listTools возвращает список инструментов сервера.
func (s *serverClient) listTools(ctx context.Context) ([]mcp.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	result, err := s.client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// callTool вызывает инструмент на сервере.
func (s *serverClient) callTool(ctx context.Context, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	return s.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: arguments,
		},
	})
}

// close завершает соединение с сервером.
func (s *serverClient) close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

// buildEnv превращает map env в срез "KEY=VALUE", добавляя к текущему окружению.
func buildEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return nil
	}

	merged := make(map[string]string)
	for _, e := range osEnviron() {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				merged[e[:i]] = e[i+1:]
				break
			}
		}
	}
	for k, v := range extra {
		merged[k] = v
	}

	env := make([]string, 0, len(merged))
	for k, v := range merged {
		env = append(env, k+"="+v)
	}
	return env
}

// osEnviron обёртка для возможности подмены в тестах.
var osEnviron = func() []string { return os.Environ() }
