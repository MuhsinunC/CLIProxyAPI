// Package openai provides response translation functionality for Claude Code to OpenAI API compatibility.
// This package handles the conversion of Claude Code API responses into OpenAI Chat Completions-compatible
// JSON format, transforming streaming events and non-streaming responses into the format
// expected by OpenAI API clients. It supports both streaming and non-streaming modes,
// handling text content, tool calls, reasoning content, and usage metadata appropriately.
package chat_completions

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var (
	dataTag = []byte("data:")
)

// unwrapResponseFormatEnvelope detects and unwraps common envelope patterns
// that Claude may use when responding to json_object requests.
// Handles: {"result": ...}, {"response": ...}, {"values": ...} (LiteLLM pattern)
// The inner value can be a string (double-encoded JSON) or an object/array.
func unwrapResponseFormatEnvelope(jsonStr string) string {
	parsed := gjson.Parse(jsonStr)
	if !parsed.IsObject() {
		return jsonStr
	}

	// Check for single-key envelope patterns (includes LiteLLM's "values" pattern)
	envelopeKeys := []string{"result", "response", "values"}
	obj := parsed.Map()
	if len(obj) == 1 {
		for _, key := range envelopeKeys {
			if val, exists := obj[key]; exists {
				// If value is a string, it might be double-encoded JSON
				if val.Type == gjson.String {
					inner := val.String()
					if gjson.Valid(inner) {
						return inner
					}
				}
				// If value is an object/array, return it directly
				if val.IsObject() || val.IsArray() {
					return val.Raw
				}
			}
		}
	}
	return jsonStr
}

// stripMarkdownCodeFences removes markdown code fences from JSON responses.
// Claude sometimes wraps JSON in ```json...``` even when instructed not to.
// This handles both ```json and ``` prefixes with optional language identifiers.
func stripMarkdownCodeFences(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}

	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return content
	}

	// Find the closing ``` fence
	lastIdx := len(lines) - 1
	for lastIdx >= 0 && strings.TrimSpace(lines[lastIdx]) == "" {
		lastIdx--
	}

	if lastIdx > 0 && strings.TrimSpace(lines[lastIdx]) == "```" {
		// Remove first line (```json or ```) and last line (```)
		innerLines := lines[1:lastIdx]
		return strings.TrimSpace(strings.Join(innerLines, "\n"))
	}

	return content
}

// ConvertAnthropicResponseToOpenAIParams holds parameters for response conversion
type ConvertAnthropicResponseToOpenAIParams struct {
	CreatedAt    int64
	ResponseID   string
	FinishReason string
	// Tool calls accumulator for streaming
	ToolCallsAccumulator map[int]*ToolCallAccumulator
	// Response format handling: tracks if we intercepted a response_format tool
	ResponseFormatToolName string // Name of injected response_format tool (empty if not applicable)
	ResponseFormatHandled  bool   // Whether we've output the response_format tool as content
	// JsonObjectMode: true when response_format.type is "json_object" (needs markdown stripping)
	JsonObjectMode bool
}

// ToolCallAccumulator holds the state for accumulating tool call data
type ToolCallAccumulator struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

