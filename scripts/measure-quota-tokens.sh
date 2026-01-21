#!/bin/bash
#
# Measure token-to-quota relationship for Claude Opus 4.5
# Collects data points as quota percentages change and calculates capacity estimates
#
# Usage: ./measure-quota-tokens.sh [5h_target] [7d_target]
#   5h_target: Target 5-hour percentage increase (default: 3)
#   7d_target: Target 7-day percentage increase (default: 1)
#
# Example: ./measure-quota-tokens.sh 5 2   # Run until 5% 5-hour and 2% 7-day increase
#
# Loads configuration from .env file (NGROK_DOMAIN, CLIPROXYAPI_KEY, MANAGEMENT_PASSWORD)
#

set -e

# Load .env if it exists
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$SCRIPT_DIR/../.env" ]; then
    # shellcheck source=/dev/null
    source "$SCRIPT_DIR/../.env"
fi

# Configuration from environment
if [ -n "$NGROK_DOMAIN" ]; then
    PROXY_URL="https://${NGROK_DOMAIN}"
else
    PROXY_URL="http://localhost:8317"
fi
MGMT_KEY="${MANAGEMENT_PASSWORD:-}"
API_KEY="${CLIPROXYAPI_KEY:-}"
MODEL="claude-opus-4-5-20251101"

# Validate required vars
if [ -z "$MGMT_KEY" ]; then
    echo "Error: MANAGEMENT_PASSWORD not set in .env"
    exit 1
fi
if [ -z "$API_KEY" ]; then
    echo "Error: CLIPROXYAPI_KEY not set in .env"
    exit 1
fi

TARGET_5H_DELTA="${1:-3}"   # Target 5-hour increase (default 3%)
TARGET_7D_DELTA="${2:-1}"   # Target 7-day increase (default 1%)

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

get_quota() {
    curl -s -X POST \
        -H "X-Management-Key: $MGMT_KEY" \
        -H "Content-Type: application/json" \
        "$PROXY_URL/v0/management/api-call" \
        -d "{\"auth_index\":\"$AUTH_INDEX\",\"method\":\"GET\",\"url\":\"https://api.anthropic.com/api/oauth/usage\",\"header\":{\"Authorization\":\"Bearer \$TOKEN\$\",\"anthropic-beta\":\"oauth-2025-04-20\",\"User-Agent\":\"claude-code/2.0.31\"}}" \
        | jq -r '.body'
}

