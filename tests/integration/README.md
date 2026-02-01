# Kiro Integration Tests

This directory contains integration tests for the Go Kiro service.

## Prerequisites

- Redis running (default: `redis://localhost:6379`)
- Valid Kiro accounts configured in Redis

## Tests

### TestKiroRequestFromFile

Loads a Kiro request JSON file and sends it directly to the Kiro API with SSE event parsing.

```bash
# Basic usage
KIRO_REQUEST_FILE=/path/to/kiro_request.json \
  go test ./tests/integration/... -v -run TestKiroRequestFromFile -timeout 120s

# With debug dump file
# 成功的
KIRO_REQUEST_FILE=/root/src/AIClient-2-API/kiro-debug/success/63dd38cb-81a0-4bad-a7fa-b3e93c76f054/kiro_request.json \
go test ./tests/integration/... -v -run TestKiroRequestFromFile -timeout 120s

# 失败
KIRO_REQUEST_FILE=/root/src/AIClient-2-API/kiro-debug-31/nodejs/d3ef718c-8678-4493-9fbe-605cb578669d/kiro_request.json \
go test ./tests/integration/... -v -run TestKiroRequestFromFile -timeout 120s

KIRO_REQUEST_FILE=/root/src/AIClient-2-API/kiro-debug/errors/1b04cb5a-9fdc-4f9c-8a24-8fcb59a11a02/kiro_request.json \
  go test ./tests/integration/... -v -run TestKiroRequestFromFile -timeout 120s

# With custom Redis URL
REDIS_URL=redis://localhost:6379 \
KIRO_REQUEST_FILE=/path/to/request.json \
  go test ./tests/integration/... -v -run TestKiroRequestFromFile
```

**Features:**
- Parses and displays SSE events with proper formatting
- Logs request summary (model ID, message count, tool descriptions)
- 5-minute timeout for large requests
- 1MB buffer for large SSE events

### TestKiroRawRequest

Simpler version that reads and sends raw data, prints full response.

```bash
INTEGRATION_TEST=true \
KIRO_REQUEST_FILE=/path/to/kiro_request.json \
  go test ./tests/integration/... -v -run TestKiroRawRequest -timeout 60s
```

### TestKiroModelNames

Tests which model names are accepted by Kiro API.

```bash
INTEGRATION_TEST=true \
  go test ./tests/integration/... -v -run TestKiroModelNames -timeout 120s
```

### TestKiroSingleModel

Tests a single model name.

```bash
INTEGRATION_TEST=true \
TEST_MODEL="claude-sonnet-4.5" \
  go test ./tests/integration/... -v -run TestKiroSingleModel -timeout 60s
```

### TestReplayAllErrors

Replays all error dumps in a directory through the Go service.

```bash
KIRO_ERRORS_DIR=/root/src/AIClient-2-API/kiro-debug/errors \
  go test ./tests/integration/... -v -run TestReplayAllErrors -timeout 600s
```

### TestReplaySpecificError

Replays a specific error by ID.

```bash
KIRO_ERROR_ID=1b04cb5a-9fdc-4f9c-8a24-8fcb59a11a02 \
KIRO_DEBUG_DIR=/root/src/AIClient-2-API/kiro-debug \
  go test ./tests/integration/... -v -run TestReplaySpecificError -timeout 60s
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_URL` | `redis://localhost:6379` | Redis connection URL |
| `KIRO_REQUEST_FILE` | - | Path to Kiro request JSON file |
| `INTEGRATION_TEST` | - | Set to `true` to enable some tests |
| `TEST_MODEL` | `CLAUDE_SONNET_4_5_20250929_V1_0` | Model name for single model test |
| `KIRO_ERRORS_DIR` | - | Directory containing error dumps |
| `KIRO_ERROR_ID` | - | Specific error ID to replay |
| `KIRO_DEBUG_DIR` | `/tmp/kiro-debug` | Base directory for debug dumps |
