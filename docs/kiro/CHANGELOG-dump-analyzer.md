# Dump Analyzer Changelog

## 2026-01-31 - Exception Payload Capture

### Added
- **Exception Payload in Metadata**: Now captures the full exception JSON payload from Kiro API in `metadata.json`
  - New field: `exception_payload` (string) - Raw exception JSON from Kiro API
  - Example: `{"message":"Encountered an unexpected error when processing the request, please try again."}`

### Changed
- **Dumper** (`internal/debug/dumper.go`):
  - Added `ExceptionPayload` field to `Metadata` struct
  - Added `SetExceptionPayload()` method to capture exception details

- **Handler** (`internal/handler/messages.go`):
  - Calls `SetExceptionPayload()` when exception is detected in stream
  - Saves both to `kiro_chunks.jsonl` AND `metadata.json`

- **Analyzer** (`.claude/skills/analyze-kiro-dumps.js`):
  - Displays formatted exception payload in session details
  - Parses and pretty-prints JSON exception messages

### Benefits
- **Better Error Visibility**: Exception details now visible in summary without opening JSONL files
- **Easier Debugging**: Specific error messages immediately available in metadata
- **Pattern Detection**: Can aggregate exception messages to identify common errors

### Example Output

**Before:**
```
--- Session: abc123 ---
Status: FAILED
Error: received exception during streaming
Error Type: stream_exception
```

**After:**
```
--- Session: abc123 ---
Status: FAILED
Error: received exception during streaming
Error Type: stream_exception
Exception: {
  "message": "Encountered an unexpected error when processing the request, please try again."
}
```

### Migration Notes
- Existing dumps without `exception_payload` will continue to work
- Analyzer handles missing field gracefully
- Rebuild Go service to get this feature: `make build`

---

## 2026-01-31 - Initial Release

### Added
- **Debug Dumper** (`internal/debug/dumper.go`):
  - Automatic capture of failed requests to `kiro-debug/errors/`
  - Optional capture of successful requests to `kiro-debug/success/`
  - Session-based directory structure with metadata and artifacts
  - Environment variables: `GO_KIRO_DEBUG_DUMP`, `GO_KIRO_ERROR_DUMP`, `GO_KIRO_DEBUG_DIR`

- **Analyzer Skill** (`.claude/skills/analyze-kiro-dumps.skill.md`):
  - Claude Code skill for analyzing dumps
  - Invocation: `/analyze-kiro-dumps [path]`

- **Analyzer Script** (`.claude/skills/analyze-kiro-dumps.js`):
  - Node.js analysis tool
  - Session discovery and metadata parsing
  - Request transformation analysis
  - Streaming chunk analysis
  - Error pattern detection
  - Account health tracking
  - Comprehensive reporting

- **NPM Command**:
  - `npm run analyze:dumps [path]` - Quick analysis command

- **Documentation**:
  - `.claude/skills/README.md` - Skills overview and dump system guide
  - `docs/kiro/dump-analyzer-guide.md` - Comprehensive usage guide

### Features
- **Automatic Error Capture**: Failures automatically dumped to disk
- **Multi-File Artifacts**: Request, response, chunks, metadata
- **Session Isolation**: Each request in separate directory
- **JSONL Streaming**: Chunk-by-chunk capture in JSON Lines format
- **Metadata Tracking**: Timing, errors, accounts, retry attempts
- **Smart Analysis**: Pattern detection, recommendations, health tracking
