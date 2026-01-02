#!/bin/bash
# Quick test script to trigger Claude thinking and capture raw response
# This bypasses Cursor to see what the actual API response looks like

set -e

# Load from .env file
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
if [ -f "$PROJECT_DIR/.env" ]; then
    export $(grep -v '^#' "$PROJECT_DIR/.env" | xargs)
fi

PROXY_URL="https://${NGROK_DOMAIN}/v1/chat/completions"
API_KEY="${CLIPROXYAPI_KEY:-test-api-key}"

echo "=== Testing Thinking Block Response ==="
echo "Proxy URL: $PROXY_URL"
echo "Model: claude-opus-4-5-20251101(xhigh)"
echo ""

# Non-streaming request to see full response
echo ">>> Testing NON-STREAMING request (to see full response structure):"
echo ""

curl -s -X POST "$PROXY_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "claude-opus-4-5-20251101(xhigh)",
    "stream": false,
    "max_tokens": 500,
    "messages": [
      {
        "role": "user", 
        "content": "What is 2+2? Think step by step."
      }
    ]
  }' | python3 -m json.tool 2>/dev/null || echo "JSON parse failed, raw output above"

echo ""
echo ""
echo ">>> Testing STREAMING request (to see chunks with reasoning_content):"
echo ""

curl -s -X POST "$PROXY_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "claude-opus-4-5-20251101(xhigh)",
    "stream": true,
    "max_tokens": 500,
    "messages": [
      {
        "role": "user", 
        "content": "What is 2+2? Think step by step."
      }
    ]
  }' | head -50

echo ""
echo ""
echo "=== Test Complete ==="
