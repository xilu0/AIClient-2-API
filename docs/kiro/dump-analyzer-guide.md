# Kiro Debug Dump Analyzer Guide

This guide explains how to use the Kiro debug dump analysis tools to troubleshoot issues in the Go Kiro service.

## Quick Start

```bash
# Analyze all captured dumps
npm run analyze:dumps

# Or use Claude Code skill
/analyze-kiro-dumps
```

## Overview

The Go Kiro service (`internal/debug/dumper.go`) automatically captures detailed debug information when requests fail. The analyzer helps you understand what went wrong by examining:

- Request transformations
- Streaming behavior
- Error patterns
- Account health
- Timing information

## Debug Dump System

### How It Works

1. **Automatic Capture**: When a request fails (or succeeds with full debug mode), the service captures:
   - Client request
   - Transformed Kiro API request
   - Streaming chunks (both raw and converted)
   - Metadata (timing, errors, accounts)

2. **Directory Structure**:
   ```
   kiro-debug/
   ├── errors/     # Failed requests (default)
   ├── success/    # Successful requests (optional)
   └── temp/       # Processing (temporary)
   ```

3. **Per-Session Files**:
   - `metadata.json` - Session info, timing, errors
   - `request.json` - Original client request
   - `kiro_request.json` - Request sent to Kiro API
   - `kiro_chunks.jsonl` - Raw Kiro response chunks
   - `claude_chunks.jsonl` - Converted Claude events
   - `response.json` / `kiro_response.json` - Final responses

### Configuration

**Default mode** (errors only):
```yaml
# docker-compose.yml
environment:
  - GO_KIRO_ERROR_DUMP=true          # Capture errors (default)
  - GO_KIRO_DEBUG_DUMP=false         # Don't capture success
```

**Full debug mode** (capture everything):
```yaml
environment:
  - GO_KIRO_DEBUG_DUMP=true          # Capture all requests
```

**Disable dumps**:
```yaml
environment:
  - GO_KIRO_ERROR_DUMP=false         # Disable error dumps
```

**Custom directory**:
```yaml
environment:
  - GO_KIRO_DEBUG_DIR=/custom/path   # Default: /tmp/kiro-debug
volumes:
  - ./my-dumps:/custom/path
```

## Using the Analyzer

### Basic Usage

```bash
# Analyze all dumps in kiro-debug/
npm run analyze:dumps

# Analyze only errors
npm run analyze:dumps kiro-debug/errors

# Analyze only successes
npm run analyze:dumps kiro-debug/success

# Analyze specific session
npm run analyze:dumps kiro-debug/errors/SESSION-UUID
```

### Understanding the Output

#### 1. Summary Report

```
=== Kiro Debug Dump Analysis ===

Directory: kiro-debug
Sessions: 10 (7 errors, 3 success)
Time Range: 2026-01-30 10:00:00 - 2026-01-31 05:45:07
```

Shows total sessions, error/success counts, and time range.

#### 2. Error Breakdown

```
Error Breakdown:
  - stream_exception: 5 (71.4%)
  - rate_limit: 2 (28.6%)
```

Groups errors by type to identify patterns.

#### 3. Model Usage

```
Top Models:
  - claude-haiku-4-5-20251001: 6 sessions
  - claude-sonnet-4-5-20250929: 4 sessions
```

Shows which models are being used most.

#### 4. Account Health

```
Account Health:
  - 35387a06...: 3/5 failures (60.0%)
  - 7d8e9f10...: 1/2 failures (50.0%)
```

Identifies problematic accounts with high failure rates.

#### 5. Session Details

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
```

Per-session analysis with key details.

#### 6. Request Transformation

```
Request Transformation:
  - Added conversationState wrapper
  - Model mapping: claude-haiku-4-5-20251001 → CLAUDE_HAIKU_4_5_20251001_V1_0
  - Prompt: 57 → 387 chars
```

Shows how the client request was transformed for Kiro API.

#### 7. Stream Analysis

```
Stream Analysis:
  - Started: Yes
  - Completed: No
  - Partial content: "```json\n{\"isNewTopic\": false..."
  - Tokens: {"input_tokens":4,"output_tokens":0,...}
