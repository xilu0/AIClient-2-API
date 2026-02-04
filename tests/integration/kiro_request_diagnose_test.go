package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/require"
)

// TestKiroRequestDiagnose progressively strips parts of a kiro_request.json to find
// which component causes the 400 "Improperly formed request" error.
//
// Usage:
//
//	KIRO_REQUEST_FILE=/path/to/kiro_request.json go test ./tests/integration/... -v -run TestKiroRequestDiagnose -timeout 300s
//
// This test sends multiple variants of the same request:
//  1. Original (compacted) — baseline
//  2. No tools — removes all tool definitions
//  3. No history — removes conversation history
//  4. No history + no tools — minimal request
//  5. Minimal with original model — just the user message with modelId
//  6. Incremental tool count — adds tools back progressively
func TestKiroRequestDiagnose(t *testing.T) {
	requestFilePath := os.Getenv("KIRO_REQUEST_FILE")
	if requestFilePath == "" {
		t.Skip("KIRO_REQUEST_FILE not set, skipping test")
	}

	// Setup
	redisClient := getRedisClient(t)
	defer redisClient.Close()
	acc, token := getHealthyAccount(t, redisClient)
	t.Logf("Using account: %s (region: %s)", acc.UUID, token.IDCRegion)

	client := kiro.NewClient(kiro.ClientOptions{
		MaxConns: 10, MaxIdleConnsPerHost: 5,
		IdleConnTimeout: 90 * time.Second, Timeout: 300 * time.Second,
		Logger: slog.Default(),
	})
	defer client.Close()

	// Read and parse original request
	rawData, err := os.ReadFile(requestFilePath)
	require.NoError(t, err, "Failed to read request file")

	var original map[string]interface{}
	require.NoError(t, json.Unmarshal(rawData, &original), "Failed to parse request JSON")

	// Helper to send and report result
	sendAndReport := func(name string, reqObj map[string]interface{}) bool {
		// Use MarshalWithoutHTMLEscape to avoid Go's default HTML escaping of <, >, &
		// which causes "Improperly formed request" errors from Kiro API
		body, err := kiro.MarshalWithoutHTMLEscape(reqObj)
		require.NoError(t, err)

		t.Logf("[%s] Sending %d bytes...", name, len(body))

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		kiroReq := &kiro.Request{
			Region:     token.IDCRegion,
			ProfileARN: acc.ProfileARN,
			Token:      token.AccessToken,
			Body:       body,
		}

		respBody, err := client.SendStreamingRequest(ctx, kiroReq)
		if err != nil {
			t.Logf("[%s] ✗ FAILED: %v", name, err)
			return false
		}
		defer respBody.Close()

		// Read a bit to confirm success
		buf := make([]byte, 4096)
		n, readErr := respBody.Read(buf)
		if readErr != nil && readErr != io.EOF {
			t.Logf("[%s] ✗ Read error: %v", name, readErr)
			return false
		}
		response := string(buf[:n])
		if len(response) > 150 {
			response = response[:150] + "..."
		}
		t.Logf("[%s] ✓ SUCCESS (first %d bytes): %s", name, n, response)
		return true
	}

	// Deep clone helper
	deepClone := func(src map[string]interface{}) map[string]interface{} {
		b, _ := json.Marshal(src)
		var dst map[string]interface{}
		json.Unmarshal(b, &dst)
		return dst
	}

	// Extract tools from the original request
	var originalTools []interface{}
	if cs, ok := original["conversationState"].(map[string]interface{}); ok {
		if cm, ok := cs["currentMessage"].(map[string]interface{}); ok {
			if uim, ok := cm["userInputMessage"].(map[string]interface{}); ok {
				if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
					if tools, ok := ctx["tools"].([]interface{}); ok {
						originalTools = tools
						t.Logf("Original has %d tools", len(tools))
					}
				}
			}
		}
	}

	// Extract history
	var originalHistory []interface{}
	if cs, ok := original["conversationState"].(map[string]interface{}); ok {
		if history, ok := cs["history"].([]interface{}); ok {
			originalHistory = history
			t.Logf("Original has %d history messages", len(history))
		}
	}

	// --- Test 1: Original (compacted) ---
	t.Log("=== Test 1: Original request ===")
	sendAndReport("original", original)

	// --- Test 2: No tools ---
	t.Log("=== Test 2: No tools ===")
	noTools := deepClone(original)
	if cs, ok := noTools["conversationState"].(map[string]interface{}); ok {
		if cm, ok := cs["currentMessage"].(map[string]interface{}); ok {
			if uim, ok := cm["userInputMessage"].(map[string]interface{}); ok {
				if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
					delete(ctx, "tools")
				}
			}
		}
	}
	sendAndReport("no-tools", noTools)

	// --- Test 3: No history ---
	t.Log("=== Test 3: No history ===")
	noHistory := deepClone(original)
	if cs, ok := noHistory["conversationState"].(map[string]interface{}); ok {
		delete(cs, "history")
	}
	sendAndReport("no-history", noHistory)

	// --- Test 4: No history + no tools ---
	t.Log("=== Test 4: No history + no tools ===")
	minimal := deepClone(original)
	if cs, ok := minimal["conversationState"].(map[string]interface{}); ok {
		delete(cs, "history")
		if cm, ok := cs["currentMessage"].(map[string]interface{}); ok {
			if uim, ok := cm["userInputMessage"].(map[string]interface{}); ok {
				if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
					delete(ctx, "tools")
				}
			}
		}
	}
	sendAndReport("no-history-no-tools", minimal)

	// --- Test 5: Binary search on tool count ---
	if len(originalTools) > 0 {
		t.Log("=== Test 5: Incremental tool count ===")

		// Phase 1: coarse search
		toolCounts := []int{1, 5, 10, 20, 40, 60, 80}
		lastSuccess := 0
		firstFailure := len(originalTools)
		for _, count := range toolCounts {
			if count > len(originalTools) {
				count = len(originalTools)
			}

			variant := deepClone(original)
			if cs, ok := variant["conversationState"].(map[string]interface{}); ok {
				if cm, ok := cs["currentMessage"].(map[string]interface{}); ok {
					if uim, ok := cm["userInputMessage"].(map[string]interface{}); ok {
						if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
							ctx["tools"] = originalTools[:count]
						}
					}
				}
			}

			name := "tools-" + itoa(count)
			if sendAndReport(name, variant) {
				lastSuccess = count
			} else {
				firstFailure = count
				break
			}

			if count >= len(originalTools) {
				break
			}
		}

		// Phase 2: binary search between lastSuccess and firstFailure
		if firstFailure > lastSuccess+1 {
			t.Logf("=== Binary search: success at %d, failure at %d ===", lastSuccess, firstFailure)
			lo, hi := lastSuccess, firstFailure
			for lo+1 < hi {
				mid := (lo + hi) / 2
				variant := deepClone(original)
				if cs, ok := variant["conversationState"].(map[string]interface{}); ok {
					if cm, ok := cs["currentMessage"].(map[string]interface{}); ok {
						if uim, ok := cm["userInputMessage"].(map[string]interface{}); ok {
							if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
								ctx["tools"] = originalTools[:mid]
							}
						}
					}
				}
				name := "tools-" + itoa(mid)
				if sendAndReport(name, variant) {
					lo = mid
				} else {
					hi = mid
				}
			}
			t.Logf("=== Boundary: %d tools OK, %d tools FAIL ===", lo, hi)

			// The problematic tool is at index lo (0-based)
			if lo < len(originalTools) {
				problemTool := originalTools[lo]
				if toolMap, ok := problemTool.(map[string]interface{}); ok {
					if spec, ok := toolMap["toolSpecification"].(map[string]interface{}); ok {
						toolName, _ := spec["name"].(string)
						desc, _ := spec["description"].(string)
						descLen := len(desc)
						if descLen > 200 {
							desc = desc[:200] + "..."
						}
						t.Logf("Problem tool [%d]: name=%s desc_len=%d", lo, toolName, descLen)
						t.Logf("  Description preview: %s", desc)

						// Check inputSchema
						if schema, ok := spec["inputSchema"].(map[string]interface{}); ok {
							schemaBytes, _ := json.Marshal(schema)
							t.Logf("  Schema size: %d bytes", len(schemaBytes))
						}
					}
				}

				// Confirm: send all tools EXCEPT the problematic one
				t.Log("=== Confirm: all tools except problematic one ===")
				confirmTools := make([]interface{}, 0, len(originalTools)-1)
				confirmTools = append(confirmTools, originalTools[:lo]...)
				confirmTools = append(confirmTools, originalTools[lo+1:]...)
				variant := deepClone(original)
				if cs, ok := variant["conversationState"].(map[string]interface{}); ok {
					if cm, ok := cs["currentMessage"].(map[string]interface{}); ok {
						if uim, ok := cm["userInputMessage"].(map[string]interface{}); ok {
							if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
								ctx["tools"] = confirmTools
							}
						}
					}
				}
				sendAndReport("all-except-tool-"+itoa(lo), variant)
			}
		}
	}

	// --- Test 6: ToolResults elimination ---
	t.Log("=== Test 6: ToolResults elimination ===")
	testToolResults(t, original, originalHistory, deepClone, sendAndReport)

	// --- Test 7: Reverse elimination on history (newest first) ---
	// Rationale: Old messages became history because previous requests succeeded.
	// The newest messages are most likely to have introduced the problem.
	if len(originalHistory) > 0 {
		t.Log("=== Test 7: History reverse elimination (newest first) ===")
		reverseEliminateHistory(t, original, originalHistory, deepClone, sendAndReport)
	}
}

