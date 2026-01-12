// Package openai provides response translation functionality for Gemini CLI to OpenAI API compatibility.
// This package handles the conversion of Gemini CLI API responses into OpenAI Chat Completions-compatible
// JSON format, transforming streaming events and non-streaming responses into the format
// expected by OpenAI API clients. It supports both streaming and non-streaming modes,
// handling text content, tool calls, reasoning content, and usage metadata appropriately.
package chat_completions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	. "github.com/router-for-me/CLIProxyAPI/v6/internal/translator/gemini/openai/chat-completions"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertCliResponseToOpenAIChatParams holds parameters for response conversion.
type convertCliResponseToOpenAIChatParams struct {
	UnixTimestamp  int64
	FunctionIndex  int
	HadToolCall    bool            // True if a tool call has been emitted in this sequence
	XMLToolBuffer  strings.Builder // Accumulates XML tool call text
	InXMLToolBlock bool            // True when inside a <tool_call> block
	TextBeforeXML  string          // Text before the XML started
	IsFirstChunk   bool            // True if this is the first chunk emitted
}

// functionCallIDCounter provides a process-wide unique counter for function call identifiers.
var functionCallIDCounter uint64

// XML tool call pattern: <tool_call>{"name": "...", "arguments": {...}}</tool_call>
var xmlToolCallRe = regexp.MustCompile(`<tool_call>\s*(\{[\s\S]*?\})\s*</tool_call>`)

// parseXMLToolCallsFromText parses <tool_call>{JSON}</tool_call> patterns from text
// Returns: parsed tool calls, remaining text (without the tool call XML)
func parseXMLToolCallsFromText(text string) ([]parsedXMLToolCall, string) {
	var calls []parsedXMLToolCall

	matches := xmlToolCallRe.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		jsonContent := match[1]

		var toolCall struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(jsonContent), &toolCall); err != nil {
			continue
		}

		// Marshal arguments back to JSON string
		argsJSON, err := json.Marshal(toolCall.Arguments)
		if err != nil {
			argsJSON = []byte("{}")
		}

		calls = append(calls, parsedXMLToolCall{
			Name:      toolCall.Name,
			Arguments: string(argsJSON),
		})
	}

	// Remove tool call XML from text
	remainingText := xmlToolCallRe.ReplaceAllString(text, "")
	remainingText = strings.TrimSpace(remainingText)

	return calls, remainingText
}

type parsedXMLToolCall struct {
	Name      string
	Arguments string
}

