# analyze-kiro-dumps

Analyze Kiro debug dump files captured by the Go service's debug dumper.

## Description

This skill analyzes request/response dumps from the Kiro debug dumper (`internal/debug/dumper.go`). It examines metadata, compares request transformations, analyzes streaming chunks, identifies error patterns, and generates comprehensive diagnostic reports.

## Usage

```
/analyze-kiro-dumps [path]
```

**Parameters:**
- `path` (optional): Path to analyze
  - If omitted: analyzes entire `kiro-debug/` directory
  - If directory: analyzes all sessions in that directory
  - If session directory: analyzes single session

**Examples:**
```bash
/analyze-kiro-dumps                                    # Analyze all dumps
/analyze-kiro-dumps kiro-debug/errors                  # Analyze only errors
/analyze-kiro-dumps kiro-debug/errors/abc123-def456    # Analyze specific session
```

## What This Skill Does

### 1. Session Discovery
- Scans for session directories in `kiro-debug/{errors,success}/`
- Groups sessions by success/failure status
- Identifies sessions with incomplete data

### 2. Metadata Analysis
- **Request Info**: session_id, request_id, account_uuid, model
- **Timing**: start_time, end_time, duration
- **Status**: success/failure, status_code, error messages
- **Error Classification**: error_type (stream_exception, rate_limit, bad_request, etc.)
- **Exception Details**: exception_payload (raw JSON from Kiro API when exception occurs)
- **Account Tracking**: tried_accounts list (for retry analysis)

### 3. Request Transformation Analysis
- **Client Request** (`request.json`): Original request from client
- **Kiro Request** (`kiro_request.json`): Transformed request to Kiro API
- **Differences**: Identifies transformations applied:
  - Prompt conversion (Claude Code metadata injection)
  - Model name mapping
  - Parameter modifications
  - Header additions

### 4. Streaming Analysis
- **Kiro Chunks** (`kiro_chunks.jsonl`): Raw Kiro API SSE events
- **Claude Chunks** (`claude_chunks.jsonl`): Converted Claude Messages API events
- **Metrics**:
  - Total chunks received
  - Chunk types distribution
  - Token usage from stream
  - Incomplete streams detection
  - Exception detection in stream

### 5. Error Pattern Detection
- **Common Errors**:
  - `stream_exception`: Kiro API returned exception during streaming
  - `rate_limit`: 429 rate limit errors
  - `bad_request`: 400 validation errors
  - `auth_error`: 401/403 authentication failures
- **Error Correlation**: Groups errors by type, model, account
- **Account Health**: Identifies problematic accounts

### 6. Report Generation
- **Summary Statistics**: Total sessions, success rate, error breakdown
- **Individual Session Reports**: Detailed analysis per session
- **Error Patterns**: Aggregated error analysis
- **Recommendations**: Actionable insights based on findings

## Files Analyzed Per Session

| File | Description |
|------|-------------|
| `metadata.json` | Session metadata (timing, status, errors, accounts) |
| `request.json` | Client request to `/v1/messages` |
| `kiro_request.json` | Transformed request sent to Kiro API |
| `kiro_chunks.jsonl` | Raw Kiro API streaming response (JSONL) |
| `claude_chunks.jsonl` | Converted Claude SSE events (JSONL) |
| `response.json` | Final response (non-streaming only) |
| `kiro_response.json` | Kiro response (non-streaming only) |

## Output Format

### Summary Report
```
=== Kiro Debug Dump Analysis ===

Directory: kiro-debug/
Sessions: 15 (10 errors, 5 success)
Time Range: 2026-01-30 10:00:00 - 2026-01-31 05:45:07

Error Breakdown:
  - stream_exception: 7 (46.7%)
  - rate_limit: 2 (13.3%)
  - bad_request: 1 (6.7%)

Top Models:
  - claude-haiku-4-5-20251001: 8 sessions
  - claude-sonnet-4-5-20250929: 7 sessions

Account Health:
  - 35387a06-f363-46f5-8f11-94e8580e0293: 5 failures
  - 7d8e9f10-a123-45b6-7c89-de0f12345678: 2 failures
```

