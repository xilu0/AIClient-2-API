#!/bin/bash

# Stress Test Script for AIClient-2-API
# Usage: ./stress-test.sh [options]

set -e

# Default configuration
HOST="${HOST:-localhost}"
PORT="${PORT:-3000}"
ENDPOINT="${ENDPOINT:-/claude-kiro-oauth/v1/messages}"
API_KEY="${API_KEY:-AI_club2026}"
MODEL="${MODEL:-claude-sonnet-4-5-20250929}"
CONCURRENCY="${CONCURRENCY:-10}"
TOTAL_REQUESTS="${TOTAL_REQUESTS:-100}"
MAX_TOKENS="${MAX_TOKENS:-100}"
PROMPT="${PROMPT:-Hello! Say hi briefly.}"
TIMEOUT="${TIMEOUT:-30}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

usage() {
    cat << EOF
Usage: $(basename "$0") [OPTIONS]

Stress test script for AIClient-2-API endpoints.

OPTIONS:
    -h, --host HOST           Target host (default: localhost)
    -p, --port PORT           Target port (default: 3000)
    -e, --endpoint ENDPOINT   API endpoint (default: /claude-kiro-oauth/v1/messages)
    -k, --api-key KEY         API key (default: AI_club2026)
    -m, --model MODEL         Model name (default: claude-sonnet-4-5-20250929)
    -c, --concurrency NUM     Concurrent requests (default: 10)
    -n, --requests NUM        Total requests (default: 100)
    -t, --max-tokens NUM      Max tokens in response (default: 100)
    -P, --prompt TEXT         Prompt text (default: "Hello! Say hi briefly.")
    -T, --timeout SEC         Request timeout in seconds (default: 30)
    --help                    Show this help message

EXAMPLES:
    # Basic test with defaults
    ./stress-test.sh

    # High concurrency test
    ./stress-test.sh -c 50 -n 500

    # Test with custom endpoint
    ./stress-test.sh -e /v1/chat/completions -m gpt-4

    # Light test
    ./stress-test.sh -c 5 -n 20

PRESETS (use environment variables):
    # Light load
    CONCURRENCY=5 TOTAL_REQUESTS=20 ./stress-test.sh

    # Medium load
    CONCURRENCY=20 TOTAL_REQUESTS=200 ./stress-test.sh

    # Heavy load
    CONCURRENCY=50 TOTAL_REQUESTS=1000 ./stress-test.sh

EOF
    exit 0
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--host)
            HOST="$2"
            shift 2
            ;;
        -p|--port)
            PORT="$2"
            shift 2
            ;;
        -e|--endpoint)
            ENDPOINT="$2"
            shift 2
            ;;
        -k|--api-key)
            API_KEY="$2"
            shift 2
            ;;
        -m|--model)
            MODEL="$2"
            shift 2
            ;;
        -c|--concurrency)
            CONCURRENCY="$2"
            shift 2
            ;;
        -n|--requests)
            TOTAL_REQUESTS="$2"
            shift 2
            ;;
        -t|--max-tokens)
            MAX_TOKENS="$2"
            shift 2
            ;;
        -P|--prompt)
            PROMPT="$2"
            shift 2
            ;;
        -T|--timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        --help)
            usage
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            usage
            ;;
    esac
done

# Construct URL
URL="http://${HOST}:${PORT}${ENDPOINT}"

# Create temp directory for results
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

