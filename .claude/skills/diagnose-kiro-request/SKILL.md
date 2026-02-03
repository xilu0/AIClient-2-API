---
name: diagnose-kiro-request
description: Use when diagnosing "Improperly formed request" 400 errors from Kiro API. Invoke when user mentions "diagnose kiro", "kiro 400 error", "improperly formed request", or needs to find which part of a kiro_request.json causes API failures.
---

# Kiro Request Diagnostic Tool

Performs elimination-based diagnosis on failing Kiro API requests to pinpoint the exact component causing "Improperly formed request" errors.

**CRITICAL**: Analysis alone is not enough. Every hypothesis MUST be validated by sending a modified request to the actual Kiro API.

## Usage

```
/diagnose-kiro-request <path-to-kiro_request.json>
```

## Core Principle: Hypothesis → Verification

**DO NOT** assume your analysis is correct. Follow this workflow:

```
1. Static Analysis → Form Hypothesis
2. Create Modified Request → Apply Hypothesized Fix
3. Send to Kiro API → Verify PASS
4. Only then → Hypothesis Confirmed
```

## Instructions

### Phase 1: Static Analysis

#### 1.1 Examine the Error Sample

```bash
# Check metadata
cat <session_dir>/metadata.json

# Analyze request structure
python3 << 'PYEOF'
import json
with open('<session_dir>/kiro_request.json') as f:
    data = json.load(f)
cs = data.get('conversationState', {})
history = cs.get('history', [])
# ... analyze structure
PYEOF
```

#### 1.2 Check Known Problem Patterns

| Pattern | How to Detect |
|---------|---------------|
| Empty input | `input: {}` for tools with required params |
| Fragmented toolUse | Multiple entries with same `toolUseId`, `input.raw_arguments` |
| Orphan toolResult | `toolUseId` has no matching toolUse in history |
| Large content | Content > 50KB |
| Invalid characters | Control characters, invalid Unicode |

#### 1.3 Form Hypothesis

Document your hypothesis clearly:

```markdown
## Hypothesis

**Problem**: history[3] contains a Read toolUse with empty input
**Expected Fix**: Remove history[3] and history[4] (the corresponding toolResult)
**Rationale**: Read tool requires file_path parameter
```

### Phase 2: Verify Hypothesis (REQUIRED)

**You MUST verify your hypothesis before considering it confirmed.**

#### 2.1 Create Modified Request

```python
import json

with open('<session_dir>/kiro_request.json') as f:
    data = json.load(f)

# Apply your hypothesized fix
# Example: Remove problematic history entries
history = data['conversationState']['history']
fixed_history = history[:3] + history[5:]  # Remove indices 3 and 4
data['conversationState']['history'] = fixed_history

with open('/tmp/fixed_kiro_request.json', 'w') as f:
    json.dump(data, f)
```

#### 2.2 Send to Kiro API

Use the integration test to send the modified request:

```bash
KIRO_REQUEST_FILE=/tmp/fixed_kiro_request.json \
go test ./tests/integration/... -v -run TestKiroRequestDiagnose -timeout 60s
```

**Expected Result**:
- `[original] ✓ SUCCESS` → Hypothesis verified!
- `[original] ✗ FAILED` → Hypothesis wrong, iterate

#### 2.3 Iterate if Needed

If verification fails:
1. Re-examine the error
2. Form new hypothesis
3. Repeat verification

### Phase 3: Implement Fix

Once hypothesis is verified:

1. **Implement code fix** in the Go service
2. **Add unit test** covering the pattern
3. **Commit changes**

### Phase 4: Post-Deployment Verification (REQUIRED)

After deployment, verify the original sample now passes:

#### 4.1 Integration Test with Original Sample

```bash
# Test that the original error sample now passes through the service
KIRO_REQUEST_FILE=<original_sample_path> \
go test ./tests/integration/... -v -run TestKiroRequestDiagnose -timeout 60s
```

#### 4.2 End-to-End Test (if available)

Send the original Claude request through the full service:

```bash
# If request.json exists in the error session
curl -X POST http://localhost:8081/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: <API_KEY>" \
  -d @<session_dir>/request.json
```

#### 4.3 Success Criteria

- [ ] Original kiro_request.json passes Kiro API
- [ ] Unit tests pass
- [ ] No regression in other tests

## Diagnostic Flow

```
┌─────────────────────────────────────────────────────────────┐
│                    Diagnosis Workflow                        │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  Phase 1: Static Analysis                                    │
│  ├─ Examine error metadata and request structure             │
│  ├─ Check for known patterns (empty input, fragments, etc)   │
│  └─ Form hypothesis                                          │
│                                                              │
│  Phase 2: Hypothesis Verification (CRITICAL)                 │
│  ├─ Create modified request with fix applied                 │
│  ├─ Send to Kiro API                                         │
│  ├─ If PASS → hypothesis confirmed                           │
│  └─ If FAIL → iterate, form new hypothesis                   │
│                                                              │
│  Phase 3: Implement Fix                                      │
│  ├─ Write code fix                                           │
│  ├─ Add unit tests                                           │
│  └─ Commit changes                                           │
│                                                              │
│  Phase 4: Post-Deployment Verification                       │
│  ├─ Run integration test with original sample                │
│  ├─ Verify original sample now passes                        │
│  └─ Confirm no regression                                    │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## Known Problem Patterns

### Pattern 1: Empty Input for Required-Param Tools

**Symptom**: `input: {}` for tools like Read, Write, Bash that require parameters

**Detection**:
```python
for tu in toolUses:
    if tu.get('input') == {} and tu.get('name') in ['Read', 'Write', 'Bash', ...]:
        print(f"Empty input: {tu}")
```

**Fix**: Remove toolUse AND corresponding toolResult

### Pattern 2: Fragmented Streaming ToolUse

**Symptom**: Multiple entries with same `toolUseId`, each with `input.raw_arguments` fragment

**Detection**:
```python
from collections import defaultdict
by_id = defaultdict(list)
for tu in toolUses:
    by_id[tu['toolUseId']].append(tu)
for tid, tus in by_id.items():
    if len(tus) > 1:
        print(f"Fragmented: {tid}")
```

**Fix**: Aggregate fragments, merge raw_arguments, parse as JSON

### Pattern 3: Orphan ToolResult

**Symptom**: toolResult references toolUseId that doesn't exist in history

**Detection**:
```python
all_ids = {tu['toolUseId'] for msg in history for tu in msg.get('toolUses', [])}
for tr in toolResults:
    if tr['toolUseId'] not in all_ids:
        print(f"Orphan: {tr}")
```

**Fix**: Remove orphan toolResult

## Test File Locations

- **Integration test**: `tests/integration/kiro_request_diagnose_test.go`
- **Unit tests**: `tests/unit/kiro_request_merge_test.go`
- **Request builder**: `internal/kiro/client.go` (`BuildRequestBody`, `parseAssistantContent`)

## Checklist

- [ ] Static analysis complete
- [ ] Hypothesis documented
- [ ] Modified request created
- [ ] Hypothesis verified via Kiro API
- [ ] Code fix implemented
- [ ] Unit test added
- [ ] Committed
- [ ] Post-deployment: original sample passes
- [ ] No regression
