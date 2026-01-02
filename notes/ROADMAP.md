# CLIProxyAPI Feature Roadmap

> **Purpose**: Track planned features and enhancements for CLIProxyAPI.

---

## Quick Status

| Milestone | Status | Priority |
|-----------|--------|----------|
| [M1: Thinking Display in Cursor](#m1-thinking-display-in-cursor) | 🟡 In Progress | High |
| [M2: Thinking Block Caching](#m2-thinking-block-caching) | ⚪ Planned | High |
| [M3: Content-Embedded Fallback](#m3-content-embedded-fallback) | ⚪ Planned | Medium |

---

## M1: Thinking Display in Cursor

**Goal**: Verify and fix thinking block display in Cursor IDE for Claude/Gemini models.

### Background

Claude's extended thinking feature provides:
1. **Thinking content**: The model's reasoning process
2. **Signature**: Cryptographic signature required for multi-turn tool loops

**Challenge**: Cursor uses OpenAI-compatible API and only forwards `role`, `content`, and `tool_calls`. It does NOT forward `reasoning_content` or custom signature fields.

### Tasks

- [x] Fixed `include_thoughts` → `includeThoughts` naming for API compatibility
- [ ] Verify thinking display works for **native Gemini models** (gemini-2.5-pro, gemini-3-flash)
- [ ] Confirm Claude-via-Antigravity thinking content format
- [ ] Test with non-exhausted quota

### Open Questions

1. Does Cursor display `reasoning_content` for thinking models?
2. Does Claude-via-Antigravity return `thought: true` in response parts?

---

## M2: Thinking Block Caching

**Goal**: Cache thinking blocks server-side to enable tool loops without errors.

### Why Caching?

When thinking is enabled and a tool loop occurs, Claude API will error if the thinking block is missing:
```
Expected `thinking` or `redacted_thinking`, but found `tool_use`.
```

**Key insight**: Only the *last* assistant turn's thinking block is needed. Claude automatically strips older thinking blocks.

### Architecture

```
REQUEST PATH:
1. Request comes in with tool_result message
2. Check cache for thinking_last:{conversation_id}
3. If found: Inject thinking block into request
4. Forward to Claude

RESPONSE PATH:
1. Response comes back from Claude
2. Extract thinking content + signature
3. Store in cache: thinking_last:{conversation_id}
4. Return response with reasoning_content for UI
```

### Configuration

```yaml
claude_thinking_cache:
  enabled: false
  backend: "upstash"           # Options: upstash, redis, file
  upstash_url: "https://xxx.upstash.io"
  upstash_token: "your-token"
  max_entries: 10000           # ~5MB at 500 bytes each
```

### Tasks

- [ ] Add config struct and YAML parsing
- [ ] Create cache wrapper (Upstash/Redis/file backends)
- [ ] Implement conversation ID generation
- [ ] Modify request translator to inject cached thinking blocks
- [ ] Modify response translator to cache thinking blocks
- [ ] Add tests

### Memory Estimation

| Conversations | Per Entry | Total |
|---------------|-----------|-------|
| 1,000 | 500 bytes | 500 KB |
| 10,000 | 500 bytes | 5 MB |

---

## M3: Content-Embedded Fallback

**Goal**: `cursor-` prefix for stateless thinking block preservation.

### How It Works

User selects model like `cursor-claude-opus-4`. Proxy embeds thinking+signature in the `content` field so Cursor forwards it back.

### Pros/Cons

| Pros | Cons |
|------|------|
| No server-side state | Pollutes chat thread |
| Works without Redis | Visible noise in chat |
| Explicit user control | Garbage if switching endpoints |

### Implementation

1. Detect `cursor-` prefix in model name
2. Strip prefix → send `claude-opus-4` to Claude
3. On response: embed `<!-- claude-thinking: {...} -->` in content
4. On request: parse embedded JSON, reconstruct thinking block

### Tasks

- [ ] Detect `cursor-` prefix in model name
- [ ] Embed thinking in content on response
- [ ] Parse embedded thinking on request
- [ ] Add tests

---

## Model Naming Convention

| Model Name | Thinking Behavior |
|------------|-------------------|
| `claude-opus-4` | Standard - errors if tool loop without cache |
| `claude-opus-4-thinking` | Thinking enabled by default |
| `cursor-claude-opus-4` | Embeds thinking in content (M3) |
| `cursor-claude-opus-4-thinking` | Same with thinking enabled |

---

## Technical Notes

### Claude Tool Loop Requirement

From Anthropic docs:
> You only need the thinking block from the **LAST** assistant turn during a tool loop.

Claude's API automatically strips `thinking` blocks from previous turns. This means:
- ✅ Only cache the most recent thinking block
- ✅ Cache eviction of older blocks is safe
- ✅ Memory requirements are minimal

### Request Translator Gap

The request translator (`claude_openai_request.go`) does NOT convert `reasoning_content` from incoming requests into Claude's native thinking blocks. It only handles text, tool_calls, and images.

**Implication**: Caching the original Claude-format thinking block (with signature) is required.

---

## References

- [Anthropic Extended Thinking Docs](https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking)
- Claude tool use with thinking: Requires thinking block from **last assistant turn only**
- Claude automatically strips thinking blocks from previous turns