// reverseEliminateHistory tests history from newest to oldest.
// Rationale: Old messages became history because previous requests succeeded.
// Newest messages are most suspicious.
func reverseEliminateHistory(
	t *testing.T,
	original map[string]interface{},
	originalHistory []interface{},
	deepClone func(map[string]interface{}) map[string]interface{},
	sendAndReport func(string, map[string]interface{}) bool,
) {
	n := len(originalHistory)
	t.Logf("History has %d messages, testing from newest (index %d) to oldest", n, n-1)

	// Step 1: Test empty history first
	variant := deepClone(original)
	cs := variant["conversationState"].(map[string]interface{})
	cs["history"] = []interface{}{}
	if !sendAndReport("history-empty", variant) {
		t.Log("Even empty history fails - problem not in history content")
		return
	}
	t.Log("Empty history succeeds - problem is in history content")

	// Step 2: Binary search to find the first problematic message
	// We look for the boundary where history[0:i] succeeds but history[0:i+1] fails
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi + 1) / 2 // bias toward higher to find first failure
		variant := deepClone(original)
		cs := variant["conversationState"].(map[string]interface{})
		cs["history"] = originalHistory[:mid]

		name := "history-keep-" + itoa(mid)
		if sendAndReport(name, variant) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	// lo is now the max number of messages that work
	boundary := lo
	if boundary >= n {
		t.Log("All history messages work - problem might be cumulative (size or elsewhere)")
		return
	}

	t.Logf("=== Boundary: history[0:%d] OK, history[0:%d] FAIL ===", boundary, boundary+1)
	t.Logf("=== Problem message is history[%d] ===", boundary)

	// Analyze the problematic message
	analyzeMessage(t, boundary, originalHistory[boundary])

	// Confirm: send all history EXCEPT the problematic message
	if boundary < n {
		t.Log("=== Confirm: all history except problematic message ===")
		confirmHistory := make([]interface{}, 0, n-1)
		confirmHistory = append(confirmHistory, originalHistory[:boundary]...)
		if boundary+1 < n {
			confirmHistory = append(confirmHistory, originalHistory[boundary+1:]...)
		}
		confirmVariant := deepClone(original)
		confirmCs := confirmVariant["conversationState"].(map[string]interface{})
		confirmCs["history"] = confirmHistory
		if sendAndReport("without-history-"+itoa(boundary), confirmVariant) {
			t.Logf("✓ Confirmed: removing only history[%d] fixes the issue", boundary)
		} else {
			t.Logf("✗ Removing history[%d] alone doesn't fix it - might be multiple issues", boundary)
		}
	}

	// Deep dive: if problem message is assistantResponseMessage with toolUses,
	// test each toolUse individually
	if boundary < n {
		deepDiveToolUses(t, original, originalHistory, boundary, deepClone, sendAndReport)
	}
}

