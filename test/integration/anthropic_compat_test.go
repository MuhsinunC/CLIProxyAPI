package integration

import (
	"net/http"
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
