#!/bin/bash
# Test script to capture cursor-claude-connector response format for tool calls
# This sends a simple request - the model should decide to use tools

# Load environment variables from .env if it exists
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
if [ -f "$PROJECT_ROOT/.env" ]; then
    set -a
    source "$PROJECT_ROOT/.env"
    set +a
fi

# Configuration - uses environment variables with defaults
PROXY_URL="${PROXY_URL:-http://localhost:8317/v1/chat/completions}"
API_KEY="${API_KEY:?Error: API_KEY environment variable is not set. Set it in .env or export it.}"
OUTPUT_FILE="$SCRIPT_DIR/reference_tool_call_response.json"

echo "=== Capturing Tool Call Response from cursor-claude-connector ==="
echo "Sending request to: $PROXY_URL"
echo ""

# Simple request that should trigger tool usage
# Using Claude model without thinking suffix to avoid temperature issues
curl -s -X POST "$PROXY_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "stream": true,
    "max_tokens": 4096,
    "messages": [
      {
        "role": "user",
        "content": "What files are in the current directory? Please list them."
      }
    ]
  }' | tee "$OUTPUT_FILE"

echo ""
echo ""
echo "=== Response saved to $OUTPUT_FILE ==="
