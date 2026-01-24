// Package integration provides integration tests for the CLI Proxy API.
// It uses mock LLM responses by default, with optional real API testing
// when TEST_REAL_API_BUDGET is set.
package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
)

// MockLLMRoundTripper intercepts HTTP requests to LLM APIs and returns mock responses.
// It implements http.RoundTripper for use as an http.Client transport.
type MockLLMRoundTripper struct {
	mu       sync.Mutex
	Requests []RecordedRequest

	// Custom response handlers for specific scenarios
	CustomResponses map[string]func(req *http.Request, body []byte) *http.Response
}

// RecordedRequest stores details of an intercepted HTTP request.
type RecordedRequest struct {
	URL     string
	Method  string
	Headers http.Header
	Body    []byte
}

// NewMockLLMRoundTripper creates a new mock transport with default behavior.
func NewMockLLMRoundTripper() *MockLLMRoundTripper {
	return &MockLLMRoundTripper{
		CustomResponses: make(map[string]func(req *http.Request, body []byte) *http.Response),
	}
}

// RoundTrip implements http.RoundTripper. It intercepts requests to LLM APIs
// and returns appropriate mock responses based on the target API.
func (m *MockLLMRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Read and record the request body
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body.Close()
		// Reset body for potential reuse
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	m.mu.Lock()
	m.Requests = append(m.Requests, RecordedRequest{
		URL:     req.URL.String(),
		Method:  req.Method,
		Headers: req.Header.Clone(),
		Body:    body,
	})
	m.mu.Unlock()

	// Check for custom response handler
	host := req.URL.Host
	if handler, ok := m.CustomResponses[host]; ok {
		return handler(req, body), nil
	}

	// Detect API type and return appropriate mock response
	if strings.Contains(host, "anthropic") {
		return m.mockClaudeResponse(req, body)
	}
	if strings.Contains(host, "openai") {
		return m.mockOpenAIResponse(req, body)
	}
	if strings.Contains(host, "generativelanguage.googleapis") || strings.Contains(host, "aiplatform.googleapis") {
		return m.mockGeminiResponse(req, body)
	}
	if strings.Contains(host, "openrouter") {
		return m.mockOpenRouterResponse(req, body)
	}

	// Default response for unknown hosts
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
		Header:     make(http.Header),
	}, nil
}

// GetRequests returns a copy of all recorded requests.
func (m *MockLLMRoundTripper) GetRequests() []RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]RecordedRequest, len(m.Requests))
	copy(result, m.Requests)
	return result
}

// ClearRequests clears all recorded requests.
func (m *MockLLMRoundTripper) ClearRequests() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Requests = nil
}

// LastRequest returns the most recently recorded request, or nil if none.
func (m *MockLLMRoundTripper) LastRequest() *RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Requests) == 0 {
		return nil
	}
	req := m.Requests[len(m.Requests)-1]
	return &req
}

// mockClaudeResponse returns a mock Anthropic Claude API response.
// Claude's API always uses SSE format even for non-streaming requests,
// so we return SSE format here for the translator to process correctly.
func (m *MockLLMRoundTripper) mockClaudeResponse(req *http.Request, body []byte) (*http.Response, error) {
	// Check if streaming is requested
	stream := gjson.GetBytes(body, "stream").Bool()

	if stream {
		return m.mockClaudeStreamResponse(req, body)
	}

	// Check for tool use in the request
	hasTools := gjson.GetBytes(body, "tools").Exists()
	model := gjson.GetBytes(body, "model").String()
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	// Build SSE events for non-streaming response
	// Claude's API returns SSE format even for non-streaming - the translator expects this
	var events []string

	// Message start event
	events = append(events, `event: message_start
data: {"type":"message_start","message":{"id":"msg_mock_001","type":"message","role":"assistant","content":[],"model":"`+model+`","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":0}}}`)

	if hasTools {
		// Content block start for tool_use
		events = append(events, `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_mock_001","name":"get_weather","input":{}}}`)

		// Input JSON delta
		events = append(events, `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"location\":\"San Francisco\"}"}}`)

		// Content block stop
		events = append(events, `event: content_block_stop
data: {"type":"content_block_stop","index":0}`)

		// Message delta with tool_use stop reason
		events = append(events, `event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":50}}`)
	} else {
		// Content block start for text
		events = append(events, `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)

		// Text delta
		events = append(events, `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"This is a mock response from Claude."}}`)

		// Content block stop
		events = append(events, `event: content_block_stop
data: {"type":"content_block_stop","index":0}`)

		// Message delta with end_turn stop reason
		events = append(events, `event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":20}}`)
	}

	// Message stop event
	events = append(events, `event: message_stop
data: {"type":"message_stop"}`)

	responseBody := []byte(strings.Join(events, "\n\n") + "\n\n")

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
	}, nil
}

