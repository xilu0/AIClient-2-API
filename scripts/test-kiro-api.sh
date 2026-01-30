#!/bin/bash
# Test script for Kiro API (Go service on port 8081)
# Usage: ./scripts/test-kiro-api.sh [model] [message]

set -e

# Configuration
HOST="${KIRO_HOST:-localhost}"
PORT="${KIRO_PORT:-8081}"
API_KEY="${KIRO_API_KEY:-AI_club2026}"
MODEL="${1:-claude-sonnet-4-5}"
MESSAGE="${2:-Hello, please respond with a short greeting.}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== Kiro API Test ===${NC}"
echo "Host: $HOST:$PORT"
echo "Model: $MODEL"
echo "Message: $MESSAGE"
echo ""

# Build request body
REQUEST_BODY=$(cat <<EOF
{
  "model": "$MODEL",
  "max_tokens": 1024,
  "stream": true,
  "messages": [
    {
      "role": "user",
      "content": "$MESSAGE"
    }
  ]
}
EOF
)

echo -e "${YELLOW}Request:${NC}"
echo "$REQUEST_BODY" | jq .
echo ""

echo -e "${YELLOW}Response (streaming):${NC}"
echo "---"

# Send request and capture response
HTTP_CODE=$(curl -s -w "\n%{http_code}" \
  -X POST "http://$HOST:$PORT/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d "$REQUEST_BODY" \
  2>&1)

# Extract HTTP code (last line) and body (everything else)
BODY=$(echo "$HTTP_CODE" | sed '$d')
CODE=$(echo "$HTTP_CODE" | tail -1)

echo "$BODY"
echo "---"
echo ""

# Check result
if [ "$CODE" -eq 200 ]; then
  echo -e "${GREEN}Success! HTTP $CODE${NC}"
else
  echo -e "${RED}Failed! HTTP $CODE${NC}"
  exit 1
fi
