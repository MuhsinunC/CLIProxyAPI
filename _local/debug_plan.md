# Tool Call Debugging Plan

**Issue**: Cursor reports "model provided invalid arguments for tool call" even though format appears correct.

**Current Status**: 
- ✅ Text content clean (no XML garbage)
- ✅ Tool calls parsed from XML
- ✅ Arguments formatted as JSON string
- ❌ Cursor still rejects tool calls

---

## Debugging Checklist

### 1. Compare with Working Implementation
- [ ] Check cursor-claude-connector logs for exact tool_calls format
- [ ] Compare field-by-field: id, index, type, function.name, function.arguments
- [ ] Check if any fields are missing (e.g., `type` field)

### 2. Parameter Name Investigation
- [ ] Check what parameter name Cursor's `read_file` tool expects (`path` vs `AbsolutePath`)
- [ ] Look at the original request's `tools` definition to see schema
- [ ] Try hardcoding a different parameter name to test

### 3. Streaming Format Tests
- [ ] Test sending tool_call with empty arguments first, then full arguments (incremental)
- [ ] Test if `finish_reason: "tool_calls"` is required or causes issues
- [ ] Check if we need to omit `native_finish_reason` field

### 4. Log Analysis
- [ ] Get cursor-claude-connector request log when it works
- [ ] Compare exact JSON structure byte-by-byte
- [ ] Check for invisible characters or encoding issues

### 5. Tool Call ID Format
- [ ] Check if ID format matters (current: `read_file-1766113104546392000-1`)
- [ ] Try simpler ID format like `toolu_` prefix (Claude style)
- [ ] Try OpenAI-style ID format

### 6. Response Structure Issues
- [ ] Verify `choices[0].delta` vs `choices[0].message` format
- [ ] Check if `content: null` is problematic
- [ ] Check if `reasoning_content: null` should be omitted

### 7. Request Schema Validation
- [ ] Look at what tools Cursor sends in the request
- [ ] Verify our response matches the expected schema
- [ ] Check if there's a strict validation happening

### 8. Testing Approach
- [ ] Create minimal test case with hardcoded working response
- [ ] Use curl to send directly to Cursor's expected format
- [ ] Bisect by removing fields one at a time

---

## Investigation Log

| Date | Test | Result | Notes |
|------|------|--------|-------|
| 2025-12-18 | Fixed arguments to JSON string | Still failing | Format looks correct in logs |
| | | | |

---

## Key Files

- Response translator: `internal/translator/antigravity/openai/chat-completions/antigravity_openai_response.go`
- Request translator: `internal/translator/antigravity/openai/chat-completions/antigravity_openai_request.go`
- Logs directory: `logs/`
- cursor-claude-connector comparison: `/Users/user/Documents/Muhsinun/Projects/GitHub/cursor-claude-connector/`

---

## Notes

- cursor-claude-connector works with same Cursor setup
- CLIProxyAPI fails on first tool call attempt
- No visible error in API response - Cursor client-side shows "invalid arguments"