// mockClaudeStreamResponse returns a mock streaming Claude response.
func (m *MockLLMRoundTripper) mockClaudeStreamResponse(req *http.Request, body []byte) (*http.Response, error) {
	model := gjson.GetBytes(body, "model").String()
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	// Build SSE stream
	events := []string{
		`event: message_start
data: {"type":"message_start","message":{"id":"msg_mock_001","type":"message","role":"assistant","content":[],"model":"` + model + `","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":0}}}

`,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"This is a mock "}}

`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"streaming response."}}

`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}

`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":10}}

`,
		`event: message_stop
data: {"type":"message_stop"}

`,
	}

	streamBody := strings.Join(events, "")

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(streamBody)),
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
	}, nil
}

// mockOpenAIResponse returns a mock OpenAI API response.
func (m *MockLLMRoundTripper) mockOpenAIResponse(req *http.Request, body []byte) (*http.Response, error) {
	stream := gjson.GetBytes(body, "stream").Bool()

	if stream {
		return m.mockOpenAIStreamResponse(req, body)
	}

	hasTools := gjson.GetBytes(body, "tools").Exists()
	model := gjson.GetBytes(body, "model").String()
	if model == "" {
		model = "gpt-4o"
	}

	var responseBody []byte
	if hasTools {
		// Response with tool_calls
		responseBody = []byte(`{
			"id": "chatcmpl-mock001",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "` + model + `",
			"choices": [
				{
					"index": 0,
					"message": {
						"role": "assistant",
						"content": null,
						"tool_calls": [
							{
								"id": "call_mock001",
								"type": "function",
								"function": {
									"name": "get_weather",
									"arguments": "{\"location\":\"San Francisco\"}"
								}
							}
						]
					},
					"finish_reason": "tool_calls"
				}
			],
			"usage": {
				"prompt_tokens": 100,
				"completion_tokens": 50,
				"total_tokens": 150
			}
		}`)
	} else {
		// Standard text response
		responseBody = []byte(`{
			"id": "chatcmpl-mock001",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "` + model + `",
			"choices": [
				{
					"index": 0,
					"message": {
						"role": "assistant",
						"content": "This is a mock response from OpenAI."
					},
					"finish_reason": "stop"
				}
			],
			"usage": {
				"prompt_tokens": 100,
				"completion_tokens": 20,
				"total_tokens": 120
			}
		}`)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}, nil
}

// mockOpenAIStreamResponse returns a mock streaming OpenAI response.
func (m *MockLLMRoundTripper) mockOpenAIStreamResponse(req *http.Request, body []byte) (*http.Response, error) {
	model := gjson.GetBytes(body, "model").String()
	if model == "" {
		model = "gpt-4o"
	}

	events := []string{
		`data: {"id":"chatcmpl-mock001","object":"chat.completion.chunk","created":1700000000,"model":"` + model + `","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

`,
		`data: {"id":"chatcmpl-mock001","object":"chat.completion.chunk","created":1700000000,"model":"` + model + `","choices":[{"index":0,"delta":{"content":"This is a mock "},"finish_reason":null}]}

`,
		`data: {"id":"chatcmpl-mock001","object":"chat.completion.chunk","created":1700000000,"model":"` + model + `","choices":[{"index":0,"delta":{"content":"streaming response."},"finish_reason":null}]}

`,
		`data: {"id":"chatcmpl-mock001","object":"chat.completion.chunk","created":1700000000,"model":"` + model + `","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

`,
		`data: [DONE]

`,
	}

	streamBody := strings.Join(events, "")

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(streamBody)),
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
	}, nil
}

