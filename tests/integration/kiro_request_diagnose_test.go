package integration

import (
	"bytes"
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
		body, err := json.Marshal(reqObj)
		require.NoError(t, err)

		// Compact
		var compacted bytes.Buffer
		json.Compact(&compacted, body)
		body = compacted.Bytes()

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

	// --- Test 6: Check specific history messages ---
	if len(originalHistory) > 0 {
		t.Log("=== Test 6: History inspection ===")
		for i, msg := range originalHistory {
			msgMap, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}

			// Log message summary
			msgType := "unknown"
			if uim, ok := msgMap["userInputMessage"]; ok && uim != nil {
				msgType = "userInput"
			}
			if ar, ok := msgMap["assistantResponseMessage"]; ok && ar != nil {
				msgType = "assistantResponse"
			}

			msgBytes, _ := json.Marshal(msgMap)
			t.Logf("History[%d]: type=%s size=%d bytes", i, msgType, len(msgBytes))
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
