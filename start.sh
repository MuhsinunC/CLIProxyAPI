#!/bin/bash

# ============================================
# CLIProxyAPI Development Startup Script
# ============================================
#
# Usage:
#   ./start.sh              # Build from source and start with ngrok (default)
#   ./start.sh --brew       # Use brew version instead of building from source
#   ./start.sh --no-ngrok   # Start without ngrok (localhost only)
#   ./start.sh --webui-dev  # Enable WebUI hot-reload mode (uses vite dev server)
#   ./start.sh --stop       # Stop any running server processes
#   ./start.sh --brew --no-ngrok  # Brew version, no ngrok
#
# Environment variables (can be set in .env):
#   USE_NGROK=true|false    # Enable/disable ngrok tunnel (default: true)
#   NGROK_DOMAIN=           # Custom ngrok domain (optional)
#   PORT=                   # Override port (reads from config.yaml by default)
#   CLIPROXY_BIN=           # Path to cliproxyapi binary
#   NGROK_BIN=              # Path to ngrok binary
#   WEBUI_DIR=              # Path to WebUI project (default: ../Cli-Proxy-API-Management-Center)
#   WEBUI_PORT=             # WebUI port (default: 5173)
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

# Script directory (used throughout)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# WebUI defaults
WEBUI_DIR="${WEBUI_DIR:-$SCRIPT_DIR/../Cli-Proxy-API-Management-Center}"
WEBUI_PORT="${WEBUI_PORT:-5173}"
WEBUI_DEV_MODE=false

# Read port from config.yaml (falls back to 8317 if not found)
CONFIG_FILE="$SCRIPT_DIR/config.yaml"
if [ -f "$CONFIG_FILE" ]; then
    CONFIG_PORT=$(grep "^port:" "$CONFIG_FILE" 2>/dev/null | awk '{print $2}')
fi
PORT="${PORT:-${CONFIG_PORT:-8317}}"

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
        --webui-dev)
            WEBUI_DEV_MODE=true
            ;;
        --stop)
            echo "Stopping CLIProxyAPI on port $PORT..."
            # Kill server process on port
            PIDS=$(lsof -ti ":$PORT" 2>/dev/null)
            if [ -n "$PIDS" ]; then
                echo "$PIDS" | xargs kill 2>/dev/null
                echo "Killed server process(es): $PIDS"
            else
                echo "No server found on port $PORT"
            fi
            # Kill ngrok tunneling to our port
            NGROK_PIDS=$(pgrep -f "ngrok.*$PORT" 2>/dev/null)
            if [ -n "$NGROK_PIDS" ]; then
                echo "$NGROK_PIDS" | xargs kill 2>/dev/null
                echo "Killed ngrok process(es): $NGROK_PIDS"
            fi
            # Kill WebUI server on its port
            echo "Stopping WebUI on port $WEBUI_PORT..."
            WEBUI_PIDS=$(lsof -ti ":$WEBUI_PORT" 2>/dev/null)
            if [ -n "$WEBUI_PIDS" ]; then
                echo "$WEBUI_PIDS" | xargs kill 2>/dev/null
                echo "Killed WebUI process(es): $WEBUI_PIDS"
            else
                echo "No WebUI server found on port $WEBUI_PORT"
            fi
            exit 0
            ;;
        --help|-h)
            sed -n '3,21p' "$0" | sed 's/^# //' | sed 's/^#//'
            exit 0
            ;;
    esac
done

# Check if ports are already in use
if lsof -i ":$PORT" >/dev/null 2>&1; then
    echo "ERROR: Port $PORT is already in use!"
    echo "Another instance may be running."
    echo ""
    lsof -i ":$PORT"
    echo ""
    echo "Run './start.sh --stop' to kill existing processes."
    exit 1
fi

if lsof -i ":$WEBUI_PORT" >/dev/null 2>&1; then
    echo "ERROR: WebUI port $WEBUI_PORT is already in use!"
    echo "Another instance may be running."
    echo ""
    lsof -i ":$WEBUI_PORT"
    echo ""
    echo "Run './start.sh --stop' to kill existing processes."
    exit 1
fi

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
WEBUI_PID=""