// mockGeminiResponse returns a mock Google Gemini API response.
func (m *MockLLMRoundTripper) mockGeminiResponse(req *http.Request, body []byte) (*http.Response, error) {
	// Check if streaming (generateContentStream vs generateContent)
	isStream := strings.Contains(req.URL.Path, "streamGenerateContent")

	if isStream {
		return m.mockGeminiStreamResponse(req, body)
	}

	// Check for function calling
	hasFunctions := gjson.GetBytes(body, "tools").Exists()

	var responseBody []byte
	if hasFunctions {
		// Response with functionCall
		responseBody = []byte(`{
			"candidates": [
				{
					"content": {
						"parts": [
							{
								"functionCall": {
									"name": "get_weather",
									"args": {"location": "San Francisco"}
								}
							}
						],
						"role": "model"
					},
					"finishReason": "STOP",
					"index": 0
				}
			],
			"usageMetadata": {
				"promptTokenCount": 100,
				"candidatesTokenCount": 50,
				"totalTokenCount": 150
			}
		}`)
	} else {
		// Standard text response
		responseBody = []byte(`{
			"candidates": [
				{
					"content": {
						"parts": [
							{
								"text": "This is a mock response from Gemini."
							}
						],
						"role": "model"
					},
					"finishReason": "STOP",
					"index": 0
				}
			],
			"usageMetadata": {
				"promptTokenCount": 100,
				"candidatesTokenCount": 20,
				"totalTokenCount": 120
			}
		}`)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}, nil
}

// mockGeminiStreamResponse returns a mock streaming Gemini response.
func (m *MockLLMRoundTripper) mockGeminiStreamResponse(req *http.Request, body []byte) (*http.Response, error) {
	// Gemini uses newline-delimited JSON for streaming
	chunks := []map[string]interface{}{
		{
			"candidates": []map[string]interface{}{
				{
					"content": map[string]interface{}{
						"parts": []map[string]string{{"text": "This is a mock "}},
						"role":  "model",
					},
					"index": 0,
				},
			},
		},
		{
			"candidates": []map[string]interface{}{
				{
					"content": map[string]interface{}{
						"parts": []map[string]string{{"text": "streaming response."}},
						"role":  "model",
					},
					"finishReason": "STOP",
					"index":        0,
				},
			},
			"usageMetadata": map[string]int{
				"promptTokenCount":     100,
				"candidatesTokenCount": 10,
				"totalTokenCount":      110,
			},
		},
	}

	var buf bytes.Buffer
	for _, chunk := range chunks {
		data, _ := json.Marshal(chunk)
		buf.Write(data)
		buf.WriteString("\n")
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(&buf),
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}, nil
}

// mockOpenRouterResponse returns a mock OpenRouter API response.
// OpenRouter uses OpenAI-compatible format.
func (m *MockLLMRoundTripper) mockOpenRouterResponse(req *http.Request, body []byte) (*http.Response, error) {
	// Check if this is a pricing endpoint
	if strings.Contains(req.URL.Path, "/models") {
		return m.mockOpenRouterModelsResponse(req)
	}

	// For chat completions, use OpenAI format
	return m.mockOpenAIResponse(req, body)
}