// analyzeMessage logs details about a problematic message
func analyzeMessage(t *testing.T, index int, msg interface{}) {
	msgMap, ok := msg.(map[string]interface{})
	if !ok {
		t.Logf("Message[%d] is not a map", index)
		return
	}

	msgBytes, _ := json.Marshal(msgMap)
	t.Logf("Problem message[%d] size: %d bytes", index, len(msgBytes))

	// Check assistantResponseMessage
	if arm, ok := msgMap["assistantResponseMessage"].(map[string]interface{}); ok {
		t.Logf("Problem message[%d] is assistantResponseMessage", index)

		// Check content
		if content, ok := arm["content"].(string); ok {
			t.Logf("  content_len=%d", len(content))
			if len(content) > 200 {
				t.Logf("  content_preview: %s...", content[:200])
			} else if len(content) > 0 {
				t.Logf("  content_preview: %s", content)
			}
		}

		// Check toolUses
		if toolUses, ok := arm["toolUses"].([]interface{}); ok {
			t.Logf("  Has %d toolUses", len(toolUses))
			for i, tu := range toolUses {
				analyzeToolUse(t, i, tu)
			}
		}
	}

	// Check userInputMessage
	if uim, ok := msgMap["userInputMessage"].(map[string]interface{}); ok {
		t.Logf("Problem message[%d] is userInputMessage", index)

		// Check content
		if content, ok := uim["content"].(string); ok {
			t.Logf("  content_len=%d", len(content))
		}

		// Check userInputMessageContext
		if ctx, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
			// Check toolResults
			if trs, ok := ctx["toolResults"].([]interface{}); ok {
				t.Logf("  Has %d toolResults in context", len(trs))
				for i, tr := range trs {
					analyzeToolResult(t, i, tr)
				}
			}
		}
	}
}

