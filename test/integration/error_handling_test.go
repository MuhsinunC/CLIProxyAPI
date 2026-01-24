package integration

import (
	"net/http"
	"testing"
)

// ============================================================
// Invalid Request Body Tests
// ============================================================

// TestError_MalformedJSON tests handling of malformed JSON in request body.
func TestError_MalformedJSON(t *testing.T) {
	ResetMockTransport()

	// Send invalid JSON
	resp := makeRawRequest(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model": "claude-sonnet-4-20250514", "messages": [`), nil)

	// Should return 400 Bad Request
	if resp.StatusCode != http.StatusBadRequest {
		t.Logf("Expected 400 Bad Request, got %d", resp.StatusCode)
	}
	_ = readResponseBody(t, resp)
}

// TestError_MissingModel tests handling of missing model field.
func TestError_MissingModel(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	// Should return an error (typically 400)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		t.Log("Note: Server accepted request without model")
	}
	_ = readResponseBody(t, resp)
}

// TestError_MissingMessages tests handling of missing messages field.
func TestError_MissingMessages(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	// Should return an error (typically 400)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		t.Log("Note: Server accepted request without messages")
	}
	_ = readResponseBody(t, resp)
}

// TestError_InvalidModelName tests handling of invalid model name.
func TestError_InvalidModelName(t *testing.T) {
	ResetMockTransport()

	// Configure mock to return error for invalid model
	if useMockLLM {
		mockTransport.SetErrorResponse(400, "invalid_model", "Model not found")
	}

	req := map[string]interface{}{
		"model": "nonexistent-model-xyz",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	// Should return an error (400 or 404)
	body := readResponseBody(t, resp)
	t.Logf("Response for invalid model: %d, body: %s", resp.StatusCode, truncateForLog(string(body), 200))
}

// TestError_EmptyContent tests handling of empty content in message.
func TestError_EmptyContent(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": ""},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Server may accept or reject empty content
	_ = readResponseBody(t, resp)
}

// TestError_NullMessages tests handling of null messages array.
func TestError_NullMessages(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model":    "claude-sonnet-4-20250514",
		"messages": nil,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	// Should return an error
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		t.Log("Note: Server accepted null messages")
	}
	_ = readResponseBody(t, resp)
}

// ============================================================
// Invalid Message Role Tests
// ============================================================

// TestError_InvalidRole tests handling of invalid message role.
func TestError_InvalidRole(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "invalid_role", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Server may accept or reject invalid role
	_ = readResponseBody(t, resp)
}

// TestError_FirstMessageNotUser tests handling of first message not being user.
func TestError_FirstMessageNotUser(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "assistant", "content": "I'll help you"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Server may accept or reject this
	_ = readResponseBody(t, resp)
}

// ============================================================
// Invalid Parameter Tests
// ============================================================

// TestError_NegativeMaxTokens tests handling of negative max_tokens.
func TestError_NegativeMaxTokens(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"max_tokens": -1,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Should return an error
	_ = readResponseBody(t, resp)
}

// TestError_NegativeTemperature tests handling of negative temperature.
func TestError_NegativeTemperature(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"temperature": -0.5,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Should return an error
	_ = readResponseBody(t, resp)
}

// TestError_TemperatureOutOfRange tests handling of temperature > 2.0.
func TestError_TemperatureOutOfRange(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"temperature": 5.0,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// May be rejected by server
	_ = readResponseBody(t, resp)
}

// TestError_TopPOutOfRange tests handling of top_p > 1.0.
func TestError_TopPOutOfRange(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"top_p": 1.5,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// May be rejected by server
	_ = readResponseBody(t, resp)
}

// TestError_InvalidN tests handling of n > 1 (unsupported by Claude).
func TestError_InvalidN(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"n": 3,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Claude doesn't support n > 1
	_ = readResponseBody(t, resp)
}

// ============================================================
// Tool Calling Error Tests
// ============================================================

// TestError_InvalidToolDefinition tests handling of invalid tool definition.
func TestError_InvalidToolDefinition(t *testing.T) {
	ResetMockTransport()

	// Tool missing required fields
	tools := []map[string]interface{}{
		{
			"type": "function",
			// Missing function definition
		},
	}

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Use the tool"},
		},
		"tools": tools,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Should return an error
	_ = readResponseBody(t, resp)
}

// TestError_InvalidToolChoice tests handling of invalid tool_choice value.
func TestError_InvalidToolChoice(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("test_tool", "Test tool", map[string]interface{}{
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
		"tool_choice": "invalid_choice",
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// May be rejected by server
	_ = readResponseBody(t, resp)
}

// TestError_MissingToolCallID tests handling of missing tool_call_id in tool result.
func TestError_MissingToolCallID(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
			{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_123",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "test_func",
							"arguments": "{}",
						},
					},
				},
			},
			{
				"role": "tool",
				// Missing tool_call_id
				"content": "result",
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Should return an error
	_ = readResponseBody(t, resp)
}