// mockOpenRouterModelsResponse returns mock model pricing info.
func (m *MockLLMRoundTripper) mockOpenRouterModelsResponse(req *http.Request) (*http.Response, error) {
	responseBody := []byte(`{
		"data": [
			{
				"id": "anthropic/claude-sonnet-4-20250514",
				"pricing": {
					"prompt": "0.000003",
					"completion": "0.000015"
				}
			},
			{
				"id": "openai/gpt-4o",
				"pricing": {
					"prompt": "0.0000025",
					"completion": "0.00001"
				}
			},
			{
				"id": "google/gemini-2.0-flash",
				"pricing": {
					"prompt": "0.0000001",
					"completion": "0.0000004"
				}
			}
		]
	}`)

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}, nil
}

// SetToolCallResponse configures the mock to return a specific tool call.
func (m *MockLLMRoundTripper) SetToolCallResponse(toolName string, toolArgs map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	argsJSON, _ := json.Marshal(toolArgs)

	m.CustomResponses["api.anthropic.com"] = func(req *http.Request, body []byte) *http.Response {
		// Build SSE events for tool call response
		var events []string

		// Message start event
		events = append(events, `event: message_start
data: {"type":"message_start","message":{"id":"msg_mock_001","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":0}}}`)

		// Content block start for tool_use
		events = append(events, `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_mock_001","name":"`+toolName+`","input":{}}}`)

		// Input JSON delta
		events = append(events, `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"`+strings.ReplaceAll(string(argsJSON), `"`, `\"`)+`"}}`)

		// Content block stop
		events = append(events, `event: content_block_stop
data: {"type":"content_block_stop","index":0}`)

		// Message delta with tool_use stop reason
		events = append(events, `event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":50}}`)

		// Message stop event
		events = append(events, `event: message_stop
data: {"type":"message_stop"}`)

		responseBody := []byte(strings.Join(events, "\n\n") + "\n\n")

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		}
	}
}

// SetThinkingResponse configures the mock to return a thinking block with signature.
func (m *MockLLMRoundTripper) SetThinkingResponse(thinkingText string, signature string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CustomResponses["api.anthropic.com"] = func(req *http.Request, body []byte) *http.Response {
		// Build SSE events for thinking response
		var events []string

		// Message start event
		events = append(events, `event: message_start
data: {"type":"message_start","message":{"id":"msg_mock_001","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":0}}}`)

		// Thinking content block start
		events = append(events, `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)

		// Thinking text delta
		thinkingEscaped := strings.ReplaceAll(thinkingText, `"`, `\"`)
		thinkingEscaped = strings.ReplaceAll(thinkingEscaped, "\n", "\\n")
		events = append(events, `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"`+thinkingEscaped+`"}}`)

		// Thinking signature
		events = append(events, `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"`+signature+`"}}`)

		// Thinking content block stop
		events = append(events, `event: content_block_stop
data: {"type":"content_block_stop","index":0}`)

		// Text content block start
		events = append(events, `event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)

		// Text delta
		events = append(events, `event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"This is the response after thinking."}}`)

		// Text content block stop
		events = append(events, `event: content_block_stop
data: {"type":"content_block_stop","index":1}`)

		// Message delta with end_turn stop reason
		events = append(events, `event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":50}}`)

		// Message stop event
		events = append(events, `event: message_stop
data: {"type":"message_stop"}`)

		responseBody := []byte(strings.Join(events, "\n\n") + "\n\n")

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		}
	}
}

// SetErrorResponse configures the mock to return an error response.
func (m *MockLLMRoundTripper) SetErrorResponse(statusCode int, errorType, errorMessage string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CustomResponses["api.anthropic.com"] = func(req *http.Request, body []byte) *http.Response {
		responseBody := []byte(`{
			"type": "error",
			"error": {
				"type": "` + errorType + `",
				"message": "` + errorMessage + `"
			}
		}`)

		return &http.Response{
			StatusCode: statusCode,
			Status:     http.StatusText(statusCode),
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}
	}
}

// ClearCustomResponses removes all custom response handlers.
func (m *MockLLMRoundTripper) ClearCustomResponses() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CustomResponses = make(map[string]func(req *http.Request, body []byte) *http.Response)
}