# Cleanup function - stops all services
cleanup() {
    echo ""
    echo "Shutting down..."

    if [ -n "$WEBUI_PID" ]; then
        echo "Stopping WebUI (PID $WEBUI_PID)..."
        kill "$WEBUI_PID" 2>/dev/null || true
    fi

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

# Build and start WebUI
build_webui() {
    if [ ! -d "$WEBUI_DIR" ]; then
        echo "ERROR: WebUI directory not found at $WEBUI_DIR"
        echo "Clone it with: git clone https://github.com/MuhsinunC/Cli-Proxy-API-Management-Center.git"
        exit 1
    fi

    echo "=========================================="
    echo "BUILDING WEBUI..."
    echo "=========================================="
    echo ""

    cd "$WEBUI_DIR" || exit 1

    # Install dependencies if node_modules doesn't exist
    if [ ! -d "node_modules" ]; then
        echo "Installing WebUI dependencies..."
        if command -v bun >/dev/null 2>&1; then
            bun install || { echo "Failed to install WebUI dependencies"; exit 1; }
        elif command -v npm >/dev/null 2>&1; then
            npm install || { echo "Failed to install WebUI dependencies"; exit 1; }
        else
            echo "ERROR: Neither bun nor npm found. Install one of them."
            exit 1
        fi
    fi

    # Build the WebUI
    if command -v bun >/dev/null 2>&1; then
        bun run build || { echo "Failed to build WebUI"; exit 1; }
    else
        npm run build || { echo "Failed to build WebUI"; exit 1; }
    fi

    echo ""
    echo "WEBUI BUILD SUCCESSFUL!"
    echo ""
}

start_webui() {
    cd "$WEBUI_DIR" || exit 1

    if [ "$WEBUI_DEV_MODE" = true ]; then
        echo "Starting WebUI in dev mode (hot-reload) on port $WEBUI_PORT..."
        if command -v bun >/dev/null 2>&1; then
            ( bun run dev 2>&1 | sed 's/^/[WEBUI] /' ) &
        else
            ( npm run dev 2>&1 | sed 's/^/[WEBUI] /' ) &
        fi
    else
        echo "Starting WebUI in preview mode on port $WEBUI_PORT..."
        if command -v bun >/dev/null 2>&1; then
            ( bun run preview 2>&1 | sed 's/^/[WEBUI] /' ) &
        else
            ( npm run preview 2>&1 | sed 's/^/[WEBUI] /' ) &
        fi
    fi
    WEBUI_PID=$!

    # Wait a moment and check if it started
    sleep 2
    if ! kill -0 "$WEBUI_PID" 2>/dev/null; then
        echo "WARNING: WebUI may have failed to start. Check output above."
    else
        echo "WebUI started (PID $WEBUI_PID)"
    fi

    # Return to script directory
    cd "$SCRIPT_DIR" || exit 1
}

# Build WebUI (only if not in dev mode, since dev mode doesn't need pre-build)
if [ "$WEBUI_DEV_MODE" = false ]; then
    build_webui
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

# Start WebUI
start_webui
echo ""

# Start ngrok if enabled
if [ "$USE_NGROK" = "true" ] || [ "$USE_NGROK" = "1" ]; then
    if [ -n "$NGROK_DOMAIN" ]; then
        echo "Starting ngrok tunnel: https://$NGROK_DOMAIN -> http://localhost:$PORT"
        ( "$NGROK_BIN" http --domain="$NGROK_DOMAIN" "$PORT" 2>&1 | sed 's/^/[NGROK] /' ) &
        NGROK_PID=$!
        NGROK_URL="https://$NGROK_DOMAIN"
    else
        echo "Starting ngrok tunnel (random URL) -> http://localhost:$PORT"
        ( "$NGROK_BIN" http "$PORT" 2>&1 | sed 's/^/[NGROK] /' ) &
        NGROK_PID=$!

        # Wait for ngrok to start and get the public URL
        sleep 2
        NGROK_URL=$(curl -s http://localhost:4040/api/tunnels 2>/dev/null | grep -o '"public_url":"https://[^"]*' | head -1 | cut -d'"' -f4)

        if [ -z "$NGROK_URL" ]; then
            echo "Warning: Could not get ngrok URL automatically."
            echo "Check http://localhost:4040 for your URL"
            NGROK_URL="<check ngrok dashboard>"
        fi
    fi

    echo "ngrok started (PID $NGROK_PID)"
    echo ""
    echo "=========================================="
    echo "API Endpoints:"
    echo "  OpenAI-compatible:    $NGROK_URL/v1/chat/completions"
    echo "  Anthropic-compatible: $NGROK_URL/v1/messages"
    echo ""
    echo "WebUI:"
    echo "  Local:                http://localhost:$WEBUI_PORT"
    if [ "$WEBUI_DEV_MODE" = true ]; then
        echo "  Mode:                 Development (hot-reload enabled)"
    else
        echo "  Mode:                 Preview (static build)"
    fi
    echo ""
    echo "Cursor Settings:"
    echo "  Base URL: $NGROK_URL/v1"
    echo "  API Key:  (your api-key from config)"
    echo "  Model:    claude-opus-4-5-20251101(xhigh)"
    echo "=========================================="
else
    echo "=========================================="
    echo "API Endpoints:"
    echo "  OpenAI-compatible:    http://localhost:$PORT/v1/chat/completions"
    echo "  Anthropic-compatible: http://localhost:$PORT/v1/messages"
    echo ""
    echo "WebUI:"
    echo "  URL:                  http://localhost:$WEBUI_PORT"
    if [ "$WEBUI_DEV_MODE" = true ]; then
        echo "  Mode:                 Development (hot-reload enabled)"
    else
        echo "  Mode:                 Preview (static build)"
    fi
    echo ""
    echo "Cursor Settings:"
    echo "  Base URL: http://localhost:$PORT/v1"
    echo "  API Key:  (your api-key from config)"
    echo "  Model:    claude-opus-4-5-20251101(xhigh)"
    echo "=========================================="
fi

echo ""
echo "Press Ctrl+C to stop"
echo ""

# Wait for CLIProxyAPI (the main process we care about)
wait $CLIPROXY_PID
cleanup