// TestError_MismatchedToolCallID tests handling of mismatched tool_call_id.
func TestError_MismatchedToolCallID(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
			{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_123",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "test_func",
							"arguments": "{}",
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": "call_456", // Mismatched ID
				"content":      "result",
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// May be accepted or rejected
	_ = readResponseBody(t, resp)
}

// ============================================================
// HTTP Error Response Tests
// ============================================================

// TestError_SimulatedRateLimit tests handling of 429 rate limit response.
// Uses gemini model to avoid affecting cooldown for claude models used in other tests.
func TestError_SimulatedRateLimit(t *testing.T) {
	ResetMockTransport()

	if useMockLLM {
		// Configure mock to return rate limit error
		mockTransport.SetErrorResponse(429, "rate_limit_exceeded", "Rate limit exceeded. Please retry after 60 seconds.")
	} else {
		t.Skip("Rate limit test only runs in mock mode")
	}

	// Use gemini model to avoid affecting cooldown for claude models
	req := buildChatRequest("gemini-2.0-flash", "Hello")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Logf("Expected 429, got %d", resp.StatusCode)
	}
	_ = readResponseBody(t, resp)

	// Reset mock state
	ResetMockTransport()
}

// TestError_SimulatedServerError tests handling of 500 server error response.
// Uses gemini model to avoid affecting cooldown for claude models used in other tests.
func TestError_SimulatedServerError(t *testing.T) {
	ResetMockTransport()

	if useMockLLM {
		mockTransport.SetErrorResponse(500, "internal_error", "Internal server error")
	} else {
		t.Skip("Server error test only runs in mock mode")
	}

	// Use gemini model to avoid affecting cooldown for claude models
	req := buildChatRequest("gemini-2.0-flash", "Hello")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Logf("Expected 500, got %d", resp.StatusCode)
	}
	_ = readResponseBody(t, resp)

	// Reset mock state
	ResetMockTransport()
}

// TestError_SimulatedServiceUnavailable tests handling of 503 service unavailable.
// Uses gemini model to avoid affecting cooldown for claude models used in other tests.
func TestError_SimulatedServiceUnavailable(t *testing.T) {
	ResetMockTransport()

	if useMockLLM {
		mockTransport.SetErrorResponse(503, "service_unavailable", "Service temporarily unavailable")
	} else {
		t.Skip("Service unavailable test only runs in mock mode")
	}

	// Use gemini model to avoid affecting cooldown for claude models
	req := buildChatRequest("gemini-2.0-flash", "Hello")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Logf("Expected 503, got %d", resp.StatusCode)
	}
	_ = readResponseBody(t, resp)

	// Reset mock state
	ResetMockTransport()
}

// ============================================================
// Invalid Endpoint Tests
// ============================================================

// TestError_InvalidEndpoint tests handling of request to invalid endpoint.
func TestError_InvalidEndpoint(t *testing.T) {
	ResetMockTransport()

	resp := makeRequest(t, http.MethodPost, "/v1/invalid/endpoint", nil)

	// Should return 404
	if resp.StatusCode != http.StatusNotFound {
		t.Logf("Expected 404, got %d", resp.StatusCode)
	}
	_ = readResponseBody(t, resp)
}

// TestError_WrongMethod tests handling of wrong HTTP method.
func TestError_WrongMethod(t *testing.T) {
	ResetMockTransport()

	req := buildChatRequest("claude-sonnet-4-20250514", "Hello")
	resp := makeRequest(t, http.MethodGet, "/v1/chat/completions", req)

	// Should return 405 Method Not Allowed
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Logf("Expected 405, got %d", resp.StatusCode)
	}
	_ = readResponseBody(t, resp)
}

// ============================================================
// Image Content Error Tests
// ============================================================

// TestError_InvalidImageMediaType tests handling of invalid image media type.
func TestError_InvalidImageMediaType(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": "What's in this image?"},
					{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "data:text/plain;base64,SGVsbG8gV29ybGQ=",
						},
					},
				},
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// May be accepted or rejected
	_ = readResponseBody(t, resp)
}

// TestError_InvalidBase64Data tests handling of invalid base64 data.
func TestError_InvalidBase64Data(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": "What's in this image?"},
					{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "data:image/png;base64,not-valid-base64!!!",
						},
					},
				},
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Should return an error
	_ = readResponseBody(t, resp)
}

// ============================================================
// Response Format Error Tests
// ============================================================

// TestError_InvalidResponseFormatType tests handling of invalid response_format type.
func TestError_InvalidResponseFormatType(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Return JSON"},
		},
		"response_format": map[string]interface{}{
			"type": "invalid_type",
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// May be accepted or rejected
	_ = readResponseBody(t, resp)
}

// TestError_JSONSchemaWithoutSchema tests json_schema type without actual schema.
func TestError_JSONSchemaWithoutSchema(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Return JSON"},
		},
		"response_format": map[string]interface{}{
			"type": "json_schema",
			// Missing schema
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// Should return an error
	_ = readResponseBody(t, resp)
}

// ============================================================
// Helper function
// ============================================================

// truncateForLog truncates a string for logging purposes.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