### Session Detail Report
```
--- Session: a0a99cb8-9a42-497c-9c94-0542b3fa7998 ---
Status: FAILED
Error: received exception during streaming
Error Type: stream_exception
Exception: {
  "message": "Encountered an unexpected error when processing the request, please try again."
}
Model: claude-haiku-4-5-20251001
Account: 35387a06-f363-46f5-8f11-94e8580e0293
Duration: 11.36s
Chunks: 6 kiro, 7 claude

Request Transformation:
  - Client prompt length: 245 chars
  - Kiro conversationState added
  - Model mapping: claude-haiku-4-5-20251001 → CLAUDE_HAIKU_4_5_20251001_V1_0

Stream Analysis:
  - Started normally (message_start received)
  - Partial content: "```json\n{\"isNewTopic\": false, \""
  - Terminated after 6 chunks (incomplete)
  - No error event in stream (connection-level failure)

Recommendation:
  - Check Kiro API connection stability
  - Consider account health check
```

## Related Files

- **Implementation**: `internal/debug/dumper.go`
- **Environment**:
  - `GO_KIRO_DEBUG_DUMP=true` - Enable success dumps
  - `GO_KIRO_DEBUG_DUMP=false` - Only error dumps (default)
  - `GO_KIRO_ERROR_DUMP=false` - Disable all dumping
  - `GO_KIRO_DEBUG_DIR` - Custom dump directory (default: `/tmp/kiro-debug`)

## Instructions

When this skill is invoked:

1. **Parse Arguments**: Extract path parameter (default: `kiro-debug/`)

2. **Discover Sessions**:
   ```javascript
   const sessions = {
     errors: findSessionDirs('kiro-debug/errors/'),
     success: findSessionDirs('kiro-debug/success/')
   };
   ```

3. **Load Session Data**: For each session:
   ```javascript
   const session = {
     metadata: JSON.parse(readFile('metadata.json')),
     request: tryReadJSON('request.json'),
     kiroRequest: tryReadJSON('kiro_request.json'),
     kiroChunks: readJSONL('kiro_chunks.jsonl'),
     claudeChunks: readJSONL('claude_chunks.jsonl')
   };
   ```

4. **Analyze**:
   - Extract timing, status, errors from metadata
   - Compare request vs kiroRequest transformations
   - Count chunk types, detect incomplete streams
   - Classify errors by type
   - Track account usage and failures

5. **Generate Report**:
   - Print summary statistics
   - Group sessions by error type
   - Highlight problematic accounts/models
   - Provide session-level details
   - Suggest actions (e.g., disable account, adjust retry logic)

6. **Special Cases**:
   - **Missing files**: Report which files are missing
   - **Malformed JSON**: Show parsing errors
   - **Empty chunks**: Flag as potential connection issue
   - **Exception in stream**: Extract exception details if available

7. **Interactive Mode** (optional):
   - Ask user if they want to see specific session details
   - Offer to export filtered results
   - Suggest follow-up actions (e.g., disable account in Redis)

## Example Analysis Workflow

```bash
# User runs skill
/analyze-kiro-dumps kiro-debug/errors

# Skill outputs:
# 1. Summary: 10 error sessions found
# 2. Error breakdown: 7 stream_exception, 2 rate_limit, 1 bad_request
# 3. List sessions with stream_exception
# 4. For each session:
#    - Show metadata
#    - Compare request transformation
#    - Analyze chunks (how many, what types, where it stopped)
#    - Extract error details
# 5. Recommendations:
#    - Account X has 5 stream_exceptions → suggest health check
#    - Model Y shows pattern of timeouts → suggest parameter tuning
```

## Tips

- **Quick triage**: Focus on metadata.json first to understand error distribution
- **Chunk analysis**: Use `wc -l *.jsonl` to quickly see if stream completed
- **Account correlation**: Track which accounts have multiple failures
- **Pattern detection**: Look for common models/prompts causing issues
- **Incomplete dumps**: Missing kiro_request.json suggests early failure before API call
