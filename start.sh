#!/bin/bash

# ============================================
# CLIProxyAPI Development Startup Script
# ============================================
#
# Usage:
#   ./start.sh              # Build from source and start with ngrok (default)
#   ./start.sh --brew       # Use brew version instead of building from source
#   ./start.sh --no-ngrok   # Start without ngrok (localhost only)
#   ./start.sh --brew --no-ngrok  # Brew version, no ngrok
#
# Environment variables (can be set in .env):
#   USE_NGROK=true|false    # Enable/disable ngrok tunnel (default: true)
#   NGROK_DOMAIN=           # Custom ngrok domain (optional)
#   PORT=8317               # CLIProxyAPI port (default: 8317)
#   CLIPROXY_BIN=           # Path to cliproxyapi binary
#   NGROK_BIN=              # Path to ngrok binary
#
# ============================================

# Load environment variables if present (before parsing args so .env can set defaults)
if [ -f ".env" ]; then
    set -a
    source .env
    set +a
fi

# Config defaults
NGROK_BIN="${NGROK_BIN:-ngrok}"
NGROK_DOMAIN="${NGROK_DOMAIN:-}"
NGROK_CONFIG_DEFAULT="$HOME/Library/Application Support/ngrok/ngrok.yml"
NGROK_CONFIG="${NGROK_CONFIG:-$NGROK_CONFIG_DEFAULT}"
CLIPROXY_BIN="${CLIPROXY_BIN:-cliproxyapi}"
PORT="${PORT:-8317}"

# Feature flags (can be set via env or command line)
USE_NGROK="${USE_NGROK:-true}"
USE_LOCAL_BUILD=true  # Default to local build for development

# Parse command line arguments
for arg in "$@"; do
    case $arg in
        --brew)
            USE_LOCAL_BUILD=false
            ;;
        --no-ngrok)
            USE_NGROK=false
            ;;
        --help|-h)
            sed -n '3,19p' "$0" | sed 's/^# //' | sed 's/^#//'
            exit 0
            ;;
    esac
done

# Print startup banner
if [ "$USE_NGROK" = "true" ] || [ "$USE_NGROK" = "1" ]; then
    echo "Starting CLIProxyAPI + ngrok tunnel..."
else
    echo "Starting CLIProxyAPI (localhost only, no ngrok)..."
fi
echo ""

# PIDs for cleanup
CLIPROXY_PID=""
NGROK_PID=""

# Cleanup function - stops both services
cleanup() {
    echo ""
    echo "Shutting down..."

    if [ -n "$NGROK_PID" ]; then
        echo "Stopping ngrok (PID $NGROK_PID)..."
        kill "$NGROK_PID" 2>/dev/null || true
    fi

    if [ -n "$CLIPROXY_PID" ]; then
        echo "Stopping CLIProxyAPI (PID $CLIPROXY_PID)..."
        kill "$CLIPROXY_PID" 2>/dev/null || true
    fi

    echo "Done."
    exit 0
}

# Set up trap to catch Ctrl+C and termination signals
trap cleanup INT TERM

# Validate CLIProxyAPI (only for brew version)
check_cliproxy() {
    if ! command -v "$CLIPROXY_BIN" >/dev/null 2>&1; then
        echo "CLIProxyAPI not found. Install it with: brew install cliproxyapi"
        exit 1
    fi
}

# Build local version from source
build_local() {
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    LOCAL_BIN="$SCRIPT_DIR/cli-proxy-api"

    echo "=========================================="
    echo "BUILDING LOCAL VERSION FROM SOURCE..."
    echo "=========================================="
    echo ""

    cd "$SCRIPT_DIR" || exit 1

    if ! go build -o cli-proxy-api ./cmd/server; then
        echo ""
        echo "BUILD FAILED! Check the errors above."
        exit 1
    fi

    echo ""
    echo "BUILD SUCCESSFUL!"
    echo ""

    # Update the binary path to use local build
    CLIPROXY_BIN="$LOCAL_BIN"
}

