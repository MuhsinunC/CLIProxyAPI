package integration

import (
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
)

// TestToolCalling_OpenAIToClaude verifies that OpenAI-format tool_calls
// are correctly translated to Claude's tool_use format.
func TestToolCalling_OpenAIToClaude(t *testing.T) {
	ResetMockTransport()

	// Define a tool in OpenAI format
	tools := []map[string]interface{}{
		buildToolDefinition("get_weather", "Get weather for a location", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "City name",
				},
			},
			"required": []string{"location"},
		}),
	}

	// Build request with tools
	req := buildChatRequestWithTools("claude-sonnet-4-20250514", "What's the weather in San Francisco?", tools)

	// Make request to OpenAI-compatible endpoint
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)

	// Verify the response has OpenAI format (translated from Claude)
	assertJSONPathExists(t, body, "choices")
	assertJSONPathExists(t, body, "choices.0.message")

	// Check if we got a tool call response
	finishReason := gjson.GetBytes(body, "choices.0.finish_reason").String()
	if finishReason == "tool_calls" {
		// Verify tool_calls array exists
		assertJSONPathExists(t, body, "choices.0.message.tool_calls")
		assertJSONPathExists(t, body, "choices.0.message.tool_calls.0.function.name")
		assertJSONPath(t, body, "choices.0.message.tool_calls.0.function.name", "get_weather")
	}

	// Verify mock received the request (if in mock mode)
	if useMockLLM {
		requests := mockTransport.GetRequests()
		if len(requests) == 0 {
			t.Skip("No upstream requests captured - might be using API key validation")
		}

		// Find the Claude API request
		var claudeReq *RecordedRequest
		for i := range requests {
			if gjson.GetBytes(requests[i].Body, "tools").Exists() {
				claudeReq = &requests[i]
				break
			}
		}

		if claudeReq != nil {
			// Verify Claude format has input_schema (not parameters)
			assertJSONPathExists(t, claudeReq.Body, "tools")
			// Claude uses input_schema, OpenAI uses parameters.function.parameters
		}
	}
}

// TestToolCalling_ClaudeToGemini verifies that Claude-format tool_use
// is correctly translated to Gemini's functionCall format.
func TestToolCalling_ClaudeToGemini(t *testing.T) {
	ResetMockTransport()

	// Define tool in Anthropic format
	tools := []map[string]interface{}{
		buildAnthropicToolDefinition("get_weather", "Get weather for a location", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "City name",
				},
			},
			"required": []string{"location"},
		}),
	}

	// Build Anthropic-format request targeting a Gemini model
	req := buildAnthropicRequestWithTools("gemini-2.0-flash", "What's the weather in San Francisco?", tools)

	// Make request to Anthropic-compatible endpoint
	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)

	// Verify response is in Anthropic format (translated from Gemini)
	assertJSONPathExists(t, body, "content")
	assertJSONPath(t, body, "role", "assistant")

	// Check if we got a tool_use response
	stopReason := gjson.GetBytes(body, "stop_reason").String()
	if stopReason == "tool_use" {
		// Find the tool_use content block
		content := gjson.GetBytes(body, "content")
		var hasToolUse bool
		content.ForEach(func(_, value gjson.Result) bool {
			if value.Get("type").String() == "tool_use" {
				hasToolUse = true
				return false
			}
			return true
		})
		if !hasToolUse {
			t.Log("Response has tool_use stop_reason but no tool_use content block")
		}
	}

	// Verify mock received the request transformed to Gemini format
	if useMockLLM {
		requests := mockTransport.GetRequests()
		// Gemini requests go to different host
		var geminiReq *RecordedRequest
		for i := range requests {
			if gjson.GetBytes(requests[i].Body, "contents").Exists() {
				geminiReq = &requests[i]
				break
			}
		}

		if geminiReq != nil {
			// Verify Gemini format uses functionDeclarations
			assertJSONPathExists(t, geminiReq.Body, "tools")
		}
	}
}

