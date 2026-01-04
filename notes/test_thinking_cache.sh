#!/bin/bash
# Test script for thinking cache functionality
# Tests that thinking blocks are cached and reinjected in tool loops

set -e

# Load environment variables
if [ -f .env ]; then
    export $(grep -v '^#' .env | xargs)
fi

API_KEY="${CLIPROXYAPI_KEY:-test-api-key}"
BASE_URL="${TEST_BASE_URL:-http://localhost:8080}"

echo "============================================"
echo "Thinking Cache Test Script"
echo "============================================"
echo "BASE_URL: $BASE_URL"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test 1: Simple thinking request (should cache thinking block)
echo -e "${YELLOW}Test 1: Simple thinking request (non-streaming)${NC}"
echo "Sending request with thinking enabled..."

RESPONSE=$(curl -s -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "claude-opus-4-5-20251101(xhigh)",
    "stream": false,
    "max_tokens": 1000,
    "messages": [
      {"role": "user", "content": "What is 2+2? Think step by step."}
    ]
  }')

# Check if response contains reasoning content
if echo "$RESPONSE" | grep -q '"reasoning"'; then
    echo -e "${GREEN}✓ Response contains reasoning field${NC}"
else
    echo -e "${RED}✗ Response missing reasoning field${NC}"
fi

if echo "$RESPONSE" | grep -q '"content"'; then
    echo -e "${GREEN}✓ Response contains content${NC}"
else
    echo -e "${RED}✗ Response missing content${NC}"
fi

echo ""

# Test 2: Streaming thinking request
echo -e "${YELLOW}Test 2: Streaming thinking request${NC}"
echo "Sending streaming request with thinking enabled..."

STREAM_RESPONSE=$(curl -s -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "claude-opus-4-5-20251101(xhigh)",
    "stream": true,
    "max_tokens": 500,
    "messages": [
      {"role": "user", "content": "What is 3+3?"}
    ]
  }' 2>&1 | head -50)

# Check for reasoning_content in streaming chunks
if echo "$STREAM_RESPONSE" | grep -q "reasoning"; then
    echo -e "${GREEN}✓ Streaming response contains reasoning${NC}"
else
    echo -e "${YELLOW}○ Streaming response may not show reasoning in first chunks${NC}"
fi

echo ""

# Test 3: Tool loop simulation (this tests if cache would inject thinking)
echo -e "${YELLOW}Test 3: Tool loop detection test${NC}"
echo "Testing tool loop scenario where thinking blocks would be needed..."

# This simulates a tool loop - assistant has tool_use but no thinking block
# The cache should either inject a cached thinking block or disable thinking
TOOL_LOOP_RESPONSE=$(curl -s -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "claude-opus-4-5-20251101(xhigh)",
    "stream": false,
    "max_tokens": 1000,
    "messages": [
      {"role": "user", "content": "What is the weather in New York?"},
      {"role": "assistant", "content": "", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\": \"New York\"}"}}]},
      {"role": "tool", "tool_call_id": "call_1", "content": "The weather in New York is 72°F and sunny."}
    ],
    "tools": [
      {"type": "function", "function": {"name": "get_weather", "description": "Get weather", "parameters": {"type": "object", "properties": {"location": {"type": "string"}}}}}
    ]
  }')

# Check if we got a valid response (not an error about thinking blocks)
if echo "$TOOL_LOOP_RESPONSE" | grep -q '"error"'; then
    ERROR_MSG=$(echo "$TOOL_LOOP_RESPONSE" | grep -o '"message":"[^"]*"' | head -1)
    if echo "$ERROR_MSG" | grep -q "thinking"; then
        echo -e "${RED}✗ Error related to thinking blocks: $ERROR_MSG${NC}"
    else
        echo -e "${YELLOW}○ Response contains error (may be unrelated): $ERROR_MSG${NC}"
    fi
else
    echo -e "${GREEN}✓ Tool loop request succeeded (no thinking block error)${NC}"
fi

if echo "$TOOL_LOOP_RESPONSE" | grep -q '"content"'; then
    echo -e "${GREEN}✓ Tool loop response has content${NC}"
fi

echo ""

# Test 4: Check SQLite cache file
echo -e "${YELLOW}Test 4: Cache persistence check${NC}"
if [ -f "thinking_cache.db" ]; then
    echo -e "${GREEN}✓ SQLite cache file exists${NC}"
    CACHE_COUNT=$(sqlite3 thinking_cache.db "SELECT COUNT(*) FROM thinking_cache;" 2>/dev/null || echo "0")
    echo "   Cache entries: $CACHE_COUNT"
else
    echo -e "${YELLOW}○ SQLite cache file not found (may not have cached yet)${NC}"
fi

echo ""
echo "============================================"
echo "Test Summary Complete"
echo "============================================"
echo ""
echo "Note: Check server logs for [THINKING-CACHE] and [CLAUDE-EXECUTOR] messages"
echo "to verify cache operations are occurring."
