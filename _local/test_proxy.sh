#!/bin/bash
# Proxy management and comparison test script
# This script manages the proxy servers and compares responses

set -e

# Load environment variables from .env if it exists
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
if [ -f "$PROJECT_ROOT/.env" ]; then
    set -a
    source "$PROJECT_ROOT/.env"
    set +a
fi

# Configuration - uses environment variables with defaults
CLIPROXYAPI_DIR="$PROJECT_ROOT"
CURSOR_CONNECTOR_DIR="${CURSOR_CONNECTOR_DIR:-$HOME/Projects/cursor-claude-connector}"
PROXY_URL="${PROXY_URL:-http://localhost:8317/v1/chat/completions}"
API_KEY="${API_KEY:?Error: API_KEY environment variable is not set. Set it in .env or export it.}"
TEST_DIR="$SCRIPT_DIR/test_results"

# Create test directory
mkdir -p "$TEST_DIR"

# Function to stop all proxy processes
stop_all_proxies() {
    echo ">>> Stopping all proxy servers..."
    # Kill any node processes running start.sh
    pkill -f "cursor-claude-connector" 2>/dev/null || true
    pkill -f "cli-proxy-api" 2>/dev/null || true
    # Give time for processes to terminate
    sleep 2
    echo ">>> All proxies stopped."
}

# Function to start cursor-claude-connector
start_cursor_connector() {
    echo ">>> Starting cursor-claude-connector..."
    cd "$CURSOR_CONNECTOR_DIR"
    ./start.sh > /tmp/cursor-connector.log 2>&1 &
    CONNECTOR_PID=$!
    echo ">>> cursor-claude-connector started with PID: $CONNECTOR_PID"
    # Wait for server to be ready
    sleep 5
    echo ">>> cursor-claude-connector should be ready."
}

# Function to start CLIProxyAPI
start_cliproxyapi() {
    echo ">>> Starting CLIProxyAPI..."
    cd "$CLIPROXYAPI_DIR"
    ./start.sh --local > /tmp/cliproxyapi.log 2>&1 &
    CLIPROXYAPI_PID=$!
    echo ">>> CLIProxyAPI started with PID: $CLIPROXYAPI_PID"
    # Wait for server to be ready
    sleep 5
    echo ">>> CLIProxyAPI should be ready."
}

# Function to send test request and capture response
send_test_request() {
    local output_file=$1
    echo ">>> Sending test request..."

    # Simple request that should trigger a response (using non-thinking model to avoid temp issues)
    curl -s -X POST "$PROXY_URL" \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer $API_KEY" \
      --max-time 30 \
      -d '{
        "model": "claude-sonnet-4-20250514",
        "stream": true,
        "max_tokens": 100,
        "messages": [
          {
            "role": "system",
            "content": "You are a helpful assistant. When asked to read a file, use the read_file tool."
          },
          {
            "role": "user",
            "content": "Say hello in exactly 5 words."
          }
        ]
      }' > "$output_file" 2>&1

    echo ">>> Response saved to: $output_file"
}

# Function to send tool call test request (using Claude-native format with type: custom)
send_tool_test_request() {
    local output_file=$1
    echo ">>> Sending tool call test request..."

    curl -s -X POST "$PROXY_URL" \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer $API_KEY" \
      --max-time 60 \
      -d '{
        "model": "claude-sonnet-4-20250514",
        "stream": true,
        "max_tokens": 500,
        "messages": [
          {
            "role": "system",
            "content": "You are a coding assistant. When asked to read files, use the read_file tool."
          },
          {
            "role": "user",
            "content": "Read the file at README.md"
          }
        ],
        "tools": [
          {
            "type": "custom",
            "name": "read_file",
            "description": "Read the contents of a file",
            "input_schema": {
              "type": "object",
              "properties": {
                "target_file": {
                  "type": "string",
                  "description": "The path of the file to read"
                }
              },
              "required": ["target_file"]
            }
          }
        ]
      }' > "$output_file" 2>&1

    echo ">>> Response saved to: $output_file"
}

# Main execution
case "${1:-compare}" in
    "stop")
        stop_all_proxies
        ;;
    "start-connector")
        stop_all_proxies
        start_cursor_connector
        ;;
    "start-cliproxy")
        stop_all_proxies
        start_cliproxyapi
        ;;
    "test-simple")
        TIMESTAMP=$(date +%Y%m%d_%H%M%S)
        send_test_request "$TEST_DIR/simple_response_$TIMESTAMP.json"
        ;;
    "test-tools")
        TIMESTAMP=$(date +%Y%m%d_%H%M%S)
        send_tool_test_request "$TEST_DIR/tool_response_$TIMESTAMP.json"
        ;;
    "compare")
        echo "=== RUNNING FULL COMPARISON TEST ==="
        TIMESTAMP=$(date +%Y%m%d_%H%M%S)

        # Step 1: Test cursor-claude-connector
        stop_all_proxies
        start_cursor_connector
        send_test_request "$TEST_DIR/connector_simple_$TIMESTAMP.json"
        send_tool_test_request "$TEST_DIR/connector_tool_$TIMESTAMP.json"

        # Step 2: Test CLIProxyAPI
        stop_all_proxies
        start_cliproxyapi
        send_test_request "$TEST_DIR/cliproxy_simple_$TIMESTAMP.json"
        send_tool_test_request "$TEST_DIR/cliproxy_tool_$TIMESTAMP.json"

        # Step 3: Compare results
        echo ""
        echo "=== COMPARISON RESULTS ==="
        echo "cursor-claude-connector simple response:"
        head -c 500 "$TEST_DIR/connector_simple_$TIMESTAMP.json"
        echo ""
        echo ""
        echo "CLIProxyAPI simple response:"
        head -c 500 "$TEST_DIR/cliproxy_simple_$TIMESTAMP.json"
        echo ""
        echo ""
        echo "cursor-claude-connector tool response:"
        head -c 500 "$TEST_DIR/connector_tool_$TIMESTAMP.json"
        echo ""
        echo ""
        echo "CLIProxyAPI tool response:"
        head -c 500 "$TEST_DIR/cliproxy_tool_$TIMESTAMP.json"
        echo ""

        # Cleanup - stop all proxies
        stop_all_proxies
        ;;
    *)
        echo "Usage: $0 {stop|start-connector|start-cliproxy|test-simple|test-tools|compare}"
        exit 1
        ;;
esac

echo ">>> Done!"