// ConvertClaudeResponseToOpenAI converts Claude Code streaming response format to OpenAI Chat Completions format.
// This function processes various Claude Code event types and transforms them into OpenAI-compatible JSON responses.
// It handles text content, tool calls, reasoning content, and usage metadata, outputting responses that match
// the OpenAI API format. The function supports incremental updates for streaming responses.
//
// Parameters:
//   - ctx: The context for the request, used for cancellation and timeout handling
//   - modelName: The name of the model being used for the response
//   - rawJSON: The raw JSON response from the Claude Code API
//   - param: A pointer to a parameter object for maintaining state between calls
//
// Returns:
//   - []string: A slice of strings, each containing an OpenAI-compatible JSON response
func ConvertClaudeResponseToOpenAI(_ context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []string {
	if *param == nil {
		*param = &ConvertAnthropicResponseToOpenAIParams{
			CreatedAt:    0,
			ResponseID:   "",
			FinishReason: "",
		}
	}

	if !bytes.HasPrefix(rawJSON, dataTag) {
		return []string{}
	}
	rawJSON = bytes.TrimSpace(rawJSON[5:])

	root := gjson.ParseBytes(rawJSON)
	eventType := root.Get("type").String()

	// Base OpenAI streaming response template
	template := `{"id":"","object":"chat.completion.chunk","created":0,"model":"","choices":[{"index":0,"delta":{},"finish_reason":null}]}`

	// Set model
	if modelName != "" {
		template, _ = sjson.Set(template, "model", modelName)
	}

	// Set response ID and creation time
	if (*param).(*ConvertAnthropicResponseToOpenAIParams).ResponseID != "" {
		template, _ = sjson.Set(template, "id", (*param).(*ConvertAnthropicResponseToOpenAIParams).ResponseID)
	}
	if (*param).(*ConvertAnthropicResponseToOpenAIParams).CreatedAt > 0 {
		template, _ = sjson.Set(template, "created", (*param).(*ConvertAnthropicResponseToOpenAIParams).CreatedAt)
	}

	switch eventType {
	case "message_start":
		// Initialize response with message metadata when a new message begins
		if message := root.Get("message"); message.Exists() {
			(*param).(*ConvertAnthropicResponseToOpenAIParams).ResponseID = message.Get("id").String()
			(*param).(*ConvertAnthropicResponseToOpenAIParams).CreatedAt = time.Now().Unix()

			template, _ = sjson.Set(template, "id", (*param).(*ConvertAnthropicResponseToOpenAIParams).ResponseID)
			template, _ = sjson.Set(template, "model", modelName)
			template, _ = sjson.Set(template, "created", (*param).(*ConvertAnthropicResponseToOpenAIParams).CreatedAt)

			// Set initial role to assistant for the response
			template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")

			// Initialize tool calls accumulator for tracking tool call progress
			if (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator == nil {
				(*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator = make(map[int]*ToolCallAccumulator)
			}

			// Detect response_format in original request
			// For json_schema: track tool name for interception
			// For json_object: set JsonObjectMode (no tool injection, needs markdown stripping)
			if rf := gjson.GetBytes(originalRequestRawJSON, "response_format"); rf.Exists() {
				rfType := rf.Get("type").String()
				origTools := gjson.GetBytes(originalRequestRawJSON, "tools")
				hasOrigTools := origTools.Exists() && origTools.IsArray() && len(origTools.Array()) > 0

				if !hasOrigTools {
					if rfType == "json_schema" {
						toolName := rf.Get("json_schema.name").String()
						if toolName == "" {
							toolName = "structured_output"
						}
						(*param).(*ConvertAnthropicResponseToOpenAIParams).ResponseFormatToolName = toolName
					} else if rfType == "json_object" {
						// json_object mode: no tool injection, track for markdown stripping
						(*param).(*ConvertAnthropicResponseToOpenAIParams).JsonObjectMode = true
					}
				}
			}
		}
		return []string{template}

	case "content_block_start":
		// Start of a content block (text, tool use, or reasoning)
		if contentBlock := root.Get("content_block"); contentBlock.Exists() {
			blockType := contentBlock.Get("type").String()

			if blockType == "tool_use" {
				// Start of tool call - initialize accumulator to track arguments
				toolCallID := contentBlock.Get("id").String()
				toolName := contentBlock.Get("name").String()
				index := int(root.Get("index").Int())

				if (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator == nil {
					(*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator = make(map[int]*ToolCallAccumulator)
				}

				(*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator[index] = &ToolCallAccumulator{
					ID:   toolCallID,
					Name: toolName,
				}

				// Don't output anything yet - wait for complete tool call
				return []string{}
			}
		}
		return []string{}

	case "content_block_delta":
		// Handle content delta (text, tool use arguments, or reasoning content)
		hasContent := false
		if delta := root.Get("delta"); delta.Exists() {
			deltaType := delta.Get("type").String()

			switch deltaType {
			case "text_delta":
				// Text content delta - send incremental text updates
				if text := delta.Get("text"); text.Exists() {
					template, _ = sjson.Set(template, "choices.0.delta.content", text.String())
					hasContent = true
				}
			case "thinking_delta":
				// Accumulate reasoning/thinking content
				if thinking := delta.Get("thinking"); thinking.Exists() {
					template, _ = sjson.Set(template, "choices.0.delta.reasoning_content", thinking.String())
					hasContent = true
				}
			case "input_json_delta":
				// Tool use input delta - accumulate arguments for tool calls
				if partialJSON := delta.Get("partial_json"); partialJSON.Exists() {
					index := int(root.Get("index").Int())
					if (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator != nil {
						if accumulator, exists := (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator[index]; exists {
							accumulator.Arguments.WriteString(partialJSON.String())
						}
					}
				}
				// Don't output anything yet - wait for complete tool call
				return []string{}
			}
		}
		if hasContent {
			return []string{template}
		} else {
			return []string{}
		}

	case "content_block_stop":
		// End of content block - output complete tool call if it's a tool_use block
		index := int(root.Get("index").Int())
		if (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator != nil {
			if accumulator, exists := (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator[index]; exists {
				// Build complete tool call with accumulated arguments
				arguments := accumulator.Arguments.String()
				if arguments == "" {
					arguments = "{}"
				}

				// Check if this is our injected response_format tool
				// Note: The executor may add a "proxy_" prefix to tool names, so check both forms
				responseFormatToolName := (*param).(*ConvertAnthropicResponseToOpenAIParams).ResponseFormatToolName
				isResponseFormatTool := responseFormatToolName != "" &&
					(accumulator.Name == responseFormatToolName || accumulator.Name == "proxy_"+responseFormatToolName)
				if isResponseFormatTool {
					// This is our injected tool - output arguments as content instead of tool_calls
					// Unwrap any envelope patterns like {"result": ...} or {"response": ...}
					arguments = unwrapResponseFormatEnvelope(arguments)
					template, _ = sjson.Set(template, "choices.0.delta.content", arguments)
					(*param).(*ConvertAnthropicResponseToOpenAIParams).ResponseFormatHandled = true

					// Clean up the accumulator for this index
					delete((*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator, index)

					return []string{template}
				}

				// Regular tool call handling
				template, _ = sjson.Set(template, "choices.0.delta.tool_calls.0.index", index)
				template, _ = sjson.Set(template, "choices.0.delta.tool_calls.0.id", accumulator.ID)
				template, _ = sjson.Set(template, "choices.0.delta.tool_calls.0.type", "function")
				template, _ = sjson.Set(template, "choices.0.delta.tool_calls.0.function.name", accumulator.Name)
				template, _ = sjson.Set(template, "choices.0.delta.tool_calls.0.function.arguments", arguments)

				// Clean up the accumulator for this index
				delete((*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator, index)

				return []string{template}
			}
		}
		return []string{}

	case "message_delta":
		// Handle message-level changes including stop reason and usage
		if delta := root.Get("delta"); delta.Exists() {
			if stopReason := delta.Get("stop_reason"); stopReason.Exists() {
				finishReason := mapAnthropicStopReasonToOpenAI(stopReason.String())

				// If response_format was handled and stop_reason was "tool_use",
				// change finish_reason to "stop" since we converted the tool call to content
				if (*param).(*ConvertAnthropicResponseToOpenAIParams).ResponseFormatHandled && stopReason.String() == "tool_use" {
					finishReason = "stop"
				}

				(*param).(*ConvertAnthropicResponseToOpenAIParams).FinishReason = finishReason
				template, _ = sjson.Set(template, "choices.0.finish_reason", finishReason)
			}
		}

		// Handle usage information for token counts
		if usage := root.Get("usage"); usage.Exists() {
			inputTokens := usage.Get("input_tokens").Int()
			outputTokens := usage.Get("output_tokens").Int()
			cacheReadInputTokens := usage.Get("cache_read_input_tokens").Int()
			cacheCreationInputTokens := usage.Get("cache_creation_input_tokens").Int()
			template, _ = sjson.Set(template, "usage.prompt_tokens", inputTokens+cacheCreationInputTokens)
			template, _ = sjson.Set(template, "usage.completion_tokens", outputTokens)
			template, _ = sjson.Set(template, "usage.total_tokens", inputTokens+outputTokens)
			template, _ = sjson.Set(template, "usage.prompt_tokens_details.cached_tokens", cacheReadInputTokens)
		}
		return []string{template}

	case "message_stop":
		// Final message event - no additional output needed
		return []string{}

	case "ping":
		// Ping events for keeping connection alive - no output needed
		return []string{}

	case "error":
		// Error event - format and return error response
		if errorData := root.Get("error"); errorData.Exists() {
			errorJSON := `{"error":{"message":"","type":""}}`
			errorJSON, _ = sjson.Set(errorJSON, "error.message", errorData.Get("message").String())
			errorJSON, _ = sjson.Set(errorJSON, "error.type", errorData.Get("type").String())
			return []string{errorJSON}
		}
		return []string{}

	default:
		// Unknown event type - ignore
		return []string{}
	}
}

// mapAnthropicStopReasonToOpenAI maps Anthropic stop reasons to OpenAI stop reasons
func mapAnthropicStopReasonToOpenAI(anthropicReason string) string {
	switch anthropicReason {
	case "end_turn":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	default:
		return "stop"
	}
}

// ConvertClaudeResponseToOpenAINonStream converts a non-streaming Claude Code response to a non-streaming OpenAI response.
// This function processes the complete Claude Code response and transforms it into a single OpenAI-compatible
// JSON response. It handles message content, tool calls, reasoning content, and usage metadata, combining all
// the information into a single response that matches the OpenAI API format.
//
// Parameters:
//   - ctx: The context for the request, used for cancellation and timeout handling
//   - modelName: The name of the model being used for the response (unused in current implementation)
//   - rawJSON: The raw JSON response from the Claude Code API
//   - param: A pointer to a parameter object for the conversion (unused in current implementation)
//
// Returns:
//   - string: An OpenAI-compatible JSON response containing all message content and metadata
func ConvertClaudeResponseToOpenAINonStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) string {
	chunks := make([][]byte, 0)

	lines := bytes.Split(rawJSON, []byte("\n"))
	for _, line := range lines {
		if !bytes.HasPrefix(line, dataTag) {
			continue
		}
		chunks = append(chunks, bytes.TrimSpace(line[5:]))
	}

	// Check if original request had response_format (json_schema or json_object) and no other tools
	// For json_schema: intercept the injected tool and return its arguments as content
	// For json_object: no tool injection, but apply markdown stripping to text content
	var responseFormatToolName string
	var jsonObjectMode bool
	if rf := gjson.GetBytes(originalRequestRawJSON, "response_format"); rf.Exists() {
		rfType := rf.Get("type").String()
		origTools := gjson.GetBytes(originalRequestRawJSON, "tools")
		hasOrigTools := origTools.Exists() && origTools.IsArray() && len(origTools.Array()) > 0

		if !hasOrigTools {
			if rfType == "json_schema" {
				responseFormatToolName = rf.Get("json_schema.name").String()
				if responseFormatToolName == "" {
					responseFormatToolName = "structured_output"
				}
			} else if rfType == "json_object" {
				// json_object mode: no tool injection, just needs markdown stripping
				jsonObjectMode = true
			}
		}
	}

	// Base OpenAI non-streaming response template
	out := `{"id":"","object":"chat.completion","created":0,"model":"","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`

	var messageID string
	var model string
	var createdAt int64
	var stopReason string
	var contentParts []string
	var reasoningParts []string
	toolCallsAccumulator := make(map[int]*ToolCallAccumulator)

	for _, chunk := range chunks {
		root := gjson.ParseBytes(chunk)
		eventType := root.Get("type").String()

		switch eventType {
		case "message_start":
			// Extract initial message metadata including ID, model, and input token count
			if message := root.Get("message"); message.Exists() {
				messageID = message.Get("id").String()
				model = message.Get("model").String()
				createdAt = time.Now().Unix()
			}

		case "content_block_start":
			// Handle different content block types at the beginning
			if contentBlock := root.Get("content_block"); contentBlock.Exists() {
				blockType := contentBlock.Get("type").String()
				if blockType == "thinking" {
					// Start of thinking/reasoning content - skip for now as it's handled in delta
					continue
				} else if blockType == "tool_use" {
					// Initialize tool call accumulator for this index
					index := int(root.Get("index").Int())
					toolCallsAccumulator[index] = &ToolCallAccumulator{
						ID:   contentBlock.Get("id").String(),
						Name: contentBlock.Get("name").String(),
					}
				}
			}

		case "content_block_delta":
			// Process incremental content updates
			if delta := root.Get("delta"); delta.Exists() {
				deltaType := delta.Get("type").String()
				switch deltaType {
				case "text_delta":
					// Accumulate text content
					if text := delta.Get("text"); text.Exists() {
						contentParts = append(contentParts, text.String())
					}
				case "thinking_delta":
					// Accumulate reasoning/thinking content
					if thinking := delta.Get("thinking"); thinking.Exists() {
						reasoningParts = append(reasoningParts, thinking.String())
					}
				case "input_json_delta":
					// Accumulate tool call arguments
					if partialJSON := delta.Get("partial_json"); partialJSON.Exists() {
						index := int(root.Get("index").Int())
						if accumulator, exists := toolCallsAccumulator[index]; exists {
							accumulator.Arguments.WriteString(partialJSON.String())
						}
					}
				}
			}

		case "content_block_stop":
			// Finalize tool call arguments for this index when content block ends
			index := int(root.Get("index").Int())
			if accumulator, exists := toolCallsAccumulator[index]; exists {
				if accumulator.Arguments.Len() == 0 {
					accumulator.Arguments.WriteString("{}")
				}
			}

		case "message_delta":
			// Extract stop reason and output token count when message ends
			if delta := root.Get("delta"); delta.Exists() {
				if sr := delta.Get("stop_reason"); sr.Exists() {
					stopReason = sr.String()
				}
			}
			if usage := root.Get("usage"); usage.Exists() {
				inputTokens := usage.Get("input_tokens").Int()
				outputTokens := usage.Get("output_tokens").Int()
				cacheReadInputTokens := usage.Get("cache_read_input_tokens").Int()
				cacheCreationInputTokens := usage.Get("cache_creation_input_tokens").Int()
				out, _ = sjson.Set(out, "usage.prompt_tokens", inputTokens+cacheCreationInputTokens)
				out, _ = sjson.Set(out, "usage.completion_tokens", outputTokens)
				out, _ = sjson.Set(out, "usage.total_tokens", inputTokens+outputTokens)
				out, _ = sjson.Set(out, "usage.prompt_tokens_details.cached_tokens", cacheReadInputTokens)
			}
		}
	}

	// Set basic response fields including message ID, creation time, and model
	out, _ = sjson.Set(out, "id", messageID)
	out, _ = sjson.Set(out, "created", createdAt)
	out, _ = sjson.Set(out, "model", model)

	// Set message content by combining all text parts
	messageContent := strings.Join(contentParts, "")
	// For json_object mode, strip any markdown code fences that Claude might add
	if jsonObjectMode && messageContent != "" {
		messageContent = stripMarkdownCodeFences(messageContent)
	}
	out, _ = sjson.Set(out, "choices.0.message.content", messageContent)

	// Add reasoning content if available (following OpenAI reasoning format)
	if len(reasoningParts) > 0 {
		reasoningContent := strings.Join(reasoningParts, "")
		// Add reasoning as a separate field in the message
		out, _ = sjson.Set(out, "choices.0.message.reasoning", reasoningContent)
	}

	// Set tool calls if any were accumulated during processing
	// Special handling: if response_format was used, intercept the injected tool and return as content
	responseFormatHandled := false
	if len(toolCallsAccumulator) > 0 {
		toolCallsCount := 0
		maxIndex := -1
		for index := range toolCallsAccumulator {
			if index > maxIndex {
				maxIndex = index
			}
		}

		for i := 0; i <= maxIndex; i++ {
			accumulator, exists := toolCallsAccumulator[i]
			if !exists {
				continue
			}

			arguments := accumulator.Arguments.String()

			// Check if this is our injected response_format tool
			// Note: The executor may add a "proxy_" prefix to tool names, so check both forms
			isResponseFormatTool := responseFormatToolName != "" &&
				(accumulator.Name == responseFormatToolName || accumulator.Name == "proxy_"+responseFormatToolName)
			if isResponseFormatTool {
				// This is our injected tool - extract arguments as content instead
				// Unwrap any envelope patterns like {"result": ...} or {"response": ...}
				arguments = unwrapResponseFormatEnvelope(arguments)
				out, _ = sjson.Set(out, "choices.0.message.content", arguments)
				responseFormatHandled = true
				// Don't add to tool_calls, skip to next
				continue
			}

			idPath := fmt.Sprintf("choices.0.message.tool_calls.%d.id", toolCallsCount)
			typePath := fmt.Sprintf("choices.0.message.tool_calls.%d.type", toolCallsCount)
			namePath := fmt.Sprintf("choices.0.message.tool_calls.%d.function.name", toolCallsCount)
			argumentsPath := fmt.Sprintf("choices.0.message.tool_calls.%d.function.arguments", toolCallsCount)

			out, _ = sjson.Set(out, idPath, accumulator.ID)
			out, _ = sjson.Set(out, typePath, "function")
			out, _ = sjson.Set(out, namePath, accumulator.Name)
			out, _ = sjson.Set(out, argumentsPath, arguments)
			toolCallsCount++
		}

		// Set finish_reason based on what was handled
		if responseFormatHandled && toolCallsCount == 0 {
			// Only response_format tool was called - finish as "stop"
			out, _ = sjson.Set(out, "choices.0.finish_reason", "stop")
		} else if toolCallsCount > 0 {
			out, _ = sjson.Set(out, "choices.0.finish_reason", "tool_calls")
		} else {
			out, _ = sjson.Set(out, "choices.0.finish_reason", mapAnthropicStopReasonToOpenAI(stopReason))
		}
	} else {
		out, _ = sjson.Set(out, "choices.0.finish_reason", mapAnthropicStopReasonToOpenAI(stopReason))
	}

	return out
}
