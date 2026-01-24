package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// makeRequest creates and sends an HTTP request to the test server.
func makeRequest(t *testing.T, method, path string, body interface{}) *http.Response {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("Failed to marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, testBaseURL+path, reqBody)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	return resp
}

// makeRawRequest creates and sends an HTTP request with raw bytes body.
func makeRawRequest(t *testing.T, method, path string, body []byte, headers map[string]string) *http.Response {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, testBaseURL+path, reqBody)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	return resp
}

// readResponseBody reads and returns the response body as bytes.
// It closes the response body after reading.
func readResponseBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	resp.Body.Close()
	return body
}

// assertStatusCode checks that the response has the expected status code.
func assertStatusCode(t *testing.T, resp *http.Response, expected int) {
	t.Helper()

	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("Expected status %d, got %d. Body: %s", expected, resp.StatusCode, string(body))
	}
}

// assertJSONPath checks that a JSON path in the response body matches the expected value.
func assertJSONPath(t *testing.T, body []byte, path string, expected interface{}) {
	t.Helper()

	result := gjson.GetBytes(body, path)
	if !result.Exists() {
		t.Fatalf("JSON path %q not found in response: %s", path, string(body))
	}

	switch exp := expected.(type) {
	case string:
		if result.String() != exp {
			t.Fatalf("Expected %s=%q, got %q", path, exp, result.String())
		}
	case int:
		if result.Int() != int64(exp) {
			t.Fatalf("Expected %s=%d, got %d", path, exp, result.Int())
		}
	case int64:
		if result.Int() != exp {
			t.Fatalf("Expected %s=%d, got %d", path, exp, result.Int())
		}
	case float64:
		if result.Float() != exp {
			t.Fatalf("Expected %s=%f, got %f", path, exp, result.Float())
		}
	case bool:
		if result.Bool() != exp {
			t.Fatalf("Expected %s=%v, got %v", path, exp, result.Bool())
		}
	default:
		t.Fatalf("Unsupported type for assertJSONPath: %T", expected)
	}
}

// assertJSONPathExists checks that a JSON path exists in the response body.
func assertJSONPathExists(t *testing.T, body []byte, path string) {
	t.Helper()

	result := gjson.GetBytes(body, path)
	if !result.Exists() {
		t.Fatalf("JSON path %q not found in response: %s", path, string(body))
	}
}

// assertJSONPathNotExists checks that a JSON path does not exist in the response body.
func assertJSONPathNotExists(t *testing.T, body []byte, path string) {
	t.Helper()

	result := gjson.GetBytes(body, path)
	if result.Exists() {
		t.Fatalf("JSON path %q should not exist but found value: %s", path, result.String())
	}
}

// assertJSONPathContains checks that a JSON path contains the expected substring.
func assertJSONPathContains(t *testing.T, body []byte, path, substring string) {
	t.Helper()

	result := gjson.GetBytes(body, path)
	if !result.Exists() {
		t.Fatalf("JSON path %q not found in response: %s", path, string(body))
	}

	if !strings.Contains(result.String(), substring) {
		t.Fatalf("Expected %s to contain %q, got %q", path, substring, result.String())
	}
}

// assertJSONArrayLength checks the length of a JSON array at the given path.
func assertJSONArrayLength(t *testing.T, body []byte, path string, expectedLen int) {
	t.Helper()

	result := gjson.GetBytes(body, path)
	if !result.Exists() {
		t.Fatalf("JSON path %q not found in response: %s", path, string(body))
	}

	if !result.IsArray() {
		t.Fatalf("JSON path %q is not an array: %s", path, result.String())
	}

	actualLen := len(result.Array())
	if actualLen != expectedLen {
		t.Fatalf("Expected %s array length %d, got %d", path, expectedLen, actualLen)
	}
}

// SSEEvent represents a Server-Sent Events event.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
	Retry int
}

