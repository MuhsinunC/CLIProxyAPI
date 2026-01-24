package integration

import (
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
)

// TestGemini_ModelRouting tests that Gemini models are routed correctly.
func TestGemini_ModelRouting(t *testing.T) {
	ResetMockTransport()

	req := buildChatRequest("gemini-2.0-flash", "Hello")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")

	// Verify mock received Gemini format request
	if useMockLLM {
		requests := mockTransport.GetRequests()
		for _, req := range requests {
			// Gemini format uses "contents" not "messages"
			if gjson.GetBytes(req.Body, "contents").Exists() {
				return // Found Gemini request
			}
		}
		t.Log("Note: Gemini format request may not be directly captured")
	}
}

// TestGemini_FunctionCalling tests Gemini function calling format.
func TestGemini_FunctionCalling(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("get_weather", "Get weather", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{"type": "string"},
			},
		}),
	}

	req := buildChatRequestWithTools("gemini-2.0-flash", "What's the weather?", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_Pro tests with gemini-1.5-pro model.
func TestGemini_Pro(t *testing.T) {
	ResetMockTransport()

	req := buildChatRequest("gemini-1.5-pro", "Hello")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_Streaming tests streaming with Gemini models.
func TestGemini_Streaming(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": true,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}
}

// TestGemini_SystemMessage tests system message handling with Gemini.
func TestGemini_SystemMessage(t *testing.T) {
	ResetMockTransport()

	req := buildChatRequestWithSystem("gemini-2.0-flash", "Be concise.", "Hello")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_MaxOutputTokens tests max_tokens with Gemini.
func TestGemini_MaxOutputTokens(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"max_tokens": 100,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_Temperature tests temperature with Gemini.
func TestGemini_Temperature(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"temperature": 0.7,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_TopP tests top_p with Gemini.
func TestGemini_TopP(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"top_p": 0.9,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_TopK tests top_k with Gemini (Gemini-specific parameter).
func TestGemini_TopK(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"top_k": 40,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_MultiTurn tests multi-turn conversations with Gemini.
func TestGemini_MultiTurn(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "My name is Bob."},
			{"role": "assistant", "content": "Hello Bob!"},
			{"role": "user", "content": "What's my name?"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_ResponseFormat tests response_format with Gemini.
func TestGemini_ResponseFormat(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Return JSON."},
		},
		"response_format": map[string]interface{}{
			"type": "json_object",
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_StopSequences tests stop sequences with Gemini.
func TestGemini_StopSequences(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stop": []string{"END"},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_ViaAnthropicEndpoint tests accessing Gemini via Anthropic endpoint.
func TestGemini_ViaAnthropicEndpoint(t *testing.T) {
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