send_request() {
    # Opus 4.5 with thinking DISABLED, max 1000 output tokens for faster measurement
    curl -s -X POST \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $API_KEY" \
        "$PROXY_URL/v1/messages" \
        -d "{\"model\":\"$MODEL\",\"max_tokens\":1000,\"thinking\":{\"type\":\"disabled\"},\"messages\":[{\"role\":\"user\",\"content\":\"Write a detailed explanation of how computers work, covering CPUs, memory, storage, and networking. Be thorough.\"}]}"
}

# Get auth index
echo -e "${YELLOW}=== QUOTA TOKEN MEASUREMENT ===${NC}"
echo "Model: $MODEL (thinking disabled)"
echo "Target: 5-hour +${TARGET_5H_DELTA}%, 7-day +${TARGET_7D_DELTA}%"
echo ""

AUTH_INDEX=$(curl -s -H "X-Management-Key: $MGMT_KEY" "$PROXY_URL/v0/management/auth-files" | jq -r '.files[] | select(.type=="claude") | .auth_index')
if [ -z "$AUTH_INDEX" ]; then
    echo -e "${RED}Error: No Claude auth found${NC}"
    exit 1
fi
echo "Auth Index: $AUTH_INDEX"
echo ""

# Get initial quota
INITIAL_QUOTA=$(get_quota)
INITIAL_5H=$(echo "$INITIAL_QUOTA" | jq -r '.five_hour.utilization')
INITIAL_7D=$(echo "$INITIAL_QUOTA" | jq -r '.seven_day.utilization')

echo -e "${CYAN}Initial State:${NC}"
echo "  5-hour: ${INITIAL_5H}%"
echo "  7-day:  ${INITIAL_7D}%"
echo ""

# Data collection files
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
DATA_DIR="/tmp/quota_measurement_${TIMESTAMP}"
mkdir -p "$DATA_DIR"

DATA_FILE="$DATA_DIR/raw_data.csv"
TRANSITIONS_5H_FILE="$DATA_DIR/transitions_5h.csv"
TRANSITIONS_7D_FILE="$DATA_DIR/transitions_7d.csv"

echo "request,input_tokens,output_tokens,total_tokens,cumulative_input,cumulative_output,cumulative_total,5h_percent,7d_percent" > "$DATA_FILE"
echo "from_percent,to_percent,cumulative_tokens" > "$TRANSITIONS_5H_FILE"
echo "from_percent,to_percent,cumulative_tokens" > "$TRANSITIONS_7D_FILE"

# Track totals
TOTAL_INPUT=0
TOTAL_OUTPUT=0
REQUEST_NUM=0
LAST_5H=$INITIAL_5H
LAST_7D=$INITIAL_7D

# Track first data point (baseline)
BASELINE_5H=$INITIAL_5H
BASELINE_7D=$INITIAL_7D

echo -e "${YELLOW}Sending requests...${NC}"
echo ""

while true; do
    REQUEST_NUM=$((REQUEST_NUM + 1))

    # Send request
    RESPONSE=$(send_request)
    INPUT=$(echo "$RESPONSE" | jq -r '.usage.input_tokens // 0')
    OUTPUT=$(echo "$RESPONSE" | jq -r '.usage.output_tokens // 0')
    TOKENS=$((INPUT + OUTPUT))

    TOTAL_INPUT=$((TOTAL_INPUT + INPUT))
    TOTAL_OUTPUT=$((TOTAL_OUTPUT + OUTPUT))
    TOTAL=$((TOTAL_INPUT + TOTAL_OUTPUT))

    # Get current quota
    CURRENT_QUOTA=$(get_quota)
    CURRENT_5H=$(echo "$CURRENT_QUOTA" | jq -r '.five_hour.utilization')
    CURRENT_7D=$(echo "$CURRENT_QUOTA" | jq -r '.seven_day.utilization')

    # Log data point
    echo "$REQUEST_NUM,$INPUT,$OUTPUT,$TOKENS,$TOTAL_INPUT,$TOTAL_OUTPUT,$TOTAL,$CURRENT_5H,$CURRENT_7D" >> "$DATA_FILE"

    # Print progress
    printf "  #%-3d: in=%-4d out=%-4d | cumulative: in=%-6d out=%-6d total=%-6d | 5h: %3.0f%% | 7d: %3.0f%%\n" \
        "$REQUEST_NUM" "$INPUT" "$OUTPUT" "$TOTAL_INPUT" "$TOTAL_OUTPUT" "$TOTAL" "$CURRENT_5H" "$CURRENT_7D"

    # Check if we hit a new percentage point and record transitions
    if [ "$CURRENT_5H" != "$LAST_5H" ]; then
        echo -e "    ${GREEN}^ 5-hour changed: ${LAST_5H}% -> ${CURRENT_5H}% at ${TOTAL} tokens${NC}"
        echo "${LAST_5H},${CURRENT_5H},${TOTAL}" >> "$TRANSITIONS_5H_FILE"
        LAST_5H=$CURRENT_5H
    fi

    if [ "$CURRENT_7D" != "$LAST_7D" ]; then
        echo -e "    ${GREEN}^ 7-day changed: ${LAST_7D}% -> ${CURRENT_7D}% at ${TOTAL} tokens${NC}"
        echo "${LAST_7D},${CURRENT_7D},${TOTAL}" >> "$TRANSITIONS_7D_FILE"
        LAST_7D=$CURRENT_7D
    fi

    # Check if we've reached BOTH targets
    DELTA_5H=$(echo "$CURRENT_5H - $BASELINE_5H" | bc)
    DELTA_7D=$(echo "$CURRENT_7D - $BASELINE_7D" | bc)

    REACHED_5H=$(echo "$DELTA_5H >= $TARGET_5H_DELTA" | bc -l)
    REACHED_7D=$(echo "$DELTA_7D >= $TARGET_7D_DELTA" | bc -l)

    if (( REACHED_5H )) && (( REACHED_7D )); then
        echo ""
        echo -e "${GREEN}=== ALL TARGETS REACHED ===${NC}"
        break
    fi

    # Safety limit
    if [ $REQUEST_NUM -ge 500 ]; then
        echo ""
        echo -e "${YELLOW}Safety limit reached (500 requests)${NC}"
        break
    fi

    sleep 0.1
done

echo ""
echo -e "${CYAN}============================================================${NC}"
echo -e "${CYAN}                    MEASUREMENT RESULTS                      ${NC}"
echo -e "${CYAN}============================================================${NC}"
echo ""
echo "Date:              $(date '+%Y-%m-%d %H:%M:%S')"
echo "Requests sent:     $REQUEST_NUM"
echo "Total input:       $TOTAL_INPUT tokens"
echo "Total output:      $TOTAL_OUTPUT tokens"
echo "Total tokens:      $TOTAL"
echo ""

# Calculate final deltas
FINAL_5H=$CURRENT_5H
FINAL_7D=$CURRENT_7D
DELTA_5H=$(echo "$FINAL_5H - $BASELINE_5H" | bc)
DELTA_7D=$(echo "$FINAL_7D - $BASELINE_7D" | bc)

echo "5-hour change:     ${BASELINE_5H}% -> ${FINAL_5H}% (delta: ${DELTA_5H}%)"
echo "7-day change:      ${BASELINE_7D}% -> ${FINAL_7D}% (delta: ${DELTA_7D}%)"
echo ""

# Analyze 5-hour transitions
echo -e "${BOLD}${CYAN}--- 5-HOUR SESSION ANALYSIS ---${NC}"
NUM_5H_TRANSITIONS=$(tail -n +2 "$TRANSITIONS_5H_FILE" | wc -l | tr -d ' ')
if [ "$NUM_5H_TRANSITIONS" -gt 0 ]; then
    echo ""
    echo "Transitions recorded: $NUM_5H_TRANSITIONS"
    echo ""

    # Calculate tokens per transition
    PREV_TOKENS=0
    TOTAL_TOKENS_PER_PERCENT=0
    TRANSITION_COUNT=0

    echo "  From%  To%    Cumulative    Delta (tokens for this 1%)"
    echo "  -----  ----   ----------    --------------------------"

    while IFS=',' read -r FROM TO CUMULATIVE; do
        DELTA_TOKENS=$((CUMULATIVE - PREV_TOKENS))
        printf "  %5.0f  %4.0f   %10d    %d\n" "$FROM" "$TO" "$CUMULATIVE" "$DELTA_TOKENS"
        TOTAL_TOKENS_PER_PERCENT=$((TOTAL_TOKENS_PER_PERCENT + DELTA_TOKENS))
        TRANSITION_COUNT=$((TRANSITION_COUNT + 1))
        PREV_TOKENS=$CUMULATIVE
    done < <(tail -n +2 "$TRANSITIONS_5H_FILE")

    echo ""

    if [ "$TRANSITION_COUNT" -gt 0 ]; then
        AVG_TOKENS_5H=$(echo "scale=0; $TOTAL_TOKENS_PER_PERCENT / $TRANSITION_COUNT" | bc)
        CAPACITY_5H=$(echo "scale=0; 100 * $AVG_TOKENS_5H" | bc)

        echo -e "  ${GREEN}Average tokens per 1%:  ${BOLD}$AVG_TOKENS_5H${NC}"
        echo -e "  ${GREEN}Estimated 5h capacity:  ${BOLD}$(printf "%'d" $CAPACITY_5H) tokens${NC}"
    fi
else
    echo "  No transitions recorded (5-hour quota did not change)"
fi

echo ""

# Analyze 7-day transitions
echo -e "${BOLD}${CYAN}--- 7-DAY WEEKLY ANALYSIS ---${NC}"
NUM_7D_TRANSITIONS=$(tail -n +2 "$TRANSITIONS_7D_FILE" | wc -l | tr -d ' ')
if [ "$NUM_7D_TRANSITIONS" -gt 0 ]; then
    echo ""
    echo "Transitions recorded: $NUM_7D_TRANSITIONS"
    echo ""

    # Calculate tokens per transition
    PREV_TOKENS=0
    TOTAL_TOKENS_PER_PERCENT=0
    TRANSITION_COUNT=0

    echo "  From%  To%    Cumulative    Delta (tokens for this 1%)"
    echo "  -----  ----   ----------    --------------------------"

    while IFS=',' read -r FROM TO CUMULATIVE; do
        DELTA_TOKENS=$((CUMULATIVE - PREV_TOKENS))
        printf "  %5.0f  %4.0f   %10d    %d\n" "$FROM" "$TO" "$CUMULATIVE" "$DELTA_TOKENS"
        TOTAL_TOKENS_PER_PERCENT=$((TOTAL_TOKENS_PER_PERCENT + DELTA_TOKENS))
        TRANSITION_COUNT=$((TRANSITION_COUNT + 1))
        PREV_TOKENS=$CUMULATIVE
    done < <(tail -n +2 "$TRANSITIONS_7D_FILE")

    echo ""

    if [ "$TRANSITION_COUNT" -gt 0 ]; then
        AVG_TOKENS_7D=$(echo "scale=0; $TOTAL_TOKENS_PER_PERCENT / $TRANSITION_COUNT" | bc)
        CAPACITY_7D=$(echo "scale=0; 100 * $AVG_TOKENS_7D" | bc)

        echo -e "  ${GREEN}Average tokens per 1%:  ${BOLD}$AVG_TOKENS_7D${NC}"
        echo -e "  ${GREEN}Estimated 7d capacity:  ${BOLD}$(printf "%'d" $CAPACITY_7D) tokens${NC}"
    fi
else
    echo "  No transitions recorded (7-day quota did not change)"
fi

echo ""

# Summary box
echo -e "${CYAN}============================================================${NC}"
echo -e "${CYAN}                      CAPACITY ESTIMATES                     ${NC}"
echo -e "${CYAN}============================================================${NC}"
echo ""

if [ "$NUM_5H_TRANSITIONS" -gt 0 ] && [ "$NUM_7D_TRANSITIONS" -gt 0 ]; then
    RATIO=$(echo "scale=1; $CAPACITY_7D / $CAPACITY_5H" | bc)

    printf "  ${BOLD}5-Hour Session Limit:${NC}  %'d tokens (~%d per 1%%)\n" "$CAPACITY_5H" "$AVG_TOKENS_5H"
    printf "  ${BOLD}7-Day Weekly Limit:${NC}    %'d tokens (~%d per 1%%)\n" "$CAPACITY_7D" "$AVG_TOKENS_7D"
    echo ""
    echo -e "  ${BOLD}Weekly/Session Ratio:${NC}  ${RATIO}x"
    echo ""
    echo "  This means you can use approximately $RATIO full sessions"
    echo "  before hitting the weekly limit."
elif [ "$NUM_5H_TRANSITIONS" -gt 0 ]; then
    printf "  ${BOLD}5-Hour Session Limit:${NC}  %'d tokens (~%d per 1%%)\n" "$CAPACITY_5H" "$AVG_TOKENS_5H"
    echo "  (7-day data insufficient - run with higher 7d target)"
elif [ "$NUM_7D_TRANSITIONS" -gt 0 ]; then
    printf "  ${BOLD}7-Day Weekly Limit:${NC}    %'d tokens (~%d per 1%%)\n" "$CAPACITY_7D" "$AVG_TOKENS_7D"
    echo "  (5-hour data insufficient - run with higher 5h target)"
else
    echo "  Insufficient data - no quota transitions recorded"
fi

echo ""
echo -e "${CYAN}============================================================${NC}"
echo ""
echo "Raw data saved to: $DATA_DIR/"
echo "  - raw_data.csv         (all request data)"
echo "  - transitions_5h.csv   (5-hour transitions)"
echo "  - transitions_7d.csv   (7-day transitions)"
echo ""
