package agent

import (
	"encoding/json"
	"testing"

	"ai-chat/internal/history"
)

// mockToolRegistry — реализация ToolRegistry для тестов.
type mockToolRegistry struct {
	definitions []ToolDefinition
	calls       []struct {
		Name string
		Args json.RawMessage
	}
	results map[string]string
}

func (m *mockToolRegistry) Definitions() []ToolDefinition {
	return m.definitions
}

func (m *mockToolRegistry) Execute(name string, args json.RawMessage) (string, error) {
	m.calls = append(m.calls, struct {
		Name string
		Args json.RawMessage
	}{Name: name, Args: args})
	if r, ok := m.results[name]; ok {
		return r, nil
	}
	return "mock result", nil
}

// mockSequenceClient возвращает разные ответы LLM по порядку.
type mockSequenceClient struct {
	responses [][]byte
	idx       int
}

func (m *mockSequenceClient) Call(payload []byte) ([]byte, error) {
	if m.idx >= len(m.responses) {
		fallback, err := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "fallback",
					},
					"finish_reason": "stop",
				},
			},
		})
		if err != nil {
			return nil, err
		}
		return fallback, nil
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

func toolCallResponse(toolName, arguments string) []byte {
	data, err := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": "",
					"tool_calls": []map[string]any{
						{
							"id":   "call_1",
							"type": "function",
							"function": map[string]any{
								"name":      toolName,
								"arguments": arguments,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     20,
			"completion_tokens": 10,
			"total_tokens":      30,
		},
	})
	if err != nil {
		panic(err)
	}
	return data
}

func textResponse(text string) []byte {
	data, err := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	})
	if err != nil {
		panic(err)
	}
	return data
}

func TestRunWithToolsInChatMode(t *testing.T) {
	tmp := t.TempDir()
	store, err := history.NewFileStore(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client := &mockSequenceClient{
		responses: [][]byte{
			toolCallResponse("test_get_weather", `{"city":"Moscow"}`),
			textResponse("В Москве сегодня солнечно"),
		},
	}

	tools := &mockToolRegistry{
		definitions: []ToolDefinition{
			{
				Name:        "test_get_weather",
				Description: "Получить погоду",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
			},
		},
		results: map[string]string{
			"test_get_weather": "{\"temperature\": 25, \"condition\": \"sunny\"}",
		},
	}

	cfg := Config{APIFormat: "openai", Model: "test"}
	agent := NewSimpleAgent(cfg, client).WithHistory(store).WithTools(tools)

	resp, err := agent.Run(AgentRequest{
		Message:      "Какая погода в Москве?",
		SessionID:    "sess-tools",
		WorkflowMode: WorkflowModeChat,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "В Москве сегодня солнечно" {
		t.Fatalf("unexpected response content: %q", resp.Content)
	}

	if len(tools.calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tools.calls))
	}
	if tools.calls[0].Name != "test_get_weather" {
		t.Fatalf("unexpected tool name: %q", tools.calls[0].Name)
	}

	session, err := store.LoadSession("sess-tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages := session.Branches["main"].Messages
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages (user, assistant tool, tool, assistant), got %d", len(messages))
	}
	if messages[0].Role != "user" {
		t.Fatalf("expected first message role user, got %q", messages[0].Role)
	}
	if messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 {
		t.Fatalf("expected assistant message with tool_calls, got role=%q, tool_calls=%d", messages[1].Role, len(messages[1].ToolCalls))
	}
	if messages[2].Role != "tool" || messages[2].ToolCallID != "call_1" {
		t.Fatalf("expected tool message with tool_call_id, got role=%q, id=%q", messages[2].Role, messages[2].ToolCallID)
	}
	if messages[3].Role != "assistant" {
		t.Fatalf("expected final assistant message, got %q", messages[3].Role)
	}
}

func TestRunWorkflowModeDoesNotUseTools(t *testing.T) {
	tmp := t.TempDir()
	store, err := history.NewFileStore(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client := &mockAPIClient{response: "План"}

	tools := &mockToolRegistry{
		definitions: []ToolDefinition{
			{Name: "test_tool", Description: "test"},
		},
	}

	cfg := Config{APIFormat: "openai", Model: "test"}
	agent := NewSimpleAgent(cfg, client).WithHistory(store).WithTools(tools)

	resp, err := agent.Run(AgentRequest{
		Message:      "Сделай что-нибудь",
		SessionID:    "sess-workflow-tools",
		WorkflowMode: WorkflowModeWorkflow,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.TaskStage != TaskStagePlanning {
		t.Fatalf("expected planning stage, got %q", resp.TaskStage)
	}
	if len(tools.calls) != 0 {
		t.Fatalf("expected no tool calls in workflow mode, got %d", len(tools.calls))
	}
}

func TestToOpenAITools(t *testing.T) {
	defs := []ToolDefinition{
		{
			Name:        "server_tool",
			Description: "описание",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x": map[string]any{"type": "string"},
				},
			},
		},
	}

	tools := toOpenAITools(defs)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Type != "function" {
		t.Fatalf("expected tool type function, got %q", tools[0].Type)
	}
	if tools[0].Function.Name != "server_tool" {
		t.Fatalf("unexpected tool name: %q", tools[0].Function.Name)
	}
}

func TestExtractOpenAIToolCalls(t *testing.T) {
	body := toolCallResponse("my_tool", `{"a":1}`)
	resp := extractResponse("openai", "text", body)

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "my_tool" {
		t.Fatalf("unexpected tool name: %q", resp.ToolCalls[0].Function.Name)
	}
	if resp.ToolCalls[0].Function.Arguments != `{"a":1}` {
		t.Fatalf("unexpected arguments: %q", resp.ToolCalls[0].Function.Arguments)
	}
}