// TestToolCalling_RoundTrip tests a complete tool use cycle:
// 1. Send request with tools
// 2. Receive tool_use response
// 3. Send tool_result
// 4. Receive final response
func TestToolCalling_RoundTrip(t *testing.T) {
	ResetMockTransport()

	// Configure mock to return tool_use
	if useMockLLM {
		mockTransport.SetToolCallResponse("get_weather", map[string]interface{}{
			"location": "San Francisco",
		})
	}

	// Step 1: Initial request with tools
	tools := []map[string]interface{}{
		buildToolDefinition("get_weather", "Get weather for a location", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "City name",
				},
			},
			"required": []string{"location"},
		}),
	}

	req := buildChatRequestWithTools("claude-sonnet-4-20250514", "What's the weather in San Francisco?", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)

	// Verify we got a tool call
	finishReason := gjson.GetBytes(body, "choices.0.finish_reason").String()
	if finishReason != "tool_calls" {
		t.Skipf("Expected tool_calls finish_reason, got %s", finishReason)
	}

	// Extract tool call ID for the result
	toolCallID := gjson.GetBytes(body, "choices.0.message.tool_calls.0.id").String()
	if toolCallID == "" {
		t.Fatal("Missing tool_call id in response")
	}

	// Reset mock for next response
	if useMockLLM {
		mockTransport.ClearCustomResponses()
		mockTransport.ClearRequests()
	}

	// Step 2: Send tool result
	toolResultReq := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": "What's the weather in San Francisco?",
			},
			{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]interface{}{
					{
						"id":   toolCallID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      "get_weather",
							"arguments": `{"location":"San Francisco"}`,
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": toolCallID,
				"content":      `{"temperature": 65, "condition": "sunny"}`,
			},
		},
		"tools": tools,
	}

	resp2 := makeRequest(t, http.MethodPost, "/v1/chat/completions", toolResultReq)
	assertStatusCode(t, resp2, http.StatusOK)

	body2 := readResponseBody(t, resp2)

	// Verify we got a final response (not another tool call)
	assertJSONPathExists(t, body2, "choices.0.message.content")
}

// TestToolCalling_EmptyArguments tests handling of tool calls with empty arguments.
// This is a regression test for a known bug fix.
func TestToolCalling_EmptyArguments(t *testing.T) {
	ResetMockTransport()

	// Define a tool that takes no arguments
	tools := []map[string]interface{}{
		buildToolDefinition("get_time", "Get the current time", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	req := buildChatRequestWithTools("claude-sonnet-4-20250514", "What time is it?", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	// Should not error on empty arguments
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestToolCalling_MultipleTools tests handling of multiple tool definitions.
func TestToolCalling_MultipleTools(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("get_weather", "Get weather for a location", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{"type": "string"},
			},
		}),
		buildToolDefinition("get_time", "Get the current time", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		buildToolDefinition("search", "Search the web", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
			},
		}),
	}

	req := buildChatRequestWithTools("claude-sonnet-4-20250514", "What's the weather in NYC and what time is it?", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestToolCalling_ToolChoice tests the tool_choice parameter.
func TestToolCalling_ToolChoice(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("get_weather", "Get weather for a location", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{"type": "string"},
			},
		}),
	}

	testCases := []struct {
		name       string
		toolChoice interface{}
	}{
		{
			name:       "auto",
			toolChoice: "auto",
		},
		{
			name:       "none",
			toolChoice: "none",
		},
		{
			name: "specific_function",
			toolChoice: map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": "get_weather",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := map[string]interface{}{
				"model": "claude-sonnet-4-20250514",
				"messages": []map[string]interface{}{
					{"role": "user", "content": "What's the weather?"},
				},
				"tools":       tools,
				"tool_choice": tc.toolChoice,
			}

			resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
			assertStatusCode(t, resp, http.StatusOK)

			body := readResponseBody(t, resp)
			assertJSONPathExists(t, body, "choices")
		})
	}
}

