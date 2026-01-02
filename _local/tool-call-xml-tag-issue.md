# Tool Call XML Tag Issue Analysis

## Problem Statement
When using Claude Code with this proxy (CLIProxyAPI) and routing requests through Claude models via Gemini Antigravity API (e.g., `gemini-claude-opus-4-5`), tool calls appear as raw XML tags in the response text instead of being properly executed.

Example of the broken output:
```
<file_read file_path="/Users/user/Documents/Muhsinun/Projects/GitHub/CLIProxyAPI/internal/util/thinking.go" start_line="1" end_line="100">

</file_read>
```

The user sees these XML tags as text, but the tool call never actually executes.

## Codebase Structure Overview

### Key Directories
| Directory | Purpose |
|-----------|---------|
| `internal/translator/` | Request/response translation between formats |
| `internal/runtime/executor/` | Executors for different providers (Antigravity, Claude, Gemini, etc.) |
| `internal/util/` | Utility functions for thinking, schemas, etc. |
| `internal/registry/` | Model definitions and configurations |

### Translation Flow for Antigravity Claude Models

1. **Request Translation** (`internal/translator/antigravity/`)
   - OpenAI format → Antigravity format (`antigravity_openai_request.go`)
   - Claude format → Antigravity format (`antigravity_claude_request.go`)
   - Tools are properly translated to `request.tools[].functionDeclarations[]` format

2. **Executor** (`internal/runtime/executor/antigravity_executor.go`)
   - Handles streaming and non-streaming requests to Antigravity API
   - `ExecuteStream()` method handles streaming responses
   - `convertStreamToNonStream()` aggregates stream chunks for non-streaming responses

3. **Response Translation** (`internal/translator/antigravity/`)
   - Antigravity → OpenAI format (`antigravity_openai_response.go`)
   - Antigravity → Claude format (`antigravity_claude_response.go`)
   - Both expect `functionCall` objects in response parts

## Root Cause Analysis

### The Core Issue
The response translators (`antigravity_claude_response.go`, `antigravity_openai_response.go`) are designed to process `functionCall` objects from the API response:

```go
// From antigravity_claude_response.go lines 115, 194
functionCallResult := partResult.Get("functionCall")
// ...
} else if functionCallResult.Exists() {
    // Process tool call
}
```

However, when the model outputs tool calls as **XML TEXT** in the content (like `<file_read>...</file_read>`), this code path is never triggered because:
1. There is no `functionCall` object in the `parts` array
2. The XML tags are just regular text in the `text` field

### Why This Happens

This is likely an **upstream issue** with the Gemini Antigravity API's handling of Claude models:

1. **Claude models via Anthropic's direct API** use a structured `tool_use` block format
2. **Gemini's native models** use `functionCall` objects
3. **Claude models through Antigravity** may be falling back to XML-style tool calls in certain scenarios

Possible causes:
- Tool schemas not being properly forwarded to the Claude model
- Claude model responding with legacy XML format instead of proper function calls
- Mismatch in tool calling configuration between request and what the model expects

### Files Where Tool Handling Occurs

| File | Function/Section |
|------|-----------------|
| `antigravity_openai_request.go` | Lines 286-356: `tools` → `request.tools[].functionDeclarations[]` |
| `antigravity_claude_request.go` | Lines 198-230: Tools conversion for Claude format |
| `antigravity_openai_response.go` | Lines 111, 139-160: `functionCall` handling |
| `antigravity_claude_response.go` | Lines 115, 194-242: `functionCall` handling |

## Investigation Status

### What's Been Verified
- ✅ Request translators properly format tools as `functionDeclarations`
- ✅ Response translators properly handle `functionCall` objects when present
- ✅ The code is structured correctly to process function calls

### What Needs Investigation
- ❓ Whether Antigravity API actually returns `functionCall` objects for Claude models
- ❓ Whether there's a missing configuration that enables proper function calling for Claude models
- ❓ Whether this is an upstream Gemini Antigravity API limitation

## Potential Solutions (To Be Investigated)

1. **Parse XML tool calls from text** (workaround)
   - Add code to detect and parse `<tool_name>...</tool_name>` patterns from text content
   - Convert them to proper `functionCall` format

2. **Ensure tool configuration is properly sent** (fix)
   - Verify `toolConfig` or similar settings are being sent in requests for Claude models
   - Check if there's a flag to enable structured function calling

3. **Check model capability** (upstream issue)
   - This might be a limitation of Claude models accessed through Antigravity
   - May need to file a bug report with the API provider

## Key Discovery: `geminiToAntigravity()` Function

Located in `internal/runtime/executor/antigravity_executor.go` (lines 1175-1215), this function prepares requests for the Antigravity API:

### Tool Configuration (Line 1189)
```go
template, _ = sjson.Set(template, "request.toolConfig.functionCallingConfig.mode", "VALIDATED")
```
This enables structured function calling for all models.

### Claude-Specific Handling (Lines 1198-1209)
```go
if strings.Contains(modelName, "claude") {
    gjson.Get(template, "request.tools").ForEach(func(key, tool gjson.Result) bool {
        tool.Get("functionDeclarations").ForEach(func(funKey, funcDecl gjson.Result) bool {
            if funcDecl.Get("parametersJsonSchema").Exists() {
                // Convert parametersJsonSchema -> parameters for Claude models
                template, _ = sjson.SetRaw(template, 
                    fmt.Sprintf("request.tools.%d.functionDeclarations.%d.parameters", key.Int(), funKey.Int()), 
                    funcDecl.Get("parametersJsonSchema").Raw)
                // ... cleanup ...
            }
            return true
        })
        return true
    })
}
```

**Important**: The proxy code is properly configured for tool calling. The issue appears to be **upstream** with how the Gemini Antigravity API processes tool calls for Claude models.

## Conclusion

This is likely an **upstream issue** with the Gemini Antigravity API, not a bug in this proxy codebase. The proxy correctly:
1. Translates tools to `functionDeclarations` format
2. Sets `toolConfig.functionCallingConfig.mode` to `VALIDATED`
3. Has Claude-specific handling for parameter schemas
4. Has response translators ready to process `functionCall` objects

The problem is that the Claude models behind Antigravity return tool calls as XML text (`<file_read>...</file_read>`) instead of proper `functionCall` objects, which the response translators cannot process.

## Next Steps
1. Report this issue to the Gemini Antigravity API team
2. Consider adding a workaround to parse XML tool calls from text content (if needed)
3. Test with logging enabled to see the actual API response format
4. Compare request/response payloads between working Gemini models and problematic Claude models
