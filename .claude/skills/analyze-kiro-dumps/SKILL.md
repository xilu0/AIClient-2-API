---
name: analyze-kiro-dumps
description: Use when analyzing Kiro debug dump files to diagnose streaming errors, request transformation issues, or account health problems. Invoke when user mentions "kiro dumps", "debug dumps", "stream errors", or asks about Kiro API failures.
---

# Kiro Debug Dump Analyzer

Analyzes request/response dumps from the Go Kiro service's debug dumper (`internal/debug/dumper.go`).

## Usage

```
/analyze-kiro-dumps [path]
```

- No path: analyzes entire `kiro-debug/` directory
- Directory path: analyzes all sessions in that directory
- Session path: analyzes single session

## Instructions

When this skill is invoked:

### 1. Discover Sessions

Scan the target path for session directories:
- `{path}/errors/` - Failed requests
- `{path}/success/` - Successful requests (if `GO_KIRO_DEBUG_DUMP=true`)

### 2. Read Session Files

For each session directory, read these files:

| File | Required | Content |
|------|----------|---------|
| `metadata.json` | Yes | Session ID, timing, status, error info, account UUID |
| `request.json` | No | Original client request |
| `kiro_request.json` | No | Transformed Kiro API request |
| `kiro_chunks.jsonl` | No | Raw Kiro SSE chunks (JSONL) |
| `claude_chunks.jsonl` | No | Converted Claude events (JSONL) |
| `kiro_response.json` | No | Non-streaming response |

### 3. Analyze and Report

**Summary**: Count sessions, group by error type, identify problematic accounts.

**Per-session analysis**:
- Compare `request.json` vs `kiro_request.json` (model mapping, prompt transformation)
- Count chunks, detect incomplete streams
- Extract error details from `metadata.json` and `exception_payload`

**Error types**:
- `stream_exception`: Stream terminated with exception
- `rate_limit`: 429 Too Many Requests
- `bad_request`: 400 validation error
- `auth_error`: 401/403 authentication failure

### 4. Output Format

```
=== Kiro Debug Dump Analysis ===

Directory: kiro-debug/
Sessions: 15 (10 errors, 5 success)

Error Breakdown:
  - stream_exception: 7 (70%)
  - rate_limit: 3 (30%)

Account Health:
  - account-uuid-1: 5 failures
  - account-uuid-2: 2 failures

--- Session: session-uuid ---
Status: FAILED
Error Type: stream_exception
Model: claude-haiku-4-5-20251001
Account: account-uuid
Duration: 11.36s
Chunks: 6 kiro, 7 claude
Recommendation: Check Kiro API connection stability
```

### 5. Actionable Recommendations

- High failure account → suggest disabling in Redis
- Pattern of stream_exception → check connection/timeout settings
- Missing `kiro_request.json` → early failure before API call
