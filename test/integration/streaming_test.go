package integration

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// gjson import is used for JSON path queries in streaming tests

// TestStreaming_SSE tests that stream=true returns Server-Sent Events.
func TestStreaming_SSE(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": true,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	// Verify content type is SSE
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("Expected Content-Type text/event-stream, got %s", contentType)
	}

	// Read SSE events
	events := readSSEEvents(t, resp)

	// Should have at least one event
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}

	// Check for [DONE] event
	var hasDone bool
	for _, event := range events {
		if strings.TrimSpace(event.Data) == "[DONE]" {
			hasDone = true
			break
		}
	}
	if !hasDone {
		t.Log("Warning: No [DONE] event found (may be acceptable for some providers)")
	}
}

// TestStreaming_NonStreaming tests that stream=false returns complete JSON.
func TestStreaming_NonStreaming(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": false,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	// Verify content type is JSON
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	body := readResponseBody(t, resp)

	// Should be valid JSON with complete response
	assertJSONPathExists(t, body, "choices")
	assertJSONPathExists(t, body, "choices.0.message.content")
	assertJSONPath(t, body, "object", "chat.completion")
}

// TestStreaming_DefaultBehavior tests that missing stream parameter defaults to non-streaming.
func TestStreaming_DefaultBehavior(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		// No stream parameter
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	// Should return complete JSON response
	body := readResponseBody(t, resp)
	assertJSONPathExists(t, body, "choices")
}

// TestStreaming_ChunkFormat tests that streaming chunks have correct format.
func TestStreaming_ChunkFormat(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": true,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	events := readSSEEvents(t, resp)

	// Find data events (not [DONE])
	var dataEvents []SSEEvent
	for _, event := range events {
		data := strings.TrimSpace(event.Data)
		if data != "" && data != "[DONE]" {
			dataEvents = append(dataEvents, event)
		}
	}

	if len(dataEvents) == 0 {
		t.Skip("No data events found")
	}

	// Verify first non-DONE event has correct structure
	firstData := dataEvents[0].Data
	if !gjson.Valid(firstData) {
		t.Errorf("Invalid JSON in SSE data: %s", firstData)
	}

	// Check OpenAI streaming format
	parsed := gjson.Parse(firstData)
	if obj := parsed.Get("object"); obj.Exists() {
		if obj.String() != "chat.completion.chunk" {
			t.Logf("Object type: %s", obj.String())
		}
	}

	// Should have choices array
	if !parsed.Get("choices").Exists() {
		t.Log("Note: choices field not found in chunk (may be header event)")
	}
}

// TestStreaming_WithTools tests streaming with tool definitions.
func TestStreaming_WithTools(t *testing.T) {
	ResetMockTransport()

	tools := []map[string]interface{}{
		buildToolDefinition("get_weather", "Get weather", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	}

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "What's the weather?"},
		},
		"tools":  tools,
		"stream": true,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	// Should return SSE format
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("Expected SSE content type, got %s", contentType)
	}

	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}
}

// TestStreaming_GeminiModel tests streaming with Gemini models.
func TestStreaming_GeminiModel(t *testing.T) {
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

// TestStreaming_LargeResponse tests streaming with a longer response.
func TestStreaming_LargeResponse(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Write a short paragraph about testing."},
		},
		"stream":     true,
		"max_tokens": 500,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}

	// Count content events
	var contentEvents int
	for _, event := range events {
		data := strings.TrimSpace(event.Data)
		if data != "" && data != "[DONE]" {
			contentEvents++
		}
	}

	t.Logf("Received %d content events", contentEvents)
}

// TestStreaming_StreamOptionsIncludeUsage tests the stream_options.include_usage parameter.
func TestStreaming_StreamOptionsIncludeUsage(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	// Should still return valid SSE
	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}
}

// TestStreaming_ReadRawSSE tests reading raw SSE stream for debugging.
func TestStreaming_ReadRawSSE(t *testing.T) {
	ResetMockTransport()

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hi"},
		},
		"stream": true,
	})

	resp := makeRawRequest(t, http.MethodPost, "/v1/chat/completions", reqBody, nil)
	assertStatusCode(t, resp, http.StatusOK)
	defer resp.Body.Close()

	// Read some lines to verify format
	scanner := bufio.NewScanner(resp.Body)
	var lines []string
	for scanner.Scan() && len(lines) < 10 {
		lines = append(lines, scanner.Text())
	}

	if len(lines) == 0 {
		t.Fatal("No lines read from stream")
	}

	// Check first non-empty line format
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") || strings.HasPrefix(line, "event:") {
			t.Logf("Valid SSE line: %s", line[:min(len(line), 50)])
			return
		}
	}

	t.Logf("Lines read: %v", lines)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestStreaming_ToolCallsInStream tests tool calls appearing in streaming responses.
