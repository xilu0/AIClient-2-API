# Claude Code Skills for AIClient-2-API

This directory contains custom Claude Code skills for analyzing and debugging the AIClient-2-API service.

## Available Skills

### analyze-kiro-dumps

Analyzes debug dumps from the Go Kiro service's debug dumper.

**Quick Start:**

```bash
# Analyze all dumps
npm run analyze:dumps

# Analyze only errors
npm run analyze:dumps kiro-debug/errors

# Analyze specific session
npm run analyze:dumps kiro-debug/errors/SESSION_ID

# Or use the skill directly in Claude Code
/analyze-kiro-dumps
/analyze-kiro-dumps kiro-debug/errors
```

**What it analyzes:**

- **Metadata**: Session ID, timing, status codes, error types
- **Request transformations**: Client request → Kiro API request
- **Streaming**: Chunk analysis, completion detection, partial content
- **Errors**: Classification, correlation, account health
- **Recommendations**: Actionable insights based on patterns

**Output includes:**

1. **Summary Report**: Overall statistics, error breakdown, model usage, account health
2. **Session Details**: Per-session analysis with transformations and recommendations
3. **Error Patterns**: Aggregated error analysis

**Example output:**

```
=== Kiro Debug Dump Analysis ===

Directory: kiro-debug
Sessions: 10 (7 errors, 3 success)
Time Range: 2026-01-30 10:00:00 - 2026-01-31 05:45:07

Error Breakdown:
  - stream_exception: 5 (71.4%)
  - rate_limit: 2 (28.6%)

Top Models:
  - claude-haiku-4-5-20251001: 6 sessions
  - claude-sonnet-4-5-20250929: 4 sessions

Account Health:
  - 35387a06...: 3/5 failures (60.0%)

=== Error Session Details ===

--- Session: a0a99cb8-9a42-497c-9c94-0542b3fa7998 ---
Status: FAILED
Error: received exception during streaming
Error Type: stream_exception
Model: claude-haiku-4-5-20251001
Account: 35387a06-f363-46f5-8f11-94e8580e0293
Duration: 11.36s
Chunks: 6 kiro, 7 claude

Request Transformation:
  - Added conversationState wrapper
  - Model mapping: claude-haiku-4-5-20251001 → CLAUDE_HAIKU_4_5_20251001_V1_0
  - Prompt: 57 → 387 chars

Stream Analysis:
  - Started: Yes
  - Completed: No
  - Partial content: "```json..."
  - Tokens: {"input_tokens":4,"output_tokens":0,...}

Recommendations:
  ⚠️  Stream terminated unexpectedly - check Kiro API connection stability
  Stream started but did not complete - possible timeout
```

## Debug Dump System

The Go Kiro service includes a comprehensive debug dumper that captures request/response data for troubleshooting.

### Configuration

**Environment Variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `GO_KIRO_DEBUG_DUMP` | `false` | Enable full debug mode (save all requests) |
| `GO_KIRO_ERROR_DUMP` | `true` | Save error requests (default: enabled) |
| `GO_KIRO_DEBUG_DIR` | `/tmp/kiro-debug` | Base directory for dumps |

**Docker Compose:**

```yaml
services:
  aiclient-go:
    environment:
      - GO_KIRO_DEBUG_DUMP=false    # Only errors (recommended)
      - GO_KIRO_ERROR_DUMP=true     # Enable error dumps
      - GO_KIRO_DEBUG_DIR=/app/kiro-debug
    volumes:
      - ./kiro-debug:/app/kiro-debug  # Persist dumps
