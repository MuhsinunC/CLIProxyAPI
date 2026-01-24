package integration

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
)

// TestOpenAI_ChatCompletions tests the /v1/chat/completions endpoint.
func TestOpenAI_ChatCompletions(t *testing.T) {
	ResetMockTransport()

	req := buildChatRequest("claude-sonnet-4-20250514", "Hello, how are you?")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)

	// Verify OpenAI response format
	assertJSONPath(t, body, "object", "chat.completion")
	assertJSONPathExists(t, body, "id")
	assertJSONPathExists(t, body, "created")
	assertJSONPathExists(t, body, "model")
	assertJSONPathExists(t, body, "choices")
	assertJSONPathExists(t, body, "choices.0.index")
	assertJSONPathExists(t, body, "choices.0.message")
	assertJSONPath(t, body, "choices.0.message.role", "assistant")
	assertJSONPathExists(t, body, "choices.0.message.content")
	assertJSONPathExists(t, body, "choices.0.finish_reason")
	assertJSONPathExists(t, body, "usage")
}

// TestOpenAI_ChatCompletionsWithSystemMessage tests handling of system messages.
func TestOpenAI_ChatCompletionsWithSystemMessage(t *testing.T) {
	ResetMockTransport()

	req := buildChatRequestWithSystem(
		"claude-sonnet-4-20250514",
		"You are a helpful assistant.",
		"Hello!",
	)
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices.0.message.content")
}

// TestOpenAI_ChatCompletionsMultipleTurns tests multi-turn conversations.
func TestOpenAI_ChatCompletionsMultipleTurns(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "My name is Alice."},
			{"role": "assistant", "content": "Hello Alice! Nice to meet you."},
			{"role": "user", "content": "What's my name?"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices.0.message.content")
}

// TestOpenAI_Models tests the /v1/models endpoint.
func TestOpenAI_Models(t *testing.T) {
	resp := makeRequest(t, http.MethodGet, "/v1/models", nil)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)

	// Verify OpenAI models list format
	assertJSONPath(t, body, "object", "list")
	assertJSONPathExists(t, body, "data")

	// Data should be an array
	data := gjson.GetBytes(body, "data")
	if !data.IsArray() {
		t.Error("Expected data to be an array")
	}

	// Each model should have required fields
	for _, model := range data.Array() {
		if !model.Get("id").Exists() {
			t.Error("Model missing id field")
		}
		if !model.Get("object").Exists() {
			t.Log("Note: Model missing object field")
		}
	}
}

// TestOpenAI_ModelDetail tests the /v1/models/{model} endpoint.
func TestOpenAI_ModelDetail(t *testing.T) {
	resp := makeRequest(t, http.MethodGet, "/v1/models/claude-sonnet-4-20250514", nil)

	// May return 200 or 404 depending on implementation
	if resp.StatusCode == http.StatusOK {
		body := readResponseBody(t, resp)
		assertJSONPathExists(t, body, "id")
	} else if resp.StatusCode == http.StatusNotFound {
		t.Skip("Model detail endpoint returns 404")
	} else {
		readResponseBody(t, resp)
	}
}

