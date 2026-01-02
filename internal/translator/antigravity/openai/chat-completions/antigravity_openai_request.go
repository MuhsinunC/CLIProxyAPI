// Package openai provides request translation functionality for OpenAI to Gemini CLI API compatibility.
// It converts OpenAI Chat Completions requests into Gemini CLI compatible JSON using gjson/sjson only.
package chat_completions

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/translator/gemini/common"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const geminiCLIFunctionThoughtSignature = "skip_thought_signature_validator"

// ConvertOpenAIRequestToAntigravity converts an OpenAI Chat Completions request (raw JSON)
// into a complete Gemini CLI request JSON. All JSON construction uses sjson and lookups use gjson.
//
// Parameters:
//   - modelName: The name of the model to use for the request
//   - rawJSON: The raw JSON request data from the OpenAI API
//   - stream: A boolean indicating if the request is for a streaming response (unused in current implementation)
//
// Returns:
//   - []byte: The transformed request data in Gemini CLI API format
func ConvertOpenAIRequestToAntigravity(modelName string, inputRawJSON []byte, _ bool) []byte {
	rawJSON := bytes.Clone(inputRawJSON)
	// Base envelope (no default thinkingConfig)
	out := []byte(`{"project":"","request":{"contents":[]},"model":"gemini-2.5-pro"}`)

	// Model
	out, _ = sjson.SetBytes(out, "model", modelName)

	// Apply thinking configuration: convert OpenAI reasoning_effort to Gemini CLI thinkingConfig.
	// Inline translation-only mapping; capability checks happen later in ApplyThinking.
	re := gjson.GetBytes(rawJSON, "reasoning_effort")
	if re.Exists() {
		effort := strings.ToLower(strings.TrimSpace(re.String()))
		if util.IsGemini3Model(modelName) {
			switch effort {
			case "none":
				out, _ = sjson.DeleteBytes(out, "request.generationConfig.thinkingConfig")
			case "auto":
				includeThoughts := true
				out = util.ApplyGeminiCLIThinkingLevel(out, "", &includeThoughts)
			default:
				if level, ok := util.ValidateGemini3ThinkingLevel(modelName, effort); ok {
					out = util.ApplyGeminiCLIThinkingLevel(out, level, nil)
				}
			}
		} else if !util.ModelUsesThinkingLevels(modelName) {
			out = util.ApplyReasoningEffortToGeminiCLI(out, effort)
		}
	}

	// Cherry Studio extension extra_body.google.thinking_config (effective only when official fields are absent)
	// Only apply for models that use numeric budgets, not discrete levels.
	if !hasOfficialThinking && util.ModelSupportsThinking(modelName) && !util.ModelUsesThinkingLevels(modelName) {
		if tc := gjson.GetBytes(rawJSON, "extra_body.google.thinking_config"); tc.Exists() && tc.IsObject() {
			var setBudget bool
			var budget int

			if v := tc.Get("thinkingBudget"); v.Exists() {
				budget = int(v.Int())
				out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.thinkingBudget", budget)
				setBudget = true
			} else if v := tc.Get("thinking_budget"); v.Exists() {
				budget = int(v.Int())
				out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.thinkingBudget", budget)
				setBudget = true
			}

			if v := tc.Get("includeThoughts"); v.Exists() {
				out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.includeThoughts", v.Bool())
			} else if v := tc.Get("include_thoughts"); v.Exists() {
				out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.includeThoughts", v.Bool())
			} else if setBudget && budget != 0 {
				out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.includeThoughts", true)
			}
		}
	}

	// Claude/Anthropic API format: thinking.type == "enabled" with budget_tokens
	// This allows Claude Code and other Claude API clients to pass thinking configuration
	if !gjson.GetBytes(out, "request.generationConfig.thinkingConfig").Exists() && util.ModelSupportsThinking(modelName) {
		if t := gjson.GetBytes(rawJSON, "thinking"); t.Exists() && t.IsObject() {
			if t.Get("type").String() == "enabled" {
				if b := t.Get("budget_tokens"); b.Exists() && b.Type == gjson.Number {
					budget := int(b.Int())
					out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.thinkingBudget", budget)
					out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.includeThoughts", true)
				}
			}
		}
	}

	// Temperature/top_p/top_k/max_tokens
	if tr := gjson.GetBytes(rawJSON, "temperature"); tr.Exists() && tr.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.temperature", tr.Num)
	}
	if tpr := gjson.GetBytes(rawJSON, "top_p"); tpr.Exists() && tpr.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.topP", tpr.Num)
	}
	if tkr := gjson.GetBytes(rawJSON, "top_k"); tkr.Exists() && tkr.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.topK", tkr.Num)
	}
	if maxTok := gjson.GetBytes(rawJSON, "max_tokens"); maxTok.Exists() && maxTok.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.maxOutputTokens", maxTok.Num)
	}

	// Candidate count (OpenAI 'n' parameter)
	if n := gjson.GetBytes(rawJSON, "n"); n.Exists() && n.Type == gjson.Number {
		if val := n.Int(); val > 1 {
			out, _ = sjson.SetBytes(out, "request.generationConfig.candidateCount", val)
		}
	}

	// Map OpenAI modalities -> Gemini CLI request.generationConfig.responseModalities
	// e.g. "modalities": ["image", "text"] -> ["IMAGE", "TEXT"]
	if mods := gjson.GetBytes(rawJSON, "modalities"); mods.Exists() && mods.IsArray() {
		var responseMods []string
		for _, m := range mods.Array() {
			switch strings.ToLower(m.String()) {
			case "text":
				responseMods = append(responseMods, "TEXT")
			case "image":
				responseMods = append(responseMods, "IMAGE")
			}
		}
		if len(responseMods) > 0 {
			out, _ = sjson.SetBytes(out, "request.generationConfig.responseModalities", responseMods)
		}
	}

	// OpenRouter-style image_config support
	// If the input uses top-level image_config.aspect_ratio, map it into request.generationConfig.imageConfig.aspectRatio.
	if imgCfg := gjson.GetBytes(rawJSON, "image_config"); imgCfg.Exists() && imgCfg.IsObject() {
		if ar := imgCfg.Get("aspect_ratio"); ar.Exists() && ar.Type == gjson.String {
			out, _ = sjson.SetBytes(out, "request.generationConfig.imageConfig.aspectRatio", ar.Str)
		}
		if size := imgCfg.Get("image_size"); size.Exists() && size.Type == gjson.String {
			out, _ = sjson.SetBytes(out, "request.generationConfig.imageConfig.imageSize", size.Str)
		}
	}

	// messages -> systemInstruction + contents
	messages := gjson.GetBytes(rawJSON, "messages")
	if messages.IsArray() {
		arr := messages.Array()
		// First pass: assistant tool_calls id->name map
		tcID2Name := map[string]string{}
		for i := 0; i < len(arr); i++ {
			m := arr[i]
			if m.Get("role").String() == "assistant" {
				tcs := m.Get("tool_calls")
				if tcs.IsArray() {
					for _, tc := range tcs.Array() {
						if tc.Get("type").String() == "function" {
							id := tc.Get("id").String()
							name := tc.Get("function.name").String()
							if id != "" && name != "" {
								tcID2Name[id] = name
							}
						}
					}
				}
				// Also check Claude-format: content array with type: "tool_use"
				content := m.Get("content")
				if content.IsArray() {
					for _, item := range content.Array() {
						if item.Get("type").String() == "tool_use" {
							id := item.Get("id").String()
							name := item.Get("name").String()
							if id != "" && name != "" {
								tcID2Name[id] = name
							}
						}
					}
				}
			}
		}

		// Second pass build systemInstruction/tool responses cache
		toolResponses := map[string]string{} // tool_call_id -> response text
		for i := 0; i < len(arr); i++ {
			m := arr[i]
			role := m.Get("role").String()
			if role == "tool" {
				toolCallID := m.Get("tool_call_id").String()
				if toolCallID != "" {
					c := m.Get("content")
					toolResponses[toolCallID] = c.Raw
				}
			}
		}

		systemPartIndex := 0
		for i := 0; i < len(arr); i++ {
			m := arr[i]
			role := m.Get("role").String()
			content := m.Get("content")

			if (role == "system" || role == "developer") && len(arr) > 1 {
				// system -> request.systemInstruction parts
				if content.Type == gjson.String && content.String() != "" {
					p := 0
					parts := gjson.GetBytes(out, "request.systemInstruction.parts")
					if parts.IsArray() {
						p = len(parts.Array())
					}
					out, _ = sjson.SetBytes(out, "request.systemInstruction.role", "system")
					out, _ = sjson.SetBytes(out, "request.systemInstruction.parts."+itoa(p)+".text", content.String())
				} else if content.IsObject() && content.Get("type").String() == "text" {
					p := 0
					parts := gjson.GetBytes(out, "request.systemInstruction.parts")
					if parts.IsArray() {
						p = len(parts.Array())
					}
					out, _ = sjson.SetBytes(out, "request.systemInstruction.role", "system")
					out, _ = sjson.SetBytes(out, "request.systemInstruction.parts."+itoa(p)+".text", content.Get("text").String())
				} else if content.IsArray() {
					// Handle array content (Cursor sends tool docs as array of text parts)
					out, _ = sjson.SetBytes(out, "request.systemInstruction.role", "system")
					content.ForEach(func(_, part gjson.Result) bool {
						if part.Get("type").String() == "text" {
							p := 0
							parts := gjson.GetBytes(out, "request.systemInstruction.parts")
							if parts.IsArray() {
								p = len(parts.Array())
							}
							out, _ = sjson.SetBytes(out, "request.systemInstruction.parts."+itoa(p)+".text", part.Get("text").String())
						}
						return true
					})
				}
			} else if role == "user" || ((role == "system" || role == "developer") && len(arr) == 1) {
				// Build single user content node to avoid splitting into multiple contents
				node := []byte(`{"role":"user","parts":[]}`)
				if content.Type == gjson.String {
					node, _ = sjson.SetBytes(node, "parts.0.text", content.String())
				} else if content.IsArray() {
					items := content.Array()
					p := 0
					for _, item := range items {
						switch item.Get("type").String() {
						case "text":
							text := item.Get("text").String()
							if text != "" {
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".text", text)
							}
							p++
						case "image_url":
							imageURL := item.Get("image_url.url").String()
							if len(imageURL) > 5 {
								pieces := strings.SplitN(imageURL[5:], ";", 2)
								if len(pieces) == 2 && len(pieces[1]) > 7 {
									mime := pieces[0]
									data := pieces[1][7:]
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.mime_type", mime)
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.data", data)
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".thoughtSignature", geminiCLIFunctionThoughtSignature)
									p++
								}
							}
						case "file":
							filename := item.Get("file.filename").String()
							fileData := item.Get("file.file_data").String()
							ext := ""
							if sp := strings.Split(filename, "."); len(sp) > 1 {
								ext = sp[len(sp)-1]
							}
							if mimeType, ok := misc.MimeTypes[ext]; ok {
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.mime_type", mimeType)
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.data", fileData)
								p++
							} else {
								log.Warnf("Unknown file name extension '%s' in user message, skip", ext)
							}
						case "tool_result":
							// Claude-format tool result: {type: "tool_result", tool_use_id: "...", content: "..."}
							toolUseID := item.Get("tool_use_id").String()
							resultContent := item.Get("content")
							if toolUseID != "" {
								funcName := tcID2Name[toolUseID]
								if funcName == "" {
									funcName = "unknown"
								}
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionResponse.id", toolUseID)
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionResponse.name", funcName)
								if resultContent.Type == gjson.String {
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionResponse.response.result", resultContent.String())
								} else if resultContent.Type == gjson.JSON {
									node, _ = sjson.SetRawBytes(node, "parts."+itoa(p)+".functionResponse.response.result", []byte(resultContent.Raw))
								} else {
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionResponse.response.result", "{}")
								}
								p++
							}
						}
					}
				}
				out, _ = sjson.SetRawBytes(out, "request.contents.-1", node)
			} else if role == "assistant" {
				node := []byte(`{"role":"model","parts":[]}`)
				p := 0
				if content.Type == gjson.String && content.String() != "" {
					node, _ = sjson.SetBytes(node, "parts.-1.text", content.String())
					p++
				} else if content.IsArray() {
					// Handle assistant content array (text, tool_use, and image_url)
					for _, item := range content.Array() {
						itemType := item.Get("type").String()
						switch itemType {
						case "text":
							textContent := item.Get("text").String()
							if textContent != "" {
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".text", textContent)
								p++
							}
						case "tool_use":
							// Convert Claude tool_use to functionCall
							tuID := item.Get("id").String()
							tuName := item.Get("name").String()
							tuInput := item.Get("input")
							if tuID != "" && tuName != "" {
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionCall.id", tuID)
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionCall.name", tuName)
								if tuInput.Exists() {
									node, _ = sjson.SetRawBytes(node, "parts."+itoa(p)+".functionCall.args", []byte(tuInput.Raw))
								} else {
									node, _ = sjson.SetRawBytes(node, "parts."+itoa(p)+".functionCall.args", []byte("{}"))
								}
								node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".thoughtSignature", geminiCLIFunctionThoughtSignature)
								p++
							}
						case "image_url":
							// If the assistant returned an inline data URL, preserve it for history fidelity.
							imageURL := item.Get("image_url.url").String()
							if len(imageURL) > 5 { // expect data:...
								pieces := strings.SplitN(imageURL[5:], ";", 2)
								if len(pieces) == 2 && len(pieces[1]) > 7 {
									mime := pieces[0]
									data := pieces[1][7:]
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.mime_type", mime)
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".inlineData.data", data)
									node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".thoughtSignature", geminiCLIFunctionThoughtSignature)
									p++
								}
							}
						}
					}
				}

				// Tool calls -> single model content with functionCall parts
				tcs := m.Get("tool_calls")
				if tcs.IsArray() {
					fIDs := make([]string, 0)
					for _, tc := range tcs.Array() {
						if tc.Get("type").String() != "function" {
							continue
						}
						fid := tc.Get("id").String()
						fname := tc.Get("function.name").String()
						fargs := tc.Get("function.arguments").String()
						node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionCall.id", fid)
						node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionCall.name", fname)
						if gjson.Valid(fargs) {
							node, _ = sjson.SetRawBytes(node, "parts."+itoa(p)+".functionCall.args", []byte(fargs))
						} else {
							node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".functionCall.args.params", []byte(fargs))
						}
						node, _ = sjson.SetBytes(node, "parts."+itoa(p)+".thoughtSignature", geminiCLIFunctionThoughtSignature)
						p++
						if fid != "" {
							fIDs = append(fIDs, fid)
						}
					}
					out, _ = sjson.SetRawBytes(out, "request.contents.-1", node)

					// Append a single tool content combining name + response per function
					toolNode := []byte(`{"role":"user","parts":[]}`)
					pp := 0
					for _, fid := range fIDs {
						if name, ok := tcID2Name[fid]; ok {
							toolNode, _ = sjson.SetBytes(toolNode, "parts."+itoa(pp)+".functionResponse.id", fid)
							toolNode, _ = sjson.SetBytes(toolNode, "parts."+itoa(pp)+".functionResponse.name", name)
							resp := toolResponses[fid]
							if resp == "" {
								resp = "{}"
							}
							// Handle non-JSON output gracefully (matches dev branch approach)
							if resp != "null" {
								parsed := gjson.Parse(resp)
								if parsed.Type == gjson.JSON {
									toolNode, _ = sjson.SetRawBytes(toolNode, "parts."+itoa(pp)+".functionResponse.response.result", []byte(parsed.Raw))
								} else {
									toolNode, _ = sjson.SetBytes(toolNode, "parts."+itoa(pp)+".functionResponse.response.result", resp)
								}
							}
							pp++
						}
					}
					if pp > 0 {
						out, _ = sjson.SetRawBytes(out, "request.contents.-1", toolNode)
					}
				} else {
					out, _ = sjson.SetRawBytes(out, "request.contents.-1", node)
				}
			}
		}
	}

	// tools -> request.tools[].functionDeclarations + request.tools[].googleSearch passthrough
	tools := gjson.GetBytes(rawJSON, "tools")
	if tools.IsArray() && len(tools.Array()) > 0 {
		functionToolNode := []byte(`{}`)
		hasFunction := false
		googleSearchNodes := make([][]byte, 0)
		for _, t := range tools.Array() {
			if t.Get("type").String() == "function" {
				fn := t.Get("function")
				if fn.Exists() && fn.IsObject() {
					fnRaw := fn.Raw
					if fn.Get("parameters").Exists() {
						renamed, errRename := util.RenameKey(fnRaw, "parameters", "parametersJsonSchema")
						if errRename != nil {
							log.Warnf("Failed to rename parameters for tool '%s': %v", fn.Get("name").String(), errRename)
							var errSet error
							fnRaw, errSet = sjson.Set(fnRaw, "parametersJsonSchema.type", "object")
							if errSet != nil {
								log.Warnf("Failed to set default schema type for tool '%s': %v", fn.Get("name").String(), errSet)
								continue
							}
							fnRaw, errSet = sjson.SetRaw(fnRaw, "parametersJsonSchema.properties", `{}`)
							if errSet != nil {
								log.Warnf("Failed to set default schema properties for tool '%s': %v", fn.Get("name").String(), errSet)
								continue
							}
						} else {
							fnRaw = renamed
						}
					} else {
						var errSet error
						fnRaw, errSet = sjson.Set(fnRaw, "parametersJsonSchema.type", "object")
						if errSet != nil {
							log.Warnf("Failed to set default schema type for tool '%s': %v", fn.Get("name").String(), errSet)
							continue
						}
						fnRaw, errSet = sjson.SetRaw(fnRaw, "parametersJsonSchema.properties", `{}`)
						if errSet != nil {
							log.Warnf("Failed to set default schema properties for tool '%s': %v", fn.Get("name").String(), errSet)
							continue
						}
					}
					fnRaw, _ = sjson.Delete(fnRaw, "strict")
					if !hasFunction {
						functionToolNode, _ = sjson.SetRawBytes(functionToolNode, "functionDeclarations", []byte("[]"))
					}
					tmp, errSet := sjson.SetRawBytes(functionToolNode, "functionDeclarations.-1", []byte(fnRaw))
					if errSet != nil {
						log.Warnf("Failed to append tool declaration for '%s': %v", fn.Get("name").String(), errSet)
						continue
					}
					functionToolNode = tmp
					hasFunction = true
				}
			} else if t.Get("name").Exists() && (t.Get("input_schema").Exists() || t.Get("type").String() == "custom") {
				// Cursor-style tool format: {"name": "...", "input_schema": {...}} (no type:function wrapper)
				// Also handles Claude API custom tools with type: "custom"
				fnRaw := `{"name":"","parametersJsonSchema":{}}`
				fnRaw, _ = sjson.Set(fnRaw, "name", t.Get("name").String())
				if desc := t.Get("description"); desc.Exists() {
					fnRaw, _ = sjson.Set(fnRaw, "description", desc.String())
				}

				// Handle input_schema -> parametersJsonSchema
				if schema := t.Get("input_schema"); schema.Exists() {
					fnRaw, _ = sjson.SetRaw(fnRaw, "parametersJsonSchema", schema.Raw)
				}

				if !hasFunction {
					toolNode, _ = sjson.SetRawBytes(toolNode, "functionDeclarations", []byte("[]"))
				}
				tmp, errSet := sjson.SetRawBytes(toolNode, "functionDeclarations.-1", []byte(fnRaw))
				if errSet != nil {
					log.Warnf("Failed to append Cursor-style tool declaration for '%s': %v", t.Get("name").String(), errSet)
					continue
				}
				toolNode = tmp
				hasFunction = true
				hasTool = true
			}
			if gs := t.Get("google_search"); gs.Exists() {
				googleToolNode := []byte(`{}`)
				var errSet error
				googleToolNode, errSet = sjson.SetRawBytes(googleToolNode, "googleSearch", []byte(gs.Raw))
				if errSet != nil {
					log.Warnf("Failed to set googleSearch tool: %v", errSet)
					continue
				}
				googleSearchNodes = append(googleSearchNodes, googleToolNode)
			}
		}
		if hasFunction || len(googleSearchNodes) > 0 {
			toolsNode := []byte("[]")
			if hasFunction {
				toolsNode, _ = sjson.SetRawBytes(toolsNode, "-1", functionToolNode)
			}
			for _, googleNode := range googleSearchNodes {
				toolsNode, _ = sjson.SetRawBytes(toolsNode, "-1", googleNode)
			}
			out, _ = sjson.SetRawBytes(out, "request.tools", toolsNode)
		}
	}

	return common.AttachDefaultSafetySettings(out, "request.safetySettings")
}

// itoa converts int to string without strconv import for few usages.
func itoa(i int) string { return fmt.Sprintf("%d", i) }
