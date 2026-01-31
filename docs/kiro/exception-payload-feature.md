# Exception Payload Capture Feature

## Overview

This feature enhances the Kiro debug dumper to capture and display the full exception payload from the Kiro API, making error diagnosis much easier.

## Problem

**Before this change:**
- When Kiro API returned an exception, only a generic error message was saved: `"received exception during streaming"`
- The actual exception details (like `{"message": "Encountered an unexpected error..."}`) were only in `kiro_chunks.jsonl`
- Developers had to open JSONL files and search for the exception to understand the error
- No way to quickly see what the actual error was from metadata alone

**Example error log:**
```json
{"time":"2026-01-31T05:45:07Z","level":"ERROR","msg":"received exception","payload":"{\"message\":\"Encountered an unexpected error when processing the request, please try again.\"}"}
```

## Solution

**After this change:**
- Exception payload is now captured in `metadata.json` as `exception_payload` field
- Analyzer displays the exception details prominently in the session report
- Error messages are immediately visible without opening JSONL files
- Easier to aggregate and analyze common exception patterns

## Implementation

### 1. Dumper Changes (`internal/debug/dumper.go`)

Added `exception_payload` field to metadata:

```go
type Metadata struct {
    // ... existing fields ...
    ExceptionPayload string `json:"exception_payload,omitempty"` // Raw exception JSON from Kiro API
    // ... existing fields ...
}

// SetExceptionPayload sets the raw exception payload from Kiro API.
func (s *Session) SetExceptionPayload(payload []byte) {
    if s == nil || len(payload) == 0 {
        return
    }
    s.mu.Lock()
    defer s.mu.Unlock()
    s.metadata.ExceptionPayload = string(payload)
}
```

### 2. Handler Changes (`internal/handler/messages.go`)

When exception is detected, save it to metadata:

```go
if msg.IsException() {
    h.logger.Error("received exception", "payload", string(msg.Payload))
    exceptionReceived = true
    if debugSession != nil {
        debugSession.AppendKiroChunk(msg.Payload)
        debugSession.SetExceptionPayload(msg.Payload) // ← NEW
    }
}
```

### 3. Analyzer Changes (`.claude/skills/analyze-kiro-dumps.js`)

Display formatted exception in session details:

```javascript
if (analysis.exceptionPayload) {
  try {
    const exception = JSON.parse(analysis.exceptionPayload);
    console.log(`Exception: ${JSON.stringify(exception, null, 2)}`);
  } catch {
    console.log(`Exception: ${analysis.exceptionPayload}`);
  }
}
```

## Usage

### Automatic Capture

New exception dumps will automatically include the payload:

**metadata.json:**
```json
{
  "session_id": "abc123-def456",
  "error": "received exception during streaming",
  "error_type": "stream_exception",
  "exception_payload": "{\"message\":\"Encountered an unexpected error when processing the request, please try again.\"}",
  "success": false
}
```

### Analyzer Output

Run the analyzer to see exception details:

```bash
npm run analyze:dumps
```

**Output:**
```
--- Session: abc123-def456 ---
Status: FAILED
Error: received exception during streaming
Error Type: stream_exception
Exception: {
  "message": "Encountered an unexpected error when processing the request, please try again."
}
Model: claude-haiku-4-5-20251001
Account: 35387a06...
Duration: 11.36s
```

## Benefits

### 1. Faster Debugging
- Exception message immediately visible in analyzer output
- No need to dig through JSONL files
- Quick understanding of what went wrong

### 2. Pattern Detection
- Easy to aggregate exception messages
- Identify common error patterns
- Track specific error types across sessions

### 3. Better Error Reporting
- Include exact error message in bug reports
- Share specific error details with Kiro team
- Correlate exceptions with specific accounts/models

## Example Error Patterns

With this feature, you can now easily identify patterns like:

### Rate Limit Errors
```json
{"message": "Rate limit exceeded for this account"}
```

### Authentication Errors
```json
{"message": "Invalid or expired authentication token"}
```

### Request Errors
```json
{"message": "Invalid request parameters: max_tokens must be positive"}
```

### Backend Errors
```json
{"message": "Encountered an unexpected error when processing the request, please try again."}
```

## Backward Compatibility

- **Old dumps** (without `exception_payload`): Continue to work, field is optional
- **Analyzer**: Gracefully handles missing field
- **No breaking changes**: All existing functionality preserved

## Testing

To test this feature:

1. **Build Go service:**
   ```bash
   make build
   ```

2. **Trigger an error** (e.g., send invalid request)

3. **Check dump:**
   ```bash
   cat kiro-debug/errors/SESSION-ID/metadata.json | jq .exception_payload
   ```

4. **Run analyzer:**
   ```bash
   npm run analyze:dumps
   ```

## Related Files

- **Dumper**: `internal/debug/dumper.go`
- **Handler**: `internal/handler/messages.go`
- **Analyzer**: `.claude/skills/analyze-kiro-dumps.js`
- **Skill**: `.claude/skills/analyze-kiro-dumps.skill.md`
- **Changelog**: `docs/kiro/CHANGELOG-dump-analyzer.md`

## Future Enhancements

Potential improvements:

1. **Exception Classification**: Parse common exception types automatically
2. **Error Aggregation**: Group sessions by exception message
3. **Alert Integration**: Trigger alerts on specific exception patterns
4. **Exception Statistics**: Track exception frequency over time

## Contributing

When adding new error handling:

1. Use `debugSession.SetExceptionPayload(payload)` when capturing errors
2. Ensure payload is valid JSON before saving
3. Update analyzer to handle new exception formats
4. Add tests for new exception types