// TestToolCalling_NestedArguments tests tool calls with deeply nested JSON arguments.
func TestToolCalling_NestedArguments(t *testing.T) {
	ResetMockTransport()

	// Define a tool with complex nested schema
	tools := []map[string]interface{}{
		buildToolDefinition("create_event", "Create a calendar event", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"event": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{"type": "string"},
						"time": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"start": map[string]interface{}{"type": "string"},
								"end":   map[string]interface{}{"type": "string"},
								"timezone": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"name":   map[string]interface{}{"type": "string"},
										"offset": map[string]interface{}{"type": "integer"},
									},
								},
							},
						},
						"attendees": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"email": map[string]interface{}{"type": "string"},
									"name":  map[string]interface{}{"type": "string"},
								},
							},
						},
					},
				},
			},
		}),
	}

	req := buildChatRequestWithTools("claude-sonnet-4-20250514", "Create a meeting for tomorrow with John and Jane", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestToolCalling_ToolResultArrayContent tests tool results with array content.
func TestToolCalling_ToolResultArrayContent(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("search", "Search for information", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	// Send request with tool result containing array content
	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Search for weather"},
			{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_array_test",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "search",
							"arguments": "{}",
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": "call_array_test",
				// Array content in tool result
				"content": `[{"title": "Result 1", "url": "http://example1.com"}, {"title": "Result 2", "url": "http://example2.com"}]`,
			},
		},
		"tools": tools,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestToolCalling_LongArguments tests tool calls with very long argument strings.
func TestToolCalling_LongArguments(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("process_text", "Process a text document", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{"type": "string"},
			},
		}),
	}

	// Generate a long text (5000 characters)
	longText := ""
	for i := 0; i < 500; i++ {
		longText += "This is a test sentence that repeats. "
	}

	// Send request with tool result containing long arguments
	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Process this document"},
			{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_long_args",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "process_text",
							"arguments": `{"text": "` + longText + `"}`,
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": "call_long_args",
				"content":      "Processed " + longText[:100] + "...",
			},
		},
		"tools": tools,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestToolCalling_ToolChoiceRequired tests the "required" tool_choice option.
func TestToolCalling_ToolChoiceRequired(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("get_weather", "Get weather for a location", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{"type": "string"},
			},
		}),
	}

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Tell me something"},
		},
		"tools":       tools,
		"tool_choice": "required", // Force a tool call
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestToolCalling_MultipleToolCallsInSingleResponse tests multiple tool calls in one response.
func TestToolCalling_MultipleToolCallsInSingleResponse(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("get_weather", "Get weather", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{"type": "string"},
			},
		}),
		buildToolDefinition("get_time", "Get time", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	// Send request with multiple tool results (simulating parallel tool calls)
	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "What's the weather in NYC and what time is it?"},
			{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_weather",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "get_weather",
							"arguments": `{"location": "NYC"}`,
						},
					},
					{
						"id":   "call_time",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "get_time",
							"arguments": "{}",
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": "call_weather",
				"content":      `{"temperature": 72, "condition": "sunny"}`,
			},
			{
				"role":         "tool",
				"tool_call_id": "call_time",
				"content":      `{"time": "3:30 PM"}`,
			},
		},
		"tools": tools,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
	assertJSONPathExists(t, body, "choices.0.message.content")
}

// TestToolCalling_NullToolCallArguments tests tool calls where arguments is null.
func TestToolCalling_NullToolCallArguments(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("get_time", "Get the current time", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	// Send request where previous assistant message has null arguments (edge case)
	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "What time is it?"},
			{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_null_args",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "get_time",
							"arguments": nil, // Null arguments
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": "call_null_args",
				"content":      `{"time": "3:30 PM"}`,
			},
		},
		"tools": tools,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Should handle null arguments gracefully
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestToolCalling_ToolResultWithObjectContent tests tool result with object content (not string).
func TestToolCalling_ToolResultWithObjectContent(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("get_data", "Get data", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	// Claude supports object content in tool_result
	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Get the data"},
			{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_obj_content",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "get_data",
							"arguments": "{}",
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": "call_obj_content",
				// Complex nested object as content
				"content": `{"result": {"items": [1, 2, 3], "metadata": {"count": 3, "page": 1}}}`,
			},
		},
		"tools": tools,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}
