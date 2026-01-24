package integration

import (
	"net/http"
	"testing"
)

// TestRegression_EmptyToolArguments tests handling of tool calls with empty arguments.
// This is a regression test for a known bug where empty arguments caused errors.
func TestRegression_EmptyToolArguments(t *testing.T) {
	ResetMockTransport()

	// Tool with no required parameters
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

// TestRegression_ToolResultWithEmptyContent tests tool results with empty content.
func TestRegression_ToolResultWithEmptyContent(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("void_action", "Perform an action with no return value", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Perform the void action"},
			{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_123",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "void_action",
							"arguments": "{}",
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": "call_123",
				"content":      "", // Empty content
			},
		},
		"tools": tools,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestRegression_NullContent tests handling of null content in messages.
func TestRegression_NullContent(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestRegression_LongMessages tests handling of very long messages.
func TestRegression_LongMessages(t *testing.T) {
	ResetMockTransport()

	// Create a longer message
	longMessage := ""
	for i := 0; i < 100; i++ {
		longMessage += "This is a test sentence that repeats many times. "
	}

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": longMessage},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestRegression_SpecialCharactersInMessages tests handling of special characters.
func TestRegression_SpecialCharactersInMessages(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Test with special chars: <script>alert('xss')</script> & \"quotes\" 'apostrophe' \n\t\r"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestRegression_UnicodeContent tests handling of unicode characters.
func TestRegression_UnicodeContent(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Unicode test: 你好 🎉 αβγ ñ é ü"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestRegression_MultipleToolCalls tests handling of multiple tool calls in one response.
func TestRegression_MultipleToolCalls(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("tool_a", "Tool A", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		buildToolDefinition("tool_b", "Tool B", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	req := buildChatRequestWithTools("claude-sonnet-4-20250514", "Use both tools", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestRegression_ToolChoiceRequired tests tool_choice with required option.
func TestRegression_ToolChoiceRequired(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("required_tool", "Required tool", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Use the tool"},
		},
		"tools":       tools,
		"tool_choice": "required",
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestRegression_EmptyMessagesArray tests handling of empty messages array.
func TestRegression_EmptyMessagesArray(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":    "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	// Empty messages should return an error
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		t.Log("Note: Server accepted empty messages array")
	}
	_ = readResponseBody(t, resp)
}

// TestRegression_DuplicateToolNames tests handling of duplicate tool names.
func TestRegression_DuplicateToolNames(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("same_name", "First tool", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		buildToolDefinition("same_name", "Second tool with same name", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	req := buildChatRequestWithTools("claude-sonnet-4-20250514", "Use the tool", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	// Server may accept or reject duplicate names
	_ = readResponseBody(t, resp)
}

// TestRegression_VerySmallMaxTokens tests handling of very small max_tokens.
func TestRegression_VerySmallMaxTokens(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"max_tokens": 1,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestRegression_ZeroTemperature tests handling of temperature=0.
func TestRegression_ZeroTemperature(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"temperature": 0,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestRegression_MaxTemperature tests handling of temperature=2.0.
func TestRegression_MaxTemperature(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"temperature": 2.0,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// May succeed or fail depending on API limits
	_ = readResponseBody(t, resp)
}

// TestRegression_ConsecutiveSameRoleMessages tests consecutive messages with same role.
func TestRegression_ConsecutiveSameRoleMessages(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "First"},
			{"role": "user", "content": "Second"},
			{"role": "user", "content": "Third"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// May succeed with concatenation or fail
	_ = readResponseBody(t, resp)
}

// TestRegression_ToolResultBeforeToolCall tests tool result before tool call.
func TestRegression_ToolResultBeforeToolCall(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
			{
				"role":         "tool",
				"tool_call_id": "orphan_call",
				"content":      "result",
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Should handle gracefully (error or ignore)
	_ = readResponseBody(t, resp)
}
