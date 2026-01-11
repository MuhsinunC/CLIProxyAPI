#!/bin/bash
# Test script for CLIProxyAPI Anthropic-compatible endpoint
# This tests that the proxy can serve as a drop-in replacement for Anthropic API
# (for use with Claude Code, etc.)
#
# Usage: ./test_anthropic_compat.sh [base_url]
#
# Environment variables (use .env file or export):
#   CLIPROXYAPI_KEY - API key for the proxy (required)
#   PROXY_BASE_URL - Base URL for the proxy (default: http://localhost:8317)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Load .env if exists
if [ -f "$PROJECT_DIR/.env" ]; then
    export $(grep -v '^#' "$PROJECT_DIR/.env" | xargs)
fi

# Configuration
BASE_URL="${1:-${PROXY_BASE_URL:-http://localhost:8317}}"
API_KEY="${CLIPROXYAPI_KEY:-}"
ENDPOINT="${BASE_URL}/v1/messages"

if [ -z "$API_KEY" ]; then
    echo "ERROR: CLIPROXYAPI_KEY not set. Set it in .env or export it."
    exit 1
fi

echo "=============================================="
echo "CLIProxyAPI Anthropic-Compatible Endpoint Tests"
echo "=============================================="
echo "Base URL: $BASE_URL"
echo "Endpoint: $ENDPOINT"
echo ""

PASS_COUNT=0
FAIL_COUNT=0

test_result() {
    local name=$1
    local success=$2
    local details=$3
    
    if [ "$success" = "true" ]; then
        echo "✅ PASS: $name"
        ((PASS_COUNT++))
    else
        echo "❌ FAIL: $name"
        echo "   Details: $details"
        ((FAIL_COUNT++))
    fi
}

# Test 1: Basic non-streaming messages
echo ""
echo "=== Test 1: Basic Messages API (non-streaming) ==="
RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 50,
        "messages": [
            {"role": "user", "content": "Say hello in exactly 3 words."}
        ]
    }' 2>&1)

if echo "$RESPONSE" | grep -q '"content"'; then
    test_result "Non-streaming messages API" "true"
else
    test_result "Non-streaming messages API" "false" "$RESPONSE"
fi

# Test 2: Streaming messages
echo ""
echo "=== Test 2: Streaming Messages API ==="
STREAM_RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 50,
        "stream": true,
        "messages": [
            {"role": "user", "content": "Say goodbye in exactly 3 words."}
        ]
    }' 2>&1)

if echo "$STREAM_RESPONSE" | grep -q 'event:\|data:'; then
    test_result "Streaming messages API" "true"
else
    test_result "Streaming messages API" "false" "$STREAM_RESPONSE"
fi

# Test 3: System parameter support
echo ""
echo "=== Test 3: System Parameter Support ==="
SYSTEM_RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 50,
        "system": "You are a pirate. Always speak like a pirate.",
        "messages": [
            {"role": "user", "content": "Say hello."}
        ]
    }' 2>&1)

if echo "$SYSTEM_RESPONSE" | grep -q '"content"'; then
    test_result "System parameter support" "true"
else
    test_result "System parameter support" "false" "$SYSTEM_RESPONSE"
fi

# Test 4: Multi-turn conversation
echo ""
echo "=== Test 4: Multi-turn Conversation ==="
MULTI_RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 50,
        "messages": [
            {"role": "user", "content": "My name is Bob."},
            {"role": "assistant", "content": "Hello Bob! Nice to meet you."},
            {"role": "user", "content": "What is my name?"}
        ]
    }' 2>&1)

if echo "$MULTI_RESPONSE" | grep -qi 'bob'; then
    test_result "Multi-turn conversation" "true"
else
    test_result "Multi-turn conversation" "false" "$MULTI_RESPONSE"
fi

# Test 5: Tool use (Anthropic format)
echo ""
echo "=== Test 5: Tool Use (Anthropic format) ==="
TOOL_RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 200,
        "messages": [
            {"role": "user", "content": "What is the weather in Tokyo?"}
        ],
        "tools": [
            {
                "name": "get_weather",
                "description": "Get current weather for a city",
                "input_schema": {
                    "type": "object",
                    "properties": {
                        "city": {"type": "string", "description": "City name"}
                    },
                    "required": ["city"]
                }
            }
        ]
    }' 2>&1)

if echo "$TOOL_RESPONSE" | grep -q '"tool_use"\|"tool_calls"'; then
    test_result "Tool use (Anthropic format)" "true"
else
    test_result "Tool use (Anthropic format)" "false" "$TOOL_RESPONSE"
fi

# Test 6: Extended thinking (if server supports it)
echo ""
echo "=== Test 6: Extended Thinking Support ==="
THINKING_RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 120 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 16000,
        "thinking": {
            "type": "enabled",
            "budget_tokens": 5000
        },
        "messages": [
            {"role": "user", "content": "What is 15 * 23?"}
        ]
    }' 2>&1)

if echo "$THINKING_RESPONSE" | grep -q '"content"\|"thinking"'; then
    test_result "Extended thinking support" "true"
else
    test_result "Extended thinking support" "false" "$THINKING_RESPONSE"
fi

# Test 7: Authorization header support (OpenAI style)
echo ""
echo "=== Test 7: Authorization Header Support ==="
AUTH_RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 50,
        "messages": [
            {"role": "user", "content": "Say test."}
        ]
    }' 2>&1)

if echo "$AUTH_RESPONSE" | grep -q '"content"'; then
    test_result "Authorization header support" "true"
else
    test_result "Authorization header support" "false" "$AUTH_RESPONSE"
fi

# Summary
echo ""
echo "=============================================="
echo "Test Summary"
echo "=============================================="
echo "Passed: $PASS_COUNT"
echo "Failed: $FAIL_COUNT"
echo ""

if [ $FAIL_COUNT -eq 0 ]; then
    echo "🎉 All tests passed! Anthropic-compatible endpoint is working."
    echo ""
    echo "Claude Code Configuration:"
    echo "  ANTHROPIC_API_KEY=<your-proxy-api-key>"
    echo "  ANTHROPIC_BASE_URL=$BASE_URL"
    exit 0
else
    echo "⚠️  Some tests failed. Review the output above."
    exit 1
fi
