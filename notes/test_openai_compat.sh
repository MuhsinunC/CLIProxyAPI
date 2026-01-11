#!/bin/bash
# Test script for CLIProxyAPI OpenAI-compatible endpoint
# This tests that the proxy can serve as a drop-in replacement for OpenAI API
#
# Usage: ./test_openai_compat.sh [base_url]
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
ENDPOINT="${BASE_URL}/v1/chat/completions"

if [ -z "$API_KEY" ]; then
    echo "ERROR: CLIPROXYAPI_KEY not set. Set it in .env or export it."
    exit 1
fi

echo "=============================================="
echo "CLIProxyAPI OpenAI-Compatible Endpoint Tests"
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

# Test 1: Basic non-streaming chat completion
echo ""
echo "=== Test 1: Basic Chat Completion (non-streaming) ==="
RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $API_KEY" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "stream": false,
        "max_tokens": 50,
        "messages": [
            {"role": "user", "content": "Say hello in exactly 3 words."}
        ]
    }' 2>&1)

if echo "$RESPONSE" | grep -q '"choices"'; then
    test_result "Non-streaming chat completion" "true"
else
    test_result "Non-streaming chat completion" "false" "$RESPONSE"
fi

# Test 2: Streaming chat completion
echo ""
echo "=== Test 2: Streaming Chat Completion ==="
STREAM_RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $API_KEY" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "stream": true,
        "max_tokens": 50,
        "messages": [
            {"role": "user", "content": "Say goodbye in exactly 3 words."}
        ]
    }' 2>&1)

if echo "$STREAM_RESPONSE" | grep -q 'data:'; then
    test_result "Streaming chat completion" "true"
else
    test_result "Streaming chat completion" "false" "$STREAM_RESPONSE"
fi

# Test 3: System message support
echo ""
echo "=== Test 3: System Message Support ==="
SYSTEM_RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $API_KEY" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "stream": false,
        "max_tokens": 50,
        "messages": [
            {"role": "system", "content": "You are a pirate. Always speak like a pirate."},
            {"role": "user", "content": "Say hello."}
        ]
    }' 2>&1)

if echo "$SYSTEM_RESPONSE" | grep -q '"choices"'; then
    test_result "System message support" "true"
else
    test_result "System message support" "false" "$SYSTEM_RESPONSE"
fi

# Test 4: Multi-turn conversation
echo ""
echo "=== Test 4: Multi-turn Conversation ==="
MULTI_RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $API_KEY" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "stream": false,
        "max_tokens": 50,
        "messages": [
            {"role": "user", "content": "My name is Alice."},
            {"role": "assistant", "content": "Hello Alice! Nice to meet you."},
            {"role": "user", "content": "What is my name?"}
        ]
    }' 2>&1)

if echo "$MULTI_RESPONSE" | grep -qi 'alice'; then
    test_result "Multi-turn conversation" "true"
else
    test_result "Multi-turn conversation" "false" "$MULTI_RESPONSE"
fi

# Test 5: Tool/Function calling (OpenAI format)
echo ""
echo "=== Test 5: Tool/Function Calling ==="
TOOL_RESPONSE=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $API_KEY" \
    --max-time 60 \
    -d '{
        "model": "claude-sonnet-4-20250514",
        "stream": false,
        "max_tokens": 200,
        "messages": [
            {"role": "user", "content": "What is the weather in San Francisco?"}
        ],
        "tools": [
            {
                "type": "function",
                "function": {
                    "name": "get_weather",
                    "description": "Get current weather for a city",
                    "parameters": {
                        "type": "object",
                        "properties": {
                            "city": {"type": "string", "description": "City name"}
                        },
                        "required": ["city"]
                    }
                }
            }
        ]
    }' 2>&1)

if echo "$TOOL_RESPONSE" | grep -q '"tool_calls"\|"function_call"'; then
    test_result "Tool/Function calling" "true"
else
    test_result "Tool/Function calling" "false" "$TOOL_RESPONSE"
fi

# Test 6: Models endpoint
echo ""
echo "=== Test 6: Models Endpoint ==="
MODELS_RESPONSE=$(curl -s -X GET "${BASE_URL}/v1/models" \
    -H "Authorization: Bearer $API_KEY" \
    --max-time 30 2>&1)

if echo "$MODELS_RESPONSE" | grep -q '"data"'; then
    test_result "Models endpoint" "true"
else
    test_result "Models endpoint" "false" "$MODELS_RESPONSE"
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
    echo "🎉 All tests passed! OpenAI-compatible endpoint is working."
    exit 0
else
    echo "⚠️  Some tests failed. Review the output above."
    exit 1
fi