# Request body
REQUEST_BODY=$(cat << EOF
{
  "model": "${MODEL}",
  "max_tokens": ${MAX_TOKENS},
  "messages": [{"role": "user", "content": "${PROMPT}"}]
}
EOF
)

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}   AIClient-2-API Stress Test${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "${YELLOW}Configuration:${NC}"
echo "  URL:          $URL"
echo "  Model:        $MODEL"
echo "  Concurrency:  $CONCURRENCY"
echo "  Total Reqs:   $TOTAL_REQUESTS"
echo "  Max Tokens:   $MAX_TOKENS"
echo "  Timeout:      ${TIMEOUT}s"
echo ""

# Check if endpoint is reachable
echo -e "${YELLOW}Checking endpoint...${NC}"
if ! curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "$URL" > /dev/null 2>&1; then
    echo -e "${RED}Warning: Endpoint may not be reachable. Continuing anyway...${NC}"
fi

# Function to make a single request and record metrics
make_request() {
    local id=$1
    local output_file="$TEMP_DIR/result_$id.txt"

    local start_time=$(date +%s.%N)

    local http_code
    http_code=$(curl -s -o "$TEMP_DIR/response_$id.txt" -w "%{http_code}" \
        --max-time "$TIMEOUT" \
        -X POST "$URL" \
        -H "Content-Type: application/json" \
        -H "X-API-Key: $API_KEY" \
        -d "$REQUEST_BODY" 2>/dev/null || echo "000")

    local end_time=$(date +%s.%N)
    local duration=$(echo "$end_time - $start_time" | bc)

    echo "$id,$http_code,$duration" >> "$output_file"
}

export -f make_request
export TEMP_DIR URL API_KEY REQUEST_BODY TIMEOUT

echo -e "${YELLOW}Starting stress test...${NC}"
echo ""

START_TIME=$(date +%s.%N)

# Run requests with parallel
seq 1 "$TOTAL_REQUESTS" | xargs -P "$CONCURRENCY" -I {} bash -c 'make_request {}'

END_TIME=$(date +%s.%N)
TOTAL_TIME=$(echo "$END_TIME - $START_TIME" | bc)

# Aggregate results
echo -e "${YELLOW}Analyzing results...${NC}"
echo ""

# Combine all result files
cat "$TEMP_DIR"/result_*.txt > "$TEMP_DIR/all_results.txt" 2>/dev/null || true

if [[ ! -s "$TEMP_DIR/all_results.txt" ]]; then
    echo -e "${RED}No results collected. Test may have failed.${NC}"
    exit 1
fi

# Calculate statistics
TOTAL_COMPLETED=$(wc -l < "$TEMP_DIR/all_results.txt")
SUCCESS_COUNT=$(grep -c ",2[0-9][0-9]," "$TEMP_DIR/all_results.txt" || echo 0)
FAILED_COUNT=$((TOTAL_COMPLETED - SUCCESS_COUNT))

# Extract durations for successful requests
grep ",2[0-9][0-9]," "$TEMP_DIR/all_results.txt" | cut -d',' -f3 > "$TEMP_DIR/durations.txt" 2>/dev/null || true

if [[ -s "$TEMP_DIR/durations.txt" ]]; then
    # Calculate min, max, avg, p50, p95, p99
    SORTED_DURATIONS=$(sort -n "$TEMP_DIR/durations.txt")

    MIN_DURATION=$(echo "$SORTED_DURATIONS" | head -1)
    MAX_DURATION=$(echo "$SORTED_DURATIONS" | tail -1)
    AVG_DURATION=$(echo "$SORTED_DURATIONS" | awk '{sum+=$1} END {printf "%.3f", sum/NR}')

    DURATION_COUNT=$(echo "$SORTED_DURATIONS" | wc -l)
    P50_IDX=$(echo "$DURATION_COUNT * 0.50" | bc | cut -d. -f1)
    P95_IDX=$(echo "$DURATION_COUNT * 0.95" | bc | cut -d. -f1)
    P99_IDX=$(echo "$DURATION_COUNT * 0.99" | bc | cut -d. -f1)

    P50_IDX=$((P50_IDX > 0 ? P50_IDX : 1))
    P95_IDX=$((P95_IDX > 0 ? P95_IDX : 1))
    P99_IDX=$((P99_IDX > 0 ? P99_IDX : 1))

    P50=$(echo "$SORTED_DURATIONS" | sed -n "${P50_IDX}p")
    P95=$(echo "$SORTED_DURATIONS" | sed -n "${P95_IDX}p")
    P99=$(echo "$SORTED_DURATIONS" | sed -n "${P99_IDX}p")
else
    MIN_DURATION="N/A"
    MAX_DURATION="N/A"
    AVG_DURATION="N/A"
    P50="N/A"
    P95="N/A"
    P99="N/A"
fi

# Calculate RPS
RPS=$(echo "scale=2; $TOTAL_COMPLETED / $TOTAL_TIME" | bc)

# Count status codes
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}   Results${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "${GREEN}Summary:${NC}"
echo "  Total Time:       ${TOTAL_TIME}s"
echo "  Requests/sec:     $RPS"
echo "  Total Requests:   $TOTAL_COMPLETED"
echo "  Successful:       $SUCCESS_COUNT ($(echo "scale=1; $SUCCESS_COUNT * 100 / $TOTAL_COMPLETED" | bc)%)"
echo "  Failed:           $FAILED_COUNT"
echo ""
echo -e "${GREEN}Latency (successful requests):${NC}"
echo "  Min:              ${MIN_DURATION}s"
echo "  Max:              ${MAX_DURATION}s"
echo "  Average:          ${AVG_DURATION}s"
echo "  P50:              ${P50}s"
echo "  P95:              ${P95}s"
echo "  P99:              ${P99}s"
echo ""

# Status code breakdown
echo -e "${GREEN}Status Code Distribution:${NC}"
cut -d',' -f2 "$TEMP_DIR/all_results.txt" | sort | uniq -c | sort -rn | while read count code; do
    case $code in
        2*)
            echo -e "  ${GREEN}$code${NC}: $count"
            ;;
        4*)
            echo -e "  ${YELLOW}$code${NC}: $count"
            ;;
        5*|000)
            echo -e "  ${RED}$code${NC}: $count"
            ;;
        *)
            echo "  $code: $count"
            ;;
    esac
done

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}   Test Complete${NC}"
echo -e "${BLUE}========================================${NC}"