// readSSEEvents reads all SSE events from a streaming response.
func readSSEEvents(t *testing.T, resp *http.Response) []SSEEvent {
	t.Helper()

	var events []SSEEvent
	scanner := bufio.NewScanner(resp.Body)
	defer resp.Body.Close()

	var currentEvent SSEEvent
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line signals end of current event
			if currentEvent.Data != "" || currentEvent.Event != "" {
				events = append(events, currentEvent)
				currentEvent = SSEEvent{}
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			currentEvent.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if currentEvent.Data != "" {
				currentEvent.Data += "\n" + data
			} else {
				currentEvent.Data = data
			}
		} else if strings.HasPrefix(line, "id:") {
			currentEvent.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		}
	}

	// Don't forget last event if no trailing newline
	if currentEvent.Data != "" || currentEvent.Event != "" {
		events = append(events, currentEvent)
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		t.Fatalf("Error reading SSE stream: %v", err)
	}

	return events
}

// assertSSEEventExists checks that an SSE event of the given type exists.
func assertSSEEventExists(t *testing.T, events []SSEEvent, eventType string) *SSEEvent {
	t.Helper()

	for _, event := range events {
		if event.Event == eventType {
			return &event
		}
	}
	t.Fatalf("SSE event type %q not found in events", eventType)
	return nil
}

// assertSSEContains checks that at least one SSE event contains the expected data.
func assertSSEContains(t *testing.T, events []SSEEvent, substring string) {
	t.Helper()

	for _, event := range events {
		if strings.Contains(event.Data, substring) {
			return
		}
	}
	t.Fatalf("No SSE event contains %q", substring)
}

// buildChatRequest creates a basic OpenAI-format chat completion request.
func buildChatRequest(model, userMessage string) map[string]interface{} {
	return map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": userMessage,
			},
		},
	}
}

// buildChatRequestWithSystem creates a chat request with a system message.
func buildChatRequestWithSystem(model, systemMessage, userMessage string) map[string]interface{} {
	return map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{
				"role":    "system",
				"content": systemMessage,
			},
			{
				"role":    "user",
				"content": userMessage,
			},
		},
	}
}

// buildChatRequestWithTools creates a chat request with tool definitions.
func buildChatRequestWithTools(model, userMessage string, tools []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": userMessage,
			},
		},
		"tools": tools,
	}
}

// buildToolDefinition creates a function tool definition.
func buildToolDefinition(name, description string, parameters map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": description,
			"parameters":  parameters,
		},
	}
}

// buildAnthropicRequest creates an Anthropic-format messages request.
func buildAnthropicRequest(model, userMessage string) map[string]interface{} {
	return map[string]interface{}{
		"model":      model,
		"max_tokens": 1024,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": userMessage,
			},
		},
	}
}

// buildAnthropicRequestWithTools creates an Anthropic request with tools.
func buildAnthropicRequestWithTools(model, userMessage string, tools []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"model":      model,
		"max_tokens": 1024,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": userMessage,
			},
		},
		"tools": tools,
	}
}

// buildAnthropicToolDefinition creates an Anthropic-format tool definition.
func buildAnthropicToolDefinition(name, description string, inputSchema map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"name":         name,
		"description":  description,
		"input_schema": inputSchema,
	}
}

// skipIfMockMode skips the test if running in mock mode.
func skipIfMockMode(t *testing.T) {
	t.Helper()
	if useMockLLM {
		t.Skip("Skipping real API test (set TEST_REAL_API_BUDGET to enable)")
	}
}

// skipIfRealMode skips the test if running in real API mode.
func skipIfRealMode(t *testing.T) {
	t.Helper()
	if !useMockLLM {
		t.Skip("Skipping mock-only test (unset TEST_REAL_API_BUDGET to enable)")
	}
}

// checkBudget verifies budget is available for real API tests.
func checkBudget(t *testing.T) {
	t.Helper()
	if budget == nil {
		return
	}
	if err := budget.CanMakeRequest(); err != nil {
		t.Skipf("Budget exhausted: %v", err)
	}
}

// recordUsage records API usage for budget tracking.
func recordUsage(t *testing.T, model string, responseBody []byte) {
	t.Helper()
	if budget != nil {
		budget.RecordRequestFromResponse(model, responseBody)
	}
}
