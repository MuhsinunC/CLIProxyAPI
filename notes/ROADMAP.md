# CLIProxyAPI Feature Roadmap

> **Purpose**: Track planned features and enhancements for CLIProxyAPI.

---

## Quick Status

| Milestone | Status | Priority |
|-----------|--------|----------|
| [M1: Thinking Display in Cursor](#m1-thinking-display-in-cursor) | ❌ Blocked (Cursor Limitation) | Low |
| [M2: Thinking Block Caching](#m2-thinking-block-caching) | ✅ Complete | High |
| [M3: Content-Embedded Fallback](#m3-content-embedded-fallback) | ⚪ Planned | Medium |

---

## M1: Thinking Display in Cursor

**Status**: ❌ **Blocked** - Cursor does not support thinking display for custom API endpoints.

### Background

Claude's extended thinking feature provides:
1. **Thinking content**: The model's reasoning process
2. **Signature**: Cryptographic signature required for multi-turn tool loops

### What We Tried

| Approach | Result |
|----------|--------|
| Send `reasoning_content` field in streaming response | ❌ Not displayed |
| Send `reasoning` field (OpenAI standard) | ❌ Not displayed |
| Send BOTH `reasoning` and `reasoning_content` | ❌ Not displayed |
| Test with DeepSeek R1 via OpenRouter | ❌ Not displayed |
| Test with Claude Opus 4.5 via our proxy | ❌ Not displayed |

### Verified Working

- ✅ Claude executor logs: `THINKING: ENABLED (budget=32768)`
- ✅ curl test shows `reasoning` field in response
- ✅ curl test shows `reasoning_content` in streaming chunks
- ✅ OpenRouter returns 233 reasoning tokens for DeepSeek R1

### Conclusion (January 2025)

**Cursor's "thinking toggle" feature only works for built-in models**. When using custom API endpoints (Override OpenAI Base URL), Cursor ignores `reasoning`/`reasoning_content` fields entirely.

This was verified by testing DeepSeek R1 via OpenRouter directly in Cursor. Even though OpenRouter confirmed return of reasoning tokens via curl, Cursor did not display them.

### Workarounds

1. **Accept limitation**: Thinking works backend-side; just not visible in Cursor UI
2. **Use curl/Postman**: Direct API calls show `reasoning` field correctly
3. **M3 fallback**: Embed thinking in `content` field with `<think>` tags (visible as text)

### Open Questions (Resolved)

| Question | Answer |
|----------|--------|
| Does Cursor display `reasoning_content` for custom models? | ❌ No |
| Does DeepSeek R1 via OpenRouter show thinking in Cursor? | ❌ No |
| Is this a field name issue? | ❌ No - both `reasoning` and `reasoning_content` ignored |

---

## M2: Thinking Block Caching

**Goal**: Cache thinking blocks server-side to enable tool loops without errors.

**Status**: ✅ **Complete** (January 2026)

### Why Caching?

When thinking is enabled and a tool loop occurs, Claude API will error if the thinking block is missing:
```
Expected `thinking` or `redacted_thinking`, but found `tool_use`.
```

**Key insight**: Only the *last* assistant turn's thinking block is needed. Claude automatically strips older thinking blocks.

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         SERVER STARTUP                          │
├─────────────────────────────────────────────────────────────────┤
│ 1. Open/Create SQLite file (thinking_cache.db)                  │
│ 2. Load top N entries (by last_accessed) into RAM cache         │
│ 3. Fill up to max_memory_mb (default 512MB)                     │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                         REQUEST PATH                            │
├─────────────────────────────────────────────────────────────────┤
│ 1. Request comes in with tool_result message                    │
│ 2. Generate conversation_id from request hash                   │
│ 3. Check RAM cache for thinking block                           │
│ 4. If not in RAM: Check SQLite (and load into RAM if found)     │
│ 5. If found: Inject thinking block into request                 │
│ 6. Forward to Claude                                            │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                         RESPONSE PATH                           │
├─────────────────────────────────────────────────────────────────┤
│ 1. Response comes back from Claude with thinking block          │
│ 2. Extract thinking content + signature                         │
│ 3. Store in RAM cache (evict LRU if over max_memory_mb)         │
│ 4. Async write to SQLite (never blocks response)                │
│ 5. Return response with reasoning_content for UI                │
└─────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| RAM-first | Fast reads, no external dependencies |
| SQLite persistence | Crash recovery, survives restarts |
| Async SQLite writes | Never slows down responses |
| RAM eviction only | SQLite is permanent archive |
| LRU eviction | Keeps recently-used entries hot |

### Configuration

```yaml
claude_thinking_cache:
  enabled: true
  max_memory_mb: 512              # RAM cache size limit (default 512MB)
  sqlite_path: "thinking_cache.db" # SQLite file path
```

### SQLite Schema

```sql
CREATE TABLE thinking_cache (
  conversation_id TEXT PRIMARY KEY,
  thinking_block  BLOB NOT NULL,    -- Claude-format thinking with signature
  created_at      INTEGER NOT NULL, -- Unix timestamp
  last_accessed   INTEGER NOT NULL  -- For LRU ordering on startup
);

CREATE INDEX idx_last_accessed ON thinking_cache(last_accessed DESC);
```

### Tasks

- [x] Design RAM + SQLite architecture
- [x] Add config struct (`ThinkingCacheConfig` in config.go)
- [x] Create RAM cache with LRU eviction (`internal/cache/thinking_cache.go`)
- [x] Implement SQLite read/write layer
- [x] Implement async write goroutine
- [x] Load top N entries on startup
- [x] Implement conversation ID generation (request hash)
- [x] Create `handleToolLoopThinking` method (inject cached blocks)
- [x] Create `cacheThinkingFromResponse` method (extract and cache)
- [x] Hook response caching into streaming path (`cacheThinkingFromStreamAccum`)
- [x] End-to-end testing with tool loops

### Memory Estimation

| Entries | Avg Entry Size | Total RAM |
|---------|---------------|-----------|
| ~50,000 | ~10 KB | ~500 MB |
| ~100,000 | ~5 KB | ~500 MB |

Note: Entry size varies based on thinking complexity. SQLite stores unlimited entries.

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
