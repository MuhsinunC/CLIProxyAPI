#!/bin/bash
# Test Gemini thinking block response
# Gemini uses native thinking via Antigravity

set -e

# Load from .env file
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
if [ -f "$PROJECT_DIR/.env" ]; then
    export $(grep -v '^#' "$PROJECT_DIR/.env" | xargs)
fi

PROXY_URL="https://${NGROK_DOMAIN}/v1/chat/completions"
API_KEY="${CLIPROXYAPI_KEY:-test-api-key}"

echo "=== Testing Gemini Thinking Block Response ==="
echo "Proxy URL: $PROXY_URL"
echo ""

# Test non-streaming with Gemini 3 Flash
echo ">>> Testing gemini-3-flash-preview NON-STREAMING:"
echo ""

curl -s -X POST "$PROXY_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "gemini-3-flash-preview",
    "stream": false,
    "max_tokens": 500,
    "messages": [
      {
        "role": "user", 
        "content": "What is 2+2? Think step by step."
      }
    ]
  }' | python3 -m json.tool 2>/dev/null || echo "JSON parse failed"

echo ""
echo ">>> Testing gemini-3-flash-preview STREAMING (first 30 lines):"
echo ""

curl -s -X POST "$PROXY_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "gemini-3-flash-preview",
    "stream": true,
    "max_tokens": 500,
    "messages": [
      {
        "role": "user", 
        "content": "What is 2+2? Think step by step."
      }
    ]
  }' | head -30

echo ""
echo "=== Done ==="
