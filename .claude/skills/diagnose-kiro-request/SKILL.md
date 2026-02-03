---
name: diagnose-kiro-request
description: Use when diagnosing "Improperly formed request" 400 errors from Kiro API. Invoke when user mentions "diagnose kiro", "kiro 400 error", "improperly formed request", or needs to find which part of a kiro_request.json causes API failures.
---

# Kiro Request Diagnostic Tool

Performs elimination-based diagnosis on failing Kiro API requests to pinpoint the exact component (tools, history messages, toolResults) causing "Improperly formed request" errors.

## Usage

```
/diagnose-kiro-request <path-to-kiro_request.json>
```

Examples:
```bash
/diagnose-kiro-request kiro-debug/errors/468649d5-d2e4-40cd-b3b6-d91da1db1cf5/kiro_request.json
/diagnose-kiro-request /tmp/failing-request.json
```

## Prerequisites

1. **Healthy Kiro account in Redis** - The test needs a working account to send requests
2. **Go environment** - Tests run via `go test`
3. **Redis running** - Account selection requires Redis

## Instructions

When this skill is invoked:

### 1. Validate Input

Check that the provided path exists and contains a valid `kiro_request.json`:

```bash
ls -la <path>
head -c 500 <path>
```

Verify it has the expected structure:
- `conversationState.currentMessage`
- `conversationState.history` (optional)
- Tools in `userInputMessageContext.tools`

### 2. Run Diagnostic Test

Execute the Go integration test with the file path:

```bash
KIRO_REQUEST_FILE=<path> go test ./tests/integration/... -v -run TestKiroRequestDiagnose -timeout 300s 2>&1 | tee /tmp/diagnose-output.txt
```

### 3. Interpret Results

The test performs these elimination steps:

| Test | What it removes | If succeeds |
|------|-----------------|-------------|
| `original` | Nothing | Baseline should FAIL |
| `no-tools` | All tools | Problem in tools |
| `no-history` | All history | Problem in history |
| `no-history-no-tools` | Both | Multiple issues |
| `tools-N` | Keep first N tools | Binary search |
| `no-toolResults` | toolResults | Problem in toolResults |
| `history-empty` | All history | Confirms history issue |
| `history-keep-N` | Keep first N messages | Binary search |

### 4. Key Patterns to Look For

**Boundary Lines** - These indicate the exact problem location:
```
=== Boundary: history[0:51] OK, history[0:52] FAIL ===
=== Problem message is history[51] ===
```

**Warning Markers** - Potential issues:
```
⚠️ EMPTY INPUT - potential problem!
⚠️ LARGE INPUT (N bytes) - potential size issue
⚠️ toolResult[0] has NO matching toolUse in history!
```

**Success Confirmations**:
```
✓ Confirmed: removing only history[51] fixes the issue
```

### 5. Generate Report

After test completion, summarize findings:

```markdown
## Kiro Request Diagnosis Report

**File**: <path>
**Status**: Problem identified / Multiple issues / Unknown

### Summary
- Original request: FAIL
- Without history: PASS
- Problem location: history[51]

### Problem Details
- Message type: assistantResponseMessage
- Contains: 3 toolUses
- Specific issue: toolUse[1] (Write) has empty input

### Recommendation
Remove or fix history[51] before retrying the request.
```

### 6. Create Minimal Reproduction (Optional)

If user wants to create a minimal test case:

1. Extract the problem element (specific history message, tool, or toolResult)
2. Create a minimal request with just that element
3. Verify it fails
4. Save as `minimal-reproduction.json`

## Diagnostic Flow Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    Elimination Flow                          │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  Step 1: Baseline                                            │
│  └─ original → FAIL (confirm problem exists)                 │
│                                                              │
│  Step 2: Coarse Elimination                                  │
│  ├─ no-tools → PASS? → Binary search tools                   │
│  ├─ no-history → PASS? → Binary search history               │
│  └─ no-toolResults → PASS? → Check orphan references         │
│                                                              │
│  Step 3: Binary Search (if applicable)                       │
│  ├─ Tools: tools-1, tools-5, tools-10... → boundary          │
│  └─ History: history-keep-N → boundary (newest first)        │
│                                                              │
│  Step 4: Deep Analysis                                       │
│  ├─ Analyze problem message fields                           │
│  ├─ Check for empty inputs, large content                    │
│  └─ Test individual toolUses in problem message              │
│                                                              │
│  Step 5: Confirm                                             │
│  └─ Remove only problem element → PASS? → Confirmed          │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## Why Reverse History Elimination?

The test searches history from **newest to oldest** because:

1. **Old messages were successful** - They became history because previous requests worked
2. **New messages are most suspicious** - They were just added and could introduce problems
3. **Faster convergence** - Binary search from the newest end finds issues quicker

## Common Issues Found

| Issue | Symptom | Fix |
|-------|---------|-----|
| Empty toolUse input | `⚠️ EMPTY INPUT` | Remove the toolUse or add valid input |
| Orphan toolResult | `NO matching toolUse in history` | Remove the toolResult |
| Large content | `⚠️ LARGE INPUT (N bytes)` | Truncate or summarize content |
| Cumulative size | All messages fail individually | Reduce overall history length |

## Test File Location

The diagnostic test is implemented in:
```
tests/integration/kiro_request_diagnose_test.go
```

Key functions:
- `TestKiroRequestDiagnose` - Main test entry point
- `reverseEliminateHistory` - Binary search on history
- `testToolResults` - ToolResults elimination
- `analyzeMessage` - Detailed message analysis
- `deepDiveToolUses` - Individual toolUse testing