```

Analyzes streaming behavior and partial responses.

#### 8. Recommendations

```
Recommendations:
  ⚠️  Stream terminated unexpectedly - check Kiro API connection stability
  Stream started but did not complete - possible timeout
  Account 35387a06... has 3 failures - consider health check
```

Actionable insights based on the analysis.

## Common Error Patterns

### stream_exception

**Symptom**: Stream starts but terminates with exception

**Analysis checklist**:
- [ ] How many chunks received before failure?
- [ ] Is there partial content?
- [ ] Does exception appear in kiro_chunks.jsonl?
- [ ] Is this account-specific or model-specific?

**Typical causes**:
- Kiro API backend error
- Connection timeout
- Malformed response from Kiro
- Account authentication expired mid-stream

**Resolution**:
1. Check if error is account-specific → disable/replace account
2. Check if error is model-specific → report to Kiro team
3. Check timing → may need timeout adjustment
4. Review exception details in kiro_chunks.jsonl

### rate_limit

**Symptom**: 429 Too Many Requests

**Analysis checklist**:
- [ ] Is this from a single account or multiple?
- [ ] What's the request frequency?
- [ ] Are requests distributed across accounts?

**Typical causes**:
- Account rate limit exceeded
- Insufficient account pool size
- Round-robin not working properly

**Resolution**:
1. Add more accounts to the pool
2. Verify round-robin selection in Redis
3. Check account cooldown settings
4. Review account usage patterns

### bad_request

**Symptom**: 400 Bad Request

**Analysis checklist**:
- [ ] Compare request.json and kiro_request.json
- [ ] Is the transformation correct?
- [ ] Are there tool inputs with empty values?
- [ ] Is the model ID valid?

**Typical causes**:
- Invalid tool input (empty arrays, null values)
- Malformed conversationState
- Unsupported model parameters
- Invalid prompt formatting

**Resolution**:
1. Review request transformation logic
2. Check for empty tool inputs (filter them out)
3. Validate model name mapping
4. Test with minimal request

### Incomplete Stream

**Symptom**: Stream starts but no completion event

**Analysis checklist**:
- [ ] How many chunks received?
- [ ] Is there a final message_stop event?
- [ ] What's the duration?
- [ ] Any timeout errors in logs?

**Typical causes**:
- Network timeout
- Long-running response
- Connection interrupted
- Client disconnected

**Resolution**:
1. Check timeout settings (GO_KIRO_KIRO_API_TIMEOUT)
2. Review network stability
3. Check if client is still connected
4. Consider implementing streaming keepalive

## Advanced Analysis

### Manual File Inspection

```bash
# View metadata
cat kiro-debug/errors/SESSION-ID/metadata.json | jq

# View request transformation
diff -u \
  <(cat kiro-debug/errors/SESSION-ID/request.json | jq) \
  <(cat kiro-debug/errors/SESSION-ID/kiro_request.json | jq)

# Count chunk types
cat kiro-debug/errors/SESSION-ID/claude_chunks.jsonl | \
  jq -r '.event' | sort | uniq -c

# Extract partial content
cat kiro-debug/errors/SESSION-ID/claude_chunks.jsonl | \
  jq -s 'map(select(.event == "content_block_delta")) | map(.data.delta.text) | join("")'

# Find all exceptions
grep -r "exception" kiro-debug/errors/*/kiro_chunks.jsonl
```

### Batch Analysis

```bash
# Find all stream_exception errors
for dir in kiro-debug/errors/*/; do
  if jq -e '.error_type == "stream_exception"' "$dir/metadata.json" >/dev/null 2>&1; then
    echo "$dir: $(jq -r '.account_uuid' "$dir/metadata.json")"
  fi
done