# Validate ngrok setup
check_ngrok() {
    if ! command -v "$NGROK_BIN" >/dev/null 2>&1; then
        echo "ngrok not found. Install it with: brew install ngrok"
        echo "Or run with --no-ngrok to skip ngrok"
        exit 1
    fi

    if [ ! -f "$NGROK_CONFIG" ]; then
        echo "ngrok config not found at '$NGROK_CONFIG'"
        echo "Run: ngrok config add-authtoken <YOUR_TOKEN>"
        echo "Or run with --no-ngrok to skip ngrok"
        exit 1
    fi

    if ! grep -q "authtoken" "$NGROK_CONFIG"; then
        echo "ngrok authtoken not set in '$NGROK_CONFIG'"
        echo "Run: ngrok config add-authtoken <YOUR_TOKEN>"
        echo "Or run with --no-ngrok to skip ngrok"
        exit 1
    fi
}

# Build local or validate brew version
if [ "$USE_LOCAL_BUILD" = true ]; then
    build_local
else
    check_cliproxy
fi

# Only check ngrok if we're using it
if [ "$USE_NGROK" = "true" ] || [ "$USE_NGROK" = "1" ]; then
    check_ngrok
fi

# Start CLIProxyAPI in background
if [ "$USE_LOCAL_BUILD" = true ]; then
    echo "=========================================="
    echo "STARTING LOCAL BUILD (not brew)"
    echo "=========================================="
else
    echo "Starting CLIProxyAPI (brew version) on port $PORT..."
fi

"$CLIPROXY_BIN" &
CLIPROXY_PID=$!
echo "CLIProxyAPI started (PID $CLIPROXY_PID)"

# Wait a moment for CLIProxyAPI to start
sleep 2

# Check if CLIProxyAPI started successfully
if ! kill -0 "$CLIPROXY_PID" 2>/dev/null; then
    echo "CLIProxyAPI failed to start!"
    exit 1
fi

echo ""

# Start ngrok if enabled
if [ "$USE_NGROK" = "true" ] || [ "$USE_NGROK" = "1" ]; then
    NGROK_LOG="/tmp/ngrok.log"

    if [ -n "$NGROK_DOMAIN" ]; then
        echo "Starting ngrok tunnel: https://$NGROK_DOMAIN -> http://localhost:$PORT"
        "$NGROK_BIN" http --domain="$NGROK_DOMAIN" "$PORT" >"$NGROK_LOG" 2>&1 &
        NGROK_PID=$!
        NGROK_URL="https://$NGROK_DOMAIN"
    else
        echo "Starting ngrok tunnel (random URL) -> http://localhost:$PORT"
        "$NGROK_BIN" http "$PORT" >"$NGROK_LOG" 2>&1 &
        NGROK_PID=$!

        # Wait for ngrok to start and get the public URL
        sleep 2
        NGROK_URL=$(curl -s http://localhost:4040/api/tunnels 2>/dev/null | grep -o '"public_url":"https://[^"]*' | head -1 | cut -d'"' -f4)

        if [ -z "$NGROK_URL" ]; then
            echo "Warning: Could not get ngrok URL automatically."
            echo "Check $NGROK_LOG or http://localhost:4040 for your URL"
            NGROK_URL="<check ngrok dashboard>"
        fi
    fi

    echo "ngrok started (PID $NGROK_PID, logs: $NGROK_LOG)"
    echo ""
    echo "=========================================="
    echo "Use these settings in Cursor:"
    echo "  Base URL: $NGROK_URL/v1"
    echo "  API Key:  (your api-key from config)"
    echo "  Model:    claude-opus-4-5-20251101(xhigh)"
    echo "=========================================="
else
    echo "=========================================="
    echo "CLIProxyAPI running (no ngrok)"
    echo "  Local URL: http://localhost:$PORT/v1"
    echo "  API Key:   (your api-key from config)"
    echo "  Model:     claude-opus-4-5-20251101(xhigh)"
    echo "=========================================="
fi

echo ""
echo "Press Ctrl+C to stop"
echo ""

# Wait for CLIProxyAPI (the main process we care about)
wait $CLIPROXY_PID
cleanup