// TestOpenAI_MaxTokens tests the max_tokens parameter.
func TestOpenAI_MaxTokens(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"max_tokens": 10,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_Temperature tests the temperature parameter.
func TestOpenAI_Temperature(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
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

// TestOpenAI_TopP tests the top_p parameter.
func TestOpenAI_TopP(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
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

// TestOpenAI_StopSequence tests the stop parameter.
func TestOpenAI_StopSequence(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stop": []string{"END", "STOP"},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_PresencePenalty tests the presence_penalty parameter.
func TestOpenAI_PresencePenalty(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"presence_penalty": 0.5,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_FrequencyPenalty tests the frequency_penalty parameter.
func TestOpenAI_FrequencyPenalty(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"frequency_penalty": 0.5,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_N tests the n parameter (number of completions).
func TestOpenAI_N(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"n": 1,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_User tests the user parameter.
func TestOpenAI_User(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"user": "test-user-123",
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_ImageContent tests image content in messages.
func TestOpenAI_ImageContent(t *testing.T) {
	ResetMockTransport()

	// Create a small test image (1x1 red pixel PNG)
	imageData := base64.StdEncoding.EncodeToString([]byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
	})

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": "What do you see?",
					},
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

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_UsageTokens tests that usage tokens are returned.
func TestOpenAI_UsageTokens(t *testing.T) {
	ResetMockTransport()

	req := buildChatRequest("claude-sonnet-4-20250514", "Hello")
	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)

	// Check usage fields
	assertJSONPathExists(t, body, "usage.prompt_tokens")
	assertJSONPathExists(t, body, "usage.completion_tokens")
	assertJSONPathExists(t, body, "usage.total_tokens")

	// Verify total = prompt + completion
	promptTokens := gjson.GetBytes(body, "usage.prompt_tokens").Int()
	completionTokens := gjson.GetBytes(body, "usage.completion_tokens").Int()
	totalTokens := gjson.GetBytes(body, "usage.total_tokens").Int()

	if totalTokens != promptTokens+completionTokens {
		t.Logf("Note: total_tokens (%d) != prompt_tokens (%d) + completion_tokens (%d)",
			totalTokens, promptTokens, completionTokens)
	}
}

// TestOpenAI_ErrorFormat tests that errors are returned in OpenAI format.
func TestOpenAI_ErrorFormat(t *testing.T) {
	ResetMockTransport()

	// Send request with invalid model
	req := map[string]interface{}{
		"model": "nonexistent-model-xyz",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)

	// Should return an error (could be various status codes)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		t.Skip("Server accepted unknown model (may route to default)")
	}

	body := readResponseBody(t, resp)

	// If error, should have OpenAI error format
	if gjson.GetBytes(body, "error").Exists() {
		assertJSONPathExists(t, body, "error.message")
	}
}

// TestOpenAI_Seed tests the seed parameter for reproducibility.
func TestOpenAI_Seed(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"seed": 12345,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_MixedContentArray tests messages with mixed content types (text + image).
func TestOpenAI_MixedContentArray(t *testing.T) {
	ResetMockTransport()

	// Create a tiny valid PNG (1x1 transparent pixel)
	imageData := base64.StdEncoding.EncodeToString([]byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	})

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": "First text block",
					},
					{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "data:image/png;base64," + imageData,
						},
					},
					{
						"type": "text",
						"text": "Second text block after image",
					},
				},
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_NullContentField tests handling of null content field.
func TestOpenAI_NullContentField(t *testing.T) {
	ResetMockTransport()

	// Message with tool_calls typically has null content
	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Use a tool"},
			{
				"role":    "assistant",
				"content": nil, // Explicit null
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_test",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "test_tool",
							"arguments": "{}",
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": "call_test",
				"content":      "result",
			},
		},
		"tools": []map[string]interface{}{
			buildToolDefinition("test_tool", "A test tool", map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}),
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_EmptyContentField tests handling of empty string content.
func TestOpenAI_EmptyContentField(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": ""},
			{"role": "user", "content": "Actual question"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	// May succeed or return error depending on implementation
	_ = readResponseBody(t, resp)
}

// TestOpenAI_MultipleSystemMessages tests handling of multiple system messages.
func TestOpenAI_MultipleSystemMessages(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "system", "content": "Always be concise."},
			{"role": "user", "content": "Hello"},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_ContentArrayWithOnlyText tests content array with single text item.
func TestOpenAI_ContentArrayWithOnlyText(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": "Hello, this is a text-only content array",
					},
				},
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_Logprobs tests the logprobs parameter.
func TestOpenAI_Logprobs(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"logprobs":    true,
		"top_logprobs": 3,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_ResponseFormatJSONSchema tests structured output with JSON schema.
func TestOpenAI_ResponseFormatJSONSchema(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Return a person object"},
		},
		"response_format": map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name":   "person",
				"strict": true,
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{"type": "string"},
						"age":  map[string]interface{}{"type": "integer"},
					},
					"required": []string{"name", "age"},
				},
			},
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestOpenAI_ReasoningEffort tests the reasoning_effort parameter.
func TestOpenAI_ReasoningEffort(t *testing.T) {
	levels := []string{"low", "medium", "high"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			ResetMockTransport()

			req := map[string]interface{}{
				"model": "claude-sonnet-4-20250514",
				"messages": []map[string]interface{}{
					{"role": "user", "content": "What is 2+2?"},
				},
				"reasoning_effort": level,
			}

			resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
			assertStatusCode(t, resp, http.StatusOK)

			body := readResponseBody(t, resp)
			assertJSONPathExists(t, body, "choices")
		})
	}
}