# Account failure rates
jq -s 'group_by(.account_uuid) | map({
  account: .[0].account_uuid,
  total: length,
  failures: map(select(.success == false)) | length
})' kiro-debug/errors/*/metadata.json kiro-debug/success/*/metadata.json
```

### Export for Further Analysis

```bash
# Export all metadata to CSV
echo "session_id,success,error_type,model,account,duration" > analysis.csv
for meta in kiro-debug/{errors,success}/*/metadata.json; do
  jq -r '[.session_id, .success, .error_type, .model, .account_uuid,
    (((.end_time | fromdate) - (.start_time | fromdate)) | tostring)] | @csv' "$meta" >> analysis.csv
done

# Import to spreadsheet for visualization
```

## Troubleshooting the Analyzer

### "No sessions found"

**Cause**: No dump files in kiro-debug/

**Solution**:
1. Check GO_KIRO_ERROR_DUMP is enabled
2. Verify GO_KIRO_DEBUG_DIR path
3. Trigger some errors to generate dumps
4. Check volume mount in docker-compose.yml

### "Error parsing JSON"

**Cause**: Corrupted dump file

**Solution**:
1. Check if dump was written during crash
2. Remove corrupted file
3. Review dumper error handling

### Analyzer crashes

**Cause**: Large number of dumps or memory issue

**Solution**:
1. Analyze specific subdirectory: `npm run analyze:dumps kiro-debug/errors`
2. Archive old dumps
3. Increase Node.js memory: `NODE_OPTIONS=--max-old-space-size=4096 npm run analyze:dumps`

## Best Practices

### Development

1. **Enable full debug mode** during development:
   ```yaml
   GO_KIRO_DEBUG_DUMP=true
   ```

2. **Review dumps** after each test run

3. **Clean up** between test sessions:
   ```bash
   rm -rf kiro-debug/errors/* kiro-debug/success/*
   ```

### Production

1. **Use error-only mode** (default):
   ```yaml
   GO_KIRO_ERROR_DUMP=true
   GO_KIRO_DEBUG_DUMP=false
   ```

2. **Persist dumps** for analysis:
   ```yaml
   volumes:
     - ./kiro-debug:/tmp/kiro-debug
   ```

3. **Rotate dumps** periodically:
   ```bash
   # Archive dumps older than 7 days
   find kiro-debug -type d -mtime +7 -exec tar -czf {}.tar.gz {} \; -exec rm -rf {} \;
   ```

4. **Monitor account health**:
   ```bash
   # Daily cron job
   0 8 * * * cd /path/to/project && npm run analyze:dumps > /tmp/dump-report.txt && cat /tmp/dump-report.txt | mail -s "Kiro Dump Report" admin@example.com
   ```

### Analysis Workflow

1. **Daily review**: Check for new errors
2. **Pattern detection**: Run analyzer to identify trends
3. **Account management**: Disable failing accounts
4. **Bug reporting**: Share dumps with Kiro team
5. **Cleanup**: Archive old dumps

## Integration with Monitoring

### Alerting

```bash
# Alert on high error rate
#!/bin/bash
ERROR_COUNT=$(npm run analyze:dumps --silent | grep "Error Breakdown" -A 10 | grep -oP '\d+(?= \()' | head -1)
if [ "$ERROR_COUNT" -gt 10 ]; then
  echo "High error rate: $ERROR_COUNT errors" | mail -s "Kiro Alert" admin@example.com
fi
```

### Metrics Export

```javascript
// Export metrics to Prometheus/Grafana
const analysis = require('./analyze-kiro-dumps.js');
// ... parse output and expose metrics endpoint
```

## Related Documentation

- **Implementation**: `internal/debug/dumper.go`
- **Skill Definition**: `.claude/skills/analyze-kiro-dumps.skill.md`
- **Analyzer Script**: `.claude/skills/analyze-kiro-dumps.js`
- **Skills README**: `.claude/skills/README.md`

## Support

If you encounter issues with the analyzer:

1. Check logs: `npm run analyze:dumps 2>&1 | tee analyzer.log`
2. Verify dump files: `ls -la kiro-debug/errors/SESSION-ID/`
3. Test with single session: `npm run analyze:dumps kiro-debug/errors/SESSION-ID`
4. Report bugs with sample dump files (redact sensitive data)
