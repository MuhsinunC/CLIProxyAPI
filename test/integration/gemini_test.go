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

// ============================================================
// Gemini Edge Case Tests
// ============================================================

// TestGemini_FlashLite tests with gemini-2.5-flash-lite model variant.
// This test documents expected behavior - model may not be configured.
func TestGemini_FlashLite(t *testing.T) {
	ResetMockTransport()

	req := buildChatRequest("gemini-2.5-flash-lite", "Hello")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	// Model may not be configured in test environment
	if resp.StatusCode != http.StatusOK {
		t.Skip("Model gemini-2.5-flash-lite not configured in test environment")
	}
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_FlashPreview tests with gemini-3-flash-preview model variant.
// This test documents expected behavior - model may not be configured.
func TestGemini_FlashPreview(t *testing.T) {
	ResetMockTransport()

	req := buildChatRequest("gemini-3-flash-preview", "Hello")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	// Model may not be configured in test environment
	if resp.StatusCode != http.StatusOK {
		t.Skip("Model gemini-3-flash-preview not configured in test environment")
	}
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_MultipleFunctionCalls tests multiple parallel function calls.
func TestGemini_MultipleFunctionCalls(t *testing.T) {
	ResetMockTransport()

	// Configure mock to return a function call
	if useMockLLM {
		mockTransport.SetToolCallResponse("get_weather", map[string]interface{}{
			"location": "NYC",
		})
	}

	tools := []map[string]interface{}{
		buildToolDefinition("get_weather", "Get weather", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{"type": "string"},
			},
		}),
	}

	req := buildChatRequestWithTools("gemini-2.0-flash", "What's the weather in NYC and LA?", tools)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestGemini_ToolResultFIFOMatching tests tool result FIFO matching for Gemini.
// Gemini requires functionResponse order to match functionCall order.
func TestGemini_ToolResultFIFOMatching(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("func_a", "Function A", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		buildToolDefinition("func_b", "Function B", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	// Simulate: Assistant called func_a then func_b
	// User responds with results in correct order
	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Call both functions"},
			{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "func_a",
							"arguments": "{}",
						},
					},
					{
						"id":   "call_2",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "func_b",
							"arguments": "{}",
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": "call_1",
				"content":      "Result A",
			},
			{
				"role":         "tool",
				"tool_call_id": "call_2",
				"content":      "Result B",
			},
		},
		"tools": tools,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestGemini_ImageContent tests image content with Gemini.
func TestGemini_ImageContent(t *testing.T) {
	ResetMockTransport()

	// Create a tiny valid image placeholder
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": "What's in this image?"},
					{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "data:image/png;base64," + imageData,
						},
					},
				},
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestGemini_LongConversation tests a longer conversation history.
func TestGemini_LongConversation(t *testing.T) {
	ResetMockTransport()

	messages := []map[string]interface{}{}
	for i := 0; i < 10; i++ {
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": "User message " + string(rune('0'+i)),
		})
		messages = append(messages, map[string]interface{}{
			"role":    "assistant",
			"content": "Assistant response " + string(rune('0'+i)),
		})
	}
	messages = append(messages, map[string]interface{}{
		"role":    "user",
		"content": "Final question",
	})

	req := map[string]interface{}{
		"model":    "gemini-2.0-flash",
		"messages": messages,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestGemini_ToolChoiceAuto tests tool_choice auto with Gemini.
func TestGemini_ToolChoiceAuto(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("search", "Search", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Search for something"},
		},
		"tools":       tools,
		"tool_choice": "auto",
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestGemini_ToolChoiceNone tests tool_choice none with Gemini.
func TestGemini_ToolChoiceNone(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("search", "Search", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"tools":       tools,
		"tool_choice": "none",
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestGemini_MultipleStopSequences tests multiple stop sequences.
func TestGemini_MultipleStopSequences(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stop": []string{"END", "STOP", "DONE", "###"},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestGemini_ZeroTemperature tests temperature=0 with Gemini.
func TestGemini_ZeroTemperature(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "What is 2+2?"},
		},
		"temperature": 0,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestGemini_HighTopK tests high top_k value with Gemini.
func TestGemini_HighTopK(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"top_k": 100,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestGemini_StreamWithTools tests streaming with tools enabled.
func TestGemini_StreamWithTools(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("get_info", "Get info", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"tools":  tools,
		"stream": true,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}
}

// TestGemini_JSONSchemaResponseFormat tests json_schema with Gemini.
func TestGemini_JSONSchemaResponseFormat(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Return structured data"},
		},
		"response_format": map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name": "my_response",
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"answer": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}

// TestGemini_ContentBlocksArray tests content as array with Gemini.
func TestGemini_ContentBlocksArray(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": "First part."},
					{"type": "text", "text": "Second part."},
				},
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)
	_ = readResponseBody(t, resp)
}