```

### Directory Structure

```
kiro-debug/
├── errors/           # Failed requests (always saved when enabled)
│   └── {session-id}/
│       ├── metadata.json        # Session metadata
│       ├── request.json         # Client request
│       ├── kiro_request.json    # Kiro API request
│       ├── kiro_chunks.jsonl    # Kiro response chunks
│       ├── claude_chunks.jsonl  # Converted Claude events
│       └── response.json        # Final response (non-streaming)
├── success/          # Successful requests (only when GO_KIRO_DEBUG_DUMP=true)
│   └── {session-id}/
│       └── ... (same files as errors/)
└── temp/             # Processing (moved to errors/success on completion)
```

### File Formats

**metadata.json** - Session metadata:
```json
{
  "session_id": "uuid",
  "request_id": "msg_...",
  "account_uuid": "uuid",
  "model": "claude-haiku-4-5-20251001",
  "start_time": "2026-01-31T05:44:55.949546405Z",
  "end_time": "2026-01-31T05:45:07.311420677Z",
  "status_code": 200,
  "error": "received exception during streaming",
  "error_type": "stream_exception",
  "exception_payload": "{\"message\":\"Encountered an unexpected error...\"}",
  "tried_accounts": ["uuid1", "uuid2"],
  "success": false
}
```

**kiro_chunks.jsonl** - Raw Kiro API chunks (JSONL):
```jsonl
{"content":"text1"}
{"content":"text2"}
{"exception":"error message"}
```

**claude_chunks.jsonl** - Converted Claude SSE events:
```jsonl
{"event":"message_start","data":{"type":"message_start","message":{...}}}
{"event":"content_block_delta","data":{"type":"content_block_delta",...}}
{"event":"message_stop","data":{"type":"message_stop"}}
```

### Common Error Types

| Error Type | Description | Typical Cause |
|------------|-------------|---------------|
| `stream_exception` | Stream terminated with exception | Kiro API error, timeout, connection issue |
| `rate_limit` | 429 Too Many Requests | Account rate limit exceeded |
| `bad_request` | 400 Bad Request | Invalid request parameters |
| `auth_error` | 401/403 Unauthorized | Account authentication failure |
| `timeout` | Request timeout | Network issue or long-running request |

### Analysis Workflow

1. **Trigger dump**: Errors are automatically captured when `GO_KIRO_ERROR_DUMP=true`
2. **Review dumps**: Check `kiro-debug/errors/` directory
3. **Run analyzer**: `npm run analyze:dumps`
4. **Identify patterns**: Look for recurring error types, problematic accounts
5. **Take action**:
   - Disable failing accounts in Redis
   - Adjust retry logic
   - Report Kiro API issues
   - Fix request transformation bugs

### Troubleshooting Tips

**No dumps generated:**
- Check `GO_KIRO_ERROR_DUMP` is not `false`
- Verify `GO_KIRO_DEBUG_DIR` is writable
- Check service logs for dumper initialization

**Incomplete dumps (missing files):**
- `metadata.json` only: Failure during session setup
- Missing `kiro_request.json`: Error before API call (likely validation)
- Missing `kiro_chunks.jsonl`: No response received from API

**Large dump directories:**
- Use `GO_KIRO_DEBUG_DUMP=false` (error-only mode)
- Rotate old dumps periodically
- Compress/archive historical dumps

### Implementation Reference

- **Go**: `internal/debug/dumper.go`
- **Analyzer**: `.claude/skills/analyze-kiro-dumps/analyze-kiro-dumps.js`
- **Skill**: `.claude/skills/analyze-kiro-dumps/SKILL.md`

## Adding New Skills

To add a new skill:

1. Create `.claude/skills/your-skill/SKILL.md` (skill entry point with frontmatter)
2. Optionally add supporting files in the same directory (scripts, templates, etc.)
3. Add to `package.json` scripts for CLI access
4. Document in this README

**Directory structure:**

```
.claude/skills/your-skill/
├── SKILL.md              # Required - skill entry point
├── helper.js             # Optional - supporting scripts
└── reference.md          # Optional - detailed reference
```

**SKILL.md template:**

```markdown
---
name: your-skill
description: Brief description of when to use this skill and what triggers it.
---

# Your Skill Name

Description of what this skill does.

## Usage

/your-skill [args]

## Instructions

When this skill is invoked:
1. Step 1
2. Step 2
...
```