// analyzeToolUse logs details about a toolUse entry
func analyzeToolUse(t *testing.T, index int, tu interface{}) {
	tuMap, ok := tu.(map[string]interface{})
	if !ok {
		return
	}

	toolUseId, _ := tuMap["toolUseId"].(string)
	name, _ := tuMap["name"].(string)
	input := tuMap["input"]
	inputBytes, _ := json.Marshal(input)

	t.Logf("  toolUse[%d]: name=%s toolUseId=%s input_size=%d", index, name, toolUseId, len(inputBytes))

	// Check for empty input (potential problem!)
	if inputMap, ok := input.(map[string]interface{}); ok {
		if len(inputMap) == 0 {
			t.Logf("    ⚠️ EMPTY INPUT - potential problem!")
		}
		// Log input keys
		keys := make([]string, 0, len(inputMap))
		for k := range inputMap {
			keys = append(keys, k)
		}
		if len(keys) > 0 && len(keys) <= 10 {
			t.Logf("    input keys: %v", keys)
		}
	}

	// Check for very large input
	if len(inputBytes) > 10000 {
		t.Logf("    ⚠️ LARGE INPUT (%d bytes) - potential size issue", len(inputBytes))
	}
}

// analyzeToolResult logs details about a toolResult entry
func analyzeToolResult(t *testing.T, index int, tr interface{}) {
	trMap, ok := tr.(map[string]interface{})
	if !ok {
		return
	}

	toolUseId, _ := trMap["toolUseId"].(string)
	status, _ := trMap["status"].(string)
	content := trMap["content"]
	contentBytes, _ := json.Marshal(content)

	t.Logf("  toolResult[%d]: toolUseId=%s status=%s content_size=%d", index, toolUseId, status, len(contentBytes))

	// Check for very large content
	if len(contentBytes) > 50000 {
		t.Logf("    ⚠️ LARGE CONTENT (%d bytes) - potential size issue", len(contentBytes))
	}
}

