package integration

import (
	"net/http"
	"strings"
	"testing"
)

// TestAnthropic_Messages tests the /v1/messages endpoint.
func TestAnthropic_Messages(t *testing.T) {
	ResetMockTransport()

	req := buildAnthropicRequest("claude-sonnet-4-20250514", "Hello, how are you?")
	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)

	// Response is in SSE format (Claude always streams internally)
	// The test just verifies the endpoint works
	_ = readResponseBody(t, resp)
}

// TestAnthropic_MessagesWithSystem tests the system parameter.
func TestAnthropic_MessagesWithSystem(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"system":     "You are a helpful assistant.",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello!"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_MessagesWithTools tests tools in Anthropic format.
func TestAnthropic_MessagesWithTools(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildAnthropicToolDefinition("get_weather", "Get weather", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{"type": "string"},
			},
		}),
	}

	req := buildAnthropicRequestWithTools("claude-sonnet-4-20250514", "What's the weather?", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_MessagesMaxTokens tests the max_tokens parameter.
func TestAnthropic_MessagesMaxTokens(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_MessagesTemperature tests the temperature parameter.
func TestAnthropic_MessagesTemperature(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":       "claude-sonnet-4-20250514",
		"max_tokens":  100,
		"temperature": 0.5,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_MessagesStopSequences tests the stop_sequences parameter.
func TestAnthropic_MessagesStopSequences(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":          "claude-sonnet-4-20250514",
		"max_tokens":     100,
		"stop_sequences": []string{"END", "STOP"},
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_MessagesStream tests streaming with Anthropic format.
func TestAnthropic_MessagesStream(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"stream":     true,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)

	// Should return SSE format
	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}
}

// TestAnthropic_MessagesMetadata tests the metadata parameter.
func TestAnthropic_MessagesMetadata(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"metadata": map[string]interface{}{
			"user_id": "test-user-123",
		},
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_MessagesTopK tests the top_k parameter.
func TestAnthropic_MessagesTopK(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"top_k":      40,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_MessagesTopP tests the top_p parameter.
func TestAnthropic_MessagesTopP(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"top_p":      0.9,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_MessagesMultiTurn tests multi-turn conversations.
func TestAnthropic_MessagesMultiTurn(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "My name is Alice."},
			{"role": "assistant", "content": "Hello Alice!"},
			{"role": "user", "content": "What's my name?"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_MessagesToGemini tests routing Anthropic requests to Gemini models.
func TestAnthropic_MessagesToGemini(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":      "gemini-2.0-flash",
		"max_tokens": 100,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_SystemAsArray tests system parameter as an array of text blocks.
func TestAnthropic_SystemAsArray(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"system": []map[string]interface{}{
			{"type": "text", "text": "You are a helpful assistant."},
			{"type": "text", "text": "Always be concise."},
		},
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_ContentBlocksArray tests messages with content as array of blocks.
func TestAnthropic_ContentBlocksArray(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": "First part"},
					{"type": "text", "text": "Second part"},
				},
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_ToolUseResponse tests tool_use content block in response.
func TestAnthropic_ToolUseResponse(t *testing.T) {
	ResetMockTransport()

	// Configure mock to return tool_use
	if useMockLLM {
		mockTransport.SetToolCallResponse("get_weather", map[string]interface{}{
			"location": "NYC",
		})
	}

	tools := []map[string]interface{}{
		buildAnthropicToolDefinition("get_weather", "Get weather for a location", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "City name",
				},
			},
		}),
	}

	req := buildAnthropicRequestWithTools("claude-sonnet-4-20250514", "What's the weather in NYC?", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)

	// Response is SSE format (Claude always streams internally)
	// Verify we get SSE events with tool_use content
	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected SSE events in response")
	}

	// Check for tool_use in content_block_start event
	foundToolUse := false
	for _, event := range events {
		if strings.Contains(event.Data, "tool_use") {
			foundToolUse = true
			break
		}
	}
	if !foundToolUse {
		for _, e := range events {
			t.Logf("Event: %s, Data: %s", e.Event, e.Data)
		}
		t.Error("Expected tool_use in SSE events")
	}
}

// TestAnthropic_ToolResultContentTypes tests tool_result with different content types.
func TestAnthropic_ToolResultContentTypes(t *testing.T) {
	ResetMockTransport()

	testCases := []struct {
		name    string
		content interface{}
	}{
		{
			name:    "string_content",
			content: "Simple string result",
		},
		{
			name: "array_content",
			content: []map[string]interface{}{
				{"type": "text", "text": "Result text"},
			},
		},
		{
			name:    "json_content",
			content: `{"temperature": 72, "condition": "sunny"}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ResetMockTransport()

			req := map[string]interface{}{
				"model":      "claude-sonnet-4-20250514",
				"max_tokens": 100,
				"messages": []map[string]interface{}{
					{"role": "user", "content": "What's the weather?"},
					{
						"role": "assistant",
						"content": []map[string]interface{}{
							{
								"type":  "tool_use",
								"id":    "toolu_test",
								"name":  "get_weather",
								"input": map[string]interface{}{"location": "NYC"},
							},
						},
					},
					{
						"role": "user",
						"content": []map[string]interface{}{
							{
								"type":        "tool_result",
								"tool_use_id": "toolu_test",
								"content":     tc.content,
							},
						},
					},
				},
				"tools": []map[string]interface{}{
					buildAnthropicToolDefinition("get_weather", "Get weather", map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					}),
				},
			}

			resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
			assertStatusCode(t, resp, http.StatusOK)
			_ = readResponseBody(t, resp)
		})
	}
}

// TestAnthropic_ThinkingResponse tests extended thinking in responses.
func TestAnthropic_ThinkingResponse(t *testing.T) {
	ResetMockTransport()

	// Configure mock to return thinking content
	if useMockLLM {
		mockTransport.SetThinkingResponse("Let me think about this...", "The answer is 4")
	}

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 1000,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "What is 2+2?"},
		},
		"thinking": map[string]interface{}{
			"type":          "enabled",
			"budget_tokens": 1024,
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestAnthropic_ToolChoiceOptions tests different tool_choice values in Anthropic format.
func TestAnthropic_ToolChoiceOptions(t *testing.T) {
	tools := []map[string]interface{}{
		buildAnthropicToolDefinition("get_weather", "Get weather", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	testCases := []struct {
		name       string
		toolChoice interface{}
	}{
		{
			name:       "auto",
			toolChoice: map[string]interface{}{"type": "auto"},
		},
		{
			name:       "any",
			toolChoice: map[string]interface{}{"type": "any"},
		},
		{
			name: "tool",
			toolChoice: map[string]interface{}{
				"type": "tool",
				"name": "get_weather",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ResetMockTransport()

			req := map[string]interface{}{
				"model":      "claude-sonnet-4-20250514",
				"max_tokens": 100,
				"messages": []map[string]interface{}{
					{"role": "user", "content": "What's the weather?"},
				},
				"tools":       tools,
				"tool_choice": tc.toolChoice,
			}

			resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
			assertStatusCode(t, resp, http.StatusOK)
			_ = readResponseBody(t, resp)
		})
	}
}

// TestAnthropic_ImageContent tests image content in Anthropic format.
func TestAnthropic_ImageContent(t *testing.T) {
	ResetMockTransport()

	// Create a tiny valid image placeholder
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

	req := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": "What do you see?"},
					{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": "image/png",
							"data":       imageData,
						},
					},
				},
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}