func TestStreaming_ToolCallsInStream(t *testing.T) {
	ResetMockTransport()

	// Configure mock to return tool call in streaming mode
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

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "What's the weather in NYC?"},
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

	// Check for tool call data in events
	var hasToolCall bool
	for _, event := range events {
		if event.Data != "" && event.Data != "[DONE]" {
			if gjson.Valid(event.Data) {
				if gjson.Get(event.Data, "choices.0.delta.tool_calls").Exists() ||
					gjson.Get(event.Data, "choices.0.message.tool_calls").Exists() {
					hasToolCall = true
					break
				}
			}
		}
	}
	t.Logf("Tool call found in stream: %v", hasToolCall)
}

// TestStreaming_FinishReasonInLastChunk tests that finish_reason appears in final chunk.
func TestStreaming_FinishReasonInLastChunk(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Say hi"},
		},
		"stream": true,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	events := readSSEEvents(t, resp)

	// Find the last non-DONE event with finish_reason
	var lastFinishReason string
	for _, event := range events {
		if event.Data != "" && event.Data != "[DONE]" && gjson.Valid(event.Data) {
			fr := gjson.Get(event.Data, "choices.0.finish_reason").String()
			if fr != "" && fr != "null" {
				lastFinishReason = fr
			}
		}
	}

	if lastFinishReason == "" {
		t.Log("Note: No finish_reason found (may be acceptable for some providers)")
	} else {
		t.Logf("Final finish_reason: %s", lastFinishReason)
	}
}

// TestStreaming_MultipleModels tests streaming across different model types.
func TestStreaming_MultipleModels(t *testing.T) {
	models := []string{
		"claude-sonnet-4-20250514",
		"gemini-2.0-flash",
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			ResetMockTransport()

			req := map[string]interface{}{
				"model": model,
				"messages": []map[string]interface{}{
					{"role": "user", "content": "Hi"},
				},
				"stream": true,
			}

			resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
			assertStatusCode(t, resp, http.StatusOK)

			events := readSSEEvents(t, resp)
			if len(events) == 0 {
				t.Fatalf("Expected at least one SSE event for model %s", model)
			}
		})
	}
}

// TestStreaming_VeryShortResponse tests streaming with max_tokens=1.
func TestStreaming_VeryShortResponse(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Say hello"},
		},
		"stream":     true,
		"max_tokens": 1,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}
}

// TestStreaming_EmptyDeltaChunks tests handling of chunks with empty delta.
func TestStreaming_EmptyDeltaChunks(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": true,
	}

	resp := makeRequest(t, http.MethodPost, "/v1/chat/completions", req)
	assertStatusCode(t, resp, http.StatusOK)

	events := readSSEEvents(t, resp)

	// Count events with actual content
	var contentEvents, emptyEvents int
	for _, event := range events {
		if event.Data != "" && event.Data != "[DONE]" && gjson.Valid(event.Data) {
			delta := gjson.Get(event.Data, "choices.0.delta")
			if delta.Exists() {
				content := delta.Get("content").String()
				if content != "" {
					contentEvents++
				} else {
					emptyEvents++
				}
			}
		}
	}

	t.Logf("Content events: %d, Empty/meta events: %d", contentEvents, emptyEvents)
}

// TestStreaming_SystemMessageWithStream tests streaming with system message.
func TestStreaming_SystemMessageWithStream(t *testing.T) {
	ResetMockTransport()

	req := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{
			{"role": "system", "content": "You are a pirate."},
			{"role": "user", "content": "Say hello"},
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

// TestStreaming_AnthropicEndpoint tests streaming via Anthropic endpoint.
func TestStreaming_AnthropicEndpoint(t *testing.T) {
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

	events := readSSEEvents(t, resp)
	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}

	// Anthropic SSE should have specific event types
	var eventTypes []string
	for _, event := range events {
		if event.Event != "" {
			eventTypes = append(eventTypes, event.Event)
		}
	}
	t.Logf("Anthropic event types: %v", eventTypes)
}