// testToolResults checks if toolResults in currentMessage cause the problem
func testToolResults(
	t *testing.T,
	original map[string]interface{},
	originalHistory []interface{},
	deepClone func(map[string]interface{}) map[string]interface{},
	sendAndReport func(string, map[string]interface{}) bool,
) {
	// Navigate to toolResults
	cs, ok := original["conversationState"].(map[string]interface{})
	if !ok {
		t.Log("No conversationState")
		return
	}
	cm, ok := cs["currentMessage"].(map[string]interface{})
	if !ok {
		t.Log("No currentMessage")
		return
	}
	uim, ok := cm["userInputMessage"].(map[string]interface{})
	if !ok {
		t.Log("No userInputMessage")
		return
	}
	ctx, ok := uim["userInputMessageContext"].(map[string]interface{})
	if !ok {
		t.Log("No userInputMessageContext")
		return
	}

	trs, ok := ctx["toolResults"].([]interface{})
	if !ok || len(trs) == 0 {
		t.Log("No toolResults to test")
		return
	}

	t.Logf("Found %d toolResults in currentMessage", len(trs))

	// Test: remove toolResults entirely
	variant := deepClone(original)
	delete(variant["conversationState"].(map[string]interface{})["currentMessage"].(map[string]interface{})["userInputMessage"].(map[string]interface{})["userInputMessageContext"].(map[string]interface{}), "toolResults")

	if sendAndReport("no-toolResults", variant) {
		t.Log("⚠️ SUCCESS without toolResults - problem might be in toolResults")

		// Check each toolResult for orphan references
		for i, tr := range trs {
			trMap, ok := tr.(map[string]interface{})
			if !ok {
				continue
			}
			toolUseId, _ := trMap["toolUseId"].(string)

			// Check if there's a matching toolUse in history
			found := findToolUseInHistory(originalHistory, toolUseId)
			if !found {
				t.Logf("  ⚠️ toolResult[%d] toolUseId=%s has NO matching toolUse in history!", i, toolUseId)
			} else {
				t.Logf("  ✓ toolResult[%d] toolUseId=%s has matching toolUse", i, toolUseId)
			}

			// Analyze the toolResult
			analyzeToolResult(t, i, tr)
		}

		// If multiple toolResults, test each one individually
		if len(trs) > 1 {
			t.Log("=== Testing each toolResult individually ===")
			for i := range trs {
				// Create request with only this toolResult removed
				testVariant := deepClone(original)
				testCtx := testVariant["conversationState"].(map[string]interface{})["currentMessage"].(map[string]interface{})["userInputMessage"].(map[string]interface{})["userInputMessageContext"].(map[string]interface{})

				remainingTrs := make([]interface{}, 0, len(trs)-1)
				remainingTrs = append(remainingTrs, trs[:i]...)
				remainingTrs = append(remainingTrs, trs[i+1:]...)
				testCtx["toolResults"] = remainingTrs

				name := "without-toolResult-" + itoa(i)
				if sendAndReport(name, testVariant) {
					t.Logf("  ⚠️ toolResult[%d] might be problematic", i)
				}
			}
		}
	} else {
		t.Log("Request still fails without toolResults - problem is elsewhere")
	}
}

// findToolUseInHistory checks if a toolUseId exists in history
func findToolUseInHistory(history []interface{}, toolUseId string) bool {
	for _, msg := range history {
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}

		// Check assistantResponseMessage
		if arm, ok := msgMap["assistantResponseMessage"].(map[string]interface{}); ok {
			if toolUses, ok := arm["toolUses"].([]interface{}); ok {
				for _, tu := range toolUses {
					tuMap, ok := tu.(map[string]interface{})
					if !ok {
						continue
					}
					if id, ok := tuMap["toolUseId"].(string); ok && id == toolUseId {
						return true
					}
				}
			}
		}
	}
	return false
}

// deepDiveToolUses tests individual toolUses within a problematic message
func deepDiveToolUses(
	t *testing.T,
	original map[string]interface{},
	originalHistory []interface{},
	problemIndex int,
	deepClone func(map[string]interface{}) map[string]interface{},
	sendAndReport func(string, map[string]interface{}) bool,
) {
	msgMap, ok := originalHistory[problemIndex].(map[string]interface{})
	if !ok {
		return
	}

	arm, ok := msgMap["assistantResponseMessage"].(map[string]interface{})
	if !ok {
		return
	}

	toolUses, ok := arm["toolUses"].([]interface{})
	if !ok || len(toolUses) == 0 {
		return
	}

	if len(toolUses) == 1 {
		t.Log("Only one toolUse in problem message - no further subdivision possible")
		return
	}

	t.Logf("=== Deep dive: testing %d toolUses in history[%d] ===", len(toolUses), problemIndex)

	// Test removing each toolUse individually
	for i := range toolUses {
		// Clone original
		variant := deepClone(original)
		cs := variant["conversationState"].(map[string]interface{})
		historyClone := make([]interface{}, len(originalHistory))
		for j := range originalHistory {
			if j == problemIndex {
				// Deep clone the problem message
				msgClone := deepClone(originalHistory[j].(map[string]interface{}))
				armClone := msgClone["assistantResponseMessage"].(map[string]interface{})

				// Remove toolUse[i]
				remainingToolUses := make([]interface{}, 0, len(toolUses)-1)
				remainingToolUses = append(remainingToolUses, toolUses[:i]...)
				remainingToolUses = append(remainingToolUses, toolUses[i+1:]...)
				armClone["toolUses"] = remainingToolUses

				historyClone[j] = msgClone
			} else {
				historyClone[j] = originalHistory[j]
			}
		}
		cs["history"] = historyClone

		tuMap := toolUses[i].(map[string]interface{})
		name, _ := tuMap["name"].(string)
		testName := "without-toolUse-" + itoa(i) + "-" + name

		if sendAndReport(testName, variant) {
			t.Logf("  ⚠️ toolUse[%d] (%s) in history[%d] is problematic", i, name, problemIndex)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