// ConvertAntigravityResponseToOpenAI translates a single chunk of a streaming response from the
// Gemini CLI API format to the OpenAI Chat Completions streaming format.
// It processes various Gemini CLI event types and transforms them into OpenAI-compatible JSON responses.
// The function handles text content, tool calls, reasoning content, and usage metadata, outputting
// responses that match the OpenAI API format. It supports incremental updates for streaming responses.
//
// Parameters:
//   - ctx: The context for the request, used for cancellation and timeout handling
//   - modelName: The name of the model being used for the response (unused in current implementation)
//   - rawJSON: The raw JSON response from the Gemini CLI API
//   - param: A pointer to a parameter object for maintaining state between calls
//
// Returns:
//   - []string: A slice of strings, each containing an OpenAI-compatible JSON response
func ConvertAntigravityResponseToOpenAI(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []string {
	if *param == nil {
		*param = &convertCliResponseToOpenAIChatParams{
			UnixTimestamp: 0,
			FunctionIndex: 0,
			HadToolCall:   false,
			IsFirstChunk:  true,
		}
	}

	if bytes.Equal(rawJSON, []byte("[DONE]")) {
		return []string{}
	}

	// Initialize the OpenAI SSE template - include both 'reasoning' and 'reasoning_content' for max compatibility
	template := `{"id":"","object":"chat.completion.chunk","created":12345,"model":"model","choices":[{"index":0,"delta":{"role":null,"content":null,"reasoning":null,"reasoning_content":null,"tool_calls":null},"finish_reason":null}]}`

	// Extract and set the model version.
	if modelVersionResult := gjson.GetBytes(rawJSON, "response.modelVersion"); modelVersionResult.Exists() {
		template, _ = sjson.Set(template, "model", modelVersionResult.String())
	}

	// Extract and set the creation timestamp.
	if createTimeResult := gjson.GetBytes(rawJSON, "response.createTime"); createTimeResult.Exists() {
		t, err := time.Parse(time.RFC3339Nano, createTimeResult.String())
		if err == nil {
			(*param).(*convertCliResponseToOpenAIChatParams).UnixTimestamp = t.Unix()
		}
		template, _ = sjson.Set(template, "created", (*param).(*convertCliResponseToOpenAIChatParams).UnixTimestamp)
	} else {
		template, _ = sjson.Set(template, "created", (*param).(*convertCliResponseToOpenAIChatParams).UnixTimestamp)
	}

	// Extract and set the response ID.
	if responseIDResult := gjson.GetBytes(rawJSON, "response.responseId"); responseIDResult.Exists() {
		template, _ = sjson.Set(template, "id", responseIDResult.String())
	}

	// Extract and set the finish reason.
	hasMetadata := false
	if finishReasonResult := gjson.GetBytes(rawJSON, "response.candidates.0.finishReason"); finishReasonResult.Exists() {
		fr := strings.ToLower(finishReasonResult.String())
		params := (*param).(*convertCliResponseToOpenAIChatParams)
		if fr == "stop" && params.HadToolCall {
			fr = "tool_calls"
		}
		template, _ = sjson.Set(template, "choices.0.finish_reason", fr)
		hasMetadata = true
	}

	// Extract and set usage metadata (token counts).
	if usageResult := gjson.GetBytes(rawJSON, "response.usageMetadata"); usageResult.Exists() {
		cachedTokenCount := usageResult.Get("cachedContentTokenCount").Int()
		if candidatesTokenCountResult := usageResult.Get("candidatesTokenCount"); candidatesTokenCountResult.Exists() {
			template, _ = sjson.Set(template, "usage.completion_tokens", candidatesTokenCountResult.Int())
		}
		if totalTokenCountResult := usageResult.Get("totalTokenCount"); totalTokenCountResult.Exists() {
			template, _ = sjson.Set(template, "usage.total_tokens", totalTokenCountResult.Int())
		}
		promptTokenCount := usageResult.Get("promptTokenCount").Int() - cachedTokenCount
		thoughtsTokenCount := usageResult.Get("thoughtsTokenCount").Int()
		template, _ = sjson.Set(template, "usage.prompt_tokens", promptTokenCount+thoughtsTokenCount)
		if thoughtsTokenCount > 0 {
			template, _ = sjson.Set(template, "usage.completion_tokens_details.reasoning_tokens", thoughtsTokenCount)
		}
		// Include cached token count if present (indicates prompt caching is working)
		if cachedTokenCount > 0 {
			var err error
			template, err = sjson.Set(template, "usage.prompt_tokens_details.cached_tokens", cachedTokenCount)
			if err != nil {
				log.Warnf("antigravity openai response: failed to set cached_tokens: %v", err)
			}
		}
	}

	// Process the main content part of the response.
	partsResult := gjson.GetBytes(rawJSON, "response.candidates.0.content.parts")
	hasFunctionCall := false
	hasContent := false // Track if we have meaningful content to emit
	if partsResult.IsArray() {
		partResults := partsResult.Array()
		for i := 0; i < len(partResults); i++ {
			partResult := partResults[i]
			partTextResult := partResult.Get("text")
			functionCallResult := partResult.Get("functionCall")
			thoughtSignatureResult := partResult.Get("thoughtSignature")
			if !thoughtSignatureResult.Exists() {
				thoughtSignatureResult = partResult.Get("thought_signature")
			}
			inlineDataResult := partResult.Get("inlineData")
			if !inlineDataResult.Exists() {
				inlineDataResult = partResult.Get("inline_data")
			}

			hasThoughtSignature := thoughtSignatureResult.Exists() && thoughtSignatureResult.String() != ""
			hasContentPayload := partTextResult.Exists() || functionCallResult.Exists() || inlineDataResult.Exists()

			// Ignore encrypted thoughtSignature but keep any actual content in the same part.
			if hasThoughtSignature && !hasContentPayload {
				continue
			}

			if partTextResult.Exists() {
				textContent := partTextResult.String()
				params := (*param).(*convertCliResponseToOpenAIChatParams)

				// ALWAYS accumulate text first to handle fragmented tool_call tags
				// Text chunks before <tool_call> must not be emitted until we know there's no tool call
				params.XMLToolBuffer.WriteString(textContent)
				accumulated := params.XMLToolBuffer.String()

				// Check if we might be in a tool call block
				containsToolStart := strings.Contains(accumulated, "<tool_call>")
				containsToolEnd := strings.Contains(accumulated, "</tool_call>")
				mightHavePartialTag := strings.Contains(accumulated, "<tool") && !containsToolStart

				if containsToolStart && containsToolEnd {
					// We have complete tool call(s) - process them
					params.InXMLToolBlock = true

					// Check if we see tool_call start but haven't marked it yet
					if !params.InXMLToolBlock && strings.Contains(accumulated, "<tool_call>") {
						params.InXMLToolBlock = true
						// Extract text before the first <tool_call>
						idx := strings.Index(accumulated, "<tool_call>")
						if idx > 0 {
							params.TextBeforeXML = accumulated[:idx]
						}
					}

					// Check if we have complete tool call blocks to parse
					if params.InXMLToolBlock && strings.Contains(accumulated, "</tool_call>") {
						// Try to parse all complete tool calls
						parsedCalls, remainingText := parseXMLToolCallsFromText(accumulated)

						if len(parsedCalls) > 0 {
							hasContent = true
							// First emit any text that came before tool calls
							if params.TextBeforeXML != "" {
								if partResult.Get("thought").Bool() {
									template, _ = sjson.Set(template, "choices.0.delta.reasoning", params.TextBeforeXML)
									template, _ = sjson.Set(template, "choices.0.delta.reasoning_content", params.TextBeforeXML)
								} else {
									template, _ = sjson.Set(template, "choices.0.delta.content", params.TextBeforeXML)
								}
								params.TextBeforeXML = ""
							}

							// Emit parsed tool calls
							for _, tc := range parsedCalls {
								hasFunctionCall = true
								params.HadToolCall = true
								toolCallsResult := gjson.Get(template, "choices.0.delta.tool_calls")
								functionCallIndex := params.FunctionIndex
								params.FunctionIndex++
								if toolCallsResult.Exists() && toolCallsResult.IsArray() {
									functionCallIndex = len(toolCallsResult.Array())
								} else {
									template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls", `[]`)
								}

								functionCallTemplate := `{"id": "","index": 0,"type": "function","function": {"name": "","arguments": ""}}`
								shortID := fmt.Sprintf("call_%x_%d", time.Now().UnixNano()%1000000, atomic.AddUint64(&functionCallIDCounter, 1))
								functionCallTemplate, _ = sjson.Set(functionCallTemplate, "id", shortID)
								functionCallTemplate, _ = sjson.Set(functionCallTemplate, "index", functionCallIndex)
								functionCallTemplate, _ = sjson.Set(functionCallTemplate, "function.name", tc.Name)
								// Use Set since tc.Arguments is already valid JSON
								functionCallTemplate, _ = sjson.Set(functionCallTemplate, "function.arguments", tc.Arguments)
								template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")
								template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls.-1", functionCallTemplate)
							}

							// Reset buffer and update with remaining (might have partial next tool_call)
							params.XMLToolBuffer.Reset()
							if remainingText != "" {
								// Check if remaining text has another partial tool_call
								if strings.Contains(remainingText, "<tool") {
									params.XMLToolBuffer.WriteString(remainingText)
								} else {
									// Remaining is regular text, clear the block tracking
									params.InXMLToolBlock = false
									if strings.TrimSpace(remainingText) != "" {
										if partResult.Get("thought").Bool() {
											template, _ = sjson.Set(template, "choices.0.delta.reasoning", remainingText)
											template, _ = sjson.Set(template, "choices.0.delta.reasoning_content", remainingText)
										} else {
											template, _ = sjson.Set(template, "choices.0.delta.content", remainingText)
										}
									}
								}
							} else {
								params.InXMLToolBlock = false
							}
						}
					}
					// While accumulating, don't emit anything yet
				} else if mightHavePartialTag || containsToolStart {
					// Still accumulating, waiting for complete tool call or end of partial tag
					// Don't emit anything yet
				} else {
					// No tool call detected - safe to emit accumulated buffer
					hasContent = true
					params.XMLToolBuffer.Reset()
					if partResult.Get("thought").Bool() {
						template, _ = sjson.Set(template, "choices.0.delta.reasoning", accumulated)
						template, _ = sjson.Set(template, "choices.0.delta.reasoning_content", accumulated)
					} else {
						template, _ = sjson.Set(template, "choices.0.delta.content", accumulated)
					}
				}
				template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")
			} else if functionCallResult.Exists() {
				// Handle function call content.
				hasFunctionCall = true
				hasContent = true
				params := (*param).(*convertCliResponseToOpenAIChatParams)
				params.HadToolCall = true
				toolCallsResult := gjson.Get(template, "choices.0.delta.tool_calls")
				functionCallIndex := (*param).(*convertCliResponseToOpenAIChatParams).FunctionIndex
				(*param).(*convertCliResponseToOpenAIChatParams).FunctionIndex++
				if toolCallsResult.Exists() && toolCallsResult.IsArray() {
					functionCallIndex = len(toolCallsResult.Array())
				} else {
					template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls", `[]`)
				}

				functionCallTemplate := `{"id": "","index": 0,"type": "function","function": {"name": "","arguments": ""}}`
				fcName := functionCallResult.Get("name").String()
				shortID := fmt.Sprintf("call_%x_%d", time.Now().UnixNano()%1000000, atomic.AddUint64(&functionCallIDCounter, 1))
				functionCallTemplate, _ = sjson.Set(functionCallTemplate, "id", shortID)
				functionCallTemplate, _ = sjson.Set(functionCallTemplate, "index", functionCallIndex)
				functionCallTemplate, _ = sjson.Set(functionCallTemplate, "function.name", fcName)
				if fcArgsResult := functionCallResult.Get("args"); fcArgsResult.Exists() {
					functionCallTemplate, _ = sjson.Set(functionCallTemplate, "function.arguments", fcArgsResult.Raw)
				}
				template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")
				template, _ = sjson.SetRaw(template, "choices.0.delta.tool_calls.-1", functionCallTemplate)
			} else if inlineDataResult.Exists() {
				data := inlineDataResult.Get("data").String()
				if data == "" {
					continue
				}
				mimeType := inlineDataResult.Get("mimeType").String()
				if mimeType == "" {
					mimeType = inlineDataResult.Get("mime_type").String()
				}
				if mimeType == "" {
					mimeType = "image/png"
				}
				imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, data)
				imagesResult := gjson.Get(template, "choices.0.delta.images")
				if !imagesResult.Exists() || !imagesResult.IsArray() {
					template, _ = sjson.SetRaw(template, "choices.0.delta.images", `[]`)
				}
				imageIndex := len(gjson.Get(template, "choices.0.delta.images").Array())
				imagePayload := `{"type":"image_url","image_url":{"url":""}}`
				imagePayload, _ = sjson.Set(imagePayload, "index", imageIndex)
				imagePayload, _ = sjson.Set(imagePayload, "image_url.url", imageURL)
				template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")
				template, _ = sjson.SetRaw(template, "choices.0.delta.images.-1", imagePayload)
			}
		}
	}

	params := (*param).(*convertCliResponseToOpenAIChatParams)

	// Ensure first chunk has a role to avoid "empty model output" errors in some clients
	if params.IsFirstChunk {
		template, _ = sjson.Set(template, "choices.0.delta.role", "assistant")
	}

	// Only emit if we have meaningful content or metadata
	if !hasContent && !hasFunctionCall && !hasMetadata && !params.IsFirstChunk {
		return []string{}
	}

	params.IsFirstChunk = false

	return []string{template}
}

// ConvertAntigravityResponseToOpenAINonStream converts a non-streaming Gemini CLI response to a non-streaming OpenAI response.
// This function processes the complete Gemini CLI response and transforms it into a single OpenAI-compatible
// JSON response. It handles message content, tool calls, reasoning content, and usage metadata, combining all
// the information into a single response that matches the OpenAI API format.
//
// Parameters:
//   - ctx: The context for the request, used for cancellation and timeout handling
//   - modelName: The name of the model being used for the response
//   - rawJSON: The raw JSON response from the Gemini CLI API
//   - param: A pointer to a parameter object for the conversion
//
// Returns:
//   - string: An OpenAI-compatible JSON response containing all message content and metadata
func ConvertAntigravityResponseToOpenAINonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) string {
	responseResult := gjson.GetBytes(rawJSON, "response")
	if responseResult.Exists() {
		return ConvertGeminiResponseToOpenAINonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, []byte(responseResult.Raw), param)
	}
	return ""
}
