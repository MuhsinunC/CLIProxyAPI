package integration

import (
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
)

// skipIfMock skips the test if we're in mock mode or budget is exhausted.
func skipIfMock(t *testing.T) {
	t.Helper()
	if useMockLLM {
		t.Skip("Skipping real API test (set TEST_REAL_API_BUDGET to enable)")
	}
	if budget == nil {
		t.Skip("Budget tracker not initialized")
	}
	if err := budget.CanMakeRequest(); err != nil {
		t.Skipf("Budget exhausted: %v", err)
	}
}

// TestRealAPI_Basic tests a basic chat completion with real API.
func TestRealAPI_Basic(t *testing.T) {
	skipIfMock(t)

	req := buildChatRequest("claude-sonnet-4-20250514", "Say 'hello' and nothing else.")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)

	// Verify OpenAI response format
	assertJSONPath(t, body, "object", "chat.completion")
	assertJSONPathExists(t, body, "id")
	assertJSONPathExists(t, body, "choices")
	assertJSONPathExists(t, body, "choices.0.message.content")
	assertJSONPath(t, body, "choices.0.message.role", "assistant")

	// Track usage
	if budget != nil {
		promptTokens := gjson.GetBytes(body, "usage.prompt_tokens").Int()
		completionTokens := gjson.GetBytes(body, "usage.completion_tokens").Int()
		budget.RecordUsage("claude-sonnet-4-20250514", int(promptTokens), int(completionTokens))
	}

	t.Logf("Response content: %s", gjson.GetBytes(body, "choices.0.message.content").String())
}

// TestRealAPI_Streaming tests streaming with real API.
func TestRealAPI_Streaming(t *testing.T) {
	skipIfMock(t)

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Count from 1 to 3."},
		},
		"stream": true,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	// Verify SSE format
	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}

	// Count content events
	var contentEvents int
	for _, event := range events {
		if event.Data != "" && event.Data != "[DONE]" {
			contentEvents++
		}
	}

	t.Logf("Received %d content events", contentEvents)
}

// TestRealAPI_Gemini tests Gemini model with real API.
func TestRealAPI_Gemini(t *testing.T) {
	skipIfMock(t)

	req := buildChatRequest("gemini-2.0-flash", "Say 'hello' and nothing else.")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)

	// Verify response format
	assertJSONPathExists(t, body, "choices")
	assertJSONPathExists(t, body, "choices.0.message.content")

	// Track usage
	if budget != nil {
		promptTokens := gjson.GetBytes(body, "usage.prompt_tokens").Int()
		completionTokens := gjson.GetBytes(body, "usage.completion_tokens").Int()
		budget.RecordUsage("gemini-2.0-flash", int(promptTokens), int(completionTokens))
	}

	t.Logf("Response content: %s", gjson.GetBytes(body, "choices.0.message.content").String())
}

// TestRealAPI_ToolCalling tests tool calling with real API.
func TestRealAPI_ToolCalling(t *testing.T) {
	skipIfMock(t)

	tools := []map[string]interface{}{
		buildToolDefinition("get_current_time", "Get the current time", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	req := buildChatRequestWithTools("claude-sonnet-4-20250514", "What time is it?", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")

	// Check if model called the tool
	finishReason := gjson.GetBytes(body, "choices.0.finish_reason").String()
	if finishReason == "tool_calls" {
		t.Log("Model called a tool as expected")
		assertJSONPathExists(t, body, "choices.0.message.tool_calls")
	} else {
		t.Logf("Model responded without tool call (finish_reason: %s)", finishReason)
	}
}

// TestRealAPI_AnthropicEndpoint tests the Anthropic-compatible endpoint with real API.
func TestRealAPI_AnthropicEndpoint(t *testing.T) {
	skipIfMock(t)

	req := buildAnthropicRequest("claude-sonnet-4-20250514", "Say 'hello' and nothing else.")
	resp := makeRequest(t, http.MethodPost, "/v1/messages", req)
	assertStatusCode(t, resp, http.StatusOK)

	// Response is in SSE format
	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}

	t.Logf("Received %d events from Anthropic endpoint", len(events))
}

// TestRealAPI_SystemMessage tests system message handling with real API.
func TestRealAPI_SystemMessage(t *testing.T) {
	skipIfMock(t)

	req := buildChatRequestWithSystem(
		"claude-sonnet-4-20250514",
		"You are a pirate. Always respond in pirate speak.",
		"Hello!",
	)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices.0.message.content")

	content := gjson.GetBytes(body, "choices.0.message.content").String()
	t.Logf("Response with system message: %s", content)
}

// TestRealAPI_MaxTokens tests max_tokens limiting with real API.
func TestRealAPI_MaxTokens(t *testing.T) {
	skipIfMock(t)

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Write a long story about a dragon."},
		},
		"max_tokens": 20,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)

	// Should complete with length or stop
	finishReason := gjson.GetBytes(body, "choices.0.finish_reason").String()
	t.Logf("Finish reason with max_tokens=20: %s", finishReason)
}

// TestRealAPI_JSONFormat tests JSON response format with real API.
func TestRealAPI_JSONFormat(t *testing.T) {
	skipIfMock(t)

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Return a JSON object with a 'greeting' field containing 'hello'."},
		},
		"response_format": map[string]interface{}{
			"type": "json_object",
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	content := gjson.GetBytes(body, "choices.0.message.content").String()

	// Verify the content is valid JSON
	if !gjson.Valid(content) {
		t.Errorf("Response content is not valid JSON: %s", content)
	} else {
		t.Logf("JSON response: %s", content)
	}
}

// TestRealAPI_MultiTurn tests multi-turn conversations with real API.
func TestRealAPI_MultiTurn(t *testing.T) {
	skipIfMock(t)

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "My name is Claude."},
			{"role": "assistant", "content": "Hello Claude! Nice to meet you."},
			{"role": "user", "content": "What is my name?"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	content := gjson.GetBytes(body, "choices.0.message.content").String()

	t.Logf("Multi-turn response: %s", content)
}

// TestRealAPI_BudgetTracking tests that budget tracking works correctly.
func TestRealAPI_BudgetTracking(t *testing.T) {
	skipIfMock(t)

	// Get initial budget state
	if budget == nil {
		t.Skip("Budget tracker not available")
	}

	summary := budget.Summary()
	t.Logf("Current budget status: %s", summary)
}
